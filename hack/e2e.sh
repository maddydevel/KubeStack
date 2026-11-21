#!/usr/bin/env bash
# Copyright 2026 The KubeStack Authors. All rights reserved.
#
# Runs the engine e2e test inside a disposable community Rancher container, against its embedded
# local cluster, with no network access needed inside the container: system charts are the bundled
# ones and the images Rancher needs are preloaded from hack/e2e-images/<version>.txt.
# Usage: hack/e2e.sh v2.15.3 [--up]
#   --up: leave Rancher running for development (make dev-up), kubeconfig in bin/rancher-dev.kubeconfig.
# E2E_IMPORT_K3S=<k3s image>: also start a real K3s cluster on a shared Docker network and import it
# (needs container-to-container networking and image pulls; CI sets it).
set -euo pipefail
cd "$(dirname "$0")/.."
RANCHER=${1:?usage: hack/e2e.sh <rancher version> [--up]}
UP=${2:-}
NAME=kubestack-e2e-rancher
export GOTOOLCHAIN=local

mkdir -p bin
[[ -n $UP ]] || CGO_ENABLED=0 go test -c -tags e2e -o bin/rancher-e2e.test ./internal/engine/rancher/

LIST=hack/e2e-images/$RANCHER.txt
[[ -f $LIST ]] || { echo "missing $LIST: run Rancher $RANCHER once, list its ErrImagePull images there"; exit 1; }
IMAGES=$(grep -v '^#' "$LIST")
for i in $IMAGES; do docker image inspect "$i" >/dev/null 2>&1 || docker pull -q "$i" >/dev/null; done
docker save $IMAGES -o bin/e2e-images-$RANCHER.tar

K3S=kubestack-e2e-k3s NET=kubestack-e2e
docker rm -f "$NAME" "$K3S" >/dev/null 2>&1 || true
docker network rm "$NET" >/dev/null 2>&1 || true
[[ -n $UP ]] || trap 'docker rm -f "$NAME" "$K3S" >/dev/null 2>&1 || true; docker network rm "$NET" >/dev/null 2>&1 || true' EXIT
NETARGS=()
if [[ -n ${E2E_IMPORT_K3S:-} ]]; then
  docker network create "$NET" >/dev/null
  NETARGS=(--network "$NET")
fi
# Privileged is required by the single-container Rancher install (embedded k3s). Test use only.
# AGGREGATION_REGISTRATION_TIMEOUT: slow first boot otherwise trips Rancher's extension-API registration
# deadline (see https://ranchermanager.docs.rancher.com/api/extension-apiserver).
PORTS=()
# 18443: Rancher's server URL for nodes. DEV_BIND=0.0.0.0 lets pods on a node on this host reach it (CI).
[[ -n $UP ]] && PORTS=(-p 127.0.0.1:16443:6443 -p "${DEV_BIND:-127.0.0.1}:18443:443")
docker run -d --name "$NAME" --privileged "${PORTS[@]}" "${NETARGS[@]}" \
  -e CATTLE_BOOTSTRAP_PASSWORD=kubestack-e2e -e CATTLE_SYSTEM_CATALOG=bundled -e AGGREGATION_REGISTRATION_TIMEOUT=10m \
  -v "$PWD/bin/e2e-images-$RANCHER.tar:/var/lib/rancher/k3s/agent/images/kubestack-e2e.tar:ro" \
  "rancher/rancher:$RANCHER" >/dev/null

echo "waiting for Rancher $RANCHER CRDs and webhook..."
ready=
for _ in $(seq 120); do
  if [[ $(docker inspect -f '{{.State.Running}}' "$NAME") != true ]]; then
    echo "Rancher container exited:"; docker logs --tail 40 "$NAME"; exit 1
  fi
  if docker exec "$NAME" kubectl get crd clusters.provisioning.cattle.io >/dev/null 2>&1 &&
     docker exec "$NAME" kubectl -n cattle-system rollout status deploy/rancher-webhook --timeout=2s >/dev/null 2>&1 &&
     docker exec "$NAME" kubectl get settings.management.cattle.io rke2-default-version >/dev/null 2>&1 &&
     docker exec "$NAME" kubectl get machines.v1beta2.cluster.x-k8s.io,etcdsnapshots.rke.cattle.io -A >/dev/null 2>&1 &&
     # CAPI webhook must be serving, or Rancher never creates the CAPI Cluster and deletion hangs
     # on auth.cattle.io/capi-resources-cleanup (see rancher/rancher#40535).
     docker exec "$NAME" kubectl -n cattle-capi-system rollout status deploy/capi-controller-manager --timeout=2s >/dev/null 2>&1; then
    ready=1; break
  fi
  sleep 5
