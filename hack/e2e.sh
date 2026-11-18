#!/usr/bin/env bash
# Copyright 2026 The KubeStack Authors. All rights reserved.
#
# Runs the engine e2e test inside a disposable community Rancher container, against its embedded
# local cluster, with no network access needed inside the container: system charts are the bundled
# ones and the images Rancher needs are preloaded from hack/e2e-images/<version>.txt.
# Usage: hack/e2e.sh v2.15.3
set -euo pipefail
cd "$(dirname "$0")/.."
RANCHER=${1:?usage: hack/e2e.sh <rancher version>}
NAME=kubestack-e2e-rancher
export GOTOOLCHAIN=local

CGO_ENABLED=0 go test -c -tags e2e -o bin/rancher-e2e.test ./internal/engine/rancher/

LIST=hack/e2e-images/$RANCHER.txt
[[ -f $LIST ]] || { echo "missing $LIST: run Rancher $RANCHER once, list its ErrImagePull images there"; exit 1; }
IMAGES=$(grep -v '^#' "$LIST")
for i in $IMAGES; do docker image inspect "$i" >/dev/null 2>&1 || docker pull -q "$i" >/dev/null; done
docker save $IMAGES -o bin/e2e-images-$RANCHER.tar

docker rm -f "$NAME" >/dev/null 2>&1 || true
trap 'docker rm -f "$NAME" >/dev/null 2>&1 || true' EXIT
# Privileged is required by the single-container Rancher install (embedded k3s). Test use only.
# AGGREGATION_REGISTRATION_TIMEOUT: slow first boot otherwise trips Rancher's extension-API registration
# deadline (see https://ranchermanager.docs.rancher.com/api/extension-apiserver).
docker run -d --name "$NAME" --privileged \
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

docker cp bin/rancher-e2e.test "$NAME":/tmp/rancher-e2e.test
if ! docker exec -e KUBECONFIG=/etc/rancher/k3s/k3s.yaml "$NAME" /tmp/rancher-e2e.test -test.v -test.timeout 10m; then
  echo "--- diagnostics: leftover e2e clusters ---"
  docker exec "$NAME" kubectl get clusters.provisioning.cattle.io,clusters.management.cattle.io -A \
    -o custom-columns='KIND:.kind,NS:.metadata.namespace,NAME:.metadata.name,DELETING:.metadata.deletionTimestamp,FINALIZERS:.metadata.finalizers' || true
  docker exec "$NAME" sh -c "kubectl -n fleet-default get clusters.provisioning.cattle.io -o jsonpath='{range .items[*]}{.metadata.name}{\"\\n\"}{range .status.conditions[*]}  {.type}={.status} {.reason} {.message}{\"\\n\"}{end}{end}'" || true
  docker logs "$NAME" 2>&1 | grep -iE 'ks-e2e|error' | tail -20
  exit 1
fi
