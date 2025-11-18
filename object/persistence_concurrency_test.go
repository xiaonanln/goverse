package object

import (
	"fmt"
	"sync"
	"testing"

	"google.golang.org/protobuf/types/known/structpb"
)

// TestToData_Concurrency verifies ToData is safe to call concurrently
func TestToData_Concurrency(t *testing.T) {
	obj := &TestPersistentObject{}
	obj.OnInit(obj, "test-concurrent")
	obj.CustomData = "initial-value"

	const goroutines = 10
	const iterations = 100

	var wg sync.WaitGroup
	errors := make(chan error, goroutines*iterations)

	// Multiple goroutines calling ToData concurrently
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_, err := obj.ToData()
				if err != nil {
					errors <- err
				}
			}
		}()
	}

	wg.Wait()
	close(errors)

	// Check for any errors
	for err := range errors {
		t.Fatalf("ToData failed during concurrent access: %v", err)
	}
}

// TestToDataAndFromData_Concurrency tests concurrent reads and writes
func TestToDataAndFromData_Concurrency(t *testing.T) {
	obj := &TestPersistentObject{}
	obj.OnInit(obj, "test-readwrite")
	obj.CustomData = "initial"

	const goroutines = 10
	const iterations = 50

	var wg sync.WaitGroup
	errors := make(chan error, goroutines*iterations*2)

	// Concurrent readers (ToData)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_, err := obj.ToData()
				if err != nil {
					errors <- err
				}
			}
		}()
	}

	// Concurrent writers (FromData)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				data, _ := structpb.NewStruct(map[string]interface{}{
					"custom_data": fmt.Sprintf("writer-%d-iter-%d", id, j),
				})
				err := obj.FromData(data)
				if err != nil {
					errors <- err
				}
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for any errors
	for err := range errors {
		t.Fatalf("Concurrent ToData/FromData failed: %v", err)
	}
}

// TestToData_RaceDetection ensures the race detector catches issues in unsafe implementations
func TestToData_RaceDetection(t *testing.T) {
	// This test should be run with -race flag to detect data races
	// go test -race ./object/
	obj := &TestPersistentObject{}
	obj.OnInit(obj, "test-race")
	obj.CustomData = "test"

	var wg sync.WaitGroup
	const concurrency = 5

	// Concurrent ToData calls - should not race because TestPersistentObject
	// doesn't use mutexes (it's a simple test object with no concurrent access patterns)
	// However, real objects that modify state during ToData would need mutexes
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				_, _ = obj.ToData()
			}
		}()
	}

	wg.Wait()
}

// TestFromData_Concurrency verifies FromData handles concurrent calls safely
func TestFromData_Concurrency(t *testing.T) {
	obj := &TestPersistentObject{}
	obj.OnInit(obj, "test-fromdata-concurrent")

	const goroutines = 10
	const iterations = 50

	var wg sync.WaitGroup
	errors := make(chan error, goroutines*iterations)

	// Multiple goroutines calling FromData concurrently
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				data, _ := structpb.NewStruct(map[string]interface{}{
					"custom_data": fmt.Sprintf("goroutine-%d-iter-%d", id, j),
				})
				err := obj.FromData(data)
				if err != nil {
					errors <- err
				}
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for any errors
	for err := range errors {
		t.Fatalf("FromData failed during concurrent access: %v", err)
	}
}

// TestMixedPersistenceOperations_Concurrency tests realistic concurrent scenarios
func TestMixedPersistenceOperations_Concurrency(t *testing.T) {
	// Create multiple objects to simulate realistic node persistence scenario
	objects := make([]*TestPersistentObject, 5)
	for i := range objects {
		obj := &TestPersistentObject{}
		obj.OnInit(obj, fmt.Sprintf("test-obj-%d", i))
		obj.CustomData = fmt.Sprintf("initial-%d", i)
		objects[i] = obj
	}

	const iterations = 20
	var wg sync.WaitGroup
	errors := make(chan error, len(objects)*iterations*2)

	// Simulate periodic persistence (reading state)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			for _, obj := range objects {
				_, err := obj.ToData()
				if err != nil {
					errors <- fmt.Errorf("persistence ToData failed: %w", err)
				}
			}
		}
	}()

	// Simulate object state updates (writing state)
	for idx, obj := range objects {
		wg.Add(1)
		go func(objIdx int, o *TestPersistentObject) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				data, _ := structpb.NewStruct(map[string]interface{}{
					"custom_data": fmt.Sprintf("updated-%d-iter-%d", objIdx, i),
				})
				err := o.FromData(data)
				if err != nil {
					errors <- fmt.Errorf("state update FromData failed: %w", err)
				}
			}
		}(idx, obj)
	}

	wg.Wait()
	close(errors)

	// Check for any errors
	for err := range errors {
		t.Fatalf("Mixed persistence operations failed: %v", err)
	}
}
