# Acceptance test on real infrastructure

The CI e2e runs against real community Rancher and imports a real K3s cluster, but it has no VMs, so it
never joins RKE2 nodes, drains them, runs an etcd snapshot or a `kubectl` OIDC login. Run this before
every release on infrastructure that resembles production. It takes about 1–2 hours.

## Prerequisites
- KubeStack installed with Helm into Rancher's local cluster (docs/sdlc.md §6), `oidc.redirectURL` set,
  `oidc.cliClientID` a **public** client, `oidc.caConfigMap` set if the IdP uses a private CA.
- 4 Linux VMs (2 vCPU, 4 GB) that reach Rancher's server URL and that your workstation reaches on 6443.
- Two IdP users: one in the admin group, one not. `kubectl` with the kubelogin plugin (`kubectl oidc-login`).
- An ID token of the admin user in `KUBESTACK_TOKEN`, `KUBESTACK_URL` pointing at KubeStack; `curl`, `jq`.

## Run
```bash
hack/acceptance.sh acc-1 v1.35.9+rke2r1 v1.36.5+rke2r1
```
The script pauses twice: to join the nodes (portal **Add nodes** → run the script on each VM; drop role
flags for the worker), and to try **Connect** as both users.

## Expected
| Step | Expected result |
|------|-----------------|
| create, registration | 201; command contains `{token}` (never the token) |
| join 4 nodes | 4 nodes Ready with roles; cluster Ready |
| snapshot | snapshot appears with `successful: true` |
| restore | 202; cluster returns to Ready |
| upgrade one minor | 202; same Idempotency-Key replays 202; all nodes on the target version |
| remove worker | 202; 3 nodes left; removing the last etcd node would be refused (409) |
| connect | admin user: `kubectl get nodes` works; non-admin: Forbidden |
| delete | 202, then 404; audit has a `success` record for every operation |

Also check by hand once per release:
- The portal disables operations a tier does not support and shows the reason.
- Air-gapped: install from `kubestack-airgap-<tag>.tar.gz` with `mirror.sh` and no internet route.
- `cosign verify` succeeds for the release image (docs/sdlc.md §5).

Record the result (date, versions, PASS/FAIL, notes) in docs/sdlc.md §8.
