#!/bin/bash
# Script to stop the demo cluster

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

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

log_section() {
    echo -e "\n${BLUE}==== $1 ====${NC}\n"
}

# Stop processes from PID file
stop_processes() {
    local pid_file=$1
    local name=$2
    
    if [ ! -f "$pid_file" ]; then
        log_warn "No $name PID file found"
        return
    fi
    
    log_info "Stopping $name processes..."
    while IFS= read -r pid; do
        if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
            log_info "Killing $name process $pid"
            kill "$pid" 2>/dev/null || true
        fi
    done < "$pid_file"
    
    # Wait a moment for graceful shutdown
    sleep 2
    
    # Force kill any remaining processes
    while IFS= read -r pid; do
        if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
            log_warn "Force killing $name process $pid"
            kill -9 "$pid" 2>/dev/null || true
        fi
    done < "$pid_file"
    
    rm -f "$pid_file"
}

# Main execution
main() {
    log_section "Stopping Goverse Demo Cluster"
    
    cd "$SCRIPT_DIR"
    
    # Stop gates
    stop_processes ".gate-pids" "gate"
    
    # Stop nodes
    stop_processes ".node-pids" "node"
    
    # Stop inspector
    if [ -f ".inspector-pid" ]; then
        log_info "Stopping inspector..."
        local pid=$(cat ".inspector-pid")
        if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
            kill "$pid" 2>/dev/null || true
            sleep 1
            if kill -0 "$pid" 2>/dev/null; then
                kill -9 "$pid" 2>/dev/null || true
            fi
        fi
        rm -f ".inspector-pid"
    fi
    
    log_info ""
    log_info "All processes stopped"
}

main
