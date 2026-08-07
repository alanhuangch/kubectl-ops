# kubectl-ops

`kubectl-ops` is a read-only kubectl plugin for common Kubernetes operational diagnostics.

The first implemented command lists recently scheduled Pods using the
`PodScheduled=True` condition transition time rather than Pod creation time:

```bash
kubectl ops pod recent --node worker-07 --since 1h
kubectl ops pod recent -A -l app=payment -o json
kubectl ops pod restarts -A --since 1h
kubectl ops node requests worker-07 --top 10
kubectl ops node requests worker-07 --extended
kubectl ops node requests worker-07 --pods
kubectl ops node requests worker-07 --pods -n production
kubectl ops node requests worker-07 -A --resource nvidia.com/gpu --only-resource --pods --top 0
kubectl ops workload resources -A
kubectl ops workload resources -A --resource-class gpu
kubectl ops workload resources -A --kind deployment,statefulset -o wide
kubectl ops node drain-check worker-07 --ignore-daemonsets
kubectl ops pod pending -n production api-7d8f
kubectl ops events timeline -A --since 30m
kubectl ops rollout explain -n production deployment/api
```

## Development

Requirements: Go 1.26 or newer.

```bash
make check
./bin/kubectl-ops --help
```

`make check` downloads and verifies modules, checks module tidiness and formatting,
runs `go vet`, validates the E2E shell scripts, executes unit tests with race
detection and coverage, and builds the binary. Individual targets remain
available as `make deps`, `make verify`, `make lint`, `make test`, and
`make build`.

Install the compiled kubectl plugin into `GOBIN` (or `GOPATH/bin` when `GOBIN`
is unset):

```bash
make install
kubectl ops version
```

The install target also installs `kubectl_complete-ops`, which lets kubectl
delegate shell completion to the plugin. Use
`make install INSTALL_DIR=/custom/bin` to select another destination. For a
`kl=kubectl` Zsh alias, load and bind kubectl completion in `.zshrc`:

```zsh
source <(kubectl completion zsh)
alias kl=kubectl
compdef _kubectl kl
```

Run the kind E2E suite for `pod recent`, `pod restarts`, and `node requests`:

```bash
make e2e
```

The target creates a two-node kind cluster, builds and loads a local scratch test image, runs the assertions, and deletes clusters that it created. To keep the cluster for debugging:

```bash
make e2e E2E_KEEP_CLUSTER=true E2E_KEEP_FIXTURES=true
```

An existing cluster can be managed and tested separately:

```bash
make e2e-cluster-create
make e2e-test
make e2e-cluster-delete
```

If Docker is selected through a non-default host or context, pass `E2E_DOCKER_HOST` or `E2E_DOCKER_CONTEXT` to make.

The GitHub Actions CI runs the same lint, test, build/install, and kind E2E
targets, and publishes the coverage file and Linux binary as short-lived build
artifacts.

To test it through kubectl, place the built `kubectl-ops` binary on `PATH` and run:

```bash
kubectl ops --help
```

## Current scope

- Local single binary; no Controller or CRD.
- Read-only Kubernetes API access.
- Stable table, wide, and JSON output for `pod recent`, `pod restarts`, `node requests`, `workload resources`, `node drain-check`, `pod pending`, `events timeline`, and `rollout explain`.
- Deterministic ordering and explicit reporting for omitted Static/Mirror Pods and Pods without a usable scheduling timestamp.

`pod restarts` aggregates regular, init, and ephemeral container statuses. Its time filter uses only the latest terminated state retained in Pod status, so it cannot reconstruct complete restart history.

`node requests` reports scheduling requests rather than live utilization. It includes Init Container rules, restartable Init Containers, Pod overhead, Pod count, and scalar extended resources. Pod consumer and resource rows include the creation time and the `PodScheduled=True` condition transition time; missing scheduling timestamps remain explicitly unknown. Top consumers show CPU, memory, and active extended resources on the same row while retaining `--resource` as the ranking key. Use `--extended` to show each active assigned Pod that requests a scalar extended resource, or `--pods` to show every active assigned Pod in the same compact one-row view. The default table shows `request (ratio)` in each resource cell; wide Pod output adds limits as `request/limit (ratio)`, while JSON retains the full resource array. Combine an explicit `--resource` with `--only-resource` to keep only that resource in summaries and Pod details; `--pods --top 0` provides the complete matching Pod inventory instead of a Top-N list. By default the command includes every Namespace on the Node; an explicit `-n/--namespace` limits Pod requests and details to that Namespace. Namespace-scoped reports leave `AVAILABLE` unknown because Pods in other Namespaces were not listed. The command returns partial allocatable-only output when Pod listing is forbidden.

`workload resources` compares planned and actual scheduling requests across standard workloads: Deployment, StatefulSet, DaemonSet, active Job, CronJob, standalone ReplicaSet, and standalone Pod. Subordinate ReplicaSets and Jobs are attributed to their Deployment or CronJob, so the same Pods are not reported twice. Deployment, StatefulSet, and standalone ReplicaSet plans use desired replicas; DaemonSet plans use desired scheduled Nodes; active Job plans use current parallelism capped by remaining completions; CronJob plans describe one run; and a standalone Pod plans one Pod. Suspended Jobs and CronJobs plan zero Pods.

Actual requests come from active owned Pods that have been assigned to a Node. Unscheduled and terminal Pods do not contribute, while rolling updates include Pods from both old and new ReplicaSets. CPU, memory, GPU extended resources, nodeSelector, and required and preferred Node affinity are reported. Use `--resource-class gpu` to show only workloads with planned or actual GPU requests, or select `cpu`, `memory`, or `all`; use `--kind deployment,statefulset` to limit workload kinds. `deployment resources` remains available as a Deployment-only compatibility view. This is a scheduling-request report, not live CPU or memory utilization; permission failures leave affected inventory or actual values explicitly unknown.

`node drain-check` is a read-only preflight for `kubectl drain`. It classifies Mirror, DaemonSet, unmanaged, terminal, and emptyDir-using Pods; evaluates current PodDisruptionBudget allowances; and simulates `--ignore-daemonsets`, `--force`, and `--delete-emptydir-data`. A PDB permission failure produces partial `Unknown` output instead of claiming the Node is ready.

`pod pending` combines observed `PodScheduled`/`FailedScheduling` data with current-state checks for cordoned Nodes, NodeName, NodeSelector, required NodeAffinity, taints, resources, Pod capacity, extended resources, and HostPort conflicts. Unsupported scheduler semantics are listed explicitly.

`events timeline` presents Event Series as a stable chronological record with first/last timestamps and occurrence counts. It supports namespace, UID, object, reason, type, controller, time, and ordering filters, with a core/v1 fallback when the events.k8s.io API cannot be used.

`rollout explain` correlates a Deployment with its owned ReplicaSets, Pods, conditions, and recent Events. It identifies progress deadline failures, replica failures, unavailable replicas, Pending or unready Pods, container waiting reasons, and restarts. Missing ReplicaSet, Pod, or Event permissions produce partial `Unknown` output.

Future work will extend workload rollout, storage, networking, and deeper Node diagnostics.
