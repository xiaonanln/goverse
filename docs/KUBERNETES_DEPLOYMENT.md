# Kubernetes Deployment Design for Goverse

> **Status**: Design Document  
> This document describes the design for deploying Goverse clusters on Kubernetes, providing a scalable, production-ready approach to running distributed Goverse applications.

---

## 1. Goals and Objectives

The Kubernetes deployment support for Goverse aims to:

- **Simplify Deployment**: Provide straightforward Kubernetes manifests for deploying Goverse clusters
- **Production-Ready**: Support highly available, scalable deployments suitable for production workloads
- **Cloud-Native Integration**: Leverage Kubernetes features like ConfigMaps, Secrets, Services, and StatefulSets
- **Observability**: Integrate with Kubernetes monitoring tools (Prometheus, Grafana) and logging infrastructure
- **Flexible Architecture**: Support various deployment topologies (single-cluster, multi-cluster, hybrid)
- **Easy Scaling**: Enable horizontal scaling of both nodes and gates
- **High Availability**: Ensure cluster resilience with proper etcd and PostgreSQL deployment patterns
- **Developer Experience**: Provide Helm charts and examples for rapid deployment

---

## 2. Architecture Overview

### 2.1 Kubernetes Components

A Goverse cluster on Kubernetes consists of several components:

```
┌─────────────────────────────────────────────────────────────────┐
│                     Kubernetes Cluster                           │
│                                                                   │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │                    Ingress / LoadBalancer                   │ │
│  │              (External client traffic entry)                │ │
│  └────────────────────┬───────────────────────────────────────┘ │
│                       │                                          │
│  ┌────────────────────▼───────────────────────────────────────┐ │
│  │               Gate Deployment (Replicas: 2-5)              │ │
│  │                                                             │ │
│  │  ┌───────────┐  ┌───────────┐  ┌───────────┐              │ │
│  │  │  Gate-1   │  │  Gate-2   │  │  Gate-3   │              │ │
│  │  │   Pod     │  │   Pod     │  │   Pod     │              │ │
│  │  └───────────┘  └───────────┘  └───────────┘              │ │
│  └────────────────────┬───────────────────────────────────────┘ │
│                       │ (Routes calls to nodes)                  │
│  ┌────────────────────▼───────────────────────────────────────┐ │
│  │             Node StatefulSet (Replicas: 3-10+)             │ │
│  │                                                             │ │
│  │  ┌───────────┐  ┌───────────┐  ┌───────────┐              │ │
│  │  │  Node-0   │  │  Node-1   │  │  Node-2   │              │ │
│  │  │   Pod     │  │   Pod     │  │   Pod     │              │ │
│  │  │ (+ PVC)   │  │ (+ PVC)   │  │ (+ PVC)   │              │ │
│  │  └─────┬─────┘  └─────┬─────┘  └─────┬─────┘              │ │
│  └────────┼──────────────┼──────────────┼─────────────────────┘ │
│           │              │              │                        │
│  ┌────────▼──────────────▼──────────────▼─────────────────────┐ │
│  │                    etcd StatefulSet                         │ │
│  │             (3 replicas for HA quorum)                      │ │
│  │  ┌───────────┐  ┌───────────┐  ┌───────────┐              │ │
│  │  │  etcd-0   │  │  etcd-1   │  │  etcd-2   │              │ │
│  │  │ (+ PVC)   │  │ (+ PVC)   │  │ (+ PVC)   │              │ │
│  │  └───────────┘  └───────────┘  └───────────┘              │ │
│  └─────────────────────────────────────────────────────────────┘ │
│                                                                   │
│  ┌─────────────────────────────────────────────────────────────┐ │
│  │              PostgreSQL StatefulSet (Optional)              │ │
│  │              (For object persistence)                       │ │
│  │  ┌───────────┐  ┌───────────┐  ┌───────────┐              │ │
│  │  │   pg-0    │  │   pg-1    │  │   pg-2    │              │ │
│  │  │ (Primary) │  │ (Replica) │  │ (Replica) │              │ │
│  │  │ (+ PVC)   │  │ (+ PVC)   │  │ (+ PVC)   │              │ │
│  │  └───────────┘  └───────────┘  └───────────┘              │ │
│  └─────────────────────────────────────────────────────────────┘ │
│                                                                   │
│  ┌─────────────────────────────────────────────────────────────┐ │
│  │             Inspector Deployment (Optional)                 │ │
│  │               (Monitoring and visualization)                │ │
│  │  ┌───────────┐                                              │ │
│  │  │Inspector  │                                              │ │
│  │  │   Pod     │                                              │ │
│  │  └───────────┘                                              │ │
│  └─────────────────────────────────────────────────────────────┘ │
└───────────────────────────────────────────────────────────────────┘
```

### 2.2 Component Descriptions

#### Goverse Nodes (StatefulSet)
- **Purpose**: Host distributed objects and execute application logic
- **Type**: StatefulSet (stable network identity required for cluster coordination)
- **Replicas**: 3-10+ (based on object load and HA requirements)
- **Networking**: Headless service for stable DNS names
- **Storage**: Optional PVCs for local state/caching (ephemeral by default)
- **Resource Requirements**: CPU and memory based on object workload

