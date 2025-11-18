#!/bin/bash
# Stop etcd and PostgreSQL Docker containers
# This script is reentrant - it can be run multiple times safely

set -euo pipefail

ETCD_CONTAINER_NAME="goverse-etcd"
POSTGRES_CONTAINER_NAME="goverse-postgres"

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo "Stopping backend services (etcd and PostgreSQL) in Docker..."

# Check if Docker is available
if ! command -v docker &> /dev/null; then
    echo -e "${RED}✗ Docker is not installed or not in PATH${NC}"
    exit 1
fi

# Stop etcd container
echo ""
echo "Stopping etcd container..."
if docker ps --format '{{.Names}}' | grep -q "^${ETCD_CONTAINER_NAME}$"; then
    docker stop ${ETCD_CONTAINER_NAME}
    echo -e "${GREEN}✓ etcd container stopped${NC}"
elif docker ps -a --format '{{.Names}}' | grep -q "^${ETCD_CONTAINER_NAME}$"; then
    echo -e "${YELLOW}etcd container is already stopped${NC}"
else
    echo -e "${YELLOW}etcd container does not exist${NC}"
fi

# Stop PostgreSQL container
echo ""
echo "Stopping PostgreSQL container..."
if docker ps --format '{{.Names}}' | grep -q "^${POSTGRES_CONTAINER_NAME}$"; then
    docker stop ${POSTGRES_CONTAINER_NAME}
    echo -e "${GREEN}✓ PostgreSQL container stopped${NC}"
elif docker ps -a --format '{{.Names}}' | grep -q "^${POSTGRES_CONTAINER_NAME}$"; then
    echo -e "${YELLOW}PostgreSQL container is already stopped${NC}"
else
    echo -e "${YELLOW}PostgreSQL container does not exist${NC}"
fi

echo ""
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}✓ Backend services stopped${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""
echo "To remove the containers completely, run:"
echo "  docker rm ${ETCD_CONTAINER_NAME} ${POSTGRES_CONTAINER_NAME}"
echo ""
echo "To start the services again, run:"
echo "  ./script/docker/start-backend.sh"
echo ""
