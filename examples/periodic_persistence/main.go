package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/xiaonanln/goverse/cluster/sharding"
	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/object"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

// Counter is a simple persistent object that maintains a count
type Counter struct {
	object.BaseObject
	mu    sync.Mutex // Protects concurrent access to Count during persistence
	Count int
}

func (c *Counter) OnCreated() {
	c.Logger.Infof("Counter created: %s", c.Id())
}

// ToData serializes the Counter state for persistence
// Note: Thread-safe implementation with mutex to prevent race conditions
// during periodic persistence saves
func (c *Counter) ToData() (proto.Message, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	data, err := structpb.NewStruct(map[string]interface{}{
		"id":    c.Id(),
		"count": float64(c.Count),
	})
	if err != nil {
		return nil, err
	}
	return data, nil
}

// FromData deserializes the Counter state
// Note: Thread-safe implementation with mutex to prevent race conditions
// during object initialization
func (c *Counter) FromData(data proto.Message) error {
	structData, ok := data.(*structpb.Struct)
	if !ok {
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if count, ok := structData.Fields["count"]; ok {
		c.Count = int(count.GetNumberValue())
	}
	return nil
}

// MockPersistenceProvider for this example (in production, use PostgreSQL)
type MockPersistenceProvider struct {
	storage   map[string][]byte
	saveCount int
}

func NewMockPersistenceProvider() *MockPersistenceProvider {
	return &MockPersistenceProvider{
		storage: make(map[string][]byte),
	}
}

func (m *MockPersistenceProvider) SaveObject(ctx context.Context, objectID, objectType string, data []byte) error {
	m.storage[objectID] = data
	m.saveCount++
	fmt.Printf("  ✓ Saved %s (type: %s) - %d bytes\n", objectID, objectType, len(data))
	return nil
}

func (m *MockPersistenceProvider) LoadObject(ctx context.Context, objectID string) ([]byte, error) {
	data, ok := m.storage[objectID]
	if !ok {
		return nil, fmt.Errorf("object not found: %s", objectID)
	}
	return data, nil
}

func (m *MockPersistenceProvider) DeleteObject(ctx context.Context, objectID string) error {
	delete(m.storage, objectID)
	return nil
}

func (m *MockPersistenceProvider) InsertOrGetReliableCall(ctx context.Context, requestID string, objectID string, objectType string, methodName string, requestData []byte) (*object.ReliableCall, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockPersistenceProvider) UpdateReliableCallStatus(ctx context.Context, id int64, status string, resultData []byte, errorMessage string) error {
	return fmt.Errorf("not implemented")
}

func (m *MockPersistenceProvider) GetPendingReliableCalls(ctx context.Context, objectID string, nextRcid int64) ([]*object.ReliableCall, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockPersistenceProvider) GetReliableCall(ctx context.Context, requestID string) (*object.ReliableCall, error) {
	return nil, fmt.Errorf("not implemented")
}

func main() {
	fmt.Println("=== Goverse Periodic Persistence Example ===")

	// Create a node
	n := node.NewNode("localhost:47000", sharding.NumShards)

	// Create persistence provider
	provider := NewMockPersistenceProvider()

	// Configure persistence
	n.SetPersistenceProvider(provider)
	n.SetPersistenceInterval(2 * time.Second) // Save every 2 seconds

	// Register the Counter type
	n.RegisterObjectType((*Counter)(nil))

	// Start the node (this will start periodic persistence)
	ctx := context.Background()
	err := n.Start(ctx)
	if err != nil {
		log.Fatalf("Failed to start node: %v", err)
	}

	fmt.Println("✓ Node started with periodic persistence enabled")
	fmt.Println("✓ Objects will be saved every 2 seconds")

	// Create some counter objects
	fmt.Println("Creating 3 counter objects...")
	for i := 1; i <= 3; i++ {
		id := fmt.Sprintf("counter-%d", i)
		_, err = n.CreateObject(ctx, "Counter", id)
		if err != nil {
			log.Fatalf("Failed to create %s: %v", id, err)
		}
		fmt.Printf("  ✓ Created %s\n", id)
	}

	fmt.Printf("\nTotal objects on node: %d\n", n.NumObjects())

	// Wait for first periodic persistence cycle
	fmt.Println("\n[Waiting 2.5 seconds for first periodic save cycle...]")
	time.Sleep(2500 * time.Millisecond)

	fmt.Printf("Save count so far: %d\n", provider.saveCount)

	// Wait for second periodic persistence cycle
	fmt.Println("\n[Waiting another 2.5 seconds for second save cycle...]")
	time.Sleep(2500 * time.Millisecond)

	fmt.Printf("Save count so far: %d\n", provider.saveCount)

	// Node.Stop() will save all objects one final time
	fmt.Println("\nStopping node (final save will occur)...")
	err = n.Stop(ctx)
	if err != nil {
		log.Fatalf("Failed to stop node: %v", err)
	}

	fmt.Println("\n=== Example Complete ===")
	fmt.Printf("Total saves: %d\n", provider.saveCount)
	fmt.Printf("Objects in storage: %d\n", len(provider.storage))
	fmt.Println("\nKey Points:")
	fmt.Println("- Periodic persistence runs automatically in the background")
	fmt.Println("- Non-persistent objects are automatically skipped")
	fmt.Println("- Node.Stop() performs a final save before shutdown")
	fmt.Println("- In production, use PostgreSQL or another persistent storage")
}
