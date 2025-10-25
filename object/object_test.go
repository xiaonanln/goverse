package object

import (
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
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
	obj.OnInit(obj, "", nil)

	// Test that ID is generated when empty
	if obj.Id() == "" {
		t.Error("OnInit should generate an ID when empty string is provided")
	}

	// Test with explicit ID
	obj2 := &TestObject{}
	obj2.OnInit(obj2, "test-id-123", nil)
	if obj2.Id() != "test-id-123" {
		t.Errorf("OnInit should use provided ID, got %s, want test-id-123", obj2.Id())
	}
}

func TestBaseObject_Id(t *testing.T) {
	obj := &TestObject{}
	obj.OnInit(obj, "my-unique-id", nil)

	if got := obj.Id(); got != "my-unique-id" {
		t.Errorf("Id() = %s; want my-unique-id", got)
	}
}

func TestBaseObject_Type(t *testing.T) {
	obj := &TestObject{}
	obj.OnInit(obj, "test-id", nil)

	if got := obj.Type(); got != "TestObject" {
		t.Errorf("Type() = %s; want TestObject", got)
	}
}

func TestBaseObject_String(t *testing.T) {
	obj := &TestObject{}
	obj.OnInit(obj, "test-id", nil)

	expected := "TestObject(test-id)"
	if got := obj.String(); got != expected {
		t.Errorf("String() = %s; want %s", got, expected)
	}
}

func TestBaseObject_LoggerInitialization(t *testing.T) {
	obj := &TestObject{}
	obj.OnInit(obj, "test-id", nil)

	if obj.Logger == nil {
		t.Error("Logger should be initialized after OnInit")
	}
}

func TestBaseObject_OnInitWithProtoMessage(t *testing.T) {
	obj := &TestObject{}
	data := &emptypb.Empty{}
	obj.OnInit(obj, "test-id", data)

	// Should not panic and should initialize properly
	if obj.Id() != "test-id" {
		t.Errorf("OnInit with proto message failed, got ID %s, want test-id", obj.Id())
	}
}

func TestBaseObject_UniqueIDs(t *testing.T) {
	// Test that multiple objects get unique IDs when no ID is provided
	obj1 := &TestObject{}
	obj1.OnInit(obj1, "", nil)

	obj2 := &TestObject{}
	obj2.OnInit(obj2, "", nil)

	if obj1.Id() == obj2.Id() {
		t.Error("Different objects should get different IDs when no ID is provided")
	}
}

func TestObjectInterface(t *testing.T) {
	// Test that TestObject implements the Object interface
	var _ Object = (*TestObject)(nil)
}

// TestProtoMessageNil verifies that OnInit handles nil proto.Message correctly
func TestBaseObject_OnInitWithNilProtoMessage(t *testing.T) {
	obj := &TestObject{}
	var nilProto proto.Message = nil
	obj.OnInit(obj, "test-id", nilProto)

	// Should not panic
	if obj.Id() != "test-id" {
		t.Errorf("OnInit with nil proto message failed, got ID %s, want test-id", obj.Id())
	}
}

func TestBaseObject_CreationTime(t *testing.T) {
	before := time.Now()
	obj := &TestObject{}
	obj.OnInit(obj, "test-id", nil)
	after := time.Now()

	creationTime := obj.CreationTime()
	if creationTime.Before(before) || creationTime.After(after) {
		t.Errorf("CreationTime() = %v; want time between %v and %v", creationTime, before, after)
	}
}

func TestBaseObject_CreationTime_IsSet(t *testing.T) {
	obj := &TestObject{}
	obj.OnInit(obj, "test-id", nil)

	creationTime := obj.CreationTime()
	if creationTime.IsZero() {
		t.Error("CreationTime should be set after OnInit")
	}
}
