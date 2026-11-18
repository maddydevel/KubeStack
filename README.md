# KubeStack

A web-based control plane for RKE2 clusters. KubeStack owns the portal, API, domain model, workflows
and audit trail; community Rancher (v2.15.3, see `deploy/compat.yaml`) runs headless behind an engine
adapter that uses only native Kubernetes CRD APIs.

```
Portal (React/TS) → KubeStack API (Go) → Engine adapter → Rancher (community, headless)
                           │
                       PostgreSQL
```

## Quick start (no cluster needed)
```bash
make run-dev        # fake engine + in-memory store + dev auth → http://localhost:8080
```

## Verify
```bash
make ci             # lint, unit + portal tests, license check, build, Postgres integration, helm lint, image
make e2e            # engine e2e against an air-gapped community Rancher container
```

## Docs
- `docs/sdlc.md` — requirements, design, traceability, test plan, release, runbook, verification record
- `docs/adr/` — architecture decisions and verified Rancher field semantics
- `api/openapi.yaml` — HTTP API contract
- `CLAUDE.md` — engineering rules for contributors

## Install
See "Operate" in `docs/sdlc.md` (Helm chart in `deploy/helm/kubestack`).
