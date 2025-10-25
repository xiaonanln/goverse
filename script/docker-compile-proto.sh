#!/bin/bash

# Compile protobuf files using Docker
echo "Compiling protobuf files..."
docker run -it --rm -v "$(pwd):/app" pulse-dev ./script/compile-proto.sh

if [ $? -eq 0 ]; then
    echo "Protobuf compilation completed successfully."
else
    echo "Protobuf compilation failed with error code $?."
    exit $?
fi