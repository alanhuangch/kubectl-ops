#!/usr/bin/env bash

set -euo pipefail

E2E_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
KIND="${KIND:-kind}"
KUBECTL="${KUBECTL:-kubectl}"
DOCKER="${DOCKER:-docker}"
JQ="${JQ:-jq}"
KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-kubectl-ops-e2e}"
KIND_CONFIG="${KIND_CONFIG:-${E2E_ROOT}/test/e2e/kind.yaml}"
KIND_NODE_IMAGE="${KIND_NODE_IMAGE:-}"
E2E_KUBECONFIG="${E2E_KUBECONFIG:-${E2E_ROOT}/.e2e/kubeconfig}"
E2E_HELPER_IMAGE="${E2E_HELPER_IMAGE:-kubectl-ops-e2e/helper:local}"
E2E_KEEP_CLUSTER="${E2E_KEEP_CLUSTER:-false}"
E2E_KEEP_FIXTURES="${E2E_KEEP_FIXTURES:-false}"
E2E_NAMESPACE="${E2E_NAMESPACE:-kubectl-ops-e2e}"
E2E_BIN="${E2E_BIN:-${E2E_ROOT}/bin/kubectl-ops}"

configure_docker() {
	if [[ -n "${E2E_DOCKER_CONTEXT:-}" ]]; then
		unset DOCKER_HOST
		export DOCKER_CONTEXT="${E2E_DOCKER_CONTEXT}"
	elif [[ -n "${E2E_DOCKER_HOST:-}" ]]; then
		export DOCKER_HOST="${E2E_DOCKER_HOST}"
	fi
}

require_command() {
	local command_name="$1"
	if ! command -v "${command_name}" >/dev/null 2>&1; then
		echo "required command not found: ${command_name}" >&2
		return 1
	fi
}

cluster_exists() {
	"${KIND}" get clusters 2>/dev/null | grep -Fxq "${KIND_CLUSTER_NAME}"
}

write_kubeconfig() {
	mkdir -p "$(dirname "${E2E_KUBECONFIG}")"
	"${KIND}" get kubeconfig --name "${KIND_CLUSTER_NAME}" >"${E2E_KUBECONFIG}"
	chmod 600 "${E2E_KUBECONFIG}"
}

export KUBECONFIG="${E2E_KUBECONFIG}"
