# KubeStack Installation Guide

Install KubeStack into Rancher's local cluster using the Helm chart. The chart installs the API server, the declarative controller, RBAC, and the CRDs.

## Prerequisites

- A Kubernetes cluster running community Rancher v2.15.3 (see `deploy/compat.yaml`)
- `helm` v3.16+
- `kubectl` configured with access to the Rancher local cluster
- An OIDC Identity Provider (IdP) reachable from the cluster (Dex, Keycloak, Okta, etc.)
- A PostgreSQL database for the durable audit trail (recommended; memory store used otherwise)
- Air-gapped: a container mirror with the KubeStack image and community Rancher images (see `deploy/airgap/`)

## Step 1 — Create the namespace and database secret

```bash
kubectl create ns kubestack

kubectl -n kubestack create secret generic kubestack-db \
  --from-literal=DATABASE_URL='postgres://postgres:<password>@<host>:5432/postgres?sslmode=disable'
```

Omit `kubestack-db` to run with an in-memory audit trail (lost on restart, dev only).

## Step 2 — Install the Helm chart

```bash
helm install kubestack deploy/helm/kubestack -n kubestack \
  --set image.registry=<your-mirror> \
  --set image.tag=v0.2.1 \
  --set databaseSecret=kubestack-db \
  --set adminGroup=<your-admin-oidc-group> \
  --set oidc.issuer=https://<idp> \
  --set oidc.clientID=<oidc-client-id> \
  --set oidc.redirectURL=https://kubestack.<host>/auth/callback \
  --set oidc.clientSecretSecret=kubestack-oidc-secret \
  --set oidc.cliClientID=<public-client-for-kubectl> \
  --set controller.enabled=true
```

Air-gapped install from a bundle:

```bash
tar xzf kubestack-airgap-v0.2.1.tar.gz
cd kubestack-airgap-v0.2.1
./mirror.sh <your-mirror-registry>
helm install kubestack deploy/helm/kubestack -n kubestack \
  --set image.registry=<your-mirror-registry> \
  --set image.tag=v0.2.1 \
  --set databaseSecret=kubestack-db
```

## Step 3 — Register the portal redirect URL with your IdP

Add `https://kubestack.<host>/auth/callback` as an authorized redirect URI in your IdP client configuration.

For a confidential client, create a Kubernetes secret:

```bash
kubectl -n kubestack create secret generic kubestack-oidc-secret \
  --from-literal=CLIENT_SECRET=<oidc-client-secret>
```

## Step 4 — Verify the install

```bash
kubectl -n kubestack get pods
kubectl -n kubestack get svc kubestack-api
```

Portal URL: `http://<kubestack-service-host>/` (or via ingress).

Sign in with an IdP user in the admin group. The first sign-in creates the admin session.

## Step 5 — Create your first cluster

1. Open the portal and click **Create cluster**.
2. Enter a cluster name (DNS-1123 label) and choose a Kubernetes version from the catalog.
3. The cluster is provisioned through Rancher (RKE2 + Traefik).
4. Refresh until **Ready** is true.

## Air-gapped considerations

- All images must be preloaded into your mirror (`deploy/airgap/images.txt`, `mirror.sh`).
- The IdP must be reachable from the cluster's private network (no public internet).
- The SBOM (`sbom.spdx.json`) and image signature are in the air-gap bundle for compliance verification.

## Uninstall

```bash
helm uninstall kubestack -n kubestack
kubectl delete ns kubestack
```

Deleting a `KubeStackCluster` with `deletionPolicy: Retain` stops management without destroying the downstream cluster. With `Delete`, the provisioned cluster is removed (or an attached cluster is disconnected).

## Next steps

- `docs/operations/ADMIN.md` — day-to-day operations and runbook
- `docs/configuration.md` — all Helm values and environment variables
- `docs/USER_GUIDE.md` — end-user portal guide
- `docs/best-practices.md` — operational best practices
