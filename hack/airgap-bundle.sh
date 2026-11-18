#!/usr/bin/env bash
# Copyright 2026 The KubeStack Authors. All rights reserved.
#
# Builds bin/kubestack-airgap-<tag>.tar.gz for air-gapped installs: the unmodified product image,
# the Helm chart, an SPDX SBOM of the image, the compliance files and a mirror script.
# Usage: hack/airgap-bundle.sh <tag> <image ref>   (the image must exist locally: make image)
set -euo pipefail
cd "$(dirname "$0")/.."
TAG=${1:?usage: hack/airgap-bundle.sh <tag> <image>}
IMAGE=${2:?usage: hack/airgap-bundle.sh <tag> <image>}
VERSION=${TAG#v}
OUT=bin/kubestack-airgap-$TAG

rm -rf "$OUT" && mkdir -p "$OUT/images"
docker save "$IMAGE" -o "$OUT/images/kubestack-api.tar"
docker run --rm -v "$PWD/$OUT:/out" anchore/syft:v1.18.1 -q docker-archive:/out/images/kubestack-api.tar -o spdx-json=/out/sbom.spdx.json
docker run --rm -v "$PWD/deploy/helm:/charts:ro" -v "$PWD/$OUT:/out" alpine/helm:3.16.4 \
  package /charts/kubestack --version "$VERSION" --app-version "$VERSION" -d /out >/dev/null
cp -r compliance "$OUT/"
cp deploy/airgap/mirror.sh "$OUT/"
echo "$IMAGE" > "$OUT/images.txt"
(cd "$OUT" && sha256sum images/* *.tgz sbom.spdx.json > SHA256SUMS)
tar -C bin -czf "$OUT.tar.gz" "$(basename "$OUT")"
echo "$OUT.tar.gz"
