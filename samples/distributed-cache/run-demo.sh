#!/bin/bash

# Simple script to demonstrate the distributed cache sample
# This script assumes etcd is already running on localhost:2379

set -e

echo "=== Distributed Cache Demo ==="
echo ""
echo "This demo will:"
echo "1. Start a cache server"
echo "2. Run the demo client"
echo ""
echo "Prerequisites:"
echo "- etcd should be running on localhost:2379"
echo "- Run: docker run -d -p 2379:2379 -p 2380:2380 --name etcd quay.io/coreos/etcd:latest"
echo "  OR: etcd (if installed locally)"
echo ""

# Check if etcd is running
if ! curl -s http://localhost:2379/health > /dev/null 2>&1; then
    echo "❌ Error: etcd is not running on localhost:2379"
    echo "Please start etcd first:"
    echo "  docker run -d -p 2379:2379 -p 2380:2380 --name etcd quay.io/coreos/etcd:latest"
    exit 1
fi

echo "✅ etcd is running"
echo ""

# Compile proto files first
echo "Compiling protocol buffers..."
cd ../../
./script/compile-proto.sh
cd samples/distributed-cache

echo ""
echo "Building cache server..."
cd server
go build -o cache-server .
cd ..

echo ""
echo "Building cache client..."
cd client
go build -o cache-client .
cd ..

echo ""
echo "Starting cache server in background..."
./server/cache-server -listen localhost:47000 -advertise localhost:47000 -client-listen localhost:48000 > server.log 2>&1 &
SERVER_PID=$!
echo "Server PID: $SERVER_PID"

# Wait for server to be ready
echo "Waiting for server to be ready..."
sleep 8

echo ""
echo "Running demo client..."
./client/cache-client -server localhost:48000

echo ""
echo "Demo complete!"
echo ""
echo "Cleaning up..."
kill $SERVER_PID 2>/dev/null || true
rm -f server.log

echo "✅ Done! Check the README.md for more information."
