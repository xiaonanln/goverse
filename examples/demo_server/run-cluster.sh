#!/bin/bash
# Script to run the demo cluster with nodes, gates, and inspector
#
# Prerequisites:
# - etcd must be running on localhost:2379
#
# This script will:
# - Build the demo server
# - Start the inspector
# - Start 10 node servers
# - Start 5 gate servers

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CONFIG_FILE="$SCRIPT_DIR/demo-cluster.yml"
NODE_LOG="$SCRIPT_DIR/node.log"
GATE_LOG="$SCRIPT_DIR/gate.log"

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

# Check if etcd is running
check_etcd() {
    log_section "Checking etcd"
    
    if nc -z 127.0.0.1 2379 2>/dev/null; then
        log_info "etcd is running on port 2379"
        return 0
    else
        log_error "etcd is not running on port 2379"
        log_error "Please start etcd first. Example:"
        echo "  docker run -d --name etcd-demo -p 2379:2379 quay.io/coreos/etcd:latest \\"
        echo "    /usr/local/bin/etcd --listen-client-urls http://0.0.0.0:2379 \\"
        echo "    --advertise-client-urls http://localhost:2379"
        return 1
    fi
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

# Build inspector if source is available
build_inspector() {
    local inspector_dir="$SCRIPT_DIR/../../cmd/inspector"
    if [ -d "$inspector_dir" ]; then
        log_info "Building inspector..."
        cd "$inspector_dir"
        if go build -o "$SCRIPT_DIR/goverse-inspector" main.go; then
            log_info "Inspector built successfully"
        else
            log_warn "Failed to build inspector, will skip if not available"
        fi
        cd "$SCRIPT_DIR"
    fi
}

# Build gate if source is available
build_gate() {
    local gate_dir="$SCRIPT_DIR/../../cmd/gate"
    if [ -d "$gate_dir" ]; then
        log_info "Building gate..."
        cd "$gate_dir"
        if go build -o "$SCRIPT_DIR/goverse-gate" main.go; then
            log_info "Gate built successfully"
        else
            log_warn "Failed to build gate, will skip if not available"
        fi
        cd "$SCRIPT_DIR"
    fi
}

# Start a node
start_node() {
    local node_id=$1
    log_info "Starting $node_id..."
    
    "$SCRIPT_DIR/demo-server" --config "$CONFIG_FILE" --node-id "$node_id" \
        >> "$NODE_LOG" 2>&1 &
    
    local pid=$!
    echo "$pid" >> "$SCRIPT_DIR/.node-pids"
    log_info "$node_id started (PID: $pid)"
}

# Start a gate
start_gate() {
    local gate_id=$1
    log_info "Starting $gate_id..."
    
    # Try using locally built gate first, then check PATH
    local gate_cmd="$SCRIPT_DIR/goverse-gate"
    if [ ! -x "$gate_cmd" ]; then
        if command -v goverse-gate &> /dev/null; then
            gate_cmd="goverse-gate"
        else
            log_warn "goverse-gate not found, skipping gate startup"
            return
        fi
    fi
    
    "$gate_cmd" --config "$CONFIG_FILE" --node-id "$gate_id" \
        >> "$GATE_LOG" 2>&1 &
    
    local pid=$!
    echo "$pid" >> "$SCRIPT_DIR/.gate-pids"
    log_info "$gate_id started (PID: $pid)"
}

# Start inspector
start_inspector() {
    log_info "Starting inspector..."
    
    # Try using locally built inspector first, then check PATH
    local inspector_cmd="$SCRIPT_DIR/goverse-inspector"
    if [ ! -x "$inspector_cmd" ]; then
        if command -v goverse-inspector &> /dev/null; then
            inspector_cmd="goverse-inspector"
        else
            log_warn "goverse-inspector not found, skipping inspector startup"
            return
        fi
    fi
    
    "$inspector_cmd" --config "$CONFIG_FILE" \
        >> "$SCRIPT_DIR/inspector.log" 2>&1 &
    
    local pid=$!
    echo "$pid" > "$SCRIPT_DIR/.inspector-pid"
    log_info "Inspector started (PID: $pid)"
    log_info "Inspector UI available at: http://localhost:48200"
}

# Main execution
main() {
    log_section "Goverse Demo Server Cluster"
    
    # Check etcd
    check_etcd || exit 1
    
    # Clean up old log files and PID files
    rm -f "$NODE_LOG" "$GATE_LOG" "$SCRIPT_DIR/inspector.log"
    rm -f "$SCRIPT_DIR/.node-pids" "$SCRIPT_DIR/.gate-pids" "$SCRIPT_DIR/.inspector-pid"
    
    # Build components
    build_demo || exit 1
    build_inspector
    build_gate
    
    # Start inspector first
    log_section "Starting Inspector"
    start_inspector
    sleep 2
    
    # Start nodes
    log_section "Starting Nodes"
    for i in {1..10}; do
        start_node "node-$i"
        sleep 1
    done
    
    # Wait for nodes to initialize
    log_info "Waiting for nodes to initialize (15 seconds)..."
    sleep 15
    
    # Start gates
    log_section "Starting Gates"
    for i in {1..5}; do
        start_gate "gate-$i"
        sleep 1
    done
    
    # Wait for cluster to stabilize
    log_info "Waiting for cluster to stabilize (10 seconds)..."
    sleep 10
    
    log_section "Cluster Status"
    log_info "All components started successfully!"
    log_info ""
    log_info "Nodes (10):"
    for i in {1..10}; do
        local port=$((47000 + i))
        local http_port=$((48000 + i))
        log_info "  - node-$i: localhost:$port (HTTP: $http_port)"
    done
    log_info ""
    log_info "Gates (5):"
    for i in {1..5}; do
        local port=$((47100 + i))
        local http_port=$((48100 + i))
        log_info "  - gate-$i: localhost:$port (HTTP: $http_port)"
    done
    log_info ""
    log_info "Inspector: http://localhost:48200"
    log_info ""
    log_info "Logs:"
    log_info "  - All nodes: $NODE_LOG"
    log_info "  - All gates: $GATE_LOG"
    log_info "  - Inspector: $SCRIPT_DIR/inspector.log"
    log_info ""
    log_info "Use ./stop-cluster.sh to stop all processes"
}

main
