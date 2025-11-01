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

// PersistentObject extends the Object interface with persistence capabilities
type PersistentObject interface {
	Object
	// IsPersistent returns whether this object should be persisted to storage
	IsPersistent() bool
	// SetPersistent sets whether this object should be persisted
	SetPersistent(persistent bool)
	// ToData serializes the object state to a map for persistence
	ToData() (map[string]interface{}, error)
	// FromData deserializes object state from a map
	FromData(data map[string]interface{}) error
}

// BasePersistentObject extends BaseObject with persistence support
type BasePersistentObject struct {
	BaseObject
	persistent bool
}

// IsPersistent returns whether this object should be persisted
func (base *BasePersistentObject) IsPersistent() bool {
	return base.persistent
}

// SetPersistent sets whether this object should be persisted
func (base *BasePersistentObject) SetPersistent(persistent bool) {
	base.persistent = persistent
}

// ToData provides a default implementation that returns an empty map
// Derived types should override this to serialize their specific state
func (base *BasePersistentObject) ToData() (map[string]interface{}, error) {
	data := map[string]interface{}{
		"id":            base.id,
		"type":          base.Type(),
		"creation_time": base.creationTime.Unix(),
		"persistent":    base.persistent,
	}
	return data, nil
}

// FromData provides a default implementation that does nothing
// Derived types should override this to deserialize their specific state
func (base *BasePersistentObject) FromData(data map[string]interface{}) error {
	// Default implementation - derived types should override
	if persistent, ok := data["persistent"].(bool); ok {
		base.persistent = persistent
	}
	return nil
}

// SavePersistentObject saves an object using the provided persistence provider
func SavePersistentObject(ctx context.Context, provider PersistenceProvider, obj PersistentObject) error {
	if !obj.IsPersistent() {
		return nil // Not a persistent object, skip
	}

	data, err := obj.ToData()
	if err != nil {
		return fmt.Errorf("failed to serialize object data: %w", err)
	}

	return provider.SaveObject(ctx, obj.Id(), obj.Type(), data)
}

// LoadPersistentObject loads an object using the provided persistence provider
func LoadPersistentObject(ctx context.Context, provider PersistenceProvider, obj PersistentObject, objectID string) error {
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
