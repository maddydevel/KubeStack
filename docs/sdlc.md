# KubeStack — SDLC record (v0.1)

One document per release covering requirements → design → build → test → release → operate.
Rules referenced as **R#** are the numbered rules in `CLAUDE.md`.

## 1. Requirements

### Functional
| ID | Requirement |
|----|-------------|
| F1 | List all managed clusters with tier, upgradeOwner, Kubernetes version and readiness |
| F2 | Create an RKE2 cluster from a catalogued version; default ingress Traefik (R7) |
| F3 | Upgrade a cluster's Kubernetes version, only when tier/upgradeOwner allow it (R5, R6) |
| F4 | Refuse upgrades that skip a minor, downgrade, lack a <24h etcd snapshot or have unready nodes |
| F5 | Delete a provisioned cluster with explicit confirmation |
| F6 | Show an audit trail of every lifecycle operation (R16) |
| F7 | Show *why* an operation is unavailable, using the server's reason (R6) |

### Non-functional
| ID | Requirement |
|----|-------------|
| N1 | Rancher reached only via CRDs; no Steve/Norman; Rancher code only in `internal/engine/rancher/` (R1–R3) |
| N2 | No public-internet calls at runtime; versions from bundled catalog (R13, R14) |
| N3 | No secrets in logs/responses/fixtures (R15) |
| N4 | OIDC authentication; mutations require the admin group |
| N5 | Destructive API calls require an `Idempotency-Key`; replays return the stored result (R17) |
| N6 | Audit "attempt" written before any engine call; if it can't be written the op is refused |
| N7 | Only allowed licenses ship (R11) |
| N8 | Runs as non-root, read-only root FS, least-privilege RBAC on Rancher's local cluster |

## 2. Design
```
Portal (React/TS) ──/api/v1──▶ server ──▶ domain.Allowed / upgrade.Check
                                  │                 │
                                  ├──▶ store (Postgres | memory): audit, idempotency
                                  └──▶ engine.Engine ──▶ rancher (dynamic client, CRDs) | fake
```
- `domain.Cluster.Allowed(op)` is the single capability gate; the API returns its result per
  operation so the UI never re-derives rules.
- Upgrade pre-flight lives in `internal/upgrade` and runs server-side only.
- Rancher field semantics: see `docs/adr/0001-headless-engine-adapter.md`; verified by e2e.
- API contract: `api/openapi.yaml`.

## 3. Traceability (requirement → implementation → test)
| Req | Implementation | Test |
|-----|----------------|------|
| F1 | `server.listClusters`, `rancher.ListClusters` | `server_test: TestCreateAndRBAC`, `rancher_test`, e2e |
| F2 | `server.createCluster`, `domain.CreateRequest.Validate`, `rancher.CreateCluster` | `TestCreateAndRBAC`, e2e (webhook accepts traefik) |
| F3 | `server.upgradeCluster`, `domain.Allowed` | `TestUpgradeFlow`, `TestCapabilityRefusals`, `upgrade_test: TestCheck`, e2e |
| F4 | `upgrade.ValidateSkew`, `upgrade.Check` | `TestValidateSkew`, `TestCheck`, `TestPreflightBlocksStaleSnapshot` |
| F5 | `server.deleteCluster`, portal `DeleteDialog` | `TestCapabilityRefusals`, `App.test: delete requires typing…`, e2e |
| F6 | `server.lifecycle`, `store.*Audit` | `TestUpgradeFlow`, `store_test` (memory + Postgres) |
| F7 | `server.view`, portal `OpButton` | `App.test: operations … disabled with the server's reason` |
| N1 | `internal/engine/rancher` only | e2e against real Rancher; review checklist |
| N2 | `upgrade/releases.json` embedded; system fonts | review checklist |
| N3 | errors don't echo tokens/DSN | review checklist |
| N4 | `auth.OIDC`, `server.wrap` | `TestCreateAndRBAC` (403 for non-admin) |
| N5 | `server.wrap` + `store.ClaimIdempotency` | `TestUpgradeFlow` (replay), `store_test` (10 concurrent claims → 1 winner) |
| N6 | `server.lifecycle` | `TestUpgradeFlow` (attempt + success records) |
| N7 | `hack/license-check.sh` | `make license-check` |
| N8 | Helm `deployment.yaml`, `rbac.yaml`, distroless image | `make helm-lint`, `make image` |

## 4. Test plan
| Level | Command | Needs |
|-------|---------|-------|
| Unit (Go, race detector) | `make test` | — |
| Component (portal, jsdom) | `make test` | — |
| Integration (PostgreSQL 17) | `make test-integration` | Docker |
| E2E (real community Rancher) | `make e2e` / `make e2e-matrix` | Docker (privileged) |
| Static | `make lint license-check helm-lint` | Docker for helm-lint |
| Packaging | `make image` | Docker |

All of it: `make ci`. CI runs the same targets (`.github/workflows/ci.yml`).

