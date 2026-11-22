# KubeStack Best Practices

Operational recommendations for running KubeStack in production.

## Cluster lifecycle

### Provisioning

- Use `tier: provisioned` only for clusters KubeStack should fully manage (RKE2 via Rancher).
- Use `tier: attached` for existing clusters you want to observe and disconnect, but not manage.
- Use `tier: hosted` (EKS/AKS/GKE) for cloud clusters — KubeStack recognizes them but does not create or upgrade them.
- Always specify a `kubernetesVersion` from the release catalog for provisioned clusters.

### Upgrades

- Upgrade one minor version at a time (skew rules enforced by the engine).
- Take an etcd snapshot before every upgrade; snapshots older than 24 hours block upgrades.
- Verify all nodes are Ready before starting an upgrade.
- Clusters with `upgradeOwner: cloud` must be upgraded through the cloud console — KubeStack refuses them.
- Clusters with `upgradeOwner: none` (imported) cannot be upgraded through KubeStack (no etcd snapshot visibility).

### Deletion

- Use `deletionPolicy: Retain` if you want to keep the downstream cluster running after removing the `KubeStackCluster` resource.
- Use `deletionPolicy: Delete` only when you want KubeStack to remove (provisioned) or disconnect (attached) the cluster.
- Type the cluster name to confirm destructive operations — the API requires an explicit confirmation.

## Identity and access

- Assign users to the admin group (`kubestack-admins` by default) only if they need full cluster lifecycle control.
- Non-admin users can read clusters and use direct access (`/access`) with their own IdP identity.
- Rotate the OIDC client secret regularly via `kubestack-oidc-secret`.
- Use a public client ID for `kubectl`/`kubelogin` (`oidc.cliClientID`).
- Never embed join tokens in scripts or commit them to version control — tokens stay in Kubernetes Secrets.

## Security

- Run the Helm chart with the default security context: `runAsNonRoot`, `readOnlyRootFilesystem`, `drop: [ALL]`.
- Keep `KUBESTACK_DEV_AUTH` unset in production (dev auth makes every request an admin).
- Restrict access to the `kubestack-db` secret — it contains the PostgreSQL DSN.
- Review audit records regularly (`GET /api/v1/audit`) — every lifecycle operation is bracketed with attempt/result records.
- Do not log or return join tokens; KubeStack never reads `status.token` from Rancher.

## High availability

- Scale the API Deployment beyond one replica if needed — it is stateless.
- The controller uses leader election (`LeaderElectionID: kubestack-controller`); only one replica processes reconciles.
- Run PostgreSQL with a standby for durable audit trail availability.
- The API exposes `/healthz` and `/readyz` probes on port 8080.

## Air-gapped operation

- Preload all container images into your mirror (`deploy/airgap/images.txt`, `mirror.sh`).
- Verify SBOM and cosign signature before installing an air-gapped bundle.
- Ensure the IdP is reachable from the cluster's private network (no public internet dependency).
- Test restore procedures at least once per release — backups are only valid if you can restore from them.

## Monitoring and observability

- Alert on API `/healthz` failures.
- Alert on audit records with `result: failure` — operations were refused.
- Monitor the controller's leader election status.
- Watch PostgreSQL connection usage if using the durable audit store.
- Review `docs/operations/DR.md` for RTO/RPO targets and restore procedures.

## Compliance

- All shipped dependencies are permissively licensed; verified by `make license-check`.
- New dependencies require an entry in `compliance/APPROVALS.md`.
- The SBOM (`sbom.spdx.json`) covers the Go binary, base image, and portal npm packages (`sbom-npm.json`).
- Release images are signed with cosign (keyless, GitHub OIDC).

## Do not

- Do not call Rancher's Steve or Norman APIs — use native CRD APIs only (`internal/engine/rancher/`).
- Do not store or log join tokens or client secrets.
- Do not bypass upgrade pre-flight checks (skew, snapshot age, node readiness).
- Do not run `KUBESTACK_DEV_AUTH=true` in production.
- Do not modify Rancher's own UI or fork Rancher source.
