#!/bin/bash
# Start etcd and PostgreSQL as Docker containers with ports exposed to localhost
# This script is reentrant - it can be run multiple times safely

set -euo pipefail

ETCD_CONTAINER_NAME="goverse-etcd"
POSTGRES_CONTAINER_NAME="goverse-postgres"

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo "Starting backend services (etcd and PostgreSQL) in Docker..."

# Check if Docker is available
if ! command -v docker &> /dev/null; then
    echo -e "${RED}✗ Docker is not installed or not in PATH${NC}"
    exit 1
fi

# Start etcd container
echo ""
echo "Starting etcd container..."
if docker ps -a --format '{{.Names}}' | grep -q "^${ETCD_CONTAINER_NAME}$"; then
    # Container exists, check if it's running
    if docker ps --format '{{.Names}}' | grep -q "^${ETCD_CONTAINER_NAME}$"; then
        echo -e "${GREEN}✓ etcd container is already running${NC}"
    else
        # Container exists but not running, start it
        echo -e "${YELLOW}etcd container exists but is stopped, starting...${NC}"
        docker start ${ETCD_CONTAINER_NAME}
        echo -e "${GREEN}✓ etcd container started${NC}"
    fi
else
    # Container doesn't exist, create and start it
    docker run -d \
        --name ${ETCD_CONTAINER_NAME} \
        -p 2379:2379 \
        -p 2380:2380 \
        quay.io/coreos/etcd:v3.5.10 \
        /usr/local/bin/etcd \
        --listen-client-urls http://0.0.0.0:2379 \
        --advertise-client-urls http://0.0.0.0:2379
    
    echo -e "${GREEN}✓ etcd container created and started${NC}"
fi

# Wait for etcd to be ready
echo "Waiting for etcd to be ready..."
for i in {1..30}; do
    if curl -s http://localhost:2379/health > /dev/null 2>&1; then
        echo -e "${GREEN}✓ etcd is ready and responding${NC}"
        break
    fi
    if [ $i -eq 30 ]; then
        echo -e "${RED}✗ etcd failed to become ready within 30 seconds${NC}"
        echo "Container logs:"
        docker logs ${ETCD_CONTAINER_NAME}
        exit 1
    fi
    sleep 1
done

# Start PostgreSQL container
echo ""
echo "Starting PostgreSQL container..."
if docker ps -a --format '{{.Names}}' | grep -q "^${POSTGRES_CONTAINER_NAME}$"; then
    # Container exists, check if it's running
    if docker ps --format '{{.Names}}' | grep -q "^${POSTGRES_CONTAINER_NAME}$"; then
        echo -e "${GREEN}✓ PostgreSQL container is already running${NC}"
    else
        # Container exists but not running, start it
        echo -e "${YELLOW}PostgreSQL container exists but is stopped, starting...${NC}"
        docker start ${POSTGRES_CONTAINER_NAME}
        echo -e "${GREEN}✓ PostgreSQL container started${NC}"
    fi
else
    # Container doesn't exist, create and start it
    docker run -d \
        --name ${POSTGRES_CONTAINER_NAME} \
        -p 5432:5432 \
        -e POSTGRES_USER=postgres \
        -e POSTGRES_PASSWORD=postgres \
        -e POSTGRES_DB=goverse \
        postgres:15-alpine
    
    echo -e "${GREEN}✓ PostgreSQL container created and started${NC}"
fi

# Wait for PostgreSQL to be ready
echo "Waiting for PostgreSQL to be ready..."
for i in {1..30}; do
    if docker exec ${POSTGRES_CONTAINER_NAME} pg_isready -U postgres > /dev/null 2>&1; then
        echo -e "${GREEN}✓ PostgreSQL is ready and accepting connections${NC}"
        break
    fi
    if [ $i -eq 30 ]; then
        echo -e "${RED}✗ PostgreSQL failed to become ready within 30 seconds${NC}"
        echo "Container logs:"
        docker logs ${POSTGRES_CONTAINER_NAME}
        exit 1
    fi
    sleep 1
done

echo ""
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}✓ Backend services are ready!${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""
echo "Service endpoints:"
echo "  etcd:       http://localhost:2379"
echo "  PostgreSQL: postgresql://postgres:postgres@localhost:5432/goverse"
echo ""
echo "Container names:"
echo "  etcd:       ${ETCD_CONTAINER_NAME}"
echo "  PostgreSQL: ${POSTGRES_CONTAINER_NAME}"
echo ""
echo "To stop the services, run:"
echo "  ./script/docker/stop-backend.sh"
echo ""
