SHELL := /bin/bash
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := help

BINARY := kubectl-ops
COMPLETION_BINARY := kubectl_complete-ops
COMPLETION_SCRIPT := $(CURDIR)/scripts/$(COMPLETION_BINARY)
GO ?= go
INSTALL ?= install
OUTPUT_DIR ?= $(CURDIR)/bin
COVERAGE_FILE ?= $(CURDIR)/coverage.out
GO_PACKAGES ?= ./...
GO_BUILD_FLAGS ?= -trimpath
GO_TEST_FLAGS ?=
VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS = -s -w \
	-X github.com/alanhuangch/kubectl-ops/internal/command.version=$(VERSION) \
	-X github.com/alanhuangch/kubectl-ops/internal/command.commit=$(COMMIT) \
	-X github.com/alanhuangch/kubectl-ops/internal/command.buildDate=$(BUILD_DATE)
BINARY_PATH := $(OUTPUT_DIR)/$(BINARY)

GOBIN ?= $(shell $(GO) env GOBIN)
ifeq ($(strip $(GOBIN)),)
GOBIN := $(shell $(GO) env GOPATH)/bin
endif
INSTALL_DIR ?= $(GOBIN)
DESTDIR ?=

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

.PHONY: help deps verify check build install test coverage lint fmt clean \
	e2e e2e-test e2e-cluster-create e2e-cluster-delete e2e-deps

help:
	@echo "kubectl-ops development targets:"
	@echo "  make deps       Download and verify Go modules"
	@echo "  make verify     Check that go.mod and go.sum are tidy"
	@echo "  make lint       Run gofmt, go vet, and shell syntax checks"
	@echo "  make test       Run unit tests with race detection and coverage"
	@echo "  make build      Build $(BINARY_PATH)"
	@echo "  make install    Install the plugin and completion adapter into INSTALL_DIR ($(INSTALL_DIR))"
	@echo "  make check      Run all non-E2E CI checks"
	@echo "  make e2e        Create a kind cluster, test, and clean it up"
	@echo "  make e2e-test   Test against an existing E2E kind cluster"

deps:
	$(GO) mod download
	$(GO) mod verify

verify:
	$(GO) mod tidy -diff

check: deps verify lint test build

build:
	mkdir -p "$(OUTPUT_DIR)"
	$(GO) build $(GO_BUILD_FLAGS) -ldflags "$(LDFLAGS)" -o "$(BINARY_PATH)" ./cmd/kubectl-ops

install: build
	$(INSTALL) -d "$(DESTDIR)$(INSTALL_DIR)"
	$(INSTALL) -m 0755 "$(BINARY_PATH)" "$(DESTDIR)$(INSTALL_DIR)/$(BINARY)"
	$(INSTALL) -m 0755 "$(COMPLETION_SCRIPT)" "$(DESTDIR)$(INSTALL_DIR)/$(COMPLETION_BINARY)"

test:
	$(GO) test -race -covermode=atomic -coverprofile="$(COVERAGE_FILE)" $(GO_TEST_FLAGS) $(GO_PACKAGES)

coverage: test
	$(GO) tool cover -func="$(COVERAGE_FILE)"

lint:
	$(GO) vet $(GO_PACKAGES)
	test -z "$$(gofmt -l $$(find . -name '*.go' -not -path './vendor/*'))"
	sh -n "$(COMPLETION_SCRIPT)"
	bash -n test/e2e/*.sh

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
	rm -f "$(BINARY_PATH)" "$(COVERAGE_FILE)"