#### Goverse Gates (Deployment)
- **Purpose**: Handle client connections (gRPC and HTTP)
- **Type**: Deployment (stateless, can be freely scaled)
- **Replicas**: 2-5 (based on client connection count)
- **Networking**: ClusterIP service + Ingress/LoadBalancer for external access
- **Storage**: None (fully stateless)
- **Resource Requirements**: Moderate CPU, low memory

#### etcd Cluster (StatefulSet)
- **Purpose**: Distributed coordination and cluster state
- **Type**: StatefulSet (persistent, stable identity required)
- **Replicas**: 3 or 5 (odd number for quorum)
- **Networking**: Headless service for peer discovery
- **Storage**: PVCs required (critical data)
- **Resource Requirements**: Moderate CPU, low-moderate memory, fast SSD storage

#### PostgreSQL (StatefulSet or External Service)
- **Purpose**: Object persistence (optional)
- **Type**: StatefulSet with replication or external managed service
- **Replicas**: 1 (single instance) or 3+ (with replication)
- **Networking**: ClusterIP service
- **Storage**: PVCs required (critical data)
- **Alternative**: Use cloud-managed PostgreSQL (RDS, Cloud SQL, Azure Database)

#### Inspector (Deployment)
- **Purpose**: Web UI for cluster visualization and monitoring
- **Type**: Deployment (optional, for ops/debugging)
- **Replicas**: 1
- **Networking**: ClusterIP service + Ingress for web access
- **Storage**: None
- **Resource Requirements**: Low CPU and memory

---

## 3. Kubernetes Resource Manifests

### 3.1 Namespace

Isolate Goverse cluster in its own namespace:

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: goverse
  labels:
    app.kubernetes.io/name: goverse
    app.kubernetes.io/component: cluster
```

### 3.2 ConfigMap

Store cluster configuration:

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
          - "etcd-0.etcd-headless.goverse.svc.cluster.local:2379"
          - "etcd-1.etcd-headless.goverse.svc.cluster.local:2379"
          - "etcd-2.etcd-headless.goverse.svc.cluster.local:2379"
        prefix: "/goverse"
      cluster_state_stability_duration: 10s
    
    inspector:
      grpc_addr: "inspector.goverse.svc.cluster.local:8081"
      http_addr: "0.0.0.0:8080"
      advertise_addr: "inspector.goverse.svc.cluster.local:8081"
    
    # Nodes configured via environment variables in StatefulSet
    nodes: []
    
    # Gates configured via environment variables in Deployment
    gates: []
```

### 3.3 Secret (for PostgreSQL credentials)

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: goverse-postgres-secret
  namespace: goverse
type: Opaque
stringData:
  POSTGRES_USER: goverse
  POSTGRES_PASSWORD: <secure-password-here>
  POSTGRES_DB: goverse
  POSTGRES_HOST: postgres.goverse.svc.cluster.local
  POSTGRES_PORT: "5432"
```

### 3.4 etcd StatefulSet

```yaml
apiVersion: v1
kind: Service
metadata:
  name: etcd-headless
  namespace: goverse
spec:
  clusterIP: None
  selector:
    app: etcd
  ports:
  - name: client
    port: 2379
    targetPort: 2379
  - name: peer
    port: 2380
    targetPort: 2380
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: etcd
  namespace: goverse
spec:
  serviceName: etcd-headless
  replicas: 3
  selector:
    matchLabels:
      app: etcd
  template:
    metadata:
      labels:
        app: etcd
    spec:
      containers:
      - name: etcd
        image: quay.io/coreos/etcd:v3.5.11
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
        - name: ETCD_INITIAL_CLUSTER
          value: "etcd-0=http://etcd-0.etcd-headless.goverse.svc.cluster.local:2380,etcd-1=http://etcd-1.etcd-headless.goverse.svc.cluster.local:2380,etcd-2=http://etcd-2.etcd-headless.goverse.svc.cluster.local:2380"
        - name: ETCD_INITIAL_CLUSTER_STATE
          value: "new"
        - name: ETCD_INITIAL_CLUSTER_TOKEN
          value: "goverse-etcd-cluster"
        - name: ETCD_LISTEN_CLIENT_URLS
          value: "http://0.0.0.0:2379"
        - name: ETCD_LISTEN_PEER_URLS
          value: "http://0.0.0.0:2380"
        - name: ETCD_ADVERTISE_CLIENT_URLS
          value: "http://$(ETCD_NAME).etcd-headless.goverse.svc.cluster.local:2379"
        - name: ETCD_INITIAL_ADVERTISE_PEER_URLS
          value: "http://$(ETCD_NAME).etcd-headless.goverse.svc.cluster.local:2380"
        - name: ETCD_DATA_DIR
          value: /var/lib/etcd
        volumeMounts:
        - name: etcd-data
          mountPath: /var/lib/etcd
        resources:
          requests:
            cpu: 100m
            memory: 128Mi
          limits:
            cpu: 500m
            memory: 512Mi
        livenessProbe:
          httpGet:
            path: /health
            port: 2379
          initialDelaySeconds: 10
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /health
            port: 2379
          initialDelaySeconds: 5
          periodSeconds: 5
  volumeClaimTemplates:
  - metadata:
      name: etcd-data
    spec:
      accessModes: [ "ReadWriteOnce" ]
      storageClassName: fast-ssd
      resources:
        requests:
          storage: 10Gi
```

### 3.5 Node StatefulSet

```yaml
apiVersion: v1
kind: Service
metadata:
  name: goverse-node-headless
  namespace: goverse
