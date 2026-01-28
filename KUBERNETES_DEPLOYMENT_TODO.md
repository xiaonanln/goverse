# Kubernetes Deployment TODO

Tasks for making Goverse fully production-ready on Kubernetes, sorted by priority.

**Current state**: Extensive k8s manifests already exist in `k8s/` (namespace, configmap, secrets, statefulsets for etcd/postgres/nodes, deployments for gates/inspector, services, ingress). Gate and Inspector have production Dockerfiles. Graceful shutdown (SIGTERM) is implemented in all components. Prometheus metrics are fully implemented. Service discovery uses Kubernetes-native DNS with etcd coordination.

---

## P0 — Blocking Issues

These must be fixed before any K8s deployment can work correctly.

### Add health/readiness endpoints to node server

**Problem**: K8s node manifests (`k8s/nodes/statefulset.yaml`) reference `/healthz` and `/ready` on port 8080, but the node server (`server/server.go`) only exposes `/metrics` on its HTTP listener. Liveness and readiness probes will fail.

- [x] Add `/healthz` endpoint to node's metrics HTTP server (simple alive check, return 200)
- [x] Add `/ready` endpoint to node's metrics HTTP server (check cluster connection, shard ownership, not shutting down)
- [x] Add `/ready` endpoint to inspector (`cmd/inspector/inspectserver/`) — currently only has `/healthz`

Gate already has both `/healthz` and `/ready` on its ops port (`gate/gateserver/http_handler.go`).

### Create production Node Dockerfile

**Problem**: `k8s/nodes/statefulset.yaml` references `goverse/node:latest` but no `docker/Dockerfile.node` exists. Only `Dockerfile.gate` and `Dockerfile.inspector` exist as production images.

- [x] Create `docker/Dockerfile.node` following the same pattern as `Dockerfile.gate` (multi-stage build, Go 1.25 → Alpine, non-root user goverse:1000, strip symbols)
- [x] The node binary entry point is the user's application (not a `cmd/node/` — there is no `cmd/node`). Document how users should build their node image using goverse as a library, or provide a base image pattern.

### Create .dockerignore

**Problem**: No `.dockerignore` exists. Docker builds send the entire repo as context including `.git`, `docs`, `samples`, `tests`, etc.

- [x] Create `.dockerignore` excluding `.git`, `.github`, `docs`, `examples`, `tests`, `*.md`, `script`, `k8s` (Note: `samples` kept for Dockerfile.node default build)

---

## P1 — High Priority

Core production readiness items.

### Add SecurityContext to k8s manifests

Dockerfiles already create a non-root user (goverse:1000), but the k8s manifests don't enforce security constraints.

- [ ] Add `securityContext` to all pod specs in `k8s/`:
  - `runAsNonRoot: true`, `runAsUser: 1000`, `runAsGroup: 1000`
  - `readOnlyRootFilesystem: true`
  - `allowPrivilegeEscalation: false`
  - `capabilities.drop: ["ALL"]`
- [ ] Add `seccompProfile: { type: RuntimeDefault }` to pod-level security context

### Create Prometheus ServiceMonitor manifests

Metrics endpoints exist and pods have `prometheus.io/scrape` annotations, but there are no ServiceMonitor CRDs for Prometheus Operator.

- [ ] Create ServiceMonitor for nodes (`/metrics` on metrics port)
- [ ] Create ServiceMonitor for gates (`/metrics` on ops port)
- [ ] Create PrometheusRule with basic alerts (node down, high error rate, shard migration stuck)

### Support environment variable overrides for configuration

Config currently loads from YAML file or CLI flags. No direct env var override mechanism exists. K8s manifests work around this by passing env vars as flag values in the container command.

- [ ] Add env var override support for key config values (e.g., `GOVERSE_ETCD_ENDPOINTS`, `GOVERSE_POSTGRES_PASSWORD`, `GOVERSE_NODE_ID`)
- [ ] Support `POD_IP` env var as automatic advertise address fallback when no explicit advertise address is set
- [ ] Document the env var → config mapping

### Add PodDisruptionBudgets

Not present in current manifests.

- [ ] Create PDB for nodes (`minAvailable: 2` or percentage-based)
- [ ] Create PDB for gates
- [ ] Create PDB for etcd

---

## P2 — Medium Priority

Important for production hardening but not blocking initial deployment.

### Add JSON logging format

Logger (`util/logger/logger.go`) only outputs text format (`[timestamp] [level] [prefix] message`). Production log aggregation (Fluentd, Loki, CloudWatch) works better with structured JSON.

- [ ] Add `LOG_FORMAT=json` environment variable support to `util/logger`
- [ ] Add `LOG_LEVEL` environment variable support (`debug|info|warn|error`)

### Add TLS/mTLS support for gRPC

All gRPC connections (node-to-node, gate-to-node, etcd client) are unencrypted. No TLS code exists anywhere in the codebase.

- [ ] Add TLS configuration to gRPC server setup (`server/server.go`)
- [ ] Add TLS for node-to-node connections (`cluster/nodeconnections/`)
- [ ] Add TLS for gate-to-node connections
- [ ] Add TLS config section to YAML config (cert_file, key_file, ca_file)
- [ ] Support certificate mounting from Kubernetes Secrets

