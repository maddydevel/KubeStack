# Disaster Recovery & Business Continuity

## Scope

KubeStack is a control plane, not a workload platform. It does not run customer workloads. Disaster recovery therefore focuses on:

1. **KubeStack itself** — the API server, controller, and their state (PostgreSQL).
2. **Downstream clusters** — provisioned through Rancher; their etcd data is managed by Rancher, not KubeStack.
3. **Audit trail** — stored in PostgreSQL; required for compliance.

## Recovery Time Objective (RTO)

| Component | RTO | Basis |
|-----------|-----|-------|
| KubeStack API + controller | < 30 min | Helm chart redeploy; stateless API, controller re-reconciles on start |
| PostgreSQL (audit store) | < 60 min | Restore from backup; operator-managed DB |
| Downstream clusters | N/A | Managed by Rancher/RKE2; out of KubeStack scope |

## Recovery Point Objective (RPO)

| Component | RPO | Basis |
|-----------|-----|-------|
| Audit records | Last backup | Operator-managed DB backup schedule |
| Cluster state | Last etcd snapshot | Managed by Rancher; KubeStack triggers snapshots via API |
| Configuration | Git | Helm values, `values.yaml`, and ADRs are version-controlled |

## Backup procedures

### PostgreSQL (audit trail)

KubeStack does not run PostgreSQL itself. Back up the operator-managed database on its own schedule. The schema is in `internal/store/schema.sql` (`audit_records`, `idempotency_keys`).

```bash
# Example: pg_dump (operator-managed, outside KubeStack)
pg_dump $DATABASE_URL > kubestack-audit-$(date +%F).sql
```

### etcd snapshots

KubeStack can trigger etcd snapshots through `POST /api/v1/clusters/{id}/snapshots`. Schedule this via your own automation. Snapshots are stored by Rancher (`ETCDSnapshot` CRDs), not KubeStack.

## Restore procedures

### KubeStack API

1. Restore PostgreSQL from backup.
2. `helm upgrade kubestack deploy/helm/kubestack -n kubestack` — the API is stateless; it reconnects to PostgreSQL and serves.
3. Verify `GET /healthz` returns 200.

### Downstream clusters

Out of KubeStack scope. Restore via Rancher/RKE2 etcd snapshot restore.

## Failover

KubeStack runs as a single Helm Deployment (`replicas: 1`). For higher availability, increase `replicas` and run PostgreSQL with a standby. The controller uses leader election (`LeaderElectionID: kubestack-controller`).

## Air-gapped considerations

All backups and restores must work without internet access. Store backup artifacts on your mirror or local storage; do not depend on external services.

## Testing

Restore procedures should be exercised at least once per release. Record results in `docs/sdlc.md` §8.