spec:
  clusterIP: None
  selector:
    app: goverse-node
  ports:
  - name: grpc
    port: 50051
    targetPort: 50051
  - name: http
    port: 8080
    targetPort: 8080
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: goverse-node
  namespace: goverse
spec:
  serviceName: goverse-node-headless
  replicas: 3
  selector:
    matchLabels:
      app: goverse-node
  template:
    metadata:
      labels:
        app: goverse-node
      annotations:
        prometheus.io/scrape: "true"
        prometheus.io/port: "8080"
        prometheus.io/path: "/metrics"
    spec:
      containers:
      - name: goverse-node
        image: goverse/node:latest
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
        - name: NODE_ID
          value: "$(POD_NAME)"
        - name: GRPC_ADDR
          value: "0.0.0.0:50051"
        - name: ADVERTISE_ADDR
          value: "$(POD_NAME).goverse-node-headless.$(POD_NAMESPACE).svc.cluster.local:50051"
        - name: HTTP_ADDR
          value: "0.0.0.0:8080"
        - name: CONFIG_FILE
          value: "/etc/goverse/config.yaml"
        envFrom:
        - secretRef:
            name: goverse-postgres-secret
        volumeMounts:
        - name: config
          mountPath: /etc/goverse
        - name: data
          mountPath: /var/lib/goverse
        resources:
          requests:
            cpu: 500m
            memory: 512Mi
          limits:
            cpu: 2000m
            memory: 2Gi
        livenessProbe:
          httpGet:
            path: /healthz
            port: 8080
          initialDelaySeconds: 30
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /ready
            port: 8080
          initialDelaySeconds: 10
          periodSeconds: 5
      volumes:
      - name: config
        configMap:
          name: goverse-config
  volumeClaimTemplates:
  - metadata:
      name: data
    spec:
      accessModes: [ "ReadWriteOnce" ]
      storageClassName: standard
      resources:
        requests:
          storage: 20Gi
```

### 3.6 Gate Deployment

```yaml
apiVersion: v1
kind: Service
metadata:
  name: goverse-gate
  namespace: goverse
spec:
  type: ClusterIP
  selector:
    app: goverse-gate
  ports:
  - name: grpc
    port: 60051
    targetPort: 60051
  - name: http
    port: 8080
    targetPort: 8080
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: goverse-gate
  namespace: goverse
spec:
  replicas: 3
  selector:
    matchLabels:
      app: goverse-gate
  template:
    metadata:
      labels:
        app: goverse-gate
      annotations:
        prometheus.io/scrape: "true"
        prometheus.io/port: "8080"
        prometheus.io/path: "/metrics"
    spec:
      containers:
      - name: goverse-gate
        image: goverse/gate:latest
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
        - name: POD_NAMESPACE
          valueFrom:
            fieldRef:
              fieldPath: metadata.namespace
        - name: GATE_ID
          value: "$(POD_NAME)"
        - name: GRPC_ADDR
          value: "0.0.0.0:60051"
        - name: HTTP_ADDR
          value: "0.0.0.0:8080"
        - name: CONFIG_FILE
          value: "/etc/goverse/config.yaml"
        volumeMounts:
        - name: config
          mountPath: /etc/goverse
        resources:
          requests:
            cpu: 200m
            memory: 256Mi
          limits:
            cpu: 1000m
            memory: 1Gi
        livenessProbe:
          httpGet:
            path: /healthz
            port: 8080
          initialDelaySeconds: 10
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /ready
            port: 8080
          initialDelaySeconds: 5
          periodSeconds: 5
      volumes:
      - name: config
        configMap:
          name: goverse-config
```

### 3.7 Ingress (for external client access)

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: goverse-gate-ingress
  namespace: goverse
  annotations:
    nginx.ingress.kubernetes.io/backend-protocol: "GRPC"
    cert-manager.io/cluster-issuer: "letsencrypt-prod"
spec:
  ingressClassName: nginx
  tls:
  - hosts:
    - goverse.example.com
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
              number: 60051
```

### 3.8 Inspector Deployment (Optional)

```yaml
apiVersion: v1
kind: Service
metadata:
  name: inspector
  namespace: goverse
spec:
  type: ClusterIP
  selector:
    app: inspector
  ports:
  - name: grpc
    port: 8081
    targetPort: 8081
  - name: http
    port: 8080
    targetPort: 8080
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: inspector
  namespace: goverse
spec:
  replicas: 1
  selector:
    matchLabels:
      app: inspector
  template:
    metadata:
      labels:
        app: inspector
    spec:
      containers:
      - name: inspector
        image: goverse/inspector:latest
        ports:
        - containerPort: 8081
          name: grpc
        - containerPort: 8080
          name: http
        env:
        - name: GRPC_ADDR
          value: "0.0.0.0:8081"
        - name: HTTP_ADDR
          value: "0.0.0.0:8080"
        - name: CONFIG_FILE
          value: "/etc/goverse/config.yaml"
        volumeMounts:
        - name: config
          mountPath: /etc/goverse
        resources:
          requests:
            cpu: 100m
            memory: 128Mi
          limits:
            cpu: 500m
            memory: 512Mi
      volumes:
      - name: config
        configMap:
          name: goverse-config
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: inspector-ingress
  namespace: goverse
  annotations:
    cert-manager.io/cluster-issuer: "letsencrypt-prod"
spec:
  ingressClassName: nginx
  tls:
  - hosts:
    - inspector.goverse.example.com
    secretName: inspector-tls
  rules:
  - host: inspector.goverse.example.com
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: inspector
            port:
              number: 8080
```

