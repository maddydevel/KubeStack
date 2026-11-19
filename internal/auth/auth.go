// Copyright 2026 The KubeStack Authors. All rights reserved.

// Package auth authenticates portal/API users via OIDC: a bearer token (API clients, proxies)
// or the session cookie set by the portal login flow (see login.go).
package auth

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
)

type User struct {
	Name   string
	Groups []string
}

func (u User) InGroup(g string) bool { return slices.Contains(u.Groups, g) }

type Authenticator interface {
	Authenticate(r *http.Request) (User, error)
}

// OIDC verifies an ID token from "Authorization: Bearer <id_token>" or the session cookie.
// The issuer must be reachable on the private network.
type OIDC struct {
	p           *oidc.Provider
	v           *oidc.IDTokenVerifier
	clientID    string
	groupsClaim string
}

func NewOIDC(ctx context.Context, issuer, clientID, groupsClaim string) (*OIDC, error) {
	p, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, err
	}
	return &OIDC{p: p, v: p.Verifier(&oidc.Config{ClientID: clientID}), clientID: clientID, groupsClaim: groupsClaim}, nil
}

// CSRFHeader must accompany every non-GET request authenticated by the session cookie. A custom
// header cannot be sent cross-site without a CORS preflight, which this API never grants.
const CSRFHeader = "X-KubeStack-Request"

func (o *OIDC) Authenticate(r *http.Request) (User, error) {
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		c, err := r.Cookie(SessionCookie)
		if err != nil {
			return User{}, errors.New("missing credentials")
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Header.Get(CSRFHeader) == "" {
			return User{}, errors.New("missing " + CSRFHeader + " header")
		}
		tok = c.Value
	}
	return o.verify(r.Context(), tok)
}

func (o *OIDC) verify(ctx context.Context, tok string) (User, error) {
	idt, err := o.v.Verify(ctx, tok)
	if err != nil {
		return User{}, errors.New("invalid token") // don't echo verifier detail or the token
	}
	var claims map[string]any
	if err := idt.Claims(&claims); err != nil {
		return User{}, err
	}
	u := User{Name: idt.Subject}
	if e, ok := claims["email"].(string); ok && e != "" {
		u.Name = e
	}
	if gs, ok := claims[o.groupsClaim].([]any); ok {
		for _, g := range gs {
			if s, ok := g.(string); ok {
				u.Groups = append(u.Groups, s)
			}
		}
	}
	return u, nil
}

// Dev authenticates every request as a fixed admin. Only for local development.
type Dev struct{ Group string }

func (d Dev) Authenticate(*http.Request) (User, error) {
	return User{Name: "dev@localhost", Groups: []string{d.Group}}, nil
}
