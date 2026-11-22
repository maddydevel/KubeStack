# KubeStack End-User Guide

KubeStack is a web-based control plane for RKE2 clusters. You use it to create, upgrade, and manage Kubernetes clusters through a portal, without calling Rancher APIs directly.

## Sign in

1. Open the KubeStack portal URL.
2. Sign in with your organisation's IdP credentials.
3. If your IdP uses a private CA not in your OS trust store, your administrator must configure it first.

## Dashboard

The dashboard lists all clusters KubeStack knows about. For each cluster you see:

| Field | Meaning |
|-------|---------|
| Name | Cluster identifier |
| Tier | `provisioned` (KubeStack created it), `hosted` (EKS/AKS/GKE), `attached` (imported) |
| Version | Kubernetes version |
| Ready | Whether the cluster is ready for operations |
| Synced | Whether the controller has reconciled it (CRD mode only) |

## Create a cluster

1. Click **Create cluster**.
2. Enter a name (DNS-1123 label: lowercase, numbers, hyphens, max 63 chars).
3. Choose a Kubernetes version from the catalog.
4. Click **Create**.

The cluster is provisioned through Rancher (RKE2 with Traefik ingress). It shows `Ready: false` until Rancher reports it ready; refresh the page.

## Upgrade a cluster

1. Click **Upgrade** on a `provisioned` cluster managed by Rancher (`upgradeOwner: rancher`).
2. Choose a target version one minor at a time.
3. Pre-flight checks run automatically: recent etcd snapshot (< 24h old), all nodes ready, version in catalog.
4. If any check fails, the upgrade is refused with a reason. Fix the issue and retry.

Clusters managed by the cloud provider (`upgradeOwner: cloud`) cannot be upgraded through KubeStack — use the cloud console.

## Delete a cluster

1. Click **Delete**.
2. Type the cluster name to confirm.
3. Choose `Retain` (disconnect only — the cluster survives) or `Delete` (remove the cluster).

## Take a snapshot

1. Click **Take snapshot** on a `provisioned` cluster.
2. The etcd snapshot is stored by Rancher.
3. Snapshots older than 24 hours block upgrades; take a fresh one before upgrading.

## Restore from a snapshot

1. Click **Restore** and choose a snapshot.
2. Restore keeps the cluster's version and configuration; only etcd data is restored.
3. This is destructive — confirm before proceeding.

## Nodes

1. Click **Nodes** to list nodes with roles (etcd, control-plane, worker) and readiness.
2. Click **Remove node** on a worker node.
3. You cannot remove the last etcd or control-plane node — that would leave the cluster without a control plane.

## Direct access (kubectl)

1. Click **Connect** on a `provisioned` cluster.
2. KubeStack returns the API server addresses, cluster CA, and IdP settings — no credentials.
3. The portal builds a kubeconfig whose user runs `kubectl oidc-login` (kubelogin). The token comes from your IdP to your machine and never passes through KubeStack.
4. You need network reach to control-plane nodes on port 6443 and the kubelogin plugin.

## Import an existing cluster

1. Click **Import cluster**.
2. Enter a name.
3. KubeStack attaches it as `attached` tier with `upgradeOwner: none`. You manage its lifecycle outside KubeStack.
4. To disconnect later, click **Delete** — the cluster itself is not destroyed.

## What you cannot do

| Operation | Reason |
|-----------|--------|
| Upgrade a `hosted` cluster | Cloud providers manage their own versions |
| Upgrade an `attached` cluster with `upgradeOwner: none` | No engine owns its version |
| See join tokens | Tokens stay in Kubernetes Secrets; you read them with your own `kubectl` |
| Call Rancher's UI APIs (Steve/Norman) | KubeStack uses native CRD APIs only |

## Getting help

- Check the runbook (`docs/operations/ADMIN.md`) for common symptoms.
- Open a GitHub issue for bugs or feature requests.
- For security issues, see `SECURITY.md` — do not open a public issue.