---

## 4. PostgreSQL Deployment Options

### Option 1: In-Cluster StatefulSet (Simple)

For development or small deployments:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: postgres
  namespace: goverse
spec:
  type: ClusterIP
  selector:
    app: postgres
  ports:
  - port: 5432
    targetPort: 5432
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: postgres
  namespace: goverse
spec:
  serviceName: postgres
  replicas: 1
  selector:
    matchLabels:
      app: postgres
  template:
    metadata:
      labels:
        app: postgres
    spec:
      containers:
      - name: postgres
        image: postgres:15
        ports:
        - containerPort: 5432
        env:
        - name: POSTGRES_USER
          valueFrom:
            secretKeyRef:
              name: goverse-postgres-secret
              key: POSTGRES_USER
        - name: POSTGRES_PASSWORD
          valueFrom:
            secretKeyRef:
              name: goverse-postgres-secret
              key: POSTGRES_PASSWORD
        - name: POSTGRES_DB
          valueFrom:
            secretKeyRef:
              name: goverse-postgres-secret
              key: POSTGRES_DB
        - name: PGDATA
          value: /var/lib/postgresql/data/pgdata
        volumeMounts:
        - name: postgres-data
          mountPath: /var/lib/postgresql/data
        resources:
          requests:
            cpu: 500m
            memory: 1Gi
          limits:
            cpu: 2000m
            memory: 4Gi
  volumeClaimTemplates:
  - metadata:
      name: postgres-data
    spec:
      accessModes: [ "ReadWriteOnce" ]
      storageClassName: fast-ssd
      resources:
        requests:
          storage: 100Gi
```

### Option 2: Cloud-Managed Database (Recommended for Production)

Use external managed PostgreSQL services:

- **AWS**: Amazon RDS for PostgreSQL
- **GCP**: Cloud SQL for PostgreSQL
- **Azure**: Azure Database for PostgreSQL

Update the Secret with external database connection details:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: goverse-postgres-secret
  namespace: goverse
type: Opaque
stringData:
  POSTGRES_USER: goverse
  POSTGRES_PASSWORD: <rds-password>
  POSTGRES_DB: goverse
  POSTGRES_HOST: goverse-db.c9akciq32.us-west-2.rds.amazonaws.com
  POSTGRES_PORT: "5432"
```

### Option 3: PostgreSQL Operator (Production HA)

Use an operator like Zalando's postgres-operator or Crunchy Data:

```yaml
apiVersion: "acid.zalan.do/v1"
kind: postgresql
metadata:
  name: goverse-postgres
  namespace: goverse
spec:
  teamId: "goverse"
  volume:
    size: 100Gi
    storageClass: fast-ssd
  numberOfInstances: 3
  users:
    goverse:
    - superuser
    - createdb
  databases:
    goverse: goverse
  postgresql:
    version: "15"
  resources:
    requests:
      cpu: 500m
      memory: 1Gi
    limits:
      cpu: 2000m
      memory: 4Gi
```

---

## 5. Helm Chart Structure

For easier deployment, provide a Helm chart:

```
goverse-helm/
├── Chart.yaml
├── values.yaml
├── templates/
│   ├── namespace.yaml
│   ├── configmap.yaml
│   ├── secret.yaml
│   ├── etcd/
│   │   ├── statefulset.yaml
│   │   └── service.yaml
│   ├── nodes/
│   │   ├── statefulset.yaml
│   │   └── service.yaml
│   ├── gates/
│   │   ├── deployment.yaml
│   │   ├── service.yaml
│   │   └── ingress.yaml
│   ├── postgres/
│   │   ├── statefulset.yaml
│   │   └── service.yaml
│   ├── inspector/
│   │   ├── deployment.yaml
│   │   ├── service.yaml
│   │   └── ingress.yaml
│   ├── rbac.yaml
│   └── servicemonitor.yaml (for Prometheus)
└── README.md
```

### Sample values.yaml

