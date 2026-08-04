#!/usr/bin/env bash

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

configure_docker

fail() {
	echo "E2E FAILURE: $*" >&2
	return 1
}

assert_json() {
	local document="$1"
	local expression="$2"
	local description="$3"
	shift 3
	if ! "${JQ}" -e "$@" "${expression}" >/dev/null <<<"${document}"; then
		echo "JSON assertion failed: ${description}" >&2
		echo "${document}" | "${JQ}" . >&2 || true
		return 1
	fi
}

delete_fixtures() {
	"${KUBECTL}" delete namespace "${E2E_NAMESPACE}" --ignore-not-found --wait=true --timeout=60s >/dev/null 2>&1 || true
	"${KUBECTL}" delete clusterrolebinding kubectl-ops-e2e-node-reader --ignore-not-found >/dev/null 2>&1 || true
	"${KUBECTL}" delete clusterrole kubectl-ops-e2e-node-reader --ignore-not-found >/dev/null 2>&1 || true
}

cleanup_fixtures() {
	if [[ "${E2E_KEEP_FIXTURES}" == "true" ]]; then
		echo "Keeping E2E fixtures in namespace ${E2E_NAMESPACE}."
		return
	fi
	delete_fixtures
}

dump_diagnostics() {
	local exit_code=$?
	if [[ "${exit_code}" -ne 0 ]]; then
		echo "Dumping kind E2E diagnostics..." >&2
		"${KUBECTL}" get nodes -o wide >&2 || true
		"${KUBECTL}" get pods -A -o wide >&2 || true
		"${KUBECTL}" describe pods -n "${E2E_NAMESPACE}" >&2 || true
		"${KUBECTL}" logs -n "${E2E_NAMESPACE}" restart-pod -c restart-helper >&2 || true
		"${KUBECTL}" logs -n "${E2E_NAMESPACE}" restart-pod -c restart-helper --previous >&2 || true
	fi
	cleanup_fixtures
	return "${exit_code}"
}
trap dump_diagnostics EXIT

[[ -x "${E2E_BIN}" ]] || fail "binary not found; run make build first: ${E2E_BIN}"
cluster_exists || fail "kind cluster ${KIND_CLUSTER_NAME} does not exist; run make e2e-cluster-create"
write_kubeconfig

echo "Building local E2E helper image..."
mkdir -p "${E2E_ROOT}/.e2e"
docker_arch="$("${DOCKER}" info --format '{{.Architecture}}')"
case "${docker_arch}" in
arm64 | aarch64)
	go_arch=arm64
	;;
amd64 | x86_64)
	go_arch=amd64
	;;
*)
	fail "unsupported Docker architecture: ${docker_arch}"
	;;
esac
CGO_ENABLED=0 GOOS=linux GOARCH="${go_arch}" go build -trimpath -o "${E2E_ROOT}/.e2e/kubectl-ops-e2e-helper" ./test/e2e/helper
"${DOCKER}" build --platform "linux/${go_arch}" -t "${E2E_HELPER_IMAGE}" -f "${E2E_ROOT}/test/e2e/helper/Dockerfile" "${E2E_ROOT}/.e2e"
"${KIND}" load docker-image "${E2E_HELPER_IMAGE}" --name "${KIND_CLUSTER_NAME}"

delete_fixtures
worker_node="$("${KUBECTL}" get nodes -l '!node-role.kubernetes.io/control-plane' -o jsonpath='{.items[0].metadata.name}')"
[[ -n "${worker_node}" ]] || fail "kind worker node was not found"
"${KUBECTL}" label node "${worker_node}" kubectl-ops.dev/e2e=true --overwrite
"${KUBECTL}" apply -f "${E2E_ROOT}/test/e2e/fixtures.yaml"

echo "Waiting for E2E Pods..."
"${KUBECTL}" wait --for=condition=Ready pod/recent-pod pod/restart-pod -n "${E2E_NAMESPACE}" --timeout=180s

