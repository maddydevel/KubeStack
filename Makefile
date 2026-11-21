# Copyright 2026 The KubeStack Authors. All rights reserved.
export GOTOOLCHAIN := local
REGISTRY ?= localhost:5000
TAG ?= dev
RANCHER ?= $(shell awk '/^  - v/{print $$2; exit}' deploy/compat.yaml)
# The community Rancher image (or its mirror) whose bundled RKE2 metadata generates the release catalog.
RANCHER_IMAGE ?= rancher/rancher:$(RANCHER)
PG_URL := postgres://postgres:test@127.0.0.1:15432/postgres?sslmode=disable

IMAGE := $(REGISTRY)/kubestack/kubestack-api:$(TAG)
GOLANGCI := docker run --rm -v $(CURDIR):/src -v $(shell go env GOMODCACHE):/go/pkg/mod:ro -w /src -e GOTOOLCHAIN=local -e GOPROXY=off -e GOFLAGS=-buildvcs=false golangci/golangci-lint:v1.64.8

.PHONY: build test test-integration lint license-check e2e e2e-matrix helm-lint image sbom airgap-bundle dev-up dev-down run-dev run-dev-rancher ci catalog catalog-check
build:
	go build -trimpath -o bin/kubestack-api ./cmd/kubestack-api
	cd portal && npm ci --no-audit --no-fund && npm run build

test:
	go test -race ./...
	cd portal && npm test

# Store contract against a real PostgreSQL in Docker.
test-integration:
	docker rm -f kubestack-test-pg >/dev/null 2>&1 || true
	docker run -d --name kubestack-test-pg -e POSTGRES_PASSWORD=test -p 127.0.0.1:15432:5432 postgres:17-alpine >/dev/null
	until docker exec kubestack-test-pg pg_isready -U postgres -h 127.0.0.1 -q; do sleep 1; done
	KUBESTACK_TEST_DATABASE_URL='$(PG_URL)' go test -race -count=1 ./internal/store/; s=$$?; docker rm -f kubestack-test-pg >/dev/null; exit $$s

# golangci-lint (GPL-3.0) runs in Docker as a dev tool only; it is never linked or shipped.
lint:
	gofmt -l . | (! grep .) && go vet ./... && go vet -tags e2e ./internal/engine/rancher/
	go mod download && $(GOLANGCI) golangci-lint run --timeout 5m --build-tags e2e ./...
	cd portal && npm run lint

license-check:
	./hack/license-check.sh

e2e:
	./hack/e2e.sh $(RANCHER)

e2e-matrix:
	for v in $$(awk '/^  - v/{print $$2}' deploy/compat.yaml); do ./hack/e2e.sh $$v || exit 1; done

helm-lint:
	docker run --rm -v $(CURDIR)/deploy/helm:/charts alpine/helm:3.16.4 lint /charts/kubestack --set oidc.issuer=https://idp.example.internal,oidc.clientID=kubestack
	# Regressions found by installing v0.2.0: kubelet cannot verify runAsNonRoot for a named image user, and the
	# image must resolve to the path mirror.sh pushes.
	docker run --rm -v $(CURDIR)/deploy/helm:/charts alpine/helm:3.16.4 template /charts/kubestack \
	  --set oidc.issuer=https://idp.example.internal,oidc.clientID=kubestack,image.registry=mirror.internal,image.tag=vX \
	  | grep -q 'runAsUser: 65532' && \
	  docker run --rm -v $(CURDIR)/deploy/helm:/charts alpine/helm:3.16.4 template /charts/kubestack \
	  --set oidc.issuer=https://idp.example.internal,oidc.clientID=kubestack,image.registry=mirror.internal,image.tag=vX \
	  | grep -q 'image: "mirror.internal/kubestack/kubestack-api:vX"'

image:
	docker build --load --build-arg REGISTRY=docker.io -t $(IMAGE) .

sbom:
	mkdir -p bin && docker save $(IMAGE) -o bin/image.tar
	docker run --rm -v $(CURDIR)/bin:/out anchore/syft:v1.18.1 -q docker-archive:/out/image.tar -o spdx-json=/out/sbom.spdx.json
	rm bin/image.tar

airgap-bundle: image
	./hack/airgap-bundle.sh $(TAG) $(IMAGE)

# A disposable community Rancher for local development (same setup as e2e), kubeconfig in bin/.
dev-up:
	./hack/e2e.sh $(RANCHER) --up

dev-down:
	docker rm -f kubestack-e2e-rancher

# Fake engine + in-memory store + dev auth; portal at http://localhost:8080
run-dev: build
	KUBESTACK_ENGINE=fake KUBESTACK_DEV_AUTH=true KUBESTACK_PORTAL_DIR=portal/dist ./bin/kubestack-api

# Real engine (from dev-up) + memory store + dev auth.
run-dev-rancher: build
	KUBESTACK_ENGINE=rancher KUBESTACK_RANCHER_KUBECONFIG=bin/rancher-dev.kubeconfig KUBESTACK_DEV_AUTH=true KUBESTACK_PORTAL_DIR=portal/dist ./bin/kubestack-api

ci: lint test license-check build test-integration helm-lint image

# RKE2 release catalog from the pinned Rancher image's bundled release metadata (no network at runtime).
catalog:
	mkdir -p bin
	docker run --rm --entrypoint cat $(RANCHER_IMAGE) /var/lib/rancher-data/driver-metadata/data.json > bin/kdm-$(RANCHER).json
	go run ./hack/gencatalog -rancher $(RANCHER) -kdm bin/kdm-$(RANCHER).json > internal/upgrade/releases.json

# Fails if the committed catalog is not what `make catalog` generates.
catalog-check: catalog
	git diff --exit-code internal/upgrade/releases.json
