package object

import (
	"context"
	"reflect"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"
)

// TestObject is a simple test implementation of the Object interface
type TestObject struct {
	BaseObject
}

func (t *TestObject) OnCreated() {
	// Simple test implementation
}

func TestBaseObject_OnInit(t *testing.T) {
	obj := &TestObject{}
	obj.OnInit(obj, "")

	// Test that ID is generated when empty
	if obj.Id() == "" {
		t.Fatal("OnInit should generate an ID when empty string is provided")
	}

	// Test with explicit ID
	obj2 := &TestObject{}
	obj2.OnInit(obj2, "test-id-123")
	if obj2.Id() != "test-id-123" {
		t.Fatalf("OnInit should use provided ID, got %s, want test-id-123", obj2.Id())
	}
}

func TestBaseObject_Id(t *testing.T) {
	obj := &TestObject{}
	obj.OnInit(obj, "my-unique-id")

	if got := obj.Id(); got != "my-unique-id" {
		t.Fatalf("Id() = %s; want my-unique-id", got)
	}
}

func TestBaseObject_Type(t *testing.T) {
	obj := &TestObject{}
	obj.OnInit(obj, "test-id")

	if got := obj.Type(); got != "TestObject" {
		t.Fatalf("Type() = %s; want TestObject", got)
	}
}

func TestBaseObject_String(t *testing.T) {
	obj := &TestObject{}
	obj.OnInit(obj, "test-id")

	expected := "TestObject(test-id)"
	if got := obj.String(); got != expected {
		t.Fatalf("String() = %s; want %s", got, expected)
	}
}

func TestBaseObject_LoggerInitialization(t *testing.T) {
	obj := &TestObject{}
	obj.OnInit(obj, "test-id")

	if obj.Logger == nil {
		t.Fatal("Logger should be initialized after OnInit")
	}
}

func TestBaseObject_OnInitWithProtoMessage(t *testing.T) {
	obj := &TestObject{}
	obj.OnInit(obj, "test-id")

	// Should not panic and should initialize properly
	if obj.Id() != "test-id" {
		t.Fatalf("OnInit with proto message failed, got ID %s, want test-id", obj.Id())
	}
}

func TestBaseObject_UniqueIDs(t *testing.T) {
	// Test that multiple objects get unique IDs when no ID is provided
	obj1 := &TestObject{}
	obj1.OnInit(obj1, "")

	obj2 := &TestObject{}
	obj2.OnInit(obj2, "")

	if obj1.Id() == obj2.Id() {
		t.Fatal("Different objects should get different IDs when no ID is provided")
	}
}

func TestObjectInterface(t *testing.T) {
	// Test that TestObject implements the Object interface
	var _ Object = (*TestObject)(nil)
}

// TestProtoMessageNil verifies that OnInit doesn't require any data parameter
func TestBaseObject_OnInitWithNilProtoMessage(t *testing.T) {
	obj := &TestObject{}
	obj.OnInit(obj, "test-id")

	// Should not panic
	if obj.Id() != "test-id" {
		t.Fatalf("OnInit failed, got ID %s, want test-id", obj.Id())
	}
}

func TestBaseObject_CreationTime(t *testing.T) {
	before := time.Now()
	obj := &TestObject{}
	obj.OnInit(obj, "test-id")
	after := time.Now()

	creationTime := obj.CreationTime()
	if creationTime.Before(before) || creationTime.After(after) {
		t.Fatalf("CreationTime() = %v; want time between %v and %v", creationTime, before, after)
	}
}

func TestBaseObject_CreationTime_IsSet(t *testing.T) {
	obj := &TestObject{}
	obj.OnInit(obj, "test-id")

	creationTime := obj.CreationTime()
	if creationTime.IsZero() {
		t.Fatal("CreationTime should be set after OnInit")
	}
}

func TestBaseObject_Context(t *testing.T) {
	obj := &TestObject{}
	obj.OnInit(obj, "test-id")

	ctx := obj.Context()
	if ctx == nil {
		t.Fatal("Context() should return a non-nil context after OnInit")
	}

	// Verify the context is not cancelled yet
	select {
	case <-ctx.Done():
		t.Fatal("Context should not be cancelled immediately after OnInit")
	default:
		// Good - context is not cancelled
	}
}

func TestBaseObject_Destroy(t *testing.T) {
	obj := &TestObject{}
	obj.OnInit(obj, "test-id")

	ctx := obj.Context()
	if ctx == nil {
		t.Fatal("Context() should return a non-nil context")
	}

	// Verify context is not cancelled before Destroy
	select {
	case <-ctx.Done():
		t.Fatal("Context should not be cancelled before calling Destroy")
	default:
		// Good - context is not cancelled
	}

	// Destroy the object
	obj.Destroy()

	// Verify context is now cancelled
	select {
	case <-ctx.Done():
		// Good - context is cancelled
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Context should be cancelled after calling Destroy")
	}
}

func TestBaseObject_Context_SameInstance(t *testing.T) {
	obj := &TestObject{}
	obj.OnInit(obj, "test-id")

	ctx1 := obj.Context()
	ctx2 := obj.Context()

	if ctx1 != ctx2 {
		t.Fatal("Multiple calls to Context() should return the same context instance")
	}
}

func TestBaseObject_Destroy_Idempotent(t *testing.T) {
	obj := &TestObject{}
	obj.OnInit(obj, "test-id")

	// Destroy multiple times should not panic
	obj.Destroy()
	obj.Destroy()
	obj.Destroy()

	// Verify context is cancelled
	ctx := obj.Context()
	select {
	case <-ctx.Done():
		// Good - context is cancelled
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Context should be cancelled after calling Destroy")
	}
}

