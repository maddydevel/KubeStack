# KubeStack System Administrator Guide

This guide covers operating a KubeStack installation: installation, configuration, day-to-day operations, and incident response.

## Installation

Install into Rancher's local cluster using the Helm chart:

```bash
kubectl create ns kubestack
kubectl -n kubestack create secret generic kubestack-db \
  --from-literal=DATABASE_URL='postgres://postgres:<password>@127.0.0.1:15432/postgres?sslmode=disable'

helm install kubestack deploy/helm/kubestack -n kubestack \
  --set image.registry=<mirror> \
  --set databaseSecret=kubestack-db \
  --set oidc.issuer=<issuer> \
  --set oidc.clientID=<client> \
  --set adminGroup=<group>
```

Portal sign-in: register `https://<host>/auth/callback` with your IdP and set `oidc.redirectURL` (plus `oidc.clientSecretSecret` for a confidential client). Leave `redirectURL` empty to front the portal with an OIDC-aware proxy that forwards `Authorization: Bearer ***` (see `docs/adr/0002-portal-login.md`).

Air-gapped install: copy `kubestack-airgap-<tag>.tar.gz` across, `tar xzf`, `./mirror.sh <mirror>` (verifies SHA256SUMS, loads and pushes the unmodified image), then `helm install` the bundled chart with `image.registry=<mirror>`.

## Configuration

All configuration is via Helm values or environment variables:

| Helm value / Env var | Meaning | Required |
|----------------------|---------|----------|
| `image.registry` | Container image registry (mirror for air-gapped) | Yes |
| `image.repository` | `kubestack/kubestack-api` | No |
| `image.tag` | Image tag | No (defaults to chart version) |
| `adminGroup` | OIDC group bound to cluster-admin | Yes |
| `oidc.issuer` | IdP issuer URL | Yes |
| `oidc.clientID` | OIDC client ID | Yes |
| `oidc.groupsClaim` | Groups claim name | No (default `groups`) |
| `oidc.redirectURL` | Portal login callback URL | No |
| `oidc.clientSecretSecret` | Secret with key `CLIENT_SECRET` | No |
| `oidc.cliClientID` | Public client for kubectl/kubelogin | No (defaults to clientID) |
| `oidc.caConfigMap` | ConfigMap with `ca.crt` for private IdP CA | No |
| `databaseSecret` | Secret with key `DATABASE_URL` | No (memory store without) |
| `rancherKubeconfigSecret` | Secret with key `kubeconfig` | No (in-cluster SA without) |
| `controller.enabled` | Deploy kubestack-controller | No (default true) |

Environment variables read by the API binary (`KUBESTACK_*`) mirror the Helm values.

## Runbook

| Symptom | Meaning / action |
|---------|------------------|
| 503 "audit trail unavailable; operation refused" | Postgres down — restore DB; no lifecycle op ran |
| 412 "pre-flight checks failed" | Expected guard: take an etcd snapshot / fix nodes / upgrade one minor at a time |
| 409 "upgrade not permitted" | Tier or `upgradeOwner` forbids it (e.g. hosted → upgrade in the cloud console) |
| 409 "…still in progress" | Same `Idempotency-Key` in flight; wait, don't retry with a new key |
| 502 "lifecycle engine error" | Rancher API unreachable/rejected; see API logs (`slog`, no secrets) |
| Cluster stuck deleting; finalizer `auth.cattle.io/capi-resources-cleanup` | Rancher never created the CAPI Cluster (CAPI webhook was down at create time). Check `cattle-capi-system/capi-controller-manager`; escalate to Rancher ops — KubeStack never strips finalizers |
| 409 "cluster changed concurrently, retry" on snapshot/restore | Someone else changed the cluster spec at the same moment; retry |
| Restore requested but nothing happens | Rancher acts only when etcd nodes are up; check cluster conditions in Rancher |
| Audit row with `attempt` but no `success`/`failure` | API crashed mid-op — check cluster state in Rancher, then record outcome |

## Logs

The API logs structured `log/slog` output. Secrets are never logged (CLAUDE.md R15). Audit records are stored in PostgreSQL (or memory for dev) and queryable via `GET /api/v1/audit`.

## Upgrades

Bump `image.tag` and Helm `appVersion`, then `helm upgrade`. The controller respects `upgradeOwner` and `domain.Allowed()` — clusters managed by Rancher (`upgradeOwner: rancher`) can be upgraded through KubeStack; clusters managed by cloud (`upgradeOwner: cloud`) or `kubestack` (not implemented) cannot.

## Uninstall

```bash
helm uninstall kubestack -n kubestack
kubectl delete ns kubestack
```

Deleting a `KubeStackCluster` resource with `deletionPolicy: Retain` stops KubeStack managing the cluster without destroying it. With `deletionPolicy: Delete`, the provisioned cluster is removed (or an attached cluster is disconnected).