```yaml
# Global settings
global:
  imageRegistry: docker.io
  imagePullSecrets: []

# Goverse cluster configuration
cluster:
  shards: 8192
  etcdPrefix: "/goverse"
  stabilityDuration: "10s"

# Node configuration
nodes:
  enabled: true
  replicas: 3
  image:
    repository: goverse/node
    tag: "latest"
    pullPolicy: IfNotPresent
  resources:
    requests:
      cpu: 500m
      memory: 512Mi
    limits:
      cpu: 2000m
      memory: 2Gi
  storage:
    enabled: true
    size: 20Gi
    storageClass: standard

# Gate configuration
gates:
  enabled: true
  replicas: 3
  image:
    repository: goverse/gate
    tag: "latest"
    pullPolicy: IfNotPresent
  resources:
    requests:
      cpu: 200m
      memory: 256Mi
    limits:
      cpu: 1000m
      memory: 1Gi
  ingress:
    enabled: true
    className: nginx
    host: goverse.example.com
    tls:
      enabled: true
      secretName: goverse-tls

# etcd configuration
etcd:
  enabled: true
  replicas: 3
  image:
    repository: quay.io/coreos/etcd
    tag: "v3.5.11"
  resources:
    requests:
      cpu: 100m
      memory: 128Mi
    limits:
      cpu: 500m
      memory: 512Mi
  storage:
    size: 10Gi
    storageClass: fast-ssd

# PostgreSQL configuration
postgres:
  enabled: true
  # Use external managed database (recommended for production)
  external:
    enabled: false
    host: ""
    port: 5432
    database: goverse
    user: goverse
    password: ""
  # In-cluster PostgreSQL
  internal:
    enabled: true
    replicas: 1
    image:
      repository: postgres
      tag: "15"
    resources:
      requests:
        cpu: 500m
        memory: 1Gi
      limits:
        cpu: 2000m
        memory: 4Gi
    storage:
      size: 100Gi
      storageClass: fast-ssd

# Inspector configuration
inspector:
  enabled: true
  image:
    repository: goverse/inspector
    tag: "latest"
    pullPolicy: IfNotPresent
  resources:
    requests:
      cpu: 100m
      memory: 128Mi
    limits:
      cpu: 500m
      memory: 512Mi
  ingress:
    enabled: true
    className: nginx
    host: inspector.goverse.example.com
    tls:
      enabled: true
      secretName: inspector-tls

# Monitoring configuration
monitoring:
  enabled: true
  serviceMonitor:
    enabled: true
    interval: 30s
```

---

## 6. Deployment Strategies

### 6.1 Development/Testing

Quick deployment for development:

```bash
# Create namespace
kubectl create namespace goverse

# Deploy etcd
kubectl apply -f k8s/etcd/

# Deploy config
kubectl apply -f k8s/configmap.yaml
kubectl apply -f k8s/secret.yaml

# Deploy nodes
kubectl apply -f k8s/nodes/

# Deploy gates
kubectl apply -f k8s/gates/

# Deploy inspector
kubectl apply -f k8s/inspector/
```

### 6.2 Production Deployment

Using Helm for production:

```bash
# Add Goverse Helm repository (once available)
helm repo add goverse https://charts.goverse.io
helm repo update

# Install with custom values
helm install goverse goverse/goverse \
  --namespace goverse \
  --create-namespace \
  --values production-values.yaml

# Or install from local chart
helm install goverse ./goverse-helm \
  --namespace goverse \
  --create-namespace \
  --values production-values.yaml
```

### 6.3 Rolling Updates

Update node images without downtime:

```bash
# Update StatefulSet image
kubectl set image statefulset/goverse-node \
  goverse-node=goverse/node:v1.2.0 \
  -n goverse

# Monitor rollout
kubectl rollout status statefulset/goverse-node -n goverse

# Rollback if needed
kubectl rollout undo statefulset/goverse-node -n goverse
```

---

## 7. Scaling Strategies

### 7.1 Horizontal Scaling

#### Scale Nodes (Adds More Object Capacity)

```bash
# Scale up nodes (shards will rebalance automatically)
kubectl scale statefulset goverse-node --replicas=5 -n goverse

# Monitor cluster state via Inspector
kubectl port-forward svc/inspector 8080:8080 -n goverse
# Open http://localhost:8080

# Scale down (with care - objects will migrate)
kubectl scale statefulset goverse-node --replicas=3 -n goverse
```

**Important Considerations**:
- Goverse automatically rebalances shards when nodes join/leave
- Wait for `cluster_state_stability_duration` before expecting rebalancing
- Monitor object migration via Inspector UI
- Scaling down may temporarily impact performance during shard migration

#### Scale Gates (Adds More Client Capacity)

```bash
# Scale gates freely (stateless)
kubectl scale deployment goverse-gate --replicas=5 -n goverse
```

Gates are stateless and can be scaled up/down instantly without impacting cluster state.

### 7.2 Vertical Scaling

Adjust resources for existing pods:

```bash
# Update node resources
kubectl set resources statefulset goverse-node \
  --limits=cpu=4000m,memory=4Gi \
  --requests=cpu=1000m,memory=1Gi \
  -n goverse
```

### 7.3 Auto-Scaling

#### Horizontal Pod Autoscaler (HPA) for Gates

```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: goverse-gate-hpa
  namespace: goverse
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: goverse-gate
  minReplicas: 2
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

**Note**: HPA for nodes (StatefulSet) requires more careful consideration:
- Nodes participate in cluster coordination
- Scaling affects shard distribution
- Consider using Vertical Pod Autoscaler (VPA) or manual scaling instead

---

## 8. Monitoring and Observability

### 8.1 Prometheus Integration

Deploy ServiceMonitor for Prometheus Operator:

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: goverse-nodes
  namespace: goverse
  labels:
    app: goverse-node
spec:
  selector:
    matchLabels:
      app: goverse-node
  endpoints:
  - port: http
    path: /metrics
    interval: 30s
---
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: goverse-gates
  namespace: goverse
  labels:
    app: goverse-gate
spec:
  selector:
    matchLabels:
      app: goverse-gate
  endpoints:
  - port: http
    path: /metrics
    interval: 30s
```

### 8.2 Grafana Dashboards

Key metrics to monitor:

- **Node Metrics**:
  - Object count per node
  - Shard distribution
  - CPU and memory usage
  - Object method call latency
  - Object activation/deactivation rate

- **Gate Metrics**:
  - Active client connections
  - Request throughput (RPS)
  - Request latency (P50, P95, P99)
  - Error rate

