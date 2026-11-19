#!/usr/bin/env bash
# Copyright 2026 The KubeStack Authors. All rights reserved.
#
# Run inside an unpacked air-gap bundle: verifies checksums, loads the images unmodified and pushes
# them to your mirror. Usage: ./mirror.sh registry.example.internal
set -euo pipefail
cd "$(dirname "$0")"
MIRROR=${1:?usage: ./mirror.sh <mirror registry>}
sha256sum -c SHA256SUMS
for tar in images/*.tar; do
  docker load -i "$tar"
done
while read -r src; do
  dst="$MIRROR/${src#*/}" # replace the source registry host, keep repository and tag
  docker tag "$src" "$dst" && docker push "$dst"
done < images.txt
echo "Install with: helm install kubestack ./kubestack-*.tgz --set image.registry=$MIRROR ..."
