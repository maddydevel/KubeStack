# ADR 0001: Headless lifecycle engine behind an adapter

Status: accepted

## Decision
KubeStack owns the portal, API, domain model, workflows and audit. Community Rancher runs headless
as the lifecycle engine, reached only through `internal/engine/rancher/` using native CRD APIs.

## Findings recorded in the adapter (verified by `make e2e` against Rancher v2.15.3)
- Downstream clusters are `provisioning.cattle.io/v1 Cluster` in `fleet-default`. `spec.rkeConfig`
  present ⇒ tier `provisioned`, upgradeOwner `rancher`; upgrades patch `spec.kubernetesVersion`.
- Hosted tier is detected via `management.cattle.io/v3 Cluster` `spec.{eks,aks,gke}Config` ⇒ upgradeOwner `cloud`.
- Imported clusters are `attached` with upgradeOwner `none` (conservative) until mgmt `rke2Config`/`k3sConfig`
  semantics are verified.
- Etcd snapshots: `rke.cattle.io/v1 ETCDSnapshot`, label `rke.cattle.io/cluster-name`, `snapshotFile.status == successful`.
- Node health: Cluster API `Machine` (v1beta2; v1beta1 deprecated in Rancher v2.15) condition `Ready=True`, label `cluster.x-k8s.io/cluster-name`.
- Snapshot/restore: bump `spec.rkeConfig.etcdSnapshotCreate.generation` / `etcdSnapshotRestore.{name,generation,restoreRKEConfig: none}`
  (Rancher v2.15.3 `pkg/apis/rke.cattle.io/v1/common_types.go`); the patch carries `metadata.resourceVersion` so
  concurrent requests conflict. The newer `operation.cattle.io/v1alpha1 ETCDSnapshotRestore` API is not used yet.
- Node roles: Machine labels `rke.cattle.io/{etcd,control-plane,worker}-role=true` (`pkg/capr/common.go`). Node removal
  deletes the Machine with a UID precondition after checking its cluster label.
- New clusters set RKE2 `ingress-controller: traefik` in `machineGlobalConfig` (accepted by the v2.15.3 webhook).
- `kubernetesVersion` must carry the `v` prefix; the webhook rejects `1.36.5+rke2r1`.

## Upgrade path
Only `provisioned` + upgradeOwner `rancher` clusters can be upgraded in this release.
upgradeOwner `kubestack` (System Upgrade Controller) is refused until implemented.
