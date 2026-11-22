# Copyright 2026 The KubeStack Authors. All rights reserved.
# Air-gapped build: all artifacts are pre-built on the host by `make build`
# (Go binary + portal bundle) and copied into the image. No network access
# is required at image build time.
ARG RUNTIME_IMAGE=gcr.io/distroless/static-debian12:nonroot
FROM ${RUNTIME_IMAGE}
COPY bin/kubestack-api /kubestack-api
COPY bin/kubestack-controller /kubestack-controller
COPY portal/dist /portal
COPY compliance/ /licenses/
ENV KUBESTACK_PORTAL_DIR=/portal
USER nonroot
ENTRYPOINT ["/kubestack-api"]
