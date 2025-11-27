#!/bin/bash
# Helper script for Counter operations
# 
# Usage:
#   ./counter.sh create <name>
#   ./counter.sh get <name>
#   ./counter.sh increment <name> <amount>
#   ./counter.sh decrement <name> <amount>
#   ./counter.sh reset <name>
#   ./counter.sh delete <name>

set -e

BASE_URL="${COUNTER_API_URL:-http://localhost:48000/api/v1/objects}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Check if protoc and go are available for encoding
if ! command -v protoc &> /dev/null; then
    echo "Warning: protoc not found. Install protobuf-compiler for full functionality."
fi

# Function to encode a protobuf message to base64
# Uses a Go helper for proper protobuf encoding
encode_proto() {
    local message_type="$1"
    local field_value="$2"
    
    # Create a temporary Go file to encode the message
    local tmp_dir=$(mktemp -d)
    local tmp_file="$tmp_dir/encode.go"
    
    cat > "$tmp_file" << 'EOF'
package main

import (
    "encoding/base64"
    "fmt"
    "os"
    "strconv"

    "google.golang.org/protobuf/proto"
    "google.golang.org/protobuf/types/known/anypb"
    pb "github.com/xiaonanln/goverse/samples/counter/proto"
)

func main() {
    if len(os.Args) < 2 {
        fmt.Fprintln(os.Stderr, "Usage: encode <message_type> [amount]")
        os.Exit(1)
    }
    
    messageType := os.Args[1]
    var msg proto.Message
    
    switch messageType {
    case "IncrementRequest":
        amount := int32(1)
        if len(os.Args) > 2 {
            val, _ := strconv.Atoi(os.Args[2])
            amount = int32(val)
        }
        msg = &pb.IncrementRequest{Amount: amount}
    case "DecrementRequest":
        amount := int32(1)
        if len(os.Args) > 2 {
            val, _ := strconv.Atoi(os.Args[2])
            amount = int32(val)
        }
        msg = &pb.DecrementRequest{Amount: amount}
    case "GetRequest":
        msg = &pb.GetRequest{}
    case "ResetRequest":
        msg = &pb.ResetRequest{}
    default:
        fmt.Fprintf(os.Stderr, "Unknown message type: %s\n", messageType)
        os.Exit(1)
    }
    
    anyMsg, err := anypb.New(msg)
    if err != nil {
        fmt.Fprintf(os.Stderr, "Failed to create Any: %v\n", err)
        os.Exit(1)
    }
    
    data, err := proto.Marshal(anyMsg)
    if err != nil {
        fmt.Fprintf(os.Stderr, "Failed to marshal: %v\n", err)
        os.Exit(1)
    }
    
    fmt.Print(base64.StdEncoding.EncodeToString(data))
}
EOF

    # Run the Go encoder
    cd "$SCRIPT_DIR" && go run "$tmp_file" "$message_type" "$field_value" 2>/dev/null
    rm -rf "$tmp_dir"
}

# Function to decode a protobuf response from base64
decode_proto() {
    local base64_data="$1"
    
    # Create a temporary Go file to decode the message
    local tmp_dir=$(mktemp -d)
    local tmp_file="$tmp_dir/decode.go"
    
    cat > "$tmp_file" << 'EOF'
package main

import (
    "encoding/base64"
    "fmt"
    "os"

    "google.golang.org/protobuf/proto"
    "google.golang.org/protobuf/types/known/anypb"
    pb "github.com/xiaonanln/goverse/samples/counter/proto"
)

func main() {
    if len(os.Args) < 2 {
        fmt.Fprintln(os.Stderr, "Usage: decode <base64_data>")
        os.Exit(1)
    }
    
    data, err := base64.StdEncoding.DecodeString(os.Args[1])
    if err != nil {
        fmt.Fprintf(os.Stderr, "Failed to decode base64: %v\n", err)
        os.Exit(1)
    }
    
    anyMsg := &anypb.Any{}
    if err := proto.Unmarshal(data, anyMsg); err != nil {
        fmt.Fprintf(os.Stderr, "Failed to unmarshal Any: %v\n", err)
        os.Exit(1)
    }
    
    resp := &pb.CounterResponse{}
    if err := anyMsg.UnmarshalTo(resp); err != nil {
        fmt.Fprintf(os.Stderr, "Failed to unmarshal CounterResponse: %v\n", err)
        os.Exit(1)
    }
    
    fmt.Printf("Counter %s = %d\n", resp.Name, resp.Value)
}
EOF

    # Run the Go decoder
    cd "$SCRIPT_DIR" && go run "$tmp_file" "$base64_data" 2>/dev/null
    rm -rf "$tmp_dir"
}

# Show usage
usage() {
    echo "Usage: $0 <command> <name> [amount]"
    echo ""
    echo "Commands:"
    echo "  create <name>             Create a new counter"
    echo "  get <name>                Get the current counter value"
    echo "  increment <name> <amount> Increment the counter by amount"
    echo "  decrement <name> <amount> Decrement the counter by amount"
    echo "  reset <name>              Reset the counter to zero"
    echo "  delete <name>             Delete the counter"
    echo ""
    echo "Environment variables:"
    echo "  COUNTER_API_URL           Base URL for the API (default: http://localhost:48000/api/v1/objects)"
    exit 1
}

