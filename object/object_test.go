package object

import (
	"testing"
	"time"
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
		t.Error("OnInit should generate an ID when empty string is provided")
	}

	// Test with explicit ID
	obj2 := &TestObject{}
	obj2.OnInit(obj2, "test-id-123")
	if obj2.Id() != "test-id-123" {
		t.Errorf("OnInit should use provided ID, got %s, want test-id-123", obj2.Id())
	}
}

func TestBaseObject_Id(t *testing.T) {
	obj := &TestObject{}
	obj.OnInit(obj, "my-unique-id")

	if got := obj.Id(); got != "my-unique-id" {
		t.Errorf("Id() = %s; want my-unique-id", got)
	}
}

func TestBaseObject_Type(t *testing.T) {
	obj := &TestObject{}
	obj.OnInit(obj, "test-id")

	if got := obj.Type(); got != "TestObject" {
		t.Errorf("Type() = %s; want TestObject", got)
	}
}

func TestBaseObject_String(t *testing.T) {
	obj := &TestObject{}
	obj.OnInit(obj, "test-id")

	expected := "TestObject(test-id)"
	if got := obj.String(); got != expected {
		t.Errorf("String() = %s; want %s", got, expected)
	}
}

func TestBaseObject_LoggerInitialization(t *testing.T) {
	obj := &TestObject{}
	obj.OnInit(obj, "test-id")

	if obj.Logger == nil {
		t.Error("Logger should be initialized after OnInit")
	}
}

func TestBaseObject_OnInitWithProtoMessage(t *testing.T) {
	obj := &TestObject{}
	obj.OnInit(obj, "test-id")

	// Should not panic and should initialize properly
	if obj.Id() != "test-id" {
		t.Errorf("OnInit with proto message failed, got ID %s, want test-id", obj.Id())
	}
}

func TestBaseObject_UniqueIDs(t *testing.T) {
	// Test that multiple objects get unique IDs when no ID is provided
	obj1 := &TestObject{}
	obj1.OnInit(obj1, "")

	obj2 := &TestObject{}
	obj2.OnInit(obj2, "")

	if obj1.Id() == obj2.Id() {
		t.Error("Different objects should get different IDs when no ID is provided")
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
		t.Errorf("OnInit failed, got ID %s, want test-id", obj.Id())
	}
}

func TestBaseObject_CreationTime(t *testing.T) {
	before := time.Now()
	obj := &TestObject{}
	obj.OnInit(obj, "test-id")
	after := time.Now()

	creationTime := obj.CreationTime()
	if creationTime.Before(before) || creationTime.After(after) {
		t.Errorf("CreationTime() = %v; want time between %v and %v", creationTime, before, after)
	}
}

func TestBaseObject_CreationTime_IsSet(t *testing.T) {
	obj := &TestObject{}
	obj.OnInit(obj, "test-id")

	creationTime := obj.CreationTime()
	if creationTime.IsZero() {
		t.Error("CreationTime should be set after OnInit")
	}
}
