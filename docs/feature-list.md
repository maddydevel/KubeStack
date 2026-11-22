# KubeStack — Feature List

Comprehensive list of features implemented in KubeStack v0.2.1+, organized by capability area.

## 1. Cluster Lifecycle Management

| Feature | Description | Tiers |
|---------|-------------|-------|
| Create cluster | Provision a new RKE2 cluster through Rancher (Traefik ingress, optional apiserver OIDC) | provisioned |
| List clusters | View all clusters with tier, version, ready state, upgrade owner | all |
| Get cluster | Inspect a single cluster's full state | all |
| Import cluster | Attach an existing CNCF-conformant cluster (RKE2/K3s or other) | attached |
| Delete cluster | Remove a provisioned cluster or disconnect an attached cluster | all |
| Disconnect cluster | Detach an imported cluster without destroying workloads | attached |
| Upgrade cluster | Bump Kubernetes version one minor at a time with skew rules | provisioned (upgradeOwner: rancher) |
| Version skew validation | Forbids downgrades, no-ops, major changes, skipped minors | provisioned |
| Pre-flight checks | Requires recent etcd snapshot (< 24h), all nodes ready, target in catalog | provisioned |
| Idempotent upgrades | Same Idempotency-Key replays safely | provisioned |

## 2. Cluster Tiers & Upgrade Ownership

| Tier | Description | upgradeOwner values |
|------|-------------|---------------------|
| provisioned | KubeStack created the cluster via Rancher | rancher |
| hosted | EKS/AKS/GKE — recognized, not created or upgraded by KubeStack | cloud |
| attached | Imported existing cluster; KubeStack observes/disconnects only | rancher or none |

- `upgradeOwner: kubestack` (System Upgrade Controller) is **not implemented** — dropped from GA by product owner.
- Upgrading imported clusters is **refused** (no etcd snapshot visibility for pre-flight).
- Capability gate `Cluster.Allowed(op)` enforces tier + upgradeOwner rules for every operation.

## 3. etcd Snapshots & Restore

| Feature | Description |
|---------|-------------|
| List snapshots | View existing etcd snapshots per cluster |
| Take snapshot | Trigger an etcd snapshot via Rancher (ETCDSnapshot CRD) |
| Restore snapshot | Restore etcd from a snapshot; version and config kept |
| Snapshot freshness | Snapshots older than 24 hours block upgrades |
| Restore config | `restoreRKEConfig: none` — configuration preserved |

## 4. Node Management

| Feature | Description |
|---------|-------------|
| List nodes | View node names, readiness, roles (etcd/controlplane/worker) |
| Remove node | Delete a CAPI Machine with UID precondition |
| Last-node protection | Refuses removing the last etcd or control-plane node |
| Drain before removal | Nodes are drained before machine deletion |

## 5. Direct Cluster Access (kubectl with Own Identity)

| Feature | Description |
|---------|-------------|
| Access endpoint | `GET /api/v1/clusters/{id}/access` — addresses, CA, OIDC settings |
| No credentials | Returns no tokens/secrets; user authenticates via their own IdP |
| kubelogin kubeconfig | Portal builds an exec-based kubeconfig using `kubectl oidc-login` |
| OIDC on API server | kube-apiserver configured with IdP issuer, client ID, claims, `oidc:` prefixes |
| Cluster-admin binding | `oidc:<admin-group>` bound to cluster-admin via additional manifest |
| Private IdP CA | Per-cluster ConfigMap copied to control-plane nodes (`oidc-ca-file`) |
| Access refusal | Refused for non-provisioned tiers with a reason |

## 6. Node Registration (Join Tokens Never Touched by KubeStack)

| Feature | Description |
|---------|-------------|
| Registration command | Returns `{command, token: {namespace, name, key}}` with `{token}` placeholder |
| Token never logged | Placeholder never inlined; adapter fails closed if token leaks |
| No Secret access | KubeStack service account has no permission to read token Secrets |
| Operator reads token | Portal shows a script whose first line reads the token with the operator's kubectl |
| Import without rkeConfig | Imported clusters start as `attached`, upgradeOwner `none` |

## 7. Authentication & Authorization

| Feature | Description |
|---------|-------------|
| OIDC login | Authorization-code flow with PKCE (S256) and nonce |
| Session cookie | `kubestack_session` holds the ID token; re-verified every request |
| Stateless sessions | No server-side session store; any replica can serve any request |
| CSRF protection | `X-KubeStack-Request` header on cookie-authenticated non-GET requests |
| Bearer tokens | Supported for API clients and OIDC-aware proxies |
| Dev auth mode | `KUBESTACK_DEV_AUTH=true` — every request is an admin (local dev only) |
| Admin group | OIDC group mapped to cluster-admin (`oidc:<group>`) |
| Login endpoints | `/auth/login`, `/auth/callback`, `/auth/logout` (mounted when redirect URL set) |

## 8. Audit Trail

| Feature | Description |
|---------|-------------|
| Attempt record | Written before every engine call; op refused if it can't be written |
| Success/failure | Result recorded after the engine call completes |
| Error capture | Failure reason stored (not echoed to user) |
| PostgreSQL store | Durable audit trail via `pgx` pool |
| Memory store | In-memory store for dev/tests (lost on restart) |
| Idempotency keys | Destructive calls require `Idempotency-Key`; replays return stored result |
| Claim/complete pattern | Atomic claim prevents duplicate execution |
| Audit list endpoint | `GET /api/v1/audit` (limit up to 500) |

## 9. Engine Architecture (Headless Rancher)

