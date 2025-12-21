#!/bin/bash
set -e

echo "================================================================"
echo "  Deploying Goverse Cluster to Kubernetes"
echo "================================================================"
echo ""

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

print_status() {
    echo -e "${GREEN}✓${NC} $1"
}

print_info() {
    echo -e "${YELLOW}ℹ${NC} $1"
}

# Check if kubectl is available
if ! command -v kubectl &> /dev/null; then
    echo "Error: kubectl not found. Please install kubectl first."
    exit 1
fi

# Check if cluster is accessible
if ! kubectl cluster-info &> /dev/null; then
    echo "Error: Cannot connect to Kubernetes cluster. Please check your kubeconfig."
    exit 1
fi

print_info "Starting deployment..."

# 1. Create namespace
print_info "Creating namespace..."
kubectl apply -f namespace.yaml
print_status "Namespace created"

# Wait for namespace
sleep 2

# 2. Create ConfigMap and Secret
print_info "Creating configuration..."
kubectl apply -f configmap.yaml
kubectl apply -f secret.yaml
print_status "Configuration created"

# 3. Deploy etcd
print_info "Deploying etcd cluster (this may take a few minutes)..."
kubectl apply -f etcd.yaml
print_status "etcd manifests applied"

# Wait for etcd to be ready
print_info "Waiting for etcd to be ready..."
kubectl wait --for=condition=ready pod -l app=etcd -n goverse --timeout=300s 2>/dev/null || {
    echo ""
    echo "Waiting for etcd pods to start..."
    sleep 30
    kubectl wait --for=condition=ready pod -l app=etcd -n goverse --timeout=300s
}
print_status "etcd cluster is ready"

# 4. Deploy PostgreSQL
print_info "Deploying PostgreSQL..."
kubectl apply -f postgres.yaml
print_status "PostgreSQL manifests applied"

# Wait for PostgreSQL to be ready
print_info "Waiting for PostgreSQL to be ready..."
kubectl wait --for=condition=ready pod -l app=postgres -n goverse --timeout=300s 2>/dev/null || {
    echo ""
    echo "Waiting for PostgreSQL pod to start..."
    sleep 20
    kubectl wait --for=condition=ready pod -l app=postgres -n goverse --timeout=300s
}
print_status "PostgreSQL is ready"

# 5. Deploy Goverse nodes
print_info "Deploying Goverse nodes..."
kubectl apply -f nodes.yaml
print_status "Node manifests applied"

# 6. Deploy Goverse gates
print_info "Deploying Goverse gates..."
kubectl apply -f gates.yaml
print_status "Gate manifests applied"

# 7. Deploy Inspector
print_info "Deploying Inspector UI..."
kubectl apply -f inspector.yaml
print_status "Inspector manifests applied"

# Wait for nodes and gates
print_info "Waiting for Goverse cluster to be ready..."
kubectl wait --for=condition=ready pod -l app=goverse-node -n goverse --timeout=300s 2>/dev/null || {
    echo ""
    echo "Waiting for node pods to start..."
    sleep 30
    kubectl wait --for=condition=ready pod -l app=goverse-node -n goverse --timeout=300s
}
print_status "Goverse nodes are ready"

kubectl wait --for=condition=ready pod -l app=goverse-gate -n goverse --timeout=300s 2>/dev/null || {
    echo ""
    echo "Waiting for gate pods to start..."
    sleep 20
    kubectl wait --for=condition=ready pod -l app=goverse-gate -n goverse --timeout=300s
}
print_status "Goverse gates are ready"

echo ""
echo "================================================================"
echo -e "${GREEN}✓ Goverse cluster deployed successfully!${NC}"
echo "================================================================"
echo ""
echo "Cluster components:"
kubectl get pods -n goverse -o wide
echo ""
echo "Services:"
kubectl get svc -n goverse
echo ""
echo "================================================================"
echo "Next steps:"
echo "================================================================"
echo ""
echo "1. Access Inspector UI:"
echo "   kubectl port-forward -n goverse svc/inspector 8080:8080"
echo "   Then open http://localhost:8080 in your browser"
echo ""
echo "2. View logs from a node:"
echo "   kubectl logs goverse-node-0 -n goverse --tail=50"
echo ""
echo "3. View logs from gates:"
echo "   kubectl logs -l app=goverse-gate -n goverse --tail=50"
echo ""
echo "4. Scale the cluster:"
echo "   kubectl scale statefulset goverse-node --replicas=5 -n goverse"
echo ""
echo "5. Clean up when done:"
echo "   kubectl delete namespace goverse"
echo ""
