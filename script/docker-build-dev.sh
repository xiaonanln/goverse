#!/bin/bash

set -euo pipefail

# Configuration
IMAGE_NAME="pulse-dev"
DOCKERFILE="docker/Dockerfile.dev"
BUILD_CONTEXT="."

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${YELLOW}Building Docker image: ${IMAGE_NAME}${NC}"

# Check if Dockerfile exists
if [[ ! -f "${DOCKERFILE}" ]]; then
    echo -e "${RED}Error: Dockerfile not found at ${DOCKERFILE}${NC}"
    exit 1
fi

# Build the image
if docker build -f "${DOCKERFILE}" -t "${IMAGE_NAME}" "${BUILD_CONTEXT}"; then
    echo -e "${GREEN}✓ Successfully built ${IMAGE_NAME}${NC}"
else
    echo -e "${RED}✗ Failed to build ${IMAGE_NAME}${NC}"
    exit 1
fi