- **Cluster Metrics**:
  - Total nodes in cluster
  - Total shards mapped
  - Cluster leadership changes
  - etcd health and latency

### 8.3 Logging

Configure centralized logging with Fluentd/Fluent Bit:

```yaml
# Fluentd DaemonSet config (simplified)
apiVersion: v1
kind: ConfigMap
metadata:
  name: fluentd-config
  namespace: kube-system
data:
  fluent.conf: |
    <match kubernetes.var.log.containers.goverse-**>
      @type elasticsearch
      host elasticsearch.logging.svc.cluster.local
      port 9200
      logstash_format true
      logstash_prefix goverse
    </match>
```

**Log Aggregation Query Examples** (in Kibana/Elasticsearch):
- `namespace:goverse AND app:goverse-node AND level:ERROR`
- `namespace:goverse AND message:*CallObject*`
- `namespace:goverse AND object_type:ChatRoom`

### 8.4 Health Checks

Implement proper health endpoints in Goverse applications:

#### Liveness Probe
Checks if the process is alive and should be restarted if failing:

```go
// /healthz endpoint
// Returns 200 if process is running
```

#### Readiness Probe
Checks if the node/gate is ready to accept traffic:

```go
// /ready endpoint
// Returns 200 if:
// - Connected to etcd
// - Cluster state synchronized
// - Node registered (for nodes)
```

---

## 9. Security Considerations

### 9.1 Network Policies

Restrict network access between components:

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: goverse-node-policy
  namespace: goverse
spec:
  podSelector:
    matchLabels:
      app: goverse-node
  policyTypes:
  - Ingress
  - Egress
  ingress:
  # Allow from gates
  - from:
    - podSelector:
        matchLabels:
          app: goverse-gate
    ports:
    - protocol: TCP
      port: 50051
  # Allow from other nodes
  - from:
    - podSelector:
        matchLabels:
          app: goverse-node
    ports:
    - protocol: TCP
      port: 50051
  egress:
  # Allow to etcd
  - to:
    - podSelector:
        matchLabels:
          app: etcd
    ports:
    - protocol: TCP
      port: 2379
  # Allow to postgres
  - to:
    - podSelector:
        matchLabels:
          app: postgres
    ports:
    - protocol: TCP
      port: 5432
  # Allow DNS
  - to:
    - namespaceSelector: {}
    ports:
    - protocol: UDP
      port: 53
```

### 9.2 RBAC Configuration

Create ServiceAccount and RBAC for Goverse components:

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: goverse-node
  namespace: goverse
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: goverse-node-role
  namespace: goverse
rules:
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["get", "list"]
- apiGroups: [""]
  resources: ["configmaps"]
  verbs: ["get", "list", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: goverse-node-rolebinding
  namespace: goverse
subjects:
- kind: ServiceAccount
  name: goverse-node
  namespace: goverse
roleRef:
  kind: Role
  name: goverse-node-role
  apiGroup: rbac.authorization.k8s.io
```

Add to Node StatefulSet:

```yaml
spec:
  template:
    spec:
      serviceAccountName: goverse-node
```

### 9.3 Pod Security Standards

Apply Pod Security Standards to namespace:

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: goverse
  labels:
    pod-security.kubernetes.io/enforce: restricted
    pod-security.kubernetes.io/audit: restricted
    pod-security.kubernetes.io/warn: restricted
```

Update Pod specs to comply:

```yaml
spec:
  template:
    spec:
      securityContext:
        runAsNonRoot: true
        runAsUser: 1000
        fsGroup: 1000
        seccompProfile:
          type: RuntimeDefault
      containers:
      - name: goverse-node
        securityContext:
          allowPrivilegeEscalation: false
          capabilities:
            drop:
            - ALL
```

### 9.4 TLS/mTLS

Enable TLS for gRPC communication:

1. Create TLS certificates (using cert-manager):

```yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: goverse-node-tls
  namespace: goverse
spec:
  secretName: goverse-node-tls
  issuerRef:
    name: ca-issuer
    kind: Issuer
  commonName: "*.goverse-node-headless.goverse.svc.cluster.local"
  dnsNames:
  - "*.goverse-node-headless.goverse.svc.cluster.local"
  - "goverse-node-headless.goverse.svc.cluster.local"
```

2. Mount certificates in pods and configure Goverse to use TLS

---

## 10. Disaster Recovery and Backup

### 10.1 etcd Backup

Regular etcd snapshots:

```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: etcd-backup
  namespace: goverse
spec:
  schedule: "0 */6 * * *"  # Every 6 hours
  jobTemplate:
    spec:
      template:
        spec:
          containers:
          - name: backup
            image: quay.io/coreos/etcd:v3.5.11
            command:
            - /bin/sh
            - -c
            - |
              ETCDCTL_API=3 etcdctl \
                --endpoints=http://etcd-0.etcd-headless.goverse.svc.cluster.local:2379 \
                snapshot save /backup/snapshot-$(date +%Y%m%d-%H%M%S).db
              # Upload to S3/GCS/Azure Blob
            volumeMounts:
            - name: backup
              mountPath: /backup
          restartPolicy: OnFailure
          volumes:
          - name: backup
            persistentVolumeClaim:
              claimName: etcd-backup-pvc
```

### 10.2 PostgreSQL Backup

Use pg_dump for regular backups:

```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: postgres-backup
  namespace: goverse
