# Complete Goverse Cluster Example

This example deploys a complete Goverse cluster with all components.

## What's Included

- 3-node etcd cluster for coordination
- 3 Goverse nodes for hosting objects
- 2 Goverse gates for client connections
- 1 PostgreSQL instance for persistence
- 1 Inspector instance for monitoring

## Prerequisites

- Kubernetes cluster (1.25+)
- kubectl configured
- 4+ CPU cores and 8+ GB RAM available in cluster
- 150+ GB storage available

## Quick Deploy

```bash
# Deploy everything in order
kubectl apply -f namespace.yaml
kubectl apply -f configmap.yaml
kubectl apply -f secret.yaml

# Wait a moment for namespace to be created
sleep 5

# Deploy etcd (critical - must be up first)
kubectl apply -f etcd.yaml

# Wait for etcd to be ready
echo "Waiting for etcd cluster..."
kubectl wait --for=condition=ready pod -l app=etcd -n goverse --timeout=300s

# Deploy PostgreSQL
kubectl apply -f postgres.yaml

# Wait for postgres to be ready
echo "Waiting for PostgreSQL..."
kubectl wait --for=condition=ready pod -l app=postgres -n goverse --timeout=300s

# Deploy Goverse nodes
kubectl apply -f nodes.yaml

# Deploy Goverse gates
kubectl apply -f gates.yaml

# Deploy Inspector
kubectl apply -f inspector.yaml

# Wait for cluster to be ready
echo "Waiting for Goverse cluster..."
kubectl wait --for=condition=ready pod -l app=goverse-node -n goverse --timeout=300s
kubectl wait --for=condition=ready pod -l app=goverse-gate -n goverse --timeout=300s

echo "Goverse cluster deployed successfully!"
echo ""
echo "Access Inspector UI:"
echo "  kubectl port-forward -n goverse svc/inspector 8080:8080"
echo "  Then open http://localhost:8080"
echo ""
echo "View cluster status:"
echo "  kubectl get pods -n goverse"
```

## Deploy Script

Or use the provided script:

```bash
chmod +x deploy.sh
./deploy.sh
```

## Verify Deployment

```bash
# Check all pods
kubectl get pods -n goverse -o wide

# Expected output:
# NAME              READY   STATUS    RESTARTS   AGE
# etcd-0            1/1     Running   0          5m
# etcd-1            1/1     Running   0          5m
# etcd-2            1/1     Running   0          5m
# goverse-node-0    1/1     Running   0          3m
# goverse-node-1    1/1     Running   0          3m
# goverse-node-2    1/1     Running   0          3m
# goverse-gate-xxx  1/1     Running   0          2m
# goverse-gate-yyy  1/1     Running   0          2m
# postgres-0        1/1     Running   0          4m
# inspector-xxx     1/1     Running   0          2m

# Check services
kubectl get svc -n goverse

# View node logs
kubectl logs goverse-node-0 -n goverse --tail=20
```

## Access Inspector

```bash
# Port-forward to local machine
kubectl port-forward -n goverse svc/inspector 8080:8080

# Open in browser: http://localhost:8080
```

## Test the Cluster

From your application:

```go
// Connect to gate
conn, err := grpc.Dial("goverse-gate.goverse.svc.cluster.local:60051", 
    grpc.WithInsecure())

// Or use HTTP gate
http://goverse-gate.goverse.svc.cluster.local:8080
```

## Cleanup

```bash
# Delete all resources
kubectl delete -f .

# Or delete namespace
kubectl delete namespace goverse
```

## Customization

### Change Replica Counts

Edit the manifests:

- `etcd.yaml`: Change `replicas: 3` to desired count (must be odd)
- `nodes.yaml`: Change `replicas: 3` to desired node count
- `gates.yaml`: Change `replicas: 2` to desired gate count

### Use External PostgreSQL

1. Comment out PostgreSQL deployment in `deploy.sh`
2. Update `secret.yaml` with external database credentials
3. Skip `kubectl apply -f postgres.yaml`

### Disable Inspector

Skip deploying inspector:

```bash
# Don't run: kubectl apply -f inspector.yaml
```

## Production Deployment

This example is suitable for development/testing. For production:

1. Use proper storage classes (SSD for etcd)
2. Configure resource limits appropriately
3. Enable TLS for all components
4. Set up monitoring and alerting
5. Configure backups for etcd and PostgreSQL
6. Use external managed database
7. Implement network policies
8. Configure Pod Disruption Budgets

See [../docs/KUBERNETES_DEPLOYMENT.md](../../docs/KUBERNETES_DEPLOYMENT.md) for production guidance.
