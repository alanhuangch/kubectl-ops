#!/usr/bin/env bash

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

configure_docker
created_cluster=false

cleanup() {
	local exit_code=$?
	if [[ "${created_cluster}" == "true" && "${E2E_KEEP_CLUSTER}" != "true" ]]; then
		"${E2E_ROOT}/test/e2e/cluster.sh" delete || true
	elif [[ "${created_cluster}" == "true" ]]; then
		echo "Keeping kind cluster ${KIND_CLUSTER_NAME}."
	fi
	return "${exit_code}"
}
trap cleanup EXIT

if ! cluster_exists; then
	"${E2E_ROOT}/test/e2e/cluster.sh" create
	created_cluster=true
else
	write_kubeconfig
	echo "Reusing existing kind cluster ${KIND_CLUSTER_NAME}; it will not be deleted automatically."
fi

"${E2E_ROOT}/test/e2e/run.sh"
