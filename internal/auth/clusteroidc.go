// Copyright 2026 The KubeStack Authors. All rights reserved.

package auth

import (
	"os"

	"kubestack.dev/kubestack/internal/domain"
)

// ClusterOIDCFromEnv is the IdP trust applied to new clusters, read from the same environment by the API
// and the controller so clusters get identical identity setup either way. Nil when KUBESTACK_OIDC_ISSUER is unset.
func ClusterOIDCFromEnv(adminGroup string) (*domain.ClusterOIDC, error) {
	issuer, client := os.Getenv("KUBESTACK_OIDC_ISSUER"), os.Getenv("KUBESTACK_OIDC_CLIENT_ID")
	if issuer == "" {
		return nil, nil
	}
	c := &domain.ClusterOIDC{IssuerURL: issuer, ClientID: client, GroupsClaim: "groups", AdminGroup: adminGroup}
	if v := os.Getenv("KUBESTACK_OIDC_GROUPS_CLAIM"); v != "" {
		c.GroupsClaim = v
	}
	// A CLI needs a public client, so it may differ from the portal's.
	if v := os.Getenv("KUBESTACK_OIDC_CLI_CLIENT_ID"); v != "" {
		c.ClientID = v
	}
	if f := os.Getenv("KUBESTACK_OIDC_CA_FILE"); f != "" {
		ca, err := os.ReadFile(f) // #nosec G304 -- operator-supplied path
		if err != nil {
			return nil, err
		}
		c.CAPEM = ca
	}
	return c, nil
}
