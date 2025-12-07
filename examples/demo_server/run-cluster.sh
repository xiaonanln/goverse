#!/bin/bash
# Script to run the demo cluster with etcd, nodes, gates, and inspector
#
# This script demonstrates:
# - Starting etcd
# - Starting multiple node servers
# - Starting gate servers
# - Starting the inspector
# - Simulating node failures and shard rebalancing

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CONFIG_FILE="$SCRIPT_DIR/demo-cluster.yml"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

log_section() {
    echo -e "\n${BLUE}==== $1 ====${NC}\n"
}

# Track PIDs for cleanup
declare -a PIDS=()

cleanup() {
    log_section "Cleaning Up"
    
    if [ ${#PIDS[@]} -gt 0 ]; then
        log_info "Stopping all processes..."
        for pid in "${PIDS[@]}"; do
            if kill -0 "$pid" 2>/dev/null; then
                log_info "Killing process $pid"
                kill "$pid" 2>/dev/null || true
            fi
        done
        sleep 2
        
        # Force kill any remaining processes
        for pid in "${PIDS[@]}"; do
            if kill -0 "$pid" 2>/dev/null; then
                log_warn "Force killing process $pid"
                kill -9 "$pid" 2>/dev/null || true
            fi
        done
    fi
    
    # Clean up etcd data if running locally
    if [ -d "/tmp/etcd-demo-data" ]; then
        log_info "Cleaning up etcd data directory"
        rm -rf /tmp/etcd-demo-data
    fi
    
    log_info "Cleanup complete"
}

trap cleanup EXIT INT TERM

# Check if etcd is already running
check_etcd() {
    log_section "Checking etcd"
    
    if nc -z 127.0.0.1 2379 2>/dev/null; then
        log_info "etcd is already running on port 2379"
        return 0
    else
        log_warn "etcd is not running on port 2379"
        return 1
    fi
}

# Start etcd if needed
start_etcd() {
    if check_etcd; then
        log_info "Using existing etcd instance"
        return 0
    fi
    
    log_section "Starting etcd"
    
    # Check if etcd is installed
    if ! command -v etcd &> /dev/null; then
        log_error "etcd is not installed. Please install etcd or start it via Docker:"
        echo "  docker run -d --name etcd-demo -p 2379:2379 quay.io/coreos/etcd:latest \\"
        echo "    /usr/local/bin/etcd --listen-client-urls http://0.0.0.0:2379 \\"
        echo "    --advertise-client-urls http://localhost:2379"
        return 1
    fi
    
    # Start etcd in background
    rm -rf /tmp/etcd-demo-data
    mkdir -p /tmp/etcd-demo-data
    
    etcd --data-dir /tmp/etcd-demo-data \
         --listen-client-urls http://0.0.0.0:2379 \
         --advertise-client-urls http://localhost:2379 \
         --listen-peer-urls http://0.0.0.0:2380 \
         --initial-advertise-peer-urls http://localhost:2380 \
         --initial-cluster-token demo-cluster \
         --initial-cluster-state new \
         &> /tmp/etcd-demo.log &
    
    local etcd_pid=$!
    PIDS+=($etcd_pid)
    
    log_info "etcd started (PID: $etcd_pid)"
    
    # Wait for etcd to be ready
    log_info "Waiting for etcd to be ready..."
    for i in {1..30}; do
        if nc -z 127.0.0.1 2379 2>/dev/null; then
            log_info "etcd is ready"
            return 0
        fi
        sleep 1
    done
    
    log_error "etcd failed to start within 30 seconds"
    return 1
}

# Build the demo server
build_demo() {
    log_section "Building Demo Server"
    
    cd "$SCRIPT_DIR"
    if go build -o demo-server main.go; then
        log_info "Demo server built successfully"
    else
        log_error "Failed to build demo server"
        return 1
    fi
}

# Start a node
start_node() {
    local node_id=$1
    log_info "Starting $node_id..."
    
    "$SCRIPT_DIR/demo-server" --config "$CONFIG_FILE" --node-id "$node_id" \
        &> "/tmp/demo-$node_id.log" &
    
    local pid=$!
    PIDS+=($pid)
    log_info "$node_id started (PID: $pid)"
}

# Start a gate
start_gate() {
    local gate_id=$1
    log_info "Starting $gate_id..."
    
    # Use goverse-gate if available, otherwise skip
    if command -v goverse-gate &> /dev/null; then
        goverse-gate --config "$CONFIG_FILE" --node-id "$gate_id" \
            &> "/tmp/demo-$gate_id.log" &
        
        local pid=$!
        PIDS+=($pid)
        log_info "$gate_id started (PID: $pid)"
    else
        log_warn "goverse-gate not found, skipping gate startup"
        log_info "Gates can be started manually with: goverse-gate --config $CONFIG_FILE --node-id $gate_id"
    fi
}

# Start inspector
start_inspector() {
    log_info "Starting inspector..."
    
    # Use goverse-inspector if available, otherwise skip
    if command -v goverse-inspector &> /dev/null; then
        goverse-inspector --config "$CONFIG_FILE" \
            &> "/tmp/demo-inspector.log" &
        
        local pid=$!
        PIDS+=($pid)
        log_info "Inspector started (PID: $pid)"
        log_info "Inspector UI available at: http://localhost:48200"
    else
        log_warn "goverse-inspector not found, skipping inspector startup"
        log_info "Inspector can be started manually"
    fi
}

# Main execution
main() {
    log_section "Goverse Demo Server Cluster"
    
    # Start etcd
    start_etcd || exit 1
    
    # Build demo server
    build_demo || exit 1
    
    # Start nodes
    log_section "Starting Nodes"
    start_node "node-1"
    sleep 2
    start_node "node-2"
    sleep 2
    start_node "node-3"
    
    # Wait for nodes to initialize
    log_info "Waiting for nodes to initialize (10 seconds)..."
    sleep 10
    
    # Start gates
    log_section "Starting Gates"
    start_gate "gate-1"
    sleep 1
    start_gate "gate-2"
    
    # Start inspector
    log_section "Starting Inspector"
    start_inspector
    
    # Wait for cluster to stabilize
    log_info "Waiting for cluster to stabilize (15 seconds)..."
    sleep 15
    
    log_section "Cluster Status"
    log_info "All components started successfully!"
    log_info ""
    log_info "Nodes:"
    log_info "  - node-1: localhost:47001 (HTTP: 48001)"
    log_info "  - node-2: localhost:47002 (HTTP: 48002)"
    log_info "  - node-3: localhost:47003 (HTTP: 48003)"
    log_info ""
    log_info "Gates:"
    log_info "  - gate-1: localhost:47101 (HTTP: 48101)"
    log_info "  - gate-2: localhost:47102 (HTTP: 48102)"
    log_info ""
    log_info "Inspector: http://localhost:48200"
    log_info ""
    log_info "Logs are available in /tmp/demo-*.log"
    log_info ""
    log_info "Demo features:"
    log_info "  - Auto-load objects created (ShardMonitor, NodeMonitor, GlobalMonitor)"
    log_info "  - NodeMonitors will periodically create SimpleCounter objects"
    log_info "  - Counters are distributed across shards"
    log_info ""
    log_info "Press Ctrl+C to stop all processes"
    
    # Wait indefinitely
    while true; do
        sleep 1
    done
}

main