done
[[ -n $ready ]] || { echo "Rancher not ready after 10 minutes"; docker logs --tail 40 "$NAME"; exit 1; }

if [[ -n $UP ]]; then
  docker exec "$NAME" cat /etc/rancher/k3s/k3s.yaml | sed 's#https://127.0.0.1:6443#https://127.0.0.1:16443#' > bin/rancher-dev.kubeconfig
  chmod 600 bin/rancher-dev.kubeconfig
  echo "Rancher $RANCHER is up. KUBECONFIG=bin/rancher-dev.kubeconfig; make run-dev-rancher; make dev-down to remove."
  exit 0
fi

TESTENV=(-e KUBECONFIG=/etc/rancher/k3s/k3s.yaml)
if [[ -n ${E2E_IMPORT_K3S:-} ]]; then
  docker run -d --name "$K3S" --privileged --network "$NET" "$E2E_IMPORT_K3S" server --disable traefik >/dev/null
  for _ in $(seq 60); do docker exec "$K3S" kubectl get nodes 2>/dev/null | grep -q " Ready" && break; sleep 3; done
  ip() { docker inspect -f "{{(index .NetworkSettings.Networks \"$NET\").IPAddress}}" "$1"; }
  docker exec "$K3S" cat /etc/rancher/k3s/k3s.yaml | sed "s#https://127.0.0.1:6443#https://$(ip "$K3S"):6443#" > bin/e2e-k3s.kubeconfig
  docker cp bin/e2e-k3s.kubeconfig "$NAME":/tmp/k3s.kubeconfig && rm -f bin/e2e-k3s.kubeconfig
  TESTENV+=(-e KUBESTACK_E2E_IMPORT_KUBECONFIG=/tmp/k3s.kubeconfig -e KUBESTACK_E2E_SERVER_URL="https://$(ip "$NAME")")
  echo "real import: K3s $E2E_IMPORT_K3S at $(ip "$K3S"), Rancher at $(ip "$NAME")"
fi

docker cp bin/rancher-e2e.test "$NAME":/tmp/rancher-e2e.test
if ! docker exec "${TESTENV[@]}" "$NAME" /tmp/rancher-e2e.test -test.v -test.timeout 15m; then
  echo "--- diagnostics: leftover e2e clusters ---"
  docker exec "$NAME" kubectl get clusters.provisioning.cattle.io,clusters.management.cattle.io -A \
    -o custom-columns='KIND:.kind,NS:.metadata.namespace,NAME:.metadata.name,DELETING:.metadata.deletionTimestamp,FINALIZERS:.metadata.finalizers' || true
  docker exec "$NAME" sh -c "kubectl -n fleet-default get clusters.provisioning.cattle.io -o jsonpath='{range .items[*]}{.metadata.name}{\"\\n\"}{range .status.conditions[*]}  {.type}={.status} {.reason} {.message}{\"\\n\"}{end}{end}'" || true
  docker logs "$NAME" 2>&1 | grep -iE 'ks-e2e|error' | tail -20
  if [[ -n ${E2E_IMPORT_K3S:-} ]]; then
    echo "--- diagnostics: imported K3s agent ---"
    docker exec "$K3S" kubectl -n cattle-system get pods 2>&1 || true
    docker exec "$K3S" kubectl -n cattle-system logs deploy/cattle-cluster-agent --tail=20 2>&1 | grep -v "^INFO: Environment" || true
  fi
  exit 1
fi
