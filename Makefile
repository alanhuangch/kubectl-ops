BINARY := kubectl-ops
VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w \
	-X github.com/alanhuangch/kubectl-ops/internal/command.version=$(VERSION) \
	-X github.com/alanhuangch/kubectl-ops/internal/command.commit=$(COMMIT) \
	-X github.com/alanhuangch/kubectl-ops/internal/command.buildDate=$(BUILD_DATE)

KIND ?= kind
KUBECTL ?= kubectl
DOCKER ?= docker
JQ ?= jq
KIND_CLUSTER_NAME ?= kubectl-ops-e2e
KIND_CONFIG ?= $(CURDIR)/test/e2e/kind.yaml
KIND_NODE_IMAGE ?=
E2E_KUBECONFIG ?= $(CURDIR)/.e2e/kubeconfig
E2E_HELPER_IMAGE ?= kubectl-ops-e2e/helper:local
E2E_KEEP_CLUSTER ?= false
E2E_KEEP_FIXTURES ?= false
E2E_DOCKER_HOST ?=
E2E_DOCKER_CONTEXT ?=

E2E_ENV = \
	KIND="$(KIND)" \
	KUBECTL="$(KUBECTL)" \
	DOCKER="$(DOCKER)" \
	JQ="$(JQ)" \
	KIND_CLUSTER_NAME="$(KIND_CLUSTER_NAME)" \
	KIND_CONFIG="$(KIND_CONFIG)" \
	KIND_NODE_IMAGE="$(KIND_NODE_IMAGE)" \
	E2E_KUBECONFIG="$(E2E_KUBECONFIG)" \
	E2E_HELPER_IMAGE="$(E2E_HELPER_IMAGE)" \
	E2E_KEEP_CLUSTER="$(E2E_KEEP_CLUSTER)" \
	E2E_KEEP_FIXTURES="$(E2E_KEEP_FIXTURES)" \
	E2E_DOCKER_HOST="$(E2E_DOCKER_HOST)" \
	E2E_DOCKER_CONTEXT="$(E2E_DOCKER_CONTEXT)"

.PHONY: build test lint fmt clean e2e e2e-test e2e-cluster-create e2e-cluster-delete e2e-deps

build:
	mkdir -p bin
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/kubectl-ops

test:
	go test ./...

lint:
	go vet ./...
	test -z "$$(gofmt -l $$(find . -name '*.go' -not -path './vendor/*'))"

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './vendor/*')

e2e: build e2e-deps
	@$(E2E_ENV) ./test/e2e/e2e.sh

e2e-test: build e2e-deps
	@$(E2E_ENV) ./test/e2e/run.sh

e2e-cluster-create: e2e-deps
	@$(E2E_ENV) ./test/e2e/cluster.sh create

e2e-cluster-delete: e2e-deps
	@$(E2E_ENV) ./test/e2e/cluster.sh delete

e2e-deps:
	@$(E2E_ENV) ./test/e2e/check-deps.sh

clean:
	rm -f bin/$(BINARY) coverage.out
