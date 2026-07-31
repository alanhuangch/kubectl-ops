#!/usr/bin/env bash

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

configure_docker

create_cluster() {
	mkdir -p "$(dirname "${E2E_KUBECONFIG}")"
	if cluster_exists; then
		echo "kind cluster ${KIND_CLUSTER_NAME} already exists; reusing it."
		write_kubeconfig
		return
	fi

	local -a create_args
	create_args=(
		create cluster
		--name "${KIND_CLUSTER_NAME}"
		--config "${KIND_CONFIG}"
		--kubeconfig "${E2E_KUBECONFIG}"
		--wait 180s
	)
	if [[ -n "${KIND_NODE_IMAGE}" ]]; then
		create_args+=(--image "${KIND_NODE_IMAGE}")
	fi
	"${KIND}" "${create_args[@]}"
}

delete_cluster() {
	if cluster_exists; then
		"${KIND}" delete cluster --name "${KIND_CLUSTER_NAME}"
	else
		echo "kind cluster ${KIND_CLUSTER_NAME} does not exist."
	fi
	rm -f "${E2E_KUBECONFIG}"
}

case "${1:-}" in
create)
	create_cluster
	;;
delete)
	delete_cluster
	;;
*)
	echo "usage: $0 create|delete" >&2
	exit 2
	;;
esac
