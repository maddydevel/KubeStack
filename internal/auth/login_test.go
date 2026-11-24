// Copyright 2026 The KubeStack Authors. All rights reserved.

package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

// fakeIdP is a minimal OIDC provider: discovery, JWKS and a token endpoint that checks PKCE.
// mint signs arbitrary claims with the IdP's key.
func fakeIdP(t *testing.T) (srv *httptest.Server, mint func(map[string]any) string) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	sig, _ := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: jose.JSONWebKey{Key: key, KeyID: "k"}}, nil)
	mint = func(c map[string]any) string {
		raw, _ := jwt.Signed(sig).Claims(c).Serialize()
		return raw
	}
	codes := map[string]string{} // code -> nonce
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"issuer": srv.URL, "authorization_endpoint": srv.URL + "/authorize",
			"token_endpoint": srv.URL + "/token", "jwks_uri": srv.URL + "/jwks", "id_token_signing_alg_values_supported": []string{"RS256"}})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "k", Algorithm: "RS256", Use: "sig"}}})
	})
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" {
			http.Error(w, "pkce required", 400)
			return
		}
		codes["c1"] = q.Get("nonce")
		http.Redirect(w, r, q.Get("redirect_uri")+"?code=c1&state="+url.QueryEscape(q.Get("state")), http.StatusFound)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		nonce, ok := codes[r.Form.Get("code")]
		if !ok || r.Form.Get("code_verifier") == "" {
			http.Error(w, "bad code", 400)
			return
		}
		raw, _ := jwt.Signed(sig).Claims(map[string]any{"iss": srv.URL, "aud": "portal", "sub": "u1", "email": "alice@example.com",
			"groups": []string{"admins"}, "nonce": nonce, "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix()}).Serialize()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"access_token": "at", "token_type": "Bearer", "id_token": raw, "expires_in": 3600})
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, mint
}

func TestLoginFlow(t *testing.T) {
	idp, _ := fakeIdP(t)
	o, err := NewOIDC(context.Background(), idp.URL, "portal", "groups")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	o.NewLogin("", "http://kubestack.test/auth/callback").Routes(mux)

	// 1. /auth/login redirects to the IdP with PKCE and sets the flow cookie.
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/auth/login", nil))
	flow := w.Result().Cookies()[0]
	res, err := http.Get(w.Header().Get("Location")) // IdP "authenticates" and redirects back
	if err == nil {
		res.Body.Close()
	}
	// The redirect target host doesn't exist; recover the callback URL from the failed request.
	var cb *url.URL
	if uerr, ok := err.(*url.Error); ok {
		cb, _ = url.Parse(uerr.URL)
	}
	if cb == nil || cb.Query().Get("code") == "" {
		t.Fatalf("no callback redirect: %v", err)
	}

	// 2. A callback with a forged state is refused.
	bad := httptest.NewRequest("GET", "/auth/callback?code=c1&state=forged", nil)
	bad.AddCookie(flow)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, bad)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("forged state: %d", w.Code)
	}

	// 2b. Right state but a different nonce (an ID token minted for another login) is refused.
	p := strings.Split(flow.Value, ".")
	replay := httptest.NewRequest("GET", cb.RequestURI(), nil)
	replay.AddCookie(&http.Cookie{Name: flow.Name, Value: p[0] + ".other-nonce." + p[2]})
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, replay)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("nonce mismatch: %d", w.Code)
	}

	// 3. The real callback sets an HttpOnly session cookie.
	req := httptest.NewRequest("GET", cb.RequestURI(), nil)
	req.AddCookie(flow)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	var session *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == SessionCookie && c.Value != "" {
			session = c
		}
	}
	if w.Code != http.StatusFound || session == nil || !session.HttpOnly {
		t.Fatalf("callback: %d %v", w.Code, w.Result().Cookies())
	}

	// 4. The cookie authenticates; mutations additionally need the CSRF header.
	get := httptest.NewRequest("GET", "/api/v1/clusters", nil)
	get.AddCookie(session)
	if u, err := o.Authenticate(get); err != nil || u.Name != "alice@example.com" || !u.InGroup("admins") {
		t.Fatalf("cookie auth: %+v %v", u, err)
	}
	post := httptest.NewRequest("POST", "/api/v1/clusters", strings.NewReader("{}"))
	post.AddCookie(session)
	if _, err := o.Authenticate(post); err == nil {
		t.Fatal("cookie-authenticated POST without CSRF header was accepted")
	}
	post.Header.Set(CSRFHeader, "1")
	if _, err := o.Authenticate(post); err != nil {
		t.Fatalf("POST with CSRF header: %v", err)
	}
	tampered := httptest.NewRequest("GET", "/", nil)
	tampered.AddCookie(&http.Cookie{Name: SessionCookie, Value: session.Value + "x"})
	if _, err := o.Authenticate(tampered); err == nil {
		t.Fatal("tampered token accepted")
	}

	// 5. A valid token issued to another client (audience) is refused as a bearer token.
	other, err := NewOIDC(context.Background(), idp.URL, "other-app", "groups")
	if err != nil {
		t.Fatal(err)
	}
	bearer := httptest.NewRequest("GET", "/", nil)
	bearer.Header.Set("Authorization", "Bearer "+session.Value)
	if _, err := other.Authenticate(bearer); err == nil {
		t.Fatal("token for a different audience accepted")
	}
	if _, err := o.Authenticate(bearer); err != nil {
		t.Fatalf("bearer token: %v", err)
	}

	// 6. Logout expires the session cookie.
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/auth/logout", nil))
	if c := w.Result().Cookies(); w.Code != http.StatusNoContent || len(c) != 1 || c[0].Name != SessionCookie || c[0].MaxAge >= 0 {
		t.Fatalf("logout: %d %v", w.Code, c)
	}
}

func TestUnverifiedEmailIsNotIdentity(t *testing.T) {
	idp, mint := fakeIdP(t)
	o, err := NewOIDC(context.Background(), idp.URL, "portal", "groups")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		verified any
		want     string
	}{{false, "u1"}, {true, "boss@example.com"}, {nil, "boss@example.com"}} {
		claims := map[string]any{"iss": idp.URL, "aud": "portal", "sub": "u1", "email": "boss@example.com",
			"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix()}
		if c.verified != nil {
			claims["email_verified"] = c.verified
		}
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Authorization", "Bearer "+mint(claims))
		if u, err := o.Authenticate(r); err != nil || u.Name != c.want {
			t.Errorf("email_verified=%v: got %q %v, want %q", c.verified, u.Name, err, c.want)
		}
	}
}
