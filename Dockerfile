# Copyright 2026 The KubeStack Authors. All rights reserved.
# Base images resolve through REGISTRY so air-gapped builds use the mirror.
ARG REGISTRY=docker.io
ARG RUNTIME_IMAGE=gcr.io/distroless/static-debian12:nonroot
FROM ${REGISTRY}/library/node:24-alpine AS portal
WORKDIR /src/portal
COPY portal/package*.json ./
RUN npm ci --no-audit --no-fund
COPY portal/ ./
RUN npm run build

FROM ${REGISTRY}/library/golang:1.23 AS api
WORKDIR /src
ENV GOTOOLCHAIN=local CGO_ENABLED=0
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
RUN go build -trimpath -ldflags=-buildid= -o /kubestack-api ./cmd/kubestack-api

FROM ${RUNTIME_IMAGE}
COPY --from=api /kubestack-api /kubestack-api
COPY --from=portal /src/portal/dist /portal
COPY compliance/ /licenses/
ENV KUBESTACK_PORTAL_DIR=/portal
USER nonroot
ENTRYPOINT ["/kubestack-api"]
