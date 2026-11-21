# CLAUDE.md — KubeStack

Guidance for Claude Code when working in this repository. Read this fully before making changes.

## What KubeStack is

KubeStack is a web-based control plane for RKE2 clusters (and, with reduced capability, other CNCF-conformant clusters). It creates, upgrades, onboards and manages clusters through a branded portal and API.

**Architecture decision (fixed):** community (open-source) Rancher runs *headless* as the lifecycle and multi-cluster engine. KubeStack owns the product: portal, API, domain model, workflows and audit. Rancher is an implementation detail behind an adapter, never the product surface.

```
KubeStack Portal (React/TS)  →  KubeStack API (Go)  →  Engine adapter  →  Rancher (community, headless)
                                       │                                         │
                                  PostgreSQL                          Downstream clusters (RKE2, EKS/AKS/GKE, imported)
```

## Non-negotiable rules

Violating any of these is a blocking defect. If a task seems to require breaking one, stop and ask.

### Rancher integration boundary
1. **Use only native Kubernetes APIs against Rancher**: CRDs on the local cluster (`provisioning.cattle.io`, `management.cattle.io`, `fleet.cattle.io`, etc.) via client-go/controller-runtime, and the passthrough proxy `/k8s/clusters/<id>/api|apis/...` for downstream clusters.
2. **Never call Steve (`/v1`, `/k8s/clusters/<id>/v1`) or Norman (`/v3`, `/v3-public`)** from product code. They are not supported public APIs. Exception: a documented, isolated workaround inside `internal/engine/rancher/` with a `// RANCHER-UNSUPPORTED-API:` comment and a linked issue.
3. **All Rancher-specific code lives in `internal/engine/rancher/`.** Nothing outside the engine package imports Rancher types or knows Rancher resource names.
4. **The KubeStack domain model is engine-neutral.** Add features to the domain API first (`api/`), then implement them in the adapter. Never shape the domain API around a Rancher CRD field.
5. **Every cluster has an `upgradeOwner`** (`kubestack` | `rancher` | `cloud` | `none`). Never run a System Upgrade Controller plan on a cluster whose Kubernetes version Rancher manages. Check before acting.
6. **Capability flags, not assumptions.** Each cluster has a tier (`provisioned`, `hosted` = EKS/AKS/GKE, `attached`) and a capability set. The UI and API must refuse operations a tier doesn't support, with a clear reason.

### Licensing, trademark and supply chain
7. **Community artifacts only.** Never reference `registry.rancher.com` (Prime registry), Prime charts, or Prime-only images (including FIPS-hardened RKE2 images and post-March-2026 ingress-nginx images). Default ingress for new RKE2 clusters is **Traefik**.
8. **Never auto-accept any EULA or terms on the customer's behalf.** The installer must surface any third-party acceptance step to the human operator.
9. **No "Rancher" or "SUSE" branding** in product names, UI labels, logos, CRD group names, image names, Helm chart names or marketing strings. Factual references ("requires Rancher v2.x") are allowed in docs only.
10. **Do not modify or rebrand upstream images.** Mirror them unmodified for air-gapped installs and keep their license and NOTICE files.
11. **Every new dependency needs a license check.** Allowed without review: Apache-2.0, MIT, BSD-2/3, ISC, MPL-2.0 (unmodified). **Requires approval** (add to `compliance/APPROVALS.md`): LGPL, EPL, any "source-available" license. **Forbidden:** AGPL, GPL in shipped binaries or images, SSPL, BSL, Commons Clause, KubeSphere-style "open source license" variants.
12. New source files carry the KubeStack copyright header (see `hack/boilerplate.go.txt`). Never remove existing upstream copyright notices.

### Air-gapped by default
13. **No runtime calls to the public internet.** No telemetry, CDN fonts, remote schemas, update checks or public chart repos at runtime. All images, charts and frontend assets are bundled or mirrored. Configurable registry mirror for everything.
14. RKE2 channel/version metadata comes from a bundled or mirrored release catalog, not `update.rke2.io`, in production mode.

### Security
15. **Never log, persist in plaintext, or return in API responses**: kubeconfigs, Rancher tokens, cloud credentials, bootstrap tokens, etcd snapshot credentials. Store secrets as Kubernetes Secrets or encrypted columns; reference them by ID.
16. **Users act as themselves.** Downstream access uses per-user identity (OIDC → Rancher impersonation). The backend's service identity is used only for lifecycle operations, and every such operation writes an audit record (who, what, cluster, before/after, result).
17. Destructive operations (delete cluster, restore etcd, remove node, force upgrade) require explicit confirmation in the UI and an idempotency key in the API.

## Repository layout

