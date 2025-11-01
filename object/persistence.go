package object

import (
	"context"
	"encoding/json"
	"fmt"
)

// PersistenceProvider is an interface for saving and loading objects
// Implementations can use different storage backends (PostgreSQL, Redis, etc.)
type PersistenceProvider interface {
	SaveObject(ctx context.Context, objectID, objectType string, data map[string]interface{}) error
	LoadObject(ctx context.Context, objectID string) (map[string]interface{}, error)
	DeleteObject(ctx context.Context, objectID string) error
}

// SaveObject saves an object using the provided persistence provider
// Returns nil if the object is not persistent (ToData returns error)
func SaveObject(ctx context.Context, provider PersistenceProvider, obj Object) error {
	data, err := obj.ToData()
	if err != nil {
		// Object is not persistent, skip silently
		return nil
	}

	return provider.SaveObject(ctx, obj.Id(), obj.Type(), data)
}

// LoadObject loads an object using the provided persistence provider
func LoadObject(ctx context.Context, provider PersistenceProvider, obj Object, objectID string) error {
	data, err := provider.LoadObject(ctx, objectID)
	if err != nil {
		return fmt.Errorf("failed to load object data: %w", err)
	}

	return obj.FromData(data)
}

// MarshalToJSON is a utility function to marshal data to JSON
func MarshalToJSON(data map[string]interface{}) ([]byte, error) {
	return json.Marshal(data)
}

// UnmarshalFromJSON is a utility function to unmarshal data from JSON
func UnmarshalFromJSON(jsonData []byte) (map[string]interface{}, error) {
	var data map[string]interface{}
	err := json.Unmarshal(jsonData, &data)
	return data, err
}