// TestObjectWithMethod is a test object with a valid method for InvokeMethod testing
type TestObjectWithMethod struct {
	BaseObject
	CallCount int
}

func (t *TestObjectWithMethod) OnCreated() {}

func (t *TestObjectWithMethod) TestMethod(ctx context.Context, req *emptypb.Empty) (*emptypb.Empty, error) {
	t.CallCount++
	return &emptypb.Empty{}, nil
}

func (t *TestObjectWithMethod) GetValue(ctx context.Context, req *emptypb.Empty) (*structpb.Struct, error) {
	return &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"count": structpb.NewNumberValue(float64(t.CallCount)),
		},
	}, nil
}

func TestBaseObject_InvokeMethod_Success(t *testing.T) {
	obj := &TestObjectWithMethod{}
	obj.OnInit(obj, "test-id")

	ctx := context.Background()
	req := &emptypb.Empty{}

	// First call
	resp, err := obj.InvokeMethod(ctx, "TestMethod", req)
	if err != nil {
		t.Fatalf("InvokeMethod failed: %v", err)
	}
	if resp == nil {
		t.Fatal("InvokeMethod returned nil response")
	}

	if obj.CallCount != 1 {
		t.Fatalf("CallCount = %d; want 1", obj.CallCount)
	}

	// Second call
	resp, err = obj.InvokeMethod(ctx, "TestMethod", req)
	if err != nil {
		t.Fatalf("InvokeMethod failed on second call: %v", err)
	}
	if resp == nil {
		t.Fatal("InvokeMethod returned nil response on second call")
	}

	if obj.CallCount != 2 {
		t.Fatalf("CallCount = %d; want 2", obj.CallCount)
	}
}

func TestBaseObject_InvokeMethod_MethodNotFound(t *testing.T) {
	obj := &TestObjectWithMethod{}
	obj.OnInit(obj, "test-id")

	ctx := context.Background()
	req := &emptypb.Empty{}

	_, err := obj.InvokeMethod(ctx, "NonExistentMethod", req)
	if err == nil {
		t.Fatal("InvokeMethod should fail for non-existent method")
	}
}

func TestBaseObject_InvokeMethod_InvalidRequestType(t *testing.T) {
	obj := &TestObjectWithMethod{}
	obj.OnInit(obj, "test-id")

	ctx := context.Background()
	// Use wrong request type
	req := &structpb.Struct{}

	_, err := obj.InvokeMethod(ctx, "TestMethod", req)
	if err == nil {
		t.Fatal("InvokeMethod should fail for invalid request type")
	}
}

func TestBaseObject_InvokeMethod_ReturnsCorrectValue(t *testing.T) {
	obj := &TestObjectWithMethod{}
	obj.OnInit(obj, "test-id")

	ctx := context.Background()
	req := &emptypb.Empty{}

	// Call GetValue which returns a struct
	resp, err := obj.InvokeMethod(ctx, "GetValue", req)
	if err != nil {
		t.Fatalf("InvokeMethod failed: %v", err)
	}

	structResp, ok := resp.(*structpb.Struct)
	if !ok {
		t.Fatalf("Response should be *structpb.Struct, got %T", resp)
	}

	// Verify count is 0 (TestMethod wasn't called)
	countVal := structResp.Fields["count"]
	if countVal == nil {
		t.Fatal("Response missing 'count' field")
	}
	if countVal.GetNumberValue() != 0 {
		t.Fatalf("count = %f; want 0", countVal.GetNumberValue())
	}
}

// TestObjectWithInvalidMethod has methods with invalid signatures for testing
type TestObjectWithInvalidMethod struct {
	BaseObject
}

func (t *TestObjectWithInvalidMethod) OnCreated() {}

// Invalid: wrong number of arguments
func (t *TestObjectWithInvalidMethod) InvalidArgs(ctx context.Context) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, nil
}

// Invalid: wrong argument types
func (t *TestObjectWithInvalidMethod) InvalidArgTypes(s string, i int) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, nil
}

// Invalid: wrong return types
func (t *TestObjectWithInvalidMethod) InvalidReturn(ctx context.Context, req *emptypb.Empty) string {
	return "invalid"
}

func TestBaseObject_InvokeMethod_InvalidMethodSignatures(t *testing.T) {
	tests := []struct {
		name       string
		methodName string
	}{
		{"InvalidArgs", "InvalidArgs"},
		{"InvalidArgTypes", "InvalidArgTypes"},
		{"InvalidReturn", "InvalidReturn"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj := &TestObjectWithInvalidMethod{}
			obj.OnInit(obj, "test-id")

			ctx := context.Background()
			req := &emptypb.Empty{}

			_, err := obj.InvokeMethod(ctx, tt.methodName, req)
			if err == nil {
				t.Fatalf("InvokeMethod should fail for method %s with invalid signature", tt.methodName)
			}
		})
	}
}

func TestIsConcreteProtoMessage(t *testing.T) {
	tests := []struct {
		name     string
		typ      reflect.Type
		expected bool
	}{
		{"EmptyProto", reflect.TypeOf((*emptypb.Empty)(nil)), true},
		{"StructProto", reflect.TypeOf((*structpb.Struct)(nil)), true},
		{"String", reflect.TypeOf(""), false},
		{"Int", reflect.TypeOf(0), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isConcreteProtoMessage(tt.typ)
			if result != tt.expected {
				t.Fatalf("isConcreteProtoMessage(%v) = %v; want %v", tt.typ, result, tt.expected)
			}
		})
	}
}