## 5. Release
1. `make ci` green; e2e green for every version in `deploy/compat.yaml`.
2. Update `compliance/THIRD-PARTY.md` if dependencies changed; add ADR if behaviour changed.
3. Bump `deploy/helm/kubestack/Chart.yaml` `version`/`appVersion`; tag `vX.Y.Z` (Conventional Commits).
4. `make image TAG=vX.Y.Z REGISTRY=<registry>`; push; add the image to `deploy/airgap/images.txt`.

## 6. Operate
**Install** (into Rancher's local cluster, in-cluster identity):
```bash
kubectl create ns kubestack
kubectl -n kubestack create secret generic kubestack-db --from-literal=DATABASE_URL='postgres://…'
helm install kubestack deploy/helm/kubestack -n kubestack \
  --set image.registry=<mirror> --set databaseSecret=kubestack-db \
  --set oidc.issuer=<issuer> --set oidc.clientID=<client> --set adminGroup=<group>
```
Put an OIDC-aware proxy in front that forwards `Authorization: Bearer <id_token>`.

**Runbook**
| Symptom | Meaning / action |
|---------|------------------|
| 503 "audit trail unavailable; operation refused" | Postgres down — restore DB; no lifecycle op ran |
| 412 "pre-flight checks failed" | Expected guard: take an etcd snapshot / fix nodes / upgrade one minor at a time |
| 409 "upgrade not permitted" | Tier or upgradeOwner forbids it (e.g. hosted → upgrade in the cloud console) |
| 409 "…still in progress" | Same Idempotency-Key in flight; wait, don't retry with a new key |
| 502 "lifecycle engine error" | Rancher API unreachable/rejected; see API logs (`slog`, no secrets) |
| Cluster stuck deleting; finalizer `auth.cattle.io/capi-resources-cleanup`; Rancher logs `no matching controller owner ref` | Rancher never created the CAPI Cluster (CAPI webhook was down at create time, rancher/rancher#40535). Check `cattle-capi-system/capi-controller-manager`; escalate to Rancher ops — KubeStack never strips finalizers |
| Audit row with `attempt` but no `success`/`failure` | API crashed mid-op — check cluster state in Rancher, then record outcome |

## 7. Known gaps (backlog)
- Node registration for new clusters (registration command embeds a bootstrap token, R15 — needs design).
- Per-user downstream access via OIDC → impersonation (R16).
- KubeStack-owned upgrades via System Upgrade Controller; `kubestack-controller` + CRDs.
- Portal login flow (relies on fronting OIDC proxy), SBOM/cosign, air-gap bundle, golangci-lint/eslint.

## 8. Verification record — 2026-11-17
| Stage | Command | Result |
|-------|---------|--------|
| Static | `make lint` (gofmt, go vet incl. e2e tag, tsc strict) | pass |
| Unit (Go, `-race`) | `make test` | pass (domain/upgrade, server, store contract, adapter vs fake dynamic client) |
| Component (portal) | `make test` | 3/3 pass |
| Licenses | `make license-check` | pass — all shipped deps permissive |
| Build | `make build` | pass |
| Integration | `make test-integration` (PostgreSQL 17) | pass — incl. 10 concurrent idempotency claims → 1 winner |
| Packaging | `make helm-lint`, `helm template`, `make image` | pass — 14 MB distroless image, runs `--read-only` as `nonroot` |
| E2E | `make e2e` (community Rancher **v2.15.3**, air-gapped) | pass — create/link/list/snapshot/nodes/upgrade/delete |

### Findings fixed because of e2e against real Rancher
1. **Stale release catalog**: Rancher v2.15.3 supports RKE2 v1.34–v1.36 (default v1.36.5); the bundled
   catalog was v1.32–v1.34. Refreshed from Rancher's own release list; e2e now fails if the catalog lacks
   Rancher's default version.
2. **Deprecated API**: CAPI `Machine` v1beta1 is deprecated in v2.15; adapter moved to v1beta2 (storage version).
3. `rke2-default-version` setting omits the `v` prefix that the webhook requires (test-side normalisation).
4. Rancher's single-container install needs its extension API registered in time; e2e sets
   `AGGREGATION_REGISTRATION_TIMEOUT=10m` and runs with bundled charts + preloaded images
   (`hack/e2e-images/<version>.txt`), so it needs no network inside the container.
5. `compat.yaml` placeholder (v2.12) replaced with the current community stable, v2.15.3.

6. **Intermittent e2e hang (2 of 5 runs)**: deleting a cluster created while Rancher's CAPI webhook was
   still starting hung forever (see runbook). e2e now waits for `capi-controller-manager` and asserts the
   CAPI Cluster exists before deleting. Verified 3/3 consecutive passes on 2026-11-18 (89s, 46s, 45s).

### Environment notes (this workstation, not product issues)
- Kiri's bridge network has no outbound DNS/NAT, and its default BuildKit builder can't resolve `gcr.io`.
  This persisted after restarting Kiri (2026-11-18): its builder returns "not found" even for `alpine:3.20`.
  `make ci` passes with a temporary host-network builder:
  `docker buildx create --name ks-hostnet --driver docker-container --driver-opt network=host`
  then `BUILDX_BUILDER=ks-hostnet make ci`.
