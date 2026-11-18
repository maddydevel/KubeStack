// Copyright 2026 The KubeStack Authors. All rights reserved.

// Package auth authenticates portal/API users via OIDC bearer tokens.
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

// OIDC verifies "Authorization: Bearer <id_token>". In production the portal sits behind an
// OIDC-aware proxy that injects this header. The issuer must be reachable on the private network.
type OIDC struct {
	v           *oidc.IDTokenVerifier
	groupsClaim string
}

func NewOIDC(ctx context.Context, issuer, clientID, groupsClaim string) (*OIDC, error) {
	p, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, err
	}
	return &OIDC{v: p.Verifier(&oidc.Config{ClientID: clientID}), groupsClaim: groupsClaim}, nil
}

func (o *OIDC) Authenticate(r *http.Request) (User, error) {
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return User{}, errors.New("missing bearer token")
	}
	idt, err := o.v.Verify(r.Context(), tok)
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
