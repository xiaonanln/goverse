# Kubernetes Deployment TODO

This document outlines all gaps and requirements for making Goverse fully compatible with Kubernetes deployment. Items are prioritized as **High**, **Medium**, or **Low** based on their criticality for production deployment.

---

## Table of Contents

1. [Container Images & Build Pipeline](#1-container-images--build-pipeline)
2. [Application Code Changes](#2-application-code-changes)
3. [Kubernetes Resources](#3-kubernetes-resources)
4. [Observability & Monitoring](#4-observability--monitoring)
5. [Security & Compliance](#5-security--compliance)
6. [Operations & Maintenance](#6-operations--maintenance)
7. [Testing & Validation](#7-testing--validation)

---

## 1. Container Images & Build Pipeline

### 1.1 Production Dockerfiles (Priority: **High**)

**Current State:**
- `docker/Dockerfile.dev` exists for development with all dependencies
- No production-optimized Dockerfile
- Dev image includes build tools, etcd, PostgreSQL (not suitable for production)

**Required:**

- [ ] **Create `docker/Dockerfile.node`** - Production image for Goverse nodes
  ```dockerfile
  # Multi-stage build example
  FROM golang:1.25-bookworm AS builder
  WORKDIR /app
  COPY go.mod go.sum ./
  RUN go mod download
  COPY . .
  RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo \
      -ldflags="-w -s" -o /goverse-node ./cmd/node/
  
  FROM gcr.io/distroless/static-debian12:nonroot
  COPY --from=builder /goverse-node /goverse-node
  USER nonroot:nonroot
  EXPOSE 50051 8080
  ENTRYPOINT ["/goverse-node"]
  ```

- [ ] **Create `docker/Dockerfile.gate`** - Production image for gates
  ```dockerfile
  # Similar multi-stage build for gate binary
  FROM golang:1.25-bookworm AS builder
  WORKDIR /app
  COPY go.mod go.sum ./
  RUN go mod download
  COPY . .
  RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo \
      -ldflags="-w -s" -o /goverse-gate ./cmd/gate/
  
  FROM gcr.io/distroless/static-debian12:nonroot
  COPY --from=builder /goverse-gate /goverse-gate
  USER nonroot:nonroot
  EXPOSE 60051 8080
  ENTRYPOINT ["/goverse-gate"]
  ```

- [ ] **Create `docker/Dockerfile.inspector`** - Production image for inspector UI
  ```dockerfile
  FROM golang:1.25-bookworm AS builder
  WORKDIR /app
  COPY go.mod go.sum ./
  RUN go mod download
  COPY . .
  RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo \
      -ldflags="-w -s" -o /goverse-inspector ./cmd/inspector/
  
  FROM gcr.io/distroless/static-debian12:nonroot
  COPY --from=builder /goverse-inspector /goverse-inspector
  COPY --from=builder /app/cmd/inspector/web /web
  USER nonroot:nonroot
  EXPOSE 8080 8081
  ENTRYPOINT ["/goverse-inspector"]
  ```

- [ ] **Create `docker/Dockerfile.pgadmin`** - Utility image for database migrations
  ```dockerfile
  FROM golang:1.25-bookworm
  WORKDIR /app
  COPY go.mod go.sum ./
  RUN go mod download
  COPY . .
  RUN go build -o /pgadmin ./cmd/pgadmin/
  ENTRYPOINT ["/pgadmin"]
  ```

### 1.2 Image Optimization (Priority: **High**)

- [ ] **Use distroless or alpine base images** for minimal attack surface
  - Current: dev image uses full Debian
  - Target: < 50MB per production image (vs ~1GB+ dev image)

- [ ] **Implement multi-stage builds** to exclude build dependencies
  - Separate build stage with Go toolchain
  - Runtime stage with only compiled binary

- [ ] **Add `.dockerignore`** to exclude unnecessary files
  ```
  # .dockerignore
  .git
  .github
  docs
  examples
  samples
  tests
  *.md
  .gitignore
  docker/Dockerfile.dev
  script
  ```

### 1.3 Build Automation (Priority: **Medium**)

- [ ] **Create GitHub Actions workflow** for automated image builds
  - File: `.github/workflows/docker-production.yml`
  - Trigger on tags (e.g., `v*`) for releases
  - Build and push to container registry (Docker Hub, GCR, ECR)
  - Multi-architecture support (amd64, arm64)

- [ ] **Add image versioning strategy**
  - Semantic versioning tags (e.g., `v1.2.3`)
  - `latest` tag for latest stable
  - `main` tag for main branch builds
  - Git SHA tags for traceability

- [ ] **Implement image scanning** in CI pipeline
  - Use Trivy, Snyk, or similar
  - Fail builds on critical vulnerabilities
  - Reference: `.github/workflows/docker.yml` for existing patterns

### 1.4 Build Scripts (Priority: **Low**)

- [ ] **Create `script/build-images.sh`** - Local image build script
  ```bash
  #!/bin/bash
  # Build all production images locally
  VERSION=${VERSION:-dev}
  docker build -f docker/Dockerfile.node -t goverse-node:${VERSION} .
  docker build -f docker/Dockerfile.gate -t goverse-gate:${VERSION} .
  docker build -f docker/Dockerfile.inspector -t goverse-inspector:${VERSION} .
  ```

- [ ] **Create `script/push-images.sh`** - Image push script with registry support

---

## 2. Application Code Changes

### 2.1 Health and Readiness Probes (Priority: **High**)

**Current State:**
- No built-in health check endpoints
- Kubernetes needs `/health` and `/ready` endpoints

**Required:**

- [ ] **Add health check endpoints to node server** (`server/server.go`)
  ```go
  // GET /health - Returns 200 if service is running
  // GET /ready - Returns 200 if node is ready (cluster connected, shards claimed)
  
  func (s *Server) registerHealthHandlers() {
      http.HandleFunc("/health", s.healthHandler)
      http.HandleFunc("/ready", s.readyHandler)
  }
  
  func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
      // Simple alive check
      w.WriteHeader(http.StatusOK)
      w.Write([]byte("ok"))
  }
  
  func (s *Server) readyHandler(w http.ResponseWriter, r *http.Request) {
      // Check cluster connection, shard ownership, etc.
      if !s.node.IsReady() {
          w.WriteHeader(http.StatusServiceUnavailable)
          w.Write([]byte("not ready"))
          return
      }
      w.WriteHeader(http.StatusOK)
      w.Write([]byte("ready"))
  }
  ```

- [ ] **Add `IsReady()` method to Node** (`node/node.go`)
  - Check cluster connection status
  - Verify shard claims are complete
  - Return false during graceful shutdown

- [ ] **Add health/ready endpoints to gate** (`gate/gateserver/gateserver.go`)
  - `/health` - Service alive
  - `/ready` - Connected to cluster nodes

- [ ] **Add health/ready endpoints to inspector** (`cmd/inspector/inspectserver/inspectserver.go`)

### 2.2 Graceful Shutdown (Priority: **High**)

**Current State:**
- Context-based shutdown exists in `server/server.go`
- Need to ensure proper SIGTERM handling for Kubernetes

**Required:**

- [ ] **Verify SIGTERM handling** in all server binaries
  - Node server should stop accepting requests
  - Drain in-flight requests (configurable timeout)
  - Release shards before exiting
  - Default grace period: 30 seconds (Kubernetes default)

- [ ] **Add `PreStopHook` support** for coordinated shutdown
  ```go
  // Allow time for load balancer to remove pod from service
  // Sleep 5-10 seconds before starting shutdown
  time.Sleep(10 * time.Second)
  ```

- [ ] **Implement connection draining** in gate
  - Stop accepting new client connections
  - Wait for existing streams to complete
  - Configurable drain timeout

### 2.3 Configuration Management (Priority: **High**)

**Current State:**
- YAML configuration via `config/config.go`
- Hardcoded defaults for development

**Required:**

- [ ] **Support environment variable overrides** for all config values
  ```go
  // Example: GOVERSE_ETCD_ENDPOINTS overrides config.cluster.etcd.endpoints
  // Example: GOVERSE_NODE_ID overrides node ID
  // Example: GOVERSE_POSTGRES_PASSWORD overrides postgres.password
  ```

- [ ] **Add ConfigMap-friendly configuration**
  - Allow mounting YAML config from ConfigMap
  - Support partial config overrides via env vars
  - Validate merged configuration at startup

- [ ] **Create configuration template** for Kubernetes
  - File: `deploy/kubernetes/configmap-template.yaml`
  - Documented placeholders for cluster-specific values

- [ ] **Support secret injection** for sensitive data
  - PostgreSQL passwords from Kubernetes Secrets
  - etcd credentials from Secrets
  - TLS certificates from Secrets

### 2.4 Service Discovery (Priority: **High**)

**Current State:**
- Static advertise addresses in config
- Not compatible with dynamic pod IPs in Kubernetes

**Required:**

- [ ] **Implement headless service discovery**
  - Use Pod's own IP from `status.podIP` env var
  - Support Kubernetes DNS: `<pod-name>.<service-name>.<namespace>.svc.cluster.local`
  - Auto-detect and use pod IP as advertise address

- [ ] **Add pod identity support**
  - Use `POD_NAME` env var for node ID (injected via downward API)
  - Use `POD_NAMESPACE` for namespace-aware operations
  - Use `POD_IP` for advertise address

  Example code change in `server/server.go`:
  ```go
  // Auto-detect advertise address from Kubernetes environment
  if advertiseAddr == "" {
      if podIP := os.Getenv("POD_IP"); podIP != "" {
          advertiseAddr = podIP + ":" + listenPort
      }
  }
  
  // Auto-detect node ID from pod name
  if nodeID == "" {
      if podName := os.Getenv("POD_NAME"); podName != "" {
          nodeID = podName
      }
  }
  ```

### 2.5 Metrics Enhancement (Priority: **Medium**)

**Current State:**
- Prometheus metrics exist (`util/metrics/`)
- Exposed on configurable HTTP port

**Required:**

- [ ] **Add Kubernetes-specific metrics**
  - `goverse_pod_info{pod_name, namespace, node_name}` - Pod identity
  - `goverse_k8s_ready` - Readiness probe status
  - `goverse_k8s_startup_time_seconds` - Time to ready state

- [ ] **Add resource usage metrics**
  - Memory usage
  - Goroutine count
  - Connection pool sizes
  - Object count per shard (already exists)

### 2.6 Logging Enhancements (Priority: **Medium**)

**Current State:**
- Structured logging via `util/logger/`
- Logs to stdout

**Required:**

- [ ] **Add JSON logging format option** for log aggregation
  ```go
  // Support LOG_FORMAT=json environment variable
  // Output structured JSON logs for Fluentd/Logstash ingestion
  ```

- [ ] **Add correlation IDs** for distributed tracing
  - Request ID propagation across service calls
  - Log request context (pod, node, trace ID)

- [ ] **Add log level control via environment**
  - `LOG_LEVEL=debug|info|warn|error`
  - Allow runtime log level changes via HTTP endpoint

### 2.7 TLS/mTLS Support (Priority: **Medium**)

**Current State:**
- gRPC connections use insecure transport
- Not production-ready

**Required:**

- [ ] **Add TLS configuration options** for gRPC servers
  - Node-to-node TLS
  - Gate-to-node TLS
  - Client-to-gate TLS (optional)

- [ ] **Support certificate mounting** from Kubernetes Secrets
  ```go
  tlsConfig := &tls.Config{
      Certificates: []tls.Certificate{cert},
      ClientAuth:   tls.RequireAndVerifyClientCert,
      ClientCAs:    caCertPool,
  }
  ```

- [ ] **Add TLS configuration to YAML config**
  ```yaml
  nodes:
    - id: "node-a"
      grpc_addr: "0.0.0.0:50051"
      tls:
        enabled: true
        cert_file: "/etc/goverse/tls/tls.crt"
        key_file: "/etc/goverse/tls/tls.key"
        ca_file: "/etc/goverse/tls/ca.crt"
  ```

### 2.8 Resource Limits Awareness (Priority: **Low**)

- [ ] **Implement GOMEMLIMIT support** for memory limit awareness
  ```go
  // Respect Kubernetes memory limits
  // Set GOMEMLIMIT to container memory limit - 100MB
  ```

- [ ] **Add automatic GOMAXPROCS tuning** based on CPU limits
  - Use `uber.go/automaxprocs` library
  - Respect Kubernetes CPU limits

---

## 3. Kubernetes Resources

### 3.1 Core Manifests (Priority: **High**)

**Required Files in `deploy/kubernetes/`:**

- [ ] **`namespace.yaml`** - Dedicated namespace
  ```yaml
  apiVersion: v1
  kind: Namespace
  metadata:
    name: goverse
    labels:
      name: goverse
  ```

- [ ] **`configmap.yaml`** - Application configuration
  ```yaml
  apiVersion: v1
  kind: ConfigMap
  metadata:
    name: goverse-config
    namespace: goverse
  data:
    config.yaml: |
      version: 1
      cluster:
        shards: 8192
        provider: "etcd"
        etcd:
          endpoints:
            - "goverse-etcd:2379"
          prefix: "/goverse"
      # ... rest of config
  ```

- [ ] **`secret.yaml`** - Sensitive configuration (template)
  ```yaml
  apiVersion: v1
  kind: Secret
  metadata:
    name: goverse-secrets
    namespace: goverse
  type: Opaque
  stringData:
    postgres-password: "CHANGEME"
    postgres-user: "goverse"
    etcd-root-password: "CHANGEME"
  ```

- [ ] **`statefulset-etcd.yaml`** - etcd cluster
  ```yaml
  apiVersion: apps/v1
  kind: StatefulSet
  metadata:
    name: goverse-etcd
    namespace: goverse
  spec:
    serviceName: goverse-etcd
    replicas: 3
    selector:
      matchLabels:
        app: goverse-etcd
    template:
      metadata:
        labels:
          app: goverse-etcd
      spec:
        containers:
        - name: etcd
          image: quay.io/coreos/etcd:v3.5.10
          ports:
          - containerPort: 2379
            name: client
          - containerPort: 2380
            name: peer
          env:
          - name: ETCD_NAME
            valueFrom:
              fieldRef:
                fieldPath: metadata.name
          # ... etcd configuration
    volumeClaimTemplates:
    - metadata:
        name: etcd-data
      spec:
        accessModes: [ "ReadWriteOnce" ]
        resources:
          requests:
            storage: 10Gi
  ```

- [ ] **`service-etcd.yaml`** - etcd headless service
  ```yaml
  apiVersion: v1
  kind: Service
  metadata:
    name: goverse-etcd
    namespace: goverse
  spec:
    clusterIP: None
    selector:
      app: goverse-etcd
    ports:
    - port: 2379
      name: client
    - port: 2380
      name: peer
  ```

- [ ] **`statefulset-postgres.yaml`** - PostgreSQL database
  ```yaml
  apiVersion: apps/v1
  kind: StatefulSet
  metadata:
    name: goverse-postgres
    namespace: goverse
  spec:
    serviceName: goverse-postgres
    replicas: 1
    selector:
      matchLabels:
        app: goverse-postgres
    template:
      metadata:
        labels:
          app: goverse-postgres
      spec:
        containers:
        - name: postgres
          image: postgres:15-alpine
          ports:
          - containerPort: 5432
          env:
          - name: POSTGRES_DB
            value: "goverse"
          - name: POSTGRES_USER
            valueFrom:
              secretKeyRef:
                name: goverse-secrets
                key: postgres-user
          - name: POSTGRES_PASSWORD
            valueFrom:
              secretKeyRef:
                name: goverse-secrets
                key: postgres-password
    volumeClaimTemplates:
    - metadata:
        name: postgres-data
      spec:
        accessModes: [ "ReadWriteOnce" ]
        resources:
          requests:
            storage: 20Gi
  ```

- [ ] **`service-postgres.yaml`** - PostgreSQL service
  ```yaml
  apiVersion: v1
  kind: Service
  metadata:
    name: goverse-postgres
    namespace: goverse
  spec:
    selector:
      app: goverse-postgres
    ports:
    - port: 5432
      targetPort: 5432
  ```

- [ ] **`statefulset-node.yaml`** - Goverse node servers
  ```yaml
  apiVersion: apps/v1
  kind: StatefulSet
  metadata:
    name: goverse-node
    namespace: goverse
  spec:
    serviceName: goverse-node
    replicas: 3
    selector:
      matchLabels:
        app: goverse-node
    template:
      metadata:
        labels:
          app: goverse-node
      spec:
        containers:
        - name: node
          image: goverse-node:latest
          ports:
          - containerPort: 50051
            name: grpc
          - containerPort: 8080
            name: http
          env:
          - name: POD_NAME
            valueFrom:
              fieldRef:
                fieldPath: metadata.name
          - name: POD_NAMESPACE
            valueFrom:
              fieldRef:
                fieldPath: metadata.namespace
          - name: POD_IP
            valueFrom:
              fieldRef:
                fieldPath: status.podIP
          - name: GOVERSE_NODE_ID
            valueFrom:
              fieldRef:
                fieldPath: metadata.name
          - name: GOVERSE_POSTGRES_PASSWORD
            valueFrom:
              secretKeyRef:
                name: goverse-secrets
                key: postgres-password
          volumeMounts:
          - name: config
            mountPath: /etc/goverse
          livenessProbe:
            httpGet:
              path: /health
              port: 8080
            initialDelaySeconds: 10
            periodSeconds: 10
          readinessProbe:
            httpGet:
              path: /ready
              port: 8080
            initialDelaySeconds: 5
            periodSeconds: 5
          resources:
            requests:
              cpu: "500m"
              memory: "512Mi"
            limits:
              cpu: "2000m"
              memory: "2Gi"
        volumes:
        - name: config
          configMap:
            name: goverse-config
  ```

- [ ] **`service-node.yaml`** - Headless service for nodes
  ```yaml
  apiVersion: v1
  kind: Service
  metadata:
    name: goverse-node
    namespace: goverse
  spec:
    clusterIP: None
    selector:
      app: goverse-node
    ports:
    - port: 50051
      name: grpc
    - port: 8080
      name: http
  ```

- [ ] **`deployment-gate.yaml`** - Gate deployment
  ```yaml
  apiVersion: apps/v1
  kind: Deployment
  metadata:
    name: goverse-gate
    namespace: goverse
  spec:
    replicas: 2
    selector:
      matchLabels:
        app: goverse-gate
    template:
      metadata:
        labels:
          app: goverse-gate
      spec:
        containers:
        - name: gate
          image: goverse-gate:latest
          ports:
          - containerPort: 60051
            name: grpc
          - containerPort: 8080
            name: http
          env:
          - name: POD_NAME
            valueFrom:
              fieldRef:
                fieldPath: metadata.name
          - name: POD_IP
            valueFrom:
              fieldRef:
                fieldPath: status.podIP
          volumeMounts:
          - name: config
            mountPath: /etc/goverse
          livenessProbe:
            httpGet:
              path: /health
              port: 8080
          readinessProbe:
            httpGet:
              path: /ready
              port: 8080
          resources:
            requests:
              cpu: "250m"
              memory: "256Mi"
            limits:
              cpu: "1000m"
              memory: "1Gi"
        volumes:
        - name: config
          configMap:
            name: goverse-config
  ```

- [ ] **`service-gate.yaml`** - Gate service (LoadBalancer or ClusterIP)
  ```yaml
  apiVersion: v1
  kind: Service
  metadata:
    name: goverse-gate
    namespace: goverse
  spec:
    type: LoadBalancer  # or ClusterIP for Ingress
    selector:
      app: goverse-gate
    ports:
    - port: 60051
      targetPort: 60051
      name: grpc
    - port: 8080
      targetPort: 8080
      name: http
  ```

- [ ] **`deployment-inspector.yaml`** - Inspector UI deployment
  ```yaml
  apiVersion: apps/v1
  kind: Deployment
  metadata:
    name: goverse-inspector
    namespace: goverse
  spec:
    replicas: 1
    selector:
      matchLabels:
        app: goverse-inspector
    template:
      metadata:
        labels:
          app: goverse-inspector
      spec:
        containers:
        - name: inspector
          image: goverse-inspector:latest
          ports:
          - containerPort: 8080
            name: http
          - containerPort: 8081
            name: grpc
          volumeMounts:
          - name: config
            mountPath: /etc/goverse
          resources:
            requests:
              cpu: "100m"
              memory: "128Mi"
            limits:
              cpu: "500m"
              memory: "512Mi"
        volumes:
        - name: config
          configMap:
            name: goverse-config
  ```

- [ ] **`service-inspector.yaml`** - Inspector service
  ```yaml
  apiVersion: v1
  kind: Service
  metadata:
    name: goverse-inspector
    namespace: goverse
  spec:
    selector:
      app: goverse-inspector
    ports:
    - port: 8080
      targetPort: 8080
      name: http
    - port: 8081
      targetPort: 8081
      name: grpc
  ```

### 3.2 Advanced Resources (Priority: **Medium**)

- [ ] **`ingress.yaml`** - Ingress for HTTP access
  ```yaml
  apiVersion: networking.k8s.io/v1
  kind: Ingress
  metadata:
    name: goverse-ingress
    namespace: goverse
    annotations:
      cert-manager.io/cluster-issuer: "letsencrypt-prod"
  spec:
    ingressClassName: nginx
    tls:
    - hosts:
      - goverse.example.com
      - inspector.goverse.example.com
      secretName: goverse-tls
    rules:
    - host: goverse.example.com
      http:
        paths:
        - path: /
          pathType: Prefix
          backend:
            service:
              name: goverse-gate
              port:
                number: 8080
    - host: inspector.goverse.example.com
      http:
        paths:
        - path: /
          pathType: Prefix
          backend:
            service:
              name: goverse-inspector
              port:
                number: 8080
  ```

- [ ] **`hpa-node.yaml`** - Horizontal Pod Autoscaler for nodes
  ```yaml
  apiVersion: autoscaling/v2
  kind: HorizontalPodAutoscaler
  metadata:
    name: goverse-node
    namespace: goverse
  spec:
    scaleTargetRef:
      apiVersion: apps/v1
      kind: StatefulSet
      name: goverse-node
    minReplicas: 3
    maxReplicas: 10
    metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 70
    - type: Resource
      resource:
        name: memory
        target:
          type: Utilization
          averageUtilization: 80
  ```

- [ ] **`hpa-gate.yaml`** - HPA for gates

- [ ] **`pdb-node.yaml`** - PodDisruptionBudget for nodes
  ```yaml
  apiVersion: policy/v1
  kind: PodDisruptionBudget
  metadata:
    name: goverse-node
    namespace: goverse
  spec:
    minAvailable: 2
    selector:
      matchLabels:
        app: goverse-node
  ```

- [ ] **`pdb-gate.yaml`** - PDB for gates

- [ ] **`networkpolicy.yaml`** - Network policies for security
  ```yaml
  apiVersion: networking.k8s.io/v1
  kind: NetworkPolicy
  metadata:
    name: goverse-node
    namespace: goverse
  spec:
    podSelector:
      matchLabels:
        app: goverse-node
    policyTypes:
    - Ingress
    - Egress
    ingress:
    - from:
      - podSelector:
          matchLabels:
            app: goverse-node
      - podSelector:
          matchLabels:
            app: goverse-gate
      ports:
      - protocol: TCP
        port: 50051
    egress:
    - to:
      - podSelector:
          matchLabels:
            app: goverse-etcd
      ports:
      - protocol: TCP
        port: 2379
    - to:
      - podSelector:
          matchLabels:
            app: goverse-postgres
      ports:
      - protocol: TCP
        port: 5432
  ```

### 3.3 Helm Chart (Priority: **Medium**)

- [ ] **Create Helm chart** at `deploy/helm/goverse/`
  - `Chart.yaml` - Chart metadata
  - `values.yaml` - Default values
  - `templates/` - Templated Kubernetes manifests
  - `README.md` - Chart usage documentation

- [ ] **Helm chart features**
  - Parameterized replica counts
  - Configurable resource limits
  - Optional components (inspector, persistence)
  - Support for external etcd/PostgreSQL
  - TLS configuration

### 3.4 Kustomize Overlays (Priority: **Low**)

- [ ] **Create Kustomize structure**
  - `deploy/kubernetes/base/` - Base manifests
  - `deploy/kubernetes/overlays/dev/` - Development overlay
  - `deploy/kubernetes/overlays/staging/` - Staging overlay
  - `deploy/kubernetes/overlays/production/` - Production overlay

- [ ] **Environment-specific customizations**
  - Dev: Single replicas, minimal resources
  - Staging: Production-like setup, limited scale
  - Production: HA setup, full resources

### 3.5 Operators (Priority: **Low**)

- [ ] **Consider Goverse Operator** for advanced lifecycle management
  - Custom Resource Definition (CRD) for GoverseCluster
  - Automated shard rebalancing
  - Intelligent scaling based on object distribution
  - Self-healing capabilities

---

## 4. Observability & Monitoring

### 4.1 Prometheus Integration (Priority: **High**)

**Current State:**
- Prometheus metrics exist in `util/metrics/`
- Metrics exposed on HTTP port

**Required:**

- [ ] **Create ServiceMonitor** for Prometheus Operator
  ```yaml
  apiVersion: monitoring.coreos.com/v1
  kind: ServiceMonitor
  metadata:
    name: goverse-node
    namespace: goverse
  spec:
    selector:
      matchLabels:
        app: goverse-node
    endpoints:
    - port: http
      path: /metrics
      interval: 30s
  ```

- [ ] **Create ServiceMonitor for gates and inspector**

- [ ] **Add PrometheusRule** for alerting
  ```yaml
  apiVersion: monitoring.coreos.com/v1
  kind: PrometheusRule
  metadata:
    name: goverse-alerts
    namespace: goverse
  spec:
    groups:
    - name: goverse
      interval: 30s
      rules:
      - alert: GoverseNodeDown
        expr: up{job="goverse-node"} == 0
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "Goverse node {{ $labels.pod }} is down"
      
      - alert: HighObjectCount
        expr: goverse_objects_total > 10000
        for: 10m
        labels:
          severity: warning
        annotations:
          summary: "High object count on {{ $labels.node }}"
      
      - alert: HighMethodCallErrorRate
        expr: |
          sum(rate(goverse_method_calls_total{status="failure"}[5m]))
          /
          sum(rate(goverse_method_calls_total[5m]))
          > 0.05
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "High error rate for method calls"
  ```

- [ ] **Create Grafana dashboards**
  - Cluster overview dashboard
  - Node metrics dashboard
  - Shard distribution dashboard
  - Method call latency dashboard
  - Example JSON in `deploy/kubernetes/grafana-dashboards/`

### 4.2 Logging (Priority: **High**)

- [ ] **Ensure stdout/stderr logging** for Kubernetes log collection
  - Already implemented in `util/logger/`
  - Verify JSON format option for structured logs

- [ ] **Add log aggregation manifests** (optional)
  - Fluentd/Fluent Bit DaemonSet for log shipping
  - Loki integration for log storage
  - Example configurations

- [ ] **Document log queries** for common scenarios
  - Finding errors for specific objects
  - Tracing request flow across nodes
  - Debugging shard migrations

### 4.3 Distributed Tracing (Priority: **Medium**)

- [ ] **Add OpenTelemetry support**
  - Instrument gRPC calls
  - Trace object method invocations
  - Export to Jaeger/Tempo

- [ ] **Add trace context propagation**
  - Propagate trace IDs across service calls
  - Link client requests to object calls

### 4.4 Dashboards and Alerts (Priority: **Medium**)

- [ ] **Create comprehensive alert rules** covering:
  - Node health (up/down)
  - Shard migration stuck
  - High error rates
  - Resource exhaustion
  - etcd connection issues
  - PostgreSQL connection issues

- [ ] **Create runbook documentation**
  - Alert remediation steps
  - Common failure scenarios
  - Recovery procedures
  - Reference: `docs/operations/runbook.md`

---

## 5. Security & Compliance

### 5.1 Pod Security (Priority: **High**)

- [ ] **Implement Pod Security Standards**
  - Use `restricted` Pod Security Standard
  - Non-root containers (already using distroless nonroot)
  - Read-only root filesystem where possible
  - Drop all capabilities

- [ ] **Add SecurityContext to all pods**
  ```yaml
  securityContext:
    runAsNonRoot: true
    runAsUser: 65532
    fsGroup: 65532
    seccompProfile:
      type: RuntimeDefault
    capabilities:
      drop:
      - ALL
  ```

### 5.2 Network Security (Priority: **High**)

- [ ] **Implement NetworkPolicies** (see section 3.2)
  - Restrict node-to-node communication
  - Restrict gate-to-node communication
  - Deny all by default, allow specific paths

- [ ] **Enable TLS for all gRPC connections**
  - Node-to-node TLS
  - Gate-to-node TLS
  - etcd client TLS

- [ ] **Add mTLS support** for mutual authentication
  - Certificate-based node authentication
  - Rotate certificates periodically

### 5.3 Secrets Management (Priority: **High**)

- [ ] **Use Kubernetes Secrets** for sensitive data
  - PostgreSQL credentials
  - etcd credentials
  - TLS certificates

- [ ] **Consider external secrets management**
  - HashiCorp Vault integration
  - AWS Secrets Manager
  - Google Secret Manager
  - External Secrets Operator

- [ ] **Implement secret rotation**
  - Automated database password rotation
  - Certificate rotation with cert-manager
  - Zero-downtime credential updates

### 5.4 RBAC (Priority: **Medium**)

- [ ] **Create ServiceAccounts** for components
  ```yaml
  apiVersion: v1
  kind: ServiceAccount
  metadata:
    name: goverse-node
    namespace: goverse
  ```

- [ ] **Create Roles and RoleBindings**
  - Minimal permissions for each component
  - Separate SA for nodes, gates, inspector
  - No cluster-admin usage

- [ ] **Add ClusterRole** if cross-namespace access needed

### 5.5 Image Security (Priority: **High**)

- [ ] **Implement image signing** with Cosign
  - Sign images in CI pipeline
  - Verify signatures in Kubernetes admission controller

- [ ] **Use admission controllers**
  - Enforce signed images only
  - Reject privileged containers
  - Enforce resource limits

- [ ] **Regular vulnerability scanning**
  - Automated Trivy scans in CI
  - Runtime scanning with Falco
  - CVE monitoring and patching

### 5.6 Audit Logging (Priority: **Low**)

- [ ] **Enable Kubernetes audit logging**
  - Log all API access to Goverse resources
  - Track configuration changes
  - Monitor secret access

- [ ] **Add application-level audit logs**
  - Log object creation/deletion
  - Log access control decisions
  - Track admin operations

---

## 6. Operations & Maintenance

### 6.1 Backup and Restore (Priority: **High**)

- [ ] **Create backup strategy documentation**
  - etcd backup procedures
  - PostgreSQL backup procedures
  - Backup retention policies

- [ ] **Implement automated backups**
  - CronJob for etcd snapshots
    ```yaml
    apiVersion: batch/v1
    kind: CronJob
    metadata:
      name: etcd-backup
      namespace: goverse
    spec:
      schedule: "0 2 * * *"  # Daily at 2 AM
      jobTemplate:
        spec:
          template:
            spec:
              containers:
              - name: backup
                image: quay.io/coreos/etcd:v3.5.10
                command:
                - /bin/sh
                - -c
                - |
                  etcdctl snapshot save /backup/etcd-$(date +%Y%m%d-%H%M%S).db
                volumeMounts:
                - name: backup
                  mountPath: /backup
              volumes:
              - name: backup
                persistentVolumeClaim:
                  claimName: etcd-backup
              restartPolicy: OnFailure
    ```

- [ ] **PostgreSQL backup job**
  - pg_dump to persistent volume
  - Offload to S3/GCS

- [ ] **Disaster recovery documentation**
  - Restore procedures
  - RTO/RPO targets
  - Test restore process regularly

### 6.2 Upgrades and Rollbacks (Priority: **High**)

- [ ] **Document rolling upgrade procedure**
  - Update strategy for StatefulSets
  - Shard migration during upgrades
  - Validation steps

- [ ] **Implement upgrade hooks**
  - Pre-upgrade database migrations (using pgadmin)
  - Post-upgrade validation
  - Automatic rollback on failure

- [ ] **Version compatibility matrix**
  - Node version compatibility
  - etcd version requirements
  - PostgreSQL version support

- [ ] **Create upgrade scripts**
  - `script/k8s-upgrade.sh` - Automated upgrade process
  - Pre-flight checks
  - Health validation between steps

### 6.3 Capacity Planning (Priority: **Medium**)

- [ ] **Document resource requirements**
  - Node resource needs per shard count
  - Gate resource needs per connection count
  - etcd sizing guidelines
  - PostgreSQL sizing guidelines

- [ ] **Add capacity planning tools**
  - Calculator for cluster size
  - Shard-to-node ratio recommendations
  - Storage growth projections

- [ ] **Monitoring for capacity issues**
  - Alert on approaching resource limits
  - Track growth trends
  - Recommend scaling actions

### 6.4 Day 2 Operations (Priority: **Medium**)

- [ ] **Create operational runbooks**
  - Node replacement procedure
  - Shard rebalancing
  - Performance troubleshooting
  - Common failure scenarios

- [ ] **Add debug tooling**
  - Debug sidecar for troubleshooting
  - Interactive shell access to pods
  - Network debugging tools

- [ ] **Implement chaos testing**
  - Pod failure injection
  - Network latency injection
  - etcd disruption testing
  - Validate graceful degradation

### 6.5 Multi-Cluster Support (Priority: **Low**)

- [ ] **Document multi-cluster patterns**
  - Cluster federation
  - Cross-cluster object routing
  - Disaster recovery clusters

- [ ] **Implement cluster linking**
  - Cross-cluster service discovery
  - Global object registry
  - Failover mechanisms

---

## 7. Testing & Validation

### 7.1 Integration Tests (Priority: **High**)

- [ ] **Create Kubernetes integration tests**
  - File: `tests/kubernetes/integration_test.go`
  - Use kind (Kubernetes in Docker) for local testing
  - Test full cluster lifecycle

- [ ] **Test scenarios**
  - [ ] Cluster bootstrap from scratch
  - [ ] Node scaling (scale up/down)
  - [ ] Gate scaling
  - [ ] Pod restart and recovery
  - [ ] ConfigMap updates
  - [ ] Secret rotation
  - [ ] Network partition handling
  - [ ] PersistentVolume failure

- [ ] **Add CI job for Kubernetes tests**
  - `.github/workflows/kubernetes-test.yml`
  - Create kind cluster
  - Deploy Goverse
  - Run test suite
  - Clean up

### 7.2 Load Testing (Priority: **High**)

- [ ] **Create Kubernetes-specific load tests**
  - Scale to production workload levels
  - Test shard rebalancing under load
  - Measure latency during pod restarts

- [ ] **Document load testing procedures**
  - Test scenarios
  - Expected performance benchmarks
  - Bottleneck identification

### 7.3 Chaos Engineering (Priority: **Medium**)

- [ ] **Implement chaos tests** using Chaos Mesh or Litmus
  - Pod deletion
  - Network latency
  - CPU stress
  - Memory pressure
  - Disk I/O disruption

- [ ] **Document failure scenarios and recovery**
  - Expected behavior during failures
  - Recovery time objectives
  - Data consistency guarantees

### 7.4 Conformance Testing (Priority: **Medium**)

- [ ] **Test on multiple Kubernetes distributions**
  - [ ] Vanilla Kubernetes (kubeadm)
  - [ ] GKE (Google Kubernetes Engine)
  - [ ] EKS (Amazon Elastic Kubernetes Service)
  - [ ] AKS (Azure Kubernetes Service)
  - [ ] OpenShift
  - [ ] k3s (lightweight)

- [ ] **Document platform-specific requirements**
  - LoadBalancer support
  - Storage class requirements
  - Ingress controller compatibility

### 7.5 End-to-End Tests (Priority: **Medium**)

- [ ] **Create E2E test suite** for Kubernetes
  - Deploy full stack (etcd, PostgreSQL, nodes, gates, inspector)
  - Run chat sample application
  - Verify all features work
  - Clean up resources

- [ ] **Automate E2E tests in CI**
  - Run on pull requests
  - Run on releases
  - Run nightly for long-running stability

---

## Summary and Priorities

### Immediate Priorities (Start Here)

1. **Production Dockerfiles** - Build optimized images for nodes, gates, inspector
2. **Health/Readiness Probes** - Add `/health` and `/ready` endpoints
3. **Core Kubernetes Manifests** - StatefulSets, Services, ConfigMaps
4. **Configuration Management** - Environment variable overrides, secret injection
5. **Service Discovery** - Pod IP and DNS-based discovery
6. **Prometheus Integration** - ServiceMonitors and basic alerts

### Short-term Goals (Next Phase)

1. **Helm Chart** - Simplify deployment with Helm
2. **TLS Support** - Enable encrypted communication
3. **Backup/Restore** - Automated backup jobs
4. **Integration Tests** - Kubernetes-specific test suite
5. **Load Testing** - Validate scalability on Kubernetes

### Long-term Goals (Future Work)

1. **Operator** - Custom controller for advanced lifecycle management
2. **Multi-cluster Support** - Federation and global routing
3. **Advanced Observability** - Distributed tracing, custom dashboards
4. **Chaos Engineering** - Systematic failure injection and testing
5. **Conformance Testing** - Validate on all major Kubernetes platforms

---

## Additional Resources

### Documentation to Create

- [ ] `docs/kubernetes/DEPLOYMENT_GUIDE.md` - Step-by-step deployment guide
- [ ] `docs/kubernetes/CONFIGURATION.md` - Kubernetes-specific configuration
- [ ] `docs/kubernetes/OPERATIONS.md` - Day 2 operations guide
- [ ] `docs/kubernetes/TROUBLESHOOTING.md` - Common issues and solutions
- [ ] `docs/kubernetes/SCALING.md` - Scaling strategies and best practices
- [ ] `docs/kubernetes/SECURITY.md` - Security hardening guide
- [ ] `deploy/kubernetes/README.md` - Quick start for Kubernetes deployment

### Examples to Add

- [ ] `examples/kubernetes/` - Complete example deployment
- [ ] `examples/kubernetes/minimal/` - Minimal single-node setup
- [ ] `examples/kubernetes/production/` - Production-ready configuration
- [ ] `examples/kubernetes/migrations/` - Database migration jobs

### Scripts to Create

- [ ] `script/k8s-deploy.sh` - Automated deployment script
- [ ] `script/k8s-upgrade.sh` - Automated upgrade script
- [ ] `script/k8s-validate.sh` - Deployment validation script
- [ ] `script/k8s-debug.sh` - Debug helper script
- [ ] `script/k8s-backup.sh` - Manual backup script
- [ ] `script/k8s-restore.sh` - Restore from backup script

---

## References

- Current Docker setup: `docker/Dockerfile.dev`, `docker/README.md`
- Existing configuration: `config/examples/multi-node.yaml`
- Prometheus metrics: `docs/PROMETHEUS_INTEGRATION.md`
- Architecture: `docs/DESIGN_OBJECTIVES.md`, `docs/GET_STARTED.md`
- Current CI/CD: `.github/workflows/docker.yml`

---

**Note:** This TODO list is comprehensive and represents the full scope of work needed for production Kubernetes deployment. Prioritization is key - start with High priority items and validate incrementally before moving to Medium and Low priority items.
