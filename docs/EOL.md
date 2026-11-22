# Decommissioning / End-of-Life Plan

## Scope

This plan covers retiring a KubeStack installation: stopping the API and controller, revoking access, and cleaning up infrastructure. It does not cover retiring downstream clusters (managed by Rancher/RKE2).

## When to decommission

- The organisation no longer needs a self-hosted control plane.
- Migration to a different platform.
- End of support for the pinned Rancher version (`deploy/compat.yaml`).

## Procedure

### 1. Stop managing clusters

For each `KubeStackCluster` resource:

- Set `deletionPolicy: Retain` if you want to keep the downstream cluster running.
- Set `deletionPolicy: Delete` if you want KubeStack to remove (provisioned) or disconnect (attached) it.
- Delete the `KubeStackCluster` resource or delete the Helm release.

```bash
# Option A: stop managing, keep clusters
kubectl -n kubestack get kubestackclusters -o yaml | \
  sed 's/deletionPolicy: Delete/deletionPolicy: Retain/' | \
  kubectl apply -f -

# Option B: full removal
helm uninstall kubestack -n kubestack
kubectl delete ns kubestack
```

### 2. Revoke access

- Remove the IdP redirect URL registration (`oidc.redirectURL`).
- Revoke the OIDC client secret if one was configured.
- Remove Kubernetes RBAC (ServiceAccount, Role, RoleBinding, ClusterRole, ClusterRoleBinding) created by the chart.
- Delete the PostgreSQL database or revoke KubeStack's credentials.

### 3. Clean up infrastructure

- Uninstall the Helm chart: `helm uninstall kubestack -n kubestack`.
- Delete the namespace: `kubectl delete ns kubestack`.
- Remove image mirror entries if the mirror is being decommissioned.
- Remove backup schedules for the audit database.

### 4. Archive records

- Export audit records before deleting PostgreSQL: `SELECT * FROM audit_records`.
- Export the Helm values (`values.yaml`) and any overrides for compliance retention.
- Keep `compliance/THIRD-PARTY.md` and `compliance/NOTICE` for license compliance records.

## Data retention

| Data | Retention | Action on decommission |
|------|-----------|------------------------|
| Audit records | Per organisation policy | Export before DB deletion |
| Cluster configs | Git | Retain in version control |
| Container images | Mirror policy | Remove from mirror if no longer needed |
| Helm chart | Git | Retain in version control |

## Downstream clusters

KubeStack decommissioning does not touch downstream clusters unless you chose `deletionPolicy: Delete`. Provisioned clusters continue running under Rancher; imported clusters continue running under their original management. You must manage them through Rancher directly after KubeStack is gone.

## Rollback

Before deleting PostgreSQL or the Helm release, confirm:

- All audit records are exported.
- No cluster is mid-operation (check `GET /api/v1/audit` for `attempt` without `success`/`failure`).
- Downstream clusters are in a known state.

If anything is in flight, wait for it to complete or record the outcome manually before proceeding.
