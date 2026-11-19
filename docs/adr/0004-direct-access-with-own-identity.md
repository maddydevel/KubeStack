# ADR 0004: Downstream access with the user's own identity (OIDC on the cluster)

**Status:** accepted (v0.2). Chosen by the product owner over proxying through Rancher.

## Context
Rule 16: people act as themselves on downstream clusters. Rancher v2.15.3's proxy honours `Impersonate-User`
only for Rancher users (`pkg/auth/requests/impersonate.go`, SubjectAccessReview against Rancher RBAC). KubeStack
runs Rancher headless, so there is no OIDC → Rancher user mapping, and coupling to Rancher's auth model is unwanted.

## Decision
- Every cluster KubeStack provisions gets kube-apiserver OIDC flags for the organisation's IdP
  (`machineGlobalConfig.kube-apiserver-arg`: issuer, client ID, `email` username claim, groups claim, `oidc:` prefixes).
- `spec.rkeConfig.additionalManifest` binds `Group oidc:<admin group>` to `cluster-admin`; other access is granted
  downstream with ordinary RBAC on `oidc:`-prefixed users and groups.
- `GET /api/v1/clusters/{id}/access` (any signed-in user) returns control-plane addresses, the cluster CA and the IdP
  settings — **no credentials**. The portal builds a kubeconfig in the browser whose user is an `exec` of
  `kubectl oidc-login` (kubelogin), so the token comes from the IdP to the user's machine and never through KubeStack.
- Addresses: CAPI Machine `status.addresses` of control-plane machines (ExternalIP preferred), port 6443. CA: management
  cluster `status.caCert`, which Rancher's agent tunnel copies from the downstream in-cluster CA
  (`pkg/tunnelserver/mcmauthorizer/tunnel.go`). `status.apiEndpoint` is the in-cluster service address; not used.

## Consequences
- Users need network reach to control-plane nodes on 6443, the kubelogin plugin, and a **public** IdP client for the
  CLI (`oidc.cliClientID`, defaults to the portal's client ID).
- Clusters created before this change, imported and hosted clusters are not configured; `access` is refused for
  non-provisioned tiers with a reason.
- The IdP's CA must be trusted by the node OS (shortcut: no `oidc-ca-file` distribution yet).
- Verified: unit tests and the e2e (webhook accepts the args and manifest; access is not-ready without nodes).
  Not verified: an actual kubectl login against a running downstream cluster (the e2e has no nodes).
