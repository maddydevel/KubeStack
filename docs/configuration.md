# KubeStack Configuration Reference

All KubeStack configuration is via Helm chart values or environment variables. The API binary and the controller read the same environment variables, so clusters get identical identity setup whether created through the API or a CRD.

## Helm values (`deploy/helm/kubestack/values.yaml`)

| Value | Environment | Required | Default | Description |
|-------|-------------|----------|---------|-------------|
| `image.registry` | `KUBESTACK_REGISTRY` | Yes | `ghcr.io/maddydevel` | Container image registry (mirror for air-gapped) |
| `image.repository` | — | No | `kubestack/kubestack-api` | Image repository |
| `image.tag` | — | No | `v0.2.1` | Image tag |
| `image.pullPolicy` | — | No | `IfNotPresent` | Image pull policy |
| `adminGroup` | `KUBESTACK_ADMIN_GROUP` | Yes | `kubestack-admins` | OIDC group bound to cluster-admin |
| `oidc.issuer` | `KUBESTACK_OIDC_ISSUER` | Yes | — | IdP issuer URL |
| `oidc.clientID` | `KUBESTACK_OIDC_CLIENT_ID` | Yes | — | OIDC client ID |
| `oidc.groupsClaim` | `KUBESTACK_OIDC_GROUPS_CLAIM` | No | `groups` | Groups claim name |
| `oidc.redirectURL` | `KUBESTACK_OIDC_REDIRECT_URL` | No | — | Portal login callback URL |
| `oidc.clientSecretSecret` | `KUBESTACK_OIDC_CLIENT_SECRET` | No | — | Secret with key `CLIENT_SECRET` |
| `oidc.cliClientID` | `KUBESTACK_OIDC_CLI_CLIENT_ID` | No | same as clientID | Public client for kubectl/kubelogin |
| `oidc.caConfigMap` | `KUBESTACK_OIDC_CA_FILE` | No | — | ConfigMap with `ca.crt` for private IdP CA |
| `databaseSecret` | `KUBESTACK_DATABASE_URL` | No | — | Secret with key `DATABASE_URL` |
| `rancherKubeconfigSecret` | `KUBESTACK_RANCHER_KUBECONFIG` | No | — | Secret with key `kubeconfig` |
| `controller.enabled` | — | No | `true` | Deploy kubestack-controller |
| `service.port` | — | No | `80` | Service port |

## Environment variables (API binary)

| Variable | Meaning | Notes |
|----------|---------|-------|
| `KUBESTACK_ENGINE` | `rancher` or `fake` | Default `rancher`; `fake` for dev |
| `KUBESTACK_ADMIN_GROUP` | Admin OIDC group | Default `kubestack-admins` |
| `KUBESTACK_OIDC_ISSUER` | IdP issuer | Required unless dev auth |
| `KUBESTACK_OIDC_CLIENT_ID` | OIDC client ID | Required unless dev auth |
| `KUBESTACK_OIDC_GROUPS_CLAIM` | Groups claim | Default `groups` |
| `KUBESTACK_OIDC_CLI_CLIENT_ID` | Public CLI client | Defaults to clientID |
| `KUBESTACK_OIDC_REDIRECT_URL` | Portal login redirect | Mounts `/auth/*` endpoints |
| `KUBESTACK_OIDC_CLIENT_SECRET` | Client secret | From secret ref |
| `KUBESTACK_OIDC_CA_FILE` | IdP CA file path | For private CAs |
| `KUBESTACK_DATABASE_URL` | PostgreSQL DSN | Memory store if unset |
| `KUBESTACK_RANCHER_KUBECONFIG` | Rancher kubeconfig path | Empty = in-cluster SA |
| `KUBESTACK_DEV_AUTH` | `true` = dev auth | Every request is admin; dev only |
| `KUBESTACK_LISTEN` | Listen address | Default `:8080` |
| `KUBESTACK_PORTAL_DIR` | Portal dist directory | Served by API |

## RBAC

The Helm chart creates:

- **ServiceAccount** `kubestack-api` in the install namespace
- **Role** `kubestack-engine` in `fleet-default` — least privilege for Rancher CRDs (`provisioning.cattle.io/clusters`, `rke.cattle.io/etcdsnapshots`, `cluster.x-k8s.io/machines`, core `configmaps` create only)
- **ClusterRole** `kubestack-engine-read` — read-only on management clusters (`management.cattle.io/clusters`, `clusterregistrationtokens`, `settings`)

KubeStack never reads join token Secrets (`status.token` is never accessed). Operators read tokens with their own `kubectl` credentials.

## IdP setup

The portal implements OIDC authorization-code flow with PKCE. The session cookie holds the ID token itself (re-verified on every request), so there is no server-side session store.

- Bearer tokens are supported for API clients and proxies.
- Cookie-authenticated non-GET requests require the `X-KubeStack-Request` CSRF header.
- Sign-out clears the KubeStack cookie only; the IdP session continues until token expiry.

## Upgrade

```bash
helm upgrade kubestack deploy/helm/kubestack -n kubestack \
  --set image.tag=v0.2.2 \
  --reuse-values
```

The controller uses leader election; only one replica processes reconciles at a time. The API is stateless and safe to scale.

## Air-gapped

All configuration must work without internet. Store container images, Helm charts, and backup artifacts on your mirror or local storage. Do not depend on external services at runtime.