spec:
  schedule: "0 2 * * *"  # Daily at 2 AM
  jobTemplate:
    spec:
      template:
        spec:
          containers:
          - name: backup
            image: postgres:15
            env:
            - name: PGPASSWORD
              valueFrom:
                secretKeyRef:
                  name: goverse-postgres-secret
                  key: POSTGRES_PASSWORD
            command:
            - /bin/sh
            - -c
            - |
              pg_dump -h postgres.goverse.svc.cluster.local \
                      -U goverse \
                      -d goverse \
                      -F c \
                      -f /backup/goverse-$(date +%Y%m%d-%H%M%S).dump
              # Upload to S3/GCS/Azure Blob
            volumeMounts:
            - name: backup
              mountPath: /backup
          restartPolicy: OnFailure
          volumes:
          - name: backup
            persistentVolumeClaim:
              claimName: postgres-backup-pvc
```

### 10.3 Restore Procedures

#### etcd Restore

```bash
# Stop etcd StatefulSet
kubectl scale statefulset etcd --replicas=0 -n goverse

# Delete PVCs
kubectl delete pvc -l app=etcd -n goverse

# Restore from snapshot (manual process)
# ... restore snapshot to new PVCs ...

# Scale back up
kubectl scale statefulset etcd --replicas=3 -n goverse
```

#### PostgreSQL Restore

```bash
# Scale down nodes to prevent writes
kubectl scale statefulset goverse-node --replicas=0 -n goverse

# Restore database
kubectl exec -it postgres-0 -n goverse -- \
  pg_restore -h localhost -U goverse -d goverse \
  -c /backup/goverse-20231220-020000.dump

# Scale nodes back up
kubectl scale statefulset goverse-node --replicas=3 -n goverse
```

---

## 11. Migration and Upgrade Procedures

### 11.1 Zero-Downtime Upgrades

For gate upgrades (stateless):

```bash
# Update image
kubectl set image deployment/goverse-gate \
  goverse-gate=goverse/gate:v1.2.0 -n goverse

# Rolling update happens automatically
kubectl rollout status deployment/goverse-gate -n goverse
```

For node upgrades (stateful):

```bash
# Use partition-based rolling update
kubectl patch statefulset goverse-node -n goverse \
  --type='json' \
  -p='[{"op": "replace", "path": "/spec/updateStrategy/rollingUpdate/partition", "value": 2}]'

# Update image
kubectl set image statefulset/goverse-node \
  goverse-node=goverse/node:v1.2.0 -n goverse

# Gradually decrease partition to roll out
kubectl patch statefulset goverse-node -n goverse \
  --type='json' \
  -p='[{"op": "replace", "path": "/spec/updateStrategy/rollingUpdate/partition", "value": 0}]'
```

### 11.2 Configuration Changes

Update ConfigMap and rolling restart:

```bash
# Update ConfigMap
kubectl edit configmap goverse-config -n goverse

# Restart nodes to pick up new config
kubectl rollout restart statefulset/goverse-node -n goverse
kubectl rollout restart deployment/goverse-gate -n goverse
```

---

## 12. Troubleshooting

### 12.1 Common Issues

#### Nodes Not Joining Cluster

Check etcd connectivity:

```bash
kubectl exec -it goverse-node-0 -n goverse -- \
  sh -c 'apk add curl && curl http://etcd-0.etcd-headless:2379/health'
```

Check node logs:

```bash
kubectl logs goverse-node-0 -n goverse --tail=100
```

#### Shard Mapping Not Updating

Check leader election:

```bash
# Look for "became leader" or "following leader" messages
kubectl logs -l app=goverse-node -n goverse | grep -i leader
```

Check cluster stability duration:

```bash
# Ensure nodes are stable for configured duration
kubectl get configmap goverse-config -n goverse -o yaml | grep stability
```

#### High Object Call Latency

Check if objects are on the right nodes:

```bash
# Use Inspector to visualize object distribution
kubectl port-forward svc/inspector 8080:8080 -n goverse
```

Check resource usage:

```bash
kubectl top pods -n goverse
```

### 12.2 Debug Commands

```bash
# Get cluster state
kubectl get all -n goverse

# Check etcd cluster health
kubectl exec -it etcd-0 -n goverse -- \
  etcdctl --endpoints=http://localhost:2379 endpoint health

# Check PostgreSQL connectivity
kubectl exec -it goverse-node-0 -n goverse -- \
  psql -h postgres.goverse.svc.cluster.local -U goverse -c 'SELECT 1'

# View recent events
kubectl get events -n goverse --sort-by='.lastTimestamp'

