package object

import (
	"context"
	"fmt"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// PersistenceProvider is an interface for saving and loading objects
// Implementations can use different storage backends (PostgreSQL, Redis, etc.)
type PersistenceProvider interface {
	SaveObject(ctx context.Context, objectID, objectType string, data []byte) error
	LoadObject(ctx context.Context, objectID string) ([]byte, error)
	DeleteObject(ctx context.Context, objectID string) error
}

// SaveObject saves an object using the provided persistence provider
// Returns nil if the object is not persistent (ToData returns error)
func SaveObject(ctx context.Context, provider PersistenceProvider, obj Object) error {
	protoMsg, err := obj.ToData()
	if err != nil {
		// Object is not persistent, skip silently
		return nil
	}

	// Convert proto.Message to JSON bytes using protojson
	jsonData, err := protojson.Marshal(protoMsg)
	if err != nil {
		return fmt.Errorf("failed to marshal proto message to JSON: %w", err)
	}

	return provider.SaveObject(ctx, obj.Id(), obj.Type(), jsonData)
}

// LoadObject loads an object using the provided persistence provider
func LoadObject(ctx context.Context, provider PersistenceProvider, obj Object, objectID string) error {
	jsonData, err := provider.LoadObject(ctx, objectID)
	if err != nil {
		return fmt.Errorf("failed to load object data: %w", err)
	}

	// Create a new instance of the expected proto.Message type
	// The object's ToData() should return the correct type as a template
	protoMsg, err := obj.ToData()
	if err != nil {
		return fmt.Errorf("object type does not support persistence: %w", err)
	}

	// Unmarshal JSON to proto.Message
	err = protojson.Unmarshal(jsonData, protoMsg)
	if err != nil {
		return fmt.Errorf("failed to unmarshal JSON to proto message: %w", err)
	}

	return obj.FromData(protoMsg)
}

// MarshalProtoToJSON is a utility function to marshal proto.Message to JSON
func MarshalProtoToJSON(msg proto.Message) ([]byte, error) {
	return protojson.Marshal(msg)
}

// UnmarshalProtoFromJSON is a utility function to unmarshal JSON to proto.Message
func UnmarshalProtoFromJSON(jsonData []byte, msg proto.Message) error {
	return protojson.Unmarshal(jsonData, msg)
}
