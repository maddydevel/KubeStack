// Copyright 2026 The KubeStack Authors. All rights reserved.

package auth

import (
	"crypto/rand"
	"encoding/base64"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const (
	SessionCookie = "kubestack_session"
	flowCookie    = "kubestack_login"
)

// Login is the portal's OIDC authorization-code flow (with PKCE and nonce). The session cookie
// holds the ID token itself, re-verified on every request, so the server keeps no session state.
// shortcut: no refresh tokens; the user signs in again when the ID token expires.
type Login struct {
	o    *OIDC
	conf oauth2.Config
}

// NewLogin configures the flow. clientSecret may be empty for a public client.
func (o *OIDC) NewLogin(clientSecret, redirectURL string) *Login {
	return &Login{o: o, conf: oauth2.Config{ClientID: o.clientID, ClientSecret: clientSecret, RedirectURL: redirectURL,
		Endpoint: o.p.Endpoint(), Scopes: []string{oidc.ScopeOpenID, "email", "profile", "groups"}}}
}

func (l *Login) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/login", l.login)
	mux.HandleFunc("GET /auth/callback", l.callback)
	mux.HandleFunc("POST /auth/logout", l.logout)
}

func random() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func (l *Login) cookie(name, value, path string, maxAge time.Duration) *http.Cookie {
	return &http.Cookie{Name: name, Value: value, Path: path, MaxAge: int(maxAge.Seconds()), HttpOnly: true,
		Secure: strings.HasPrefix(l.conf.RedirectURL, "https://"), SameSite: http.SameSiteLaxMode}
}

func (l *Login) login(w http.ResponseWriter, r *http.Request) {
	state, nonce, verifier := random(), random(), oauth2.GenerateVerifier()
	http.SetCookie(w, l.cookie(flowCookie, state+"."+nonce+"."+verifier, "/auth/", 10*time.Minute))
	http.Redirect(w, r, l.conf.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), http.StatusFound)
}

func (l *Login) callback(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(flowCookie)
	parts := []string{}
	if err == nil {
		parts = strings.Split(c.Value, ".")
	}
	http.SetCookie(w, l.cookie(flowCookie, "", "/auth/", -1))
	if len(parts) != 3 || r.URL.Query().Get("state") != parts[0] {
		http.Error(w, "login expired or invalid; start again at /auth/login", http.StatusBadRequest)
		return
	}
	tok, err := l.conf.Exchange(r.Context(), r.URL.Query().Get("code"), oauth2.VerifierOption(parts[2]))
	if err != nil {
		slog.Warn("oidc code exchange failed") // the error may quote the IdP response; don't log it
		http.Error(w, "login failed", http.StatusUnauthorized)
		return
	}
	raw, _ := tok.Extra("id_token").(string)
	idt, err := l.o.v.Verify(r.Context(), raw)
	if err != nil || idt.Nonce != parts[1] {
		http.Error(w, "login failed", http.StatusUnauthorized)
		return
	}
	// SameSite=Lax so the IdP's top-level redirect back still carries it; CSRF is handled by CSRFHeader.
	http.SetCookie(w, l.cookie(SessionCookie, raw, "/", time.Until(idt.Expiry)))
	http.Redirect(w, r, "/", http.StatusFound)
}

// logout ends the KubeStack session only; the IdP session (if any) is left to the IdP.
func (l *Login) logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, l.cookie(SessionCookie, "", "/", -1))
	w.WriteHeader(http.StatusNoContent)
}