| Feature | Description |
|---------|-------------|
| CRD-only adapter | Uses native Kubernetes CRD APIs only — no Steve, no Norman |
| provisioning.cattle.io | Clusters in `fleet-default` namespace |
| rke.cattle.io | ETCD snapshots |
| cluster.x-k8s.io | CAPI Machines (node health, removal) |
| management.cattle.io | Cluster registration tokens, settings, hosted cluster detection |
| Rancher v2.15.3 | Pinned and verified via e2e |
| Air-gapped e2e | Images preloaded from `hack/e2e-images/<version>.txt` |
| Fake engine | Fully in-memory, mutex-guarded, seeds 3 demo clusters |
| Engine interface | 13 methods; both rancher and fake implementations complete |

## 10. Declarative Clusters (kubestack-controller + CRDs)

| Feature | Description |
|---------|-------------|
| KubeStackCluster CRD | `kubestack.dev` API, `v1alpha1`, `shortName: ksc` |
| Spec fields | `tier` (provisioned/attached), `kubernetesVersion`, `deletionPolicy` (Retain/Delete) |
| Status fields | `tier`, `upgradeOwner`, `kubernetesVersion`, `ready`, conditions (`Synced`) |
| Controller | Reconciles CRDs → engine with same capability gate, pre-flight, audit |
| Leader election | Controller uses leader election; only one replica processes |
| Health probes | `/healthz` and `/readyz` on :8081 |
| Metrics disabled | `metrics server bind address: 0` |
| CRD install | Helm chart installs CRDs + controller Deployment + RBAC |
| deepcopy | controller-gen generated `zz_generated.deepcopy.go` |

## 11. Release Catalog

| Feature | Description |
|---------|-------------|
| RKE2 catalog | Generated from pinned Rancher image's bundled release metadata |
| `make catalog` | Extracts `data.json` from Rancher image, generates `releases.json` |
| `catalog-check` | Fails if committed catalog is stale |
| Version skew | Validates target against catalog and skew rules |
| No runtime network | Catalog bundled at build time |

## 12. Portal UI (React/TS + Vite + Tailwind)

| Feature | Description |
|---------|-------------|
| Cluster dashboard | Stat cards (total, ready, not ready, snapshots) |
| Cluster cards | Tier badge, version, ready status, capability chips |
| Inline actions | Upgrade, snapshot, restore, nodes, access (connect), delete/disconnect |
| Confirmation dialogs | Type cluster name to confirm destructive operations |
| Registration command | Copyable shell script (token read by operator's kubectl) |
| Access command | Copyable kubelogin command |
| Audit table | Recent activity with time, actor, action, cluster, result |
| Responsive | Grid layout (1/2/3 columns by breakpoint) |
| Dark mode | Linear-inspired design system (indigo-violet accent) |
| Air-gapped | No CDN fonts; system fonts only |

## 13. Packaging & Distribution

| Feature | Description |
|---------|-------------|
| Helm chart | `deploy/helm/kubestack/` with values, templates, RBAC |
| Distroless image | `gcr.io/distroless/static-debian12:nonroot`, non-root user |
| Static binary | `CGO_ENABLED=0` Go build |
| Air-gapped build | No `npm ci`/`go mod download` inside Docker; host artifacts copied |
| SBOM | Syft SPDX for image + `npm ls --json --omit=dev` for portal packages |
| cosign signing | Keyless (GitHub OIDC) release signing |
| SBOM attestation | cosign attest with SPDX predicate |
| Air-gap bundle | `make airgap-bundle` — image tar, Helm chart, SBOM, mirror script, SHA256SUMS |
| Image registry | Configurable mirror (`image.registry`) |

## 14. CI/CD

| Pipeline | Stages |
|----------|--------|
| `.github/workflows/ci.yml` | lint, test, license-check, build, test-integration (PostgreSQL 17), helm-lint, image, e2e (community Rancher) |
| `.github/workflows/release.yml` | push tag → build, lint, test, airgap-bundle, cosign sign, SBOM attest, GitHub release |
| `.github/workflows/nodes.yml` | Weekly real-node acceptance (4 KVM VMs, Dex, Rancher, PostgreSQL) |

## 15. Compliance & Licensing

| Feature | Description |
|---------|-------------|
| License check | `hack/license-check.sh` — permissive licenses only |
| Approvals file | `compliance/APPROVALS.md` for LGPL/EPL/source-available deps |
| Third-party inventory | `compliance/THIRD-PARTY.md` — Go modules, portal npm packages, dev tools |
| NOTICE file | License notices for shipped dependencies |
| No GPL in binaries | golangci-lint (GPL-3.0) is a dev tool only, never linked |
| Air-gapped by default | No public internet calls at runtime (CLAUDE.md rules) |

## 16. Not Implemented (Out of Scope for GA)

| Feature | Status | Reason |
|---------|--------|--------|
| KubeStack-owned upgrades (SUC) | Dropped | Product owner decision; no cluster gets `upgradeOwner: kubestack` |
| Upgrading imported clusters | Refused | No etcd snapshot visibility; pre-flight unbypassable |
| `kubestack-controller` (pre-v0.2.1) | Now implemented | Added via CRDs + controller (issue #5) |
| `make generate` | Implemented | controller-gen deepcopy + CRD |
| TS client for portal | Open | `api.ts` remains hand-written |
| Real kubectl login e2e | Not exercised | Needs KVM nodes + Dex + real RKE2 nodes |
| Hosted cluster creation | Not supported | Recognized only; cloud provider manages |
| Rancher UI extensions | Out of scope | No Steve/Norman, no Rancher UI modifications |
| Rancher Prime features | Out of scope | Community Rancher only |
