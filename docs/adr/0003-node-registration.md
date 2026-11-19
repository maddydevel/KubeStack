# ADR 0003: Node registration without handling join tokens

**Status:** accepted (v0.2)

## Context
New RKE2 clusters need nodes, and imported clusters need an agent manifest. Both commands embed a
join token, which rule 15 forbids KubeStack from logging, persisting in plaintext or returning.

## Finding (Rancher v2.15.3 source)
`pkg/controllers/dashboard/clusterregistrationtoken/status.go` builds `status.nodeCommand` (system-agent
install) and `status.command` (`kubectl apply -f <server-url>/v3/import/{token}_<cluster>.yaml`) with a
literal `{token}` placeholder. `token_secret.go` stores the token in the Secret named by
`status.tokenSecretName` (key `token`, prefix `crt-token-`) in the management cluster's namespace.
Commands are empty until the `server-url` setting is set.

## Decision
- `GET /api/v1/clusters/{id}/registration` returns `{command, token: {namespace, name, key}}`; the command
  keeps the placeholder. The adapter refuses (without echoing) any command that lacks the placeholder, so a
  future engine change that inlines tokens fails closed. `status.token` is never read.
- KubeStack's service account has **no Secret access**. The portal shows a script whose first line reads the
  token with the operator's own `kubectl` credentials, so Kubernetes RBAC and audit decide who may join nodes.
- Import creates a `provisioning.cattle.io` cluster without `rkeConfig` (tier `attached`, upgradeOwner `none`).
  Deleting an attached cluster disconnects it; the cluster itself is not destroyed.

## Consequences
- Operators need read access to Secrets in the management cluster's namespace on Rancher's local cluster.
- Not ready until Rancher has a server URL: the API answers 409 with the reason.
