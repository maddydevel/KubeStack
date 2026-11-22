# ADR 0005: Declarative clusters (KubeStackCluster CRD and kubestack-controller)

**Status:** accepted (v0.3, issue #5).

## Decision
- `KubeStackCluster` (`kubestack.dev/v1alpha1`, cluster-scoped, short name `ksc`). The resource name is the cluster ID.
  Spec: `tier` (`provisioned` | `attached`, immutable), `kubernetesVersion` (provisioned, required by CEL),
  `deletionPolicy` (`Retain` default | `Delete`). Status: tier, upgradeOwner, version, ready, `Synced` condition.
  Engine-neutral strings; nothing engine-specific in the type.
- `kubestack-controller` (same image, own Deployment and ServiceAccount, leader election) reconciles each resource
  through `engine.Engine` only:
  - not in the engine → create (provisioned, catalog-validated, IdP trust as in the API) or import (attached);
  - already in the engine with the same name and tier → adopted (no create);
  - `spec.kubernetesVersion` differs → `Cluster.Allowed(upgrade)` then the same `upgrade.Check` pre-flight as the API
    (capability, upgradeOwner, skew, snapshot, node health). Failures are reported in `Synced`, retried every minute.
  - Every engine write goes through `audit.Run`, shared with the API; if the attempt record can't be written the
    operation is refused. Audit actor: `system:kubestack-controller`.
- Destructive delete (rule 17): deleting the resource only releases the cluster unless `deletionPolicy: Delete` was set
  explicitly — that is the confirmation. A finalizer makes the delete run once (the idempotency key). Clusters whose
  tier forbids delete (hosted) keep the finalizer and report why until the policy is set back to `Retain`.

## Consequences
- Who may create, edit or delete KubeStackClusters is controlled by Kubernetes RBAC on the management cluster; grant
  it only to people who would be in the KubeStack admin group. The editing user is not recorded in the audit trail
  (would need an admission webhook).
- Metrics endpoint disabled; probes on :8081.