# Check node readiness
kubectl get pods -l app=goverse-node -n goverse -o wide
```

---

## 13. Best Practices

### 13.1 Resource Sizing

**Development/Testing**:
- Nodes: 3 replicas, 500m CPU, 512Mi memory
- Gates: 2 replicas, 200m CPU, 256Mi memory
- etcd: 3 replicas, 100m CPU, 128Mi memory

**Production (Small)**:
- Nodes: 5 replicas, 1000m CPU, 1Gi memory
- Gates: 3 replicas, 500m CPU, 512Mi memory
- etcd: 3 replicas, 500m CPU, 512Mi memory

**Production (Large)**:
- Nodes: 10+ replicas, 2000m CPU, 2Gi memory
- Gates: 5+ replicas, 1000m CPU, 1Gi memory
- etcd: 5 replicas, 1000m CPU, 1Gi memory

### 13.2 Storage Classes

- **etcd**: Use fast SSD storage (gp3 on AWS, pd-ssd on GCP)
- **PostgreSQL**: Use fast SSD storage with high IOPS
- **Nodes**: Standard storage is sufficient (object state is optional)

### 13.3 High Availability

- Always run etcd with 3 or 5 replicas (odd number for quorum)
- Run minimum 3 node replicas for production
- Run minimum 2 gate replicas for client HA
- Use Pod Anti-Affinity to spread replicas across nodes/zones
- Use Pod Disruption Budgets to protect against disruptions

### 13.4 Cost Optimization

- Use node pools with appropriate instance types
- Enable cluster autoscaling for gate deployments
- Use Spot/Preemptible instances for non-critical components
- Set appropriate resource requests/limits to avoid over-provisioning
- Use Horizontal Pod Autoscaler for gates based on load

---

## 14. Example: Multi-Zone Deployment

Deploy across multiple availability zones for maximum HA:

```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: goverse-node
  namespace: goverse
spec:
  # ... other config ...
  template:
    spec:
      affinity:
        podAntiAffinity:
          preferredDuringSchedulingIgnoredDuringExecution:
          - weight: 100
            podAffinityTerm:
              labelSelector:
                matchLabels:
                  app: goverse-node
              topologyKey: topology.kubernetes.io/zone
          - weight: 50
            podAffinityTerm:
              labelSelector:
                matchLabels:
                  app: goverse-node
              topologyKey: kubernetes.io/hostname
      tolerations:
      - key: node.kubernetes.io/not-ready
        operator: Exists
        effect: NoExecute
        tolerationSeconds: 300
---
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: goverse-node-pdb
  namespace: goverse
spec:
  minAvailable: 2
  selector:
    matchLabels:
      app: goverse-node
```

---

## 15. Next Steps

### 15.1 Implementation Roadmap

**Phase 1: Core Manifests** (Week 1-2)
- Create base Kubernetes manifests
- Implement ConfigMap and Secret management
- Deploy etcd StatefulSet
- Deploy Node StatefulSet
- Deploy Gate Deployment

**Phase 2: Helm Chart** (Week 3-4)
- Create Helm chart structure
- Parameterize all configurations
- Add values.yaml with sensible defaults
- Test Helm deployment

**Phase 3: Monitoring Integration** (Week 5)
- Add Prometheus ServiceMonitor
- Create Grafana dashboards
- Implement health check endpoints
- Configure logging

**Phase 4: Production Hardening** (Week 6-7)
- Implement security policies
- Add backup/restore procedures
- Test disaster recovery
- Performance tuning
- Documentation

**Phase 5: CI/CD Integration** (Week 8)
- Create container images with CI/CD
- Automate testing in Kubernetes
- Implement GitOps workflows
- Publish Helm charts

### 15.2 Documentation Requirements

- **User Guide**: Step-by-step deployment instructions
- **Operations Guide**: Monitoring, scaling, troubleshooting
- **Reference**: Complete configuration options
- **Examples**: Sample applications deployed on Kubernetes

### 15.3 Testing Strategy

- Unit tests for Helm chart templating
- Integration tests with Kind/Minikube
- E2E tests with sample applications
- Load testing to validate scaling
- Chaos engineering (pod failures, network partitions)

---

## 16. Conclusion

This design provides a comprehensive approach to deploying Goverse clusters on Kubernetes, leveraging cloud-native patterns and Kubernetes primitives for scalability, reliability, and operational excellence.

Key benefits of Kubernetes deployment:

✅ **Scalability**: Horizontal scaling of nodes and gates  
✅ **High Availability**: Multi-zone deployment with automatic failover  
✅ **Observability**: Native integration with Prometheus and logging  
✅ **Automation**: GitOps workflows and automated deployments  
✅ **Flexibility**: Support for various cloud providers and on-premises  
✅ **Developer Experience**: Simple deployment with Helm charts  

The design balances simplicity for development/testing with production-grade features for enterprise deployments.

---

## Appendix A: Quick Start Commands

### Deploy Minimal Cluster (Development)

```bash
# Create namespace
kubectl create namespace goverse

# Deploy etcd
kubectl apply -f https://raw.githubusercontent.com/xiaonanln/goverse/main/k8s/etcd/

# Deploy Goverse
helm install goverse goverse/goverse \
  --namespace goverse \
  --set nodes.replicas=3 \
  --set gates.replicas=2 \
  --set postgres.internal.enabled=true \
  --set inspector.enabled=true

# Access Inspector
kubectl port-forward -n goverse svc/inspector 8080:8080
# Open http://localhost:8080
```

### Deploy Production Cluster

```bash
# Create namespace
kubectl create namespace goverse-prod

# Deploy with external database
helm install goverse goverse/goverse \
  --namespace goverse-prod \
  --values production-values.yaml \
  --set postgres.external.enabled=true \
  --set postgres.external.host=goverse-db.us-west-2.rds.amazonaws.com \
  --set postgres.internal.enabled=false \
  --set nodes.replicas=10 \
  --set gates.replicas=5 \
  --set etcd.replicas=5
```

---

**License**: Apache-2.0  
**Contributing**: Contributions welcome! See repository for guidelines.  
**Support**: File issues on GitHub for bugs or feature requests.
