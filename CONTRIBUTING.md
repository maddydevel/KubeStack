# Contributing to KubeStack

Thank you for contributing. KubeStack is a self-hosted control plane for RKE2 clusters, with community Rancher running headless behind a CRD-only engine adapter.

## Ground rules

Read `CLAUDE.md` fully before making changes. Its rules are non-negotiable:

1. Rancher only via native CRD APIs (`internal/engine/rancher/` only) — no Steve/Norman calls.
2. Domain model is engine-neutral (`api/` first).
3. Air-gapped by default — no public-internet calls at runtime.
4. No secrets in logs, responses, or fixtures.
5. Every lifecycle operation writes an audit record.
6. Capability flags and `upgradeOwner` respected.
7. New dependencies need a license check (`compliance/APPROVALS.md`).

## How to work in this repo

1. **Plan before coding** for anything touching `api/`, `internal/engine/`, `internal/upgrade/`, or auth.
2. **API first:** change `api/openapi.yaml` or `api/v1alpha1/`, run `make generate`, then implement backend, then portal.
3. **New engine capability:** add it to the `Engine` interface, implement it in `rancher/` and `fake/`, add a capability flag, add an e2e test.
4. **Tests are required:** unit tests for domain/upgrade logic; adapter tests against the fake engine; e2e for anything that changes cluster state.
5. **Keep changes small and focused.** No drive-by refactors.
6. **Commits:** Conventional Commits (`feat:`, `fix:`, `chore:` …). Note any new dependency and its license in the commit body.

## Local development

```bash
# Clone
git clone https://github.com/maddydevel/KubeStack.git
cd KubeStack

# Quick start (no cluster needed)
make run-dev          # fake engine + memory store + dev auth → http://localhost:8080

# Verify everything
make ci               # lint, unit + portal tests, license check, build, Postgres integration, helm lint, image

# Disposable community Rancher for e2e/dev
make dev-up           # starts Rancher; kubeconfig in bin/rancher-dev.kubeconfig
make run-dev-rancher  # real engine + memory store + dev auth
make dev-down         # stop Rancher
```

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
  store/                  # PostgreSQL access (pgx)
portal/                   # React + TypeScript + Vite + Tailwind
deploy/
  helm/kubestack/         # Product Helm chart
  airgap/                 # Image lists, mirror scripts, bundle builder
compliance/
  APPROVALS.md  NOTICE  THIRD-PARTY.md
docs/
  adr/                    # Architecture decision records
hack/                     # Scripts, boilerplate, dev cluster setup
```

## Making a release

1. `make ci` green; `make e2e` green for every version in `deploy/compat.yaml`; `docs/acceptance.md` passed on real VMs.
2. Update `compliance/THIRD-PARTY.md` if dependencies changed; add an ADR if behavior changed.
3. Bump `deploy/helm/kubestack/Chart.yaml` `version`/`appVersion`; tag `vX.Y.Z`.
4. Push the tag. `.github/workflows/release.yml` builds the image, pushes to GHCR, signs it with cosign (keyless), attaches the SPDX SBOM attestation, and publishes the air-gap bundle.

## License

Apache-2.0. All shipped dependencies are permissively licensed; verified by `make license-check`.
