package main

import (
	"encoding/base64"
	"fmt"
	"log"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// This example demonstrates how to encode protobuf messages for HTTP requests
// to the Goverse HTTP Gate.
//
// For a full working example with a running server, see the integration tests
// in gate/gateserver/http_handler_test.go

func main() {
	log.Println("Goverse HTTP Gate - Request Encoding Example")
	log.Println("==============================================")
	log.Println("")
	log.Println("This example shows how to encode protobuf messages for HTTP requests.")
	log.Println("")

	// Example 1: Create Object
	log.Println("1. Create Object Request:")
	log.Println("   curl -X POST http://localhost:8080/api/v1/objects/create/Counter/my-counter")
	log.Println("")

	// Example 2: Call Object Method with Int32 argument
	log.Println("2. Call Object Method (with Int32 argument):")
	encodeAndPrintRequest(&wrapperspb.Int32Value{Value: 5}, "Counter", "my-counter", "Increment")
	log.Println("")

	// Example 3: Call Object Method with String argument
	log.Println("3. Call Object Method (with String argument):")
	encodeAndPrintRequest(&wrapperspb.StringValue{Value: "hello"}, "MyObject", "obj-1", "Echo")
	log.Println("")

	// Example 4: Delete Object
	log.Println("4. Delete Object Request:")
	log.Println("   curl -X POST http://localhost:8080/api/v1/objects/delete/my-counter")
	log.Println("")

	log.Println("Response Format:")
	log.Println("================")
	log.Println("")
	log.Println("Success Response (CallObject):")
	log.Println(`  {"response":"<base64-encoded protobuf Any>"}`)
	log.Println("")
	log.Println("Success Response (CreateObject):")
	log.Println(`  {"id":"object-id"}`)
	log.Println("")
	log.Println("Success Response (DeleteObject):")
	log.Println(`  {"success":true}`)
	log.Println("")
	log.Println("Error Response:")
	log.Println(`  {"error":"error message","code":"ERROR_CODE"}`)
	log.Println("")
}

func encodeAndPrintRequest(reqMsg proto.Message, objType, objID, method string) {
	// Step 1: Wrap message in google.protobuf.Any
	anyReq, err := anypb.New(reqMsg)
	if err != nil {
		log.Fatalf("Failed to create Any: %v", err)
	}

	// Step 2: Marshal to bytes
	reqBytes, err := proto.Marshal(anyReq)
	if err != nil {
		log.Fatalf("Failed to marshal: %v", err)
	}

	// Step 3: Base64 encode
	encodedReq := base64.StdEncoding.EncodeToString(reqBytes)

	// Print curl command
	url := fmt.Sprintf("http://localhost:8080/api/v1/objects/call/%s/%s/%s", objType, objID, method)
	log.Printf("   curl -X POST %s \\", url)
	log.Println(`     -H "Content-Type: application/json" \`)
	log.Printf(`     -d '{"request":"%s"}'`, encodedReq)
	log.Println("")
}
