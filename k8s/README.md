# Goverse Kubernetes Deployment

This directory contains Kubernetes manifests for deploying Goverse clusters.

## Directory Structure

```
k8s/
├── base/           # Base configuration (namespace, configmap, secrets)
├── etcd/           # etcd cluster for coordination
├── nodes/          # Goverse node StatefulSet
├── gates/          # Goverse gate Deployment
├── postgres/       # PostgreSQL for persistence (optional)
├── inspector/      # Inspector UI for monitoring (optional)
└── examples/       # Complete deployment examples
```

## Quick Start

### Prerequisites

- Kubernetes cluster (1.25+)
- kubectl configured
- At least 3 worker nodes recommended for production

### Deploy Minimal Cluster

```bash
# 1. Create namespace and base configuration
kubectl apply -f base/

# 2. Deploy etcd cluster
kubectl apply -f etcd/

# 3. Wait for etcd to be ready
kubectl wait --for=condition=ready pod -l app=etcd -n goverse --timeout=300s

# 4. Deploy Goverse nodes
kubectl apply -f nodes/

# 5. Deploy Goverse gates
kubectl apply -f gates/

# 6. (Optional) Deploy PostgreSQL for persistence
kubectl apply -f postgres/

# 7. (Optional) Deploy Inspector UI
kubectl apply -f inspector/
```

### Verify Deployment

```bash
# Check all pods are running
kubectl get pods -n goverse

# Check services
kubectl get svc -n goverse

# Check StatefulSets
kubectl get statefulset -n goverse

# View logs from a node
kubectl logs goverse-node-0 -n goverse --tail=50

# View logs from a gate
kubectl logs -l app=goverse-gate -n goverse --tail=50
```

### Access Inspector UI

```bash
# Port-forward to access locally
kubectl port-forward -n goverse svc/inspector 8080:8080

# Open http://localhost:8080 in your browser
```

## Configuration

### ConfigMap

Edit `base/configmap.yaml` to customize cluster configuration:

- etcd endpoints
- shard count (default: 8192)
- cluster stability duration
- inspector settings

### Secrets

Update `base/secret.yaml` with secure credentials:

```bash
# Generate a secure password
POSTGRES_PASSWORD=$(openssl rand -base64 32)

# Update secret
kubectl create secret generic goverse-postgres-secret \
  --from-literal=POSTGRES_USER=goverse \
  --from-literal=POSTGRES_PASSWORD=$POSTGRES_PASSWORD \
  --from-literal=POSTGRES_DB=goverse \
  --from-literal=POSTGRES_HOST=postgres.goverse.svc.cluster.local \
  --from-literal=POSTGRES_PORT=5432 \
  -n goverse \
  --dry-run=client -o yaml | kubectl apply -f -
```

## Scaling

### Scale Nodes (Horizontal)

```bash
# Scale up to 5 nodes
kubectl scale statefulset goverse-node --replicas=5 -n goverse

# Scale down to 3 nodes (shards will rebalance)
kubectl scale statefulset goverse-node --replicas=3 -n goverse
```

### Scale Gates (Horizontal)

```bash
# Scale up gates for more client capacity
kubectl scale deployment goverse-gate --replicas=5 -n goverse
```

## Monitoring

### Pod Status

```bash
# Watch pod status
kubectl get pods -n goverse -w

# Check pod resource usage
kubectl top pods -n goverse
```

### Logs

```bash
# Stream logs from all gates
kubectl logs -f -l app=goverse-gate -n goverse

# Stream logs from all nodes
kubectl logs -f -l app=goverse-node -n goverse

# View logs from specific pod
kubectl logs goverse-node-0 -n goverse --tail=100 -f
```

### Events

```bash
# View recent events
kubectl get events -n goverse --sort-by='.lastTimestamp'
```

## Troubleshooting

### Nodes Not Starting

Check etcd connectivity:

```bash
# Test etcd from a node pod
kubectl exec -it goverse-node-0 -n goverse -- sh
# Inside pod (if curl is available):
curl http://etcd-0.etcd-headless.goverse.svc.cluster.local:2379/health
```

### Database Connection Issues

Verify PostgreSQL is running:

```bash
kubectl get pods -l app=postgres -n goverse
kubectl logs postgres-0 -n goverse
```

Test database connection:

```bash
kubectl exec -it postgres-0 -n goverse -- psql -U goverse -d goverse -c 'SELECT 1'
```

### Check etcd Cluster Health

```bash
kubectl exec -it etcd-0 -n goverse -- \
  etcdctl --endpoints=http://localhost:2379 endpoint health
```

## Cleanup

Remove the entire Goverse deployment:

```bash
# Delete all resources
kubectl delete -f inspector/
kubectl delete -f postgres/
kubectl delete -f gates/
kubectl delete -f nodes/
kubectl delete -f etcd/
kubectl delete -f base/

# Or delete the entire namespace
kubectl delete namespace goverse
```

## Production Considerations

### High Availability

1. **etcd**: Use 3 or 5 replicas
2. **Nodes**: Use at least 3 replicas
3. **Gates**: Use at least 2 replicas
4. **Multi-zone**: Spread pods across availability zones

### Storage

- Use SSD storage class for etcd (low latency required)
- Use appropriate storage class for PostgreSQL
- Consider using cloud-managed databases for PostgreSQL

### Security

1. Update secrets with strong passwords
2. Enable TLS for ingress
3. Configure network policies
4. Use RBAC for pod service accounts
5. Enable Pod Security Standards

### Monitoring

1. Deploy Prometheus for metrics collection
2. Create Grafana dashboards for visualization
3. Set up alerting for critical events
4. Use centralized logging (ELK, Loki, etc.)

## Advanced Deployments

See [docs/KUBERNETES_DEPLOYMENT.md](../docs/KUBERNETES_DEPLOYMENT.md) for:

- Helm chart deployment
- Multi-zone setup
- Network policies
- TLS/mTLS configuration
- Backup and restore procedures
- CI/CD integration
- Production best practices

## Support

For issues or questions:
- Check [Troubleshooting Guide](../docs/KUBERNETES_DEPLOYMENT.md#12-troubleshooting)
- File an issue on GitHub
- Consult the [main documentation](../docs/)