```
cmd/
  kubestack-api/          # API server entrypoint
  kubestack-controller/   # Operator: reconciles KubeStack CRDs → engine
api/                      # KubeStack CRD types + OpenAPI spec (source of truth)
internal/
  domain/                 # Engine-neutral models, validation, capability logic
  engine/                 # Engine interface
    rancher/              # ONLY place that knows Rancher
    fake/                 # In-memory engine for tests
  upgrade/                # Pre-flight checks, version skew rules, plans
  audit/                  # Audit trail
  auth/                   # OIDC, session, RBAC mapping
  store/                  # PostgreSQL access (sqlc or pgx)
portal/                   # React + TypeScript + Vite + Tailwind
deploy/
  helm/kubestack/         # Product Helm chart
  airgap/                 # Image lists, mirror scripts, bundle builder
compliance/
  LICENSES/  NOTICE  SBOM/  THIRD-PARTY.md  APPROVALS.md
docs/
  adr/                    # Architecture decision records
hack/                     # Scripts, boilerplate, dev cluster setup
test/
  e2e/                    # Runs against real Rancher versions
```

## Tech stack

- **Backend:** Go (latest stable), controller-runtime, client-go, chi or net/http, pgx, PostgreSQL. Structured logging via `log/slog`.
- **Portal:** React, TypeScript (strict), Vite, Tailwind, TanStack Query. No direct calls from the browser to Rancher or downstream clusters; everything goes through the KubeStack API.
- **Packaging:** Helm, container images built reproducibly with SBOMs (Syft) and signatures (cosign).
- **Supported Rancher versions:** declared in `deploy/compat.yaml`. Keep it current; CI tests every listed version.

## Commands

```bash
make build             # api binary + portal bundle
make test              # Go unit tests (-race) + portal component tests
make lint              # gofmt, go vet, golangci-lint (Docker), tsc --noEmit, eslint
make license-check     # hack/license-check.sh; fails on forbidden licenses
make test-integration  # store contract against PostgreSQL in Docker
make e2e [RANCHER=v2.x.y]  # engine e2e in an air-gapped community Rancher container
make e2e-matrix        # e2e against every version in deploy/compat.yaml
make helm-lint         # chart lint (helm in Docker)
make image             # container image (REGISTRY=<mirror> TAG=<tag>)
make sbom              # SPDX SBOM of the image (syft in Docker) -> bin/sbom.spdx.json
make airgap-bundle     # image + chart + SBOM + compliance + mirror.sh -> bin/kubestack-airgap-<tag>.tar.gz
make run-dev           # fake engine + memory store + dev auth at http://localhost:8080
make dev-up / dev-down # disposable community Rancher; then make run-dev-rancher
make catalog           # RKE2 release catalog from the pinned Rancher image's bundled metadata (RANCHER_IMAGE=<mirror>)
make ci                # everything above except e2e, sbom, airgap-bundle and dev-up
```
Releases: pushing a `v*.*.*` tag runs `.github/workflows/release.yml` (push, cosign keyless sign, SBOM attestation, bundle).
Not yet implemented: `generate` (there are no CRD types yet).
New Rancher version: add it to `deploy/compat.yaml` and its preload list to `hack/e2e-images/`, then `make catalog`.

Run `make lint test license-check` before considering any task done.

## How to work in this repo

- **Plan before coding** for anything touching `api/`, `internal/engine/`, `internal/upgrade/` or auth. State the plan, the files affected and how it respects the rules above.
- **API first:** change the OpenAPI/CRD in `api/`, run `make generate`, then implement backend, then portal.
- **New engine capability:** add it to the `Engine` interface, implement it in `rancher/` and `fake/`, add a capability flag, and add an e2e test.
- **Tests are required:** unit tests for domain and upgrade logic; adapter tests against the fake engine; e2e for anything that changes cluster state. Never weaken or delete a failing test to make CI pass.
- **Upgrades:** enforce Kubernetes version-skew rules (no skipping minor versions, no downgrades), require a recent etcd snapshot, and check node health before starting. These checks live in `internal/upgrade/` and are never bypassable from the UI.
- **Rancher behavior questions:** check upstream docs and the Rancher source for the pinned version; don't guess field semantics. Record non-obvious findings in `docs/adr/` or a code comment with a link.
- **Keep changes small** and focused on the task. No drive-by refactors of unrelated code.
- **Commits:** Conventional Commits (`feat:`, `fix:`, `chore:` …). Note any new dependency and its license in the commit body.

## Definition of done

- [ ] Lint, unit tests and license check pass
- [ ] No Steve/Norman calls, no Rancher types outside `internal/engine/rancher/`
- [ ] No secrets in logs, responses or fixtures
- [ ] Works with no internet access (air-gapped mode)
- [ ] Capability flags and `upgradeOwner` respected
- [ ] Audit record written for any lifecycle operation
- [ ] Docs/ADR updated if behavior or architecture changed
- [ ] `compliance/THIRD-PARTY.md` updated if dependencies changed

## Out of scope unless explicitly requested

- Rancher UI extensions or modifying Rancher's own UI
- Forking or patching Rancher source
- Rancher Prime features, registries or support tooling
- Non-RKE2 cluster *provisioning* outside what the engine adapter already exposes