### Create Helm chart

Current deployment is raw YAML only. Helm simplifies parameterization, upgrades, and GitOps workflows.

- [ ] Create chart at `deploy/helm/goverse/` with `Chart.yaml`, `values.yaml`, `templates/`
- [ ] Parameterize: replica counts, resource limits, image tags, etcd/postgres addresses, optional components (inspector, persistence)
- [ ] Support external etcd and PostgreSQL (not deployed by the chart)

### Add NetworkPolicy manifests

No network policies exist in current manifests.

- [ ] Default deny-all policy for goverse namespace
- [ ] Allow node-to-node gRPC
- [ ] Allow gate-to-node gRPC
- [ ] Allow node/gate-to-etcd
- [ ] Allow node-to-postgres

### Add GOMAXPROCS and GOMEMLIMIT support

Go uses all available CPUs by default, which is wrong when K8s CPU limits are set. Memory limits also need alignment.

- [ ] Add `github.com/uber-go/automaxprocs` for automatic CPU limit detection
- [ ] Set `GOMEMLIMIT` in Dockerfiles or document how to set it in k8s manifests relative to memory limits

### Build automation for production images

- [ ] Create GitHub Actions workflow for building and pushing production images on tags
- [ ] Multi-architecture support (amd64, arm64)
- [ ] Image vulnerability scanning (Trivy) in CI

---

## P3 — Low Priority / Future Work

### Kustomize overlays

- [ ] Create `deploy/kubernetes/base/` from existing `k8s/` manifests
- [ ] Create overlays for dev (single replica, minimal resources), staging, and production

### Grafana dashboards

- [ ] Cluster overview dashboard (nodes, gates, shard distribution)
- [ ] Method call latency and error rate dashboard
- [ ] Export as JSON in `deploy/kubernetes/grafana-dashboards/`

### OpenTelemetry / distributed tracing

- [ ] Instrument gRPC calls with OpenTelemetry
- [ ] Propagate trace context across node-to-node and gate-to-node calls
- [ ] Export to Jaeger/Tempo

### Backup automation

- [ ] CronJob for etcd snapshots
- [ ] CronJob for PostgreSQL pg_dump
- [ ] Document restore procedures

### HorizontalPodAutoscaler

- [ ] HPA for nodes (based on CPU/memory or custom metrics like object count)
- [ ] HPA for gates (based on connection count)

Note: Node HPA requires care since scaling triggers shard rebalancing.

### Kubernetes integration tests

- [ ] CI job using kind (Kubernetes in Docker)
- [ ] Test cluster bootstrap, node scaling, pod restart recovery
- [ ] Test gate scaling independently of nodes

### Chaos engineering

- [ ] Pod failure injection tests (validate shard failover)
- [ ] Network partition tests (validate split-brain handling)
- [ ] Document expected behavior and recovery times

### Goverse Operator (long-term)

- [ ] CRD for GoverseCluster custom resource
- [ ] Automated shard rebalancing on scale events
- [ ] Intelligent scaling based on object distribution

---

## Already Done (removed from TODO)

These items from the original TODO are already implemented:

- **Graceful shutdown / SIGTERM handling** — All components (server, gate, inspector) handle SIGINT/SIGTERM with `GracefulStop()` and 5-second timeouts
- **Core Kubernetes manifests** — Full set exists in `k8s/` (namespace, configmap, secrets, statefulsets, deployments, services, ingress)
- **Gate and Inspector production Dockerfiles** — Multi-stage builds with Alpine, non-root user
- **Prometheus metrics** — 16+ metrics covering objects, method calls, shards, gates, clients
- **Metrics endpoints** — `/metrics` exposed on node (metrics port) and gate (ops port)
- **Service discovery** — Kubernetes DNS-based with headless services for StatefulSets
- **Pod identity** — K8s manifests inject `POD_NAME`, `POD_NAMESPACE` via downward API
- **Gate health/readiness endpoints** — `/healthz` and `/ready` on ops port
- **Inspector health endpoint** — `/healthz` on HTTP port
- **Connection draining** — Via gRPC `GracefulStop()` in all components
- **ConfigMap-based configuration** — Config YAML mounted from ConfigMap
- **Secret injection** — PostgreSQL password from Kubernetes Secret
- **Resource requests/limits** — Set in all k8s manifests

---

## References

- Existing k8s manifests: `k8s/`
- Production Dockerfiles: `docker/Dockerfile.gate`, `docker/Dockerfile.node`, `docker/Dockerfile.inspector`
- Dev Dockerfile: `docker/Dockerfile.dev`
- Prometheus metrics: `util/metrics/metrics.go`
- Server shutdown: `server/server.go`
- Gate ops endpoints: `gate/gateserver/http_handler.go`
- K8s deployment docs: `docs/KUBERNETES_DEPLOYMENT.md`
- Prometheus docs: `docs/PROMETHEUS_INTEGRATION.md`
