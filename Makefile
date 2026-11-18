# Copyright 2026 The KubeStack Authors. All rights reserved.
export GOTOOLCHAIN := local
REGISTRY ?= localhost:5000
TAG ?= dev
RANCHER ?= $(shell awk '/^  - v/{print $$2; exit}' deploy/compat.yaml)
PG_URL := postgres://postgres:test@127.0.0.1:15432/postgres?sslmode=disable

.PHONY: build test test-integration lint license-check e2e e2e-matrix helm-lint image run-dev ci
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

lint:
	gofmt -l . | (! grep .) && go vet ./... && go vet -tags e2e ./internal/engine/rancher/
	cd portal && npm run lint

license-check:
	./hack/license-check.sh

e2e:
	./hack/e2e.sh $(RANCHER)

e2e-matrix:
	for v in $$(awk '/^  - v/{print $$2}' deploy/compat.yaml); do ./hack/e2e.sh $$v || exit 1; done

helm-lint:
	docker run --rm -v $(CURDIR)/deploy/helm:/charts alpine/helm:3.16.4 lint /charts/kubestack --set oidc.issuer=https://idp.example.internal,oidc.clientID=kubestack

image:
	docker build --build-arg REGISTRY=docker.io -t $(REGISTRY)/kubestack/kubestack-api:$(TAG) .

# Fake engine + in-memory store + dev auth; portal at http://localhost:8080
run-dev: build
	KUBESTACK_ENGINE=fake KUBESTACK_DEV_AUTH=true KUBESTACK_PORTAL_DIR=portal/dist ./bin/kubestack-api

ci: lint test license-check build test-integration helm-lint image