restart_deadline=$((SECONDS + 120))
while ((SECONDS < restart_deadline)); do
	restart_count="$("${KUBECTL}" get pod restart-pod -n "${E2E_NAMESPACE}" -o json | "${JQ}" -r '.status.containerStatuses[0].restartCount // 0')"
	if [[ "${restart_count}" -ge 1 ]]; then
		break
	fi
	sleep 1
done
[[ "${restart_count:-0}" -ge 1 ]] || fail "restart-pod did not restart"

echo "Checking pod recent..."
recent_json="$("${E2E_BIN}" pod recent -n "${E2E_NAMESPACE}" --node "${worker_node}" --since 10m --limit 20 -o json)"
assert_json "${recent_json}" '.completeness == "Complete"' "pod recent completeness"
assert_json "${recent_json}" '.items | any(.pod == "recent-pod" and .node == $node)' "recent-pod is listed" --arg node "${worker_node}"

echo "Checking pod restarts..."
restarts_json="$("${E2E_BIN}" pod restarts -n "${E2E_NAMESPACE}" --node "${worker_node}" --since 10m -o json)"
assert_json "${restarts_json}" '.items | any(.pod == "restart-pod" and .container == "restart-helper" and .restarts >= 1 and .exitCode == 137 and .classification == "SIGKILL")' "restart-pod termination is classified"

echo "Checking node requests..."
requests_json="$("${E2E_BIN}" node requests "${worker_node}" --resource cpu --top 50 -o json)"
assert_json "${requests_json}" '.completeness == "Complete" and .source == "CurrentState"' "node requests completeness"
assert_json "${requests_json}" '.consumers | any(.namespace == "kubectl-ops-e2e" and .pod == "recent-pod" and .request == "250m" and .createdAt != null and .scheduledAt != null)' "recent-pod CPU request and lifecycle times"
assert_json "${requests_json}" '.consumers | any(.namespace == "kubectl-ops-e2e" and .pod == "restart-pod" and .request == "100m")' "restart-pod CPU request"
assert_json "${requests_json}" '.resources | any(.resource == "cpu" and .requested != null and .available != null and .ratio != null)' "CPU aggregate is available"

pod_resources_json="$("${E2E_BIN}" node requests "${worker_node}" --pods --top 0 -n "${E2E_NAMESPACE}" -o json)"
assert_json "${pod_resources_json}" '.namespace == "kubectl-ops-e2e" and ([.resources[].available] | all(. == null))' "namespace-scoped node requests"
assert_json "${pod_resources_json}" '.podResources | any(.namespace == "kubectl-ops-e2e" and .pod == "recent-pod" and .createdAt != null and .scheduledAt != null and (.resources | any(.resource == "cpu" and .request == "250m")) and (.resources | any(.resource == "memory" and .request == "64Mi")))' "recent-pod resource breakdown and lifecycle times"

filtered_resources_json="$("${E2E_BIN}" node requests "${worker_node}" -A --resource cpu --only-resource --pods --top 0 -o json)"
assert_json "${filtered_resources_json}" '(.resources | length == 1 and .[0].resource == "cpu") and (.podResources | all(.resources | length == 1 and .[0].resource == "cpu"))' "single resource filter"

echo "Checking node requests permission degradation..."
partial_json="$("${E2E_BIN}" node requests "${worker_node}" --pods -n "${E2E_NAMESPACE}" --as "system:serviceaccount:${E2E_NAMESPACE}:node-reader" -o json)"
assert_json "${partial_json}" '.completeness == "Partial"' "forbidden Pod list produces Partial output"
assert_json "${partial_json}" '[.resources[].requested] | all(. == null)' "partial resource requests are unknown"
assert_json "${partial_json}" '.podResources == null' "partial Pod resource breakdown is unknown"
assert_json "${partial_json}" '.warnings | length > 0' "partial output includes a warning"

echo "kind E2E tests passed."
