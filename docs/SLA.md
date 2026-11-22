# KubeStack Service Level Agreement (SLA)

## Overview

KubeStack is self-hosted software you run on your own infrastructure. There is no hosted service, no vendor uptime guarantee, and no vendor support team. This SLA defines the operational targets you should aim for when self-hosting, and the support model.

## Support model

| Channel | Availability | Response |
|---------|--------------|----------|
| GitHub issues | Business hours | Best effort |
| Security advisories | Private draft advisory | Within 5 business days |
| Vendor SLA | None | Self-hosted |

## Operational targets (self-hosted)

| Metric | Target | How measured |
|--------|--------|--------------|
| API availability | ≥ 99.5% | `GET /healthz` HTTP 200 |
| Controller availability | ≥ 99.0% | `/readyz` HTTP 200 |
| Audit record durability | 100% of attempted ops | `GET /api/v1/audit` |
| Upgrade pre-flight accuracy | 100% | No bypassable checks (CLAUDE.md R4, R17) |

## Escalation

1. Check the runbook (`docs/operations/ADMIN.md`) for the symptom.
2. Check API logs (`slog`, no secrets) and Rancher logs.
3. Open a GitHub issue with: KubeStack version, Rancher version, steps to reproduce, relevant log lines (redact secrets).

## Exclusions

The following are outside the scope of any SLA:

- Downtime caused by your infrastructure (network, PostgreSQL, Rancher).
- Downtime caused by air-gapped mirror maintenance.
- Downtime caused by operator error (Helm misconfiguration, wrong `Idempotency-Key` reuse).
- Downstream cluster availability (managed by Rancher/RKE2, not KubeStack).

## Changes

This SLA is reviewed at each minor release. Changes are recorded in release notes.