# Check arguments
if [ $# -lt 2 ]; then
    usage
fi

COMMAND="$1"
NAME="$2"
AMOUNT="${3:-1}"

# Execute command
case "$COMMAND" in
    create)
        echo "Creating counter: Counter-$NAME..."
        RESPONSE=$(curl -s -X POST "$BASE_URL/create/Counter/Counter-$NAME" \
            -H "Content-Type: application/json" \
            -d '{"request":""}')
        
        # Check for error
        if echo "$RESPONSE" | grep -q '"error"'; then
            echo "Error: $(echo "$RESPONSE" | grep -o '"error":"[^"]*"' | cut -d'"' -f4)"
            exit 1
        fi
        
        echo "Created counter: $(echo "$RESPONSE" | grep -o '"id":"[^"]*"' | cut -d'"' -f4)"
        ;;
    
    get)
        echo "Getting counter: Counter-$NAME..."
        REQUEST=$(encode_proto "GetRequest")
        RESPONSE=$(curl -s -X POST "$BASE_URL/call/Counter/Counter-$NAME/Get" \
            -H "Content-Type: application/json" \
            -d "{\"request\":\"$REQUEST\"}")
        
        # Check for error
        if echo "$RESPONSE" | grep -q '"error"'; then
            echo "Error: $(echo "$RESPONSE" | grep -o '"error":"[^"]*"' | cut -d'"' -f4)"
            exit 1
        fi
        
        # Extract and decode response
        ENCODED_RESP=$(echo "$RESPONSE" | grep -o '"response":"[^"]*"' | cut -d'"' -f4)
        decode_proto "$ENCODED_RESP"
        ;;
    
    increment)
        if [ $# -lt 3 ]; then
            echo "Error: increment requires an amount"
            usage
        fi
        echo "Incrementing counter: Counter-$NAME by $AMOUNT..."
        REQUEST=$(encode_proto "IncrementRequest" "$AMOUNT")
        RESPONSE=$(curl -s -X POST "$BASE_URL/call/Counter/Counter-$NAME/Increment" \
            -H "Content-Type: application/json" \
            -d "{\"request\":\"$REQUEST\"}")
        
        # Check for error
        if echo "$RESPONSE" | grep -q '"error"'; then
            echo "Error: $(echo "$RESPONSE" | grep -o '"error":"[^"]*"' | cut -d'"' -f4)"
            exit 1
        fi
        
        # Extract and decode response
        ENCODED_RESP=$(echo "$RESPONSE" | grep -o '"response":"[^"]*"' | cut -d'"' -f4)
        decode_proto "$ENCODED_RESP"
        ;;
    
    decrement)
        if [ $# -lt 3 ]; then
            echo "Error: decrement requires an amount"
            usage
        fi
        echo "Decrementing counter: Counter-$NAME by $AMOUNT..."
        REQUEST=$(encode_proto "DecrementRequest" "$AMOUNT")
        RESPONSE=$(curl -s -X POST "$BASE_URL/call/Counter/Counter-$NAME/Decrement" \
            -H "Content-Type: application/json" \
            -d "{\"request\":\"$REQUEST\"}")
        
        # Check for error
        if echo "$RESPONSE" | grep -q '"error"'; then
            echo "Error: $(echo "$RESPONSE" | grep -o '"error":"[^"]*"' | cut -d'"' -f4)"
            exit 1
        fi
        
        # Extract and decode response
        ENCODED_RESP=$(echo "$RESPONSE" | grep -o '"response":"[^"]*"' | cut -d'"' -f4)
        decode_proto "$ENCODED_RESP"
        ;;
    
    reset)
        echo "Resetting counter: Counter-$NAME..."
        REQUEST=$(encode_proto "ResetRequest")
        RESPONSE=$(curl -s -X POST "$BASE_URL/call/Counter/Counter-$NAME/Reset" \
            -H "Content-Type: application/json" \
            -d "{\"request\":\"$REQUEST\"}")
        
        # Check for error
        if echo "$RESPONSE" | grep -q '"error"'; then
            echo "Error: $(echo "$RESPONSE" | grep -o '"error":"[^"]*"' | cut -d'"' -f4)"
            exit 1
        fi
        
        # Extract and decode response
        ENCODED_RESP=$(echo "$RESPONSE" | grep -o '"response":"[^"]*"' | cut -d'"' -f4)
        decode_proto "$ENCODED_RESP"
        ;;
    
    delete)
        echo "Deleting counter: Counter-$NAME..."
        RESPONSE=$(curl -s -X POST "$BASE_URL/delete/Counter-$NAME" \
            -H "Content-Type: application/json")
        
        # Check for error
        if echo "$RESPONSE" | grep -q '"error"'; then
            echo "Error: $(echo "$RESPONSE" | grep -o '"error":"[^"]*"' | cut -d'"' -f4)"
            exit 1
        fi
        
        echo "Deleted counter: Counter-$NAME"
        ;;
    
    *)
        echo "Unknown command: $COMMAND"
        usage
        ;;
esac
