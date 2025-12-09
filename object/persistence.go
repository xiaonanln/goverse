package object

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// ReliableCall represents a tracked remote call for exactly-once semantics
type ReliableCall struct {
	ID          int64
	RequestID   string
	ObjectID    string
	ObjectType  string
	MethodName  string
	RequestData []byte
	ResultData  []byte
	Error       string
	Status      string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// PersistenceProvider is an interface for saving and loading objects
// Implementations can use different storage backends (PostgreSQL, Redis, etc.)
type PersistenceProvider interface {
	SaveObject(ctx context.Context, objectID, objectType string, data []byte) error
	LoadObject(ctx context.Context, objectID string) ([]byte, error)
	DeleteObject(ctx context.Context, objectID string) error

	// ReliableCall methods for exactly-once semantics
	InsertOrGetReliableCall(ctx context.Context, requestID string, objectID string, objectType string, methodName string, requestData []byte) (*ReliableCall, error)
	UpdateReliableCallStatus(ctx context.Context, id int64, status string, resultData []byte, errorMessage string) error
	GetPendingReliableCalls(ctx context.Context, objectID string, nextRcid int64) ([]*ReliableCall, error)
	GetReliableCall(ctx context.Context, requestID string) (*ReliableCall, error)
}

// SaveObject saves object data using the provided persistence provider
// The caller should call obj.ToData() to get the proto.Message before calling this function
func SaveObject(ctx context.Context, provider PersistenceProvider, objectID, objectType string, data proto.Message) error {
	// Convert proto.Message to JSON bytes using protojson
	jsonData, err := protojson.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal proto message to JSON: %w", err)
	}

	return provider.SaveObject(ctx, objectID, objectType, jsonData)
}

// ErrObjectNotFound is returned when an object is not found in persistence storage
var ErrObjectNotFound = fmt.Errorf("object not found in persistence storage")

// LoadObject loads object data from the provided persistence provider
// Returns the data as proto.Message. The caller should create a proto.Message instance
// to unmarshal into, then call obj.FromData() to restore the object state
func LoadObject(ctx context.Context, provider PersistenceProvider, objectID string, protoMsg proto.Message) error {
	jsonData, err := provider.LoadObject(ctx, objectID)
	if err != nil {
		return fmt.Errorf("failed to load object data: %w", err)
	}

	// If no data was found (provider returned nil, nil), return ErrObjectNotFound
	if jsonData == nil {
		return ErrObjectNotFound
	}

	// Unmarshal JSON to proto.Message
	err = protojson.Unmarshal(jsonData, protoMsg)
	if err != nil {
		return fmt.Errorf("failed to unmarshal JSON to proto message: %w", err)
	}

	return nil
}

// MarshalProtoToJSON is a utility function to marshal proto.Message to JSON
func MarshalProtoToJSON(msg proto.Message) ([]byte, error) {
	return protojson.Marshal(msg)
}

// UnmarshalProtoFromJSON is a utility function to unmarshal JSON to proto.Message
func UnmarshalProtoFromJSON(jsonData []byte, msg proto.Message) error {
	return protojson.Unmarshal(jsonData, msg)
}
