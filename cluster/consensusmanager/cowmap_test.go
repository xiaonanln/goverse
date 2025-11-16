package consensusmanager

import (
	"sync"
	"testing"
)

func TestCowMap_NewAndBasicOps(t *testing.T) {
	m := newCowMap[string, int]()
	
	// Test initial state
	if m.len() != 0 {
		t.Errorf("Expected empty map, got len %d", m.len())
	}
	
	// Test set and get
	m.set("key1", 10)
	m.set("key2", 20)
	
	if m.len() != 2 {
		t.Errorf("Expected len 2, got %d", m.len())
	}
	
	val, ok := m.get("key1")
	if !ok || val != 10 {
		t.Errorf("Expected key1=10, got %v, %v", val, ok)
	}
	
	val, ok = m.get("key2")
	if !ok || val != 20 {
		t.Errorf("Expected key2=20, got %v, %v", val, ok)
	}
	
	// Test non-existent key
	_, ok = m.get("key3")
	if ok {
		t.Error("Expected key3 to not exist")
	}
}

func TestCowMap_Delete(t *testing.T) {
	m := newCowMap[string, int]()
	m.set("key1", 10)
	m.set("key2", 20)
	
	m.delete("key1")
	
	if m.len() != 1 {
		t.Errorf("Expected len 1 after delete, got %d", m.len())
	}
	
	_, ok := m.get("key1")
	if ok {
		t.Error("Expected key1 to be deleted")
	}
	
	val, ok := m.get("key2")
	if !ok || val != 20 {
		t.Errorf("Expected key2=20 to still exist, got %v, %v", val, ok)
	}
}

func TestCowMap_Clone(t *testing.T) {
	m1 := newCowMap[string, int]()
	m1.set("key1", 10)
	m1.set("key2", 20)
	
	// Clone the map
	m2 := m1.clone()
	
	// Verify both maps have the same data
	if m1.len() != 2 || m2.len() != 2 {
		t.Errorf("Expected both maps to have len 2, got %d and %d", m1.len(), m2.len())
	}
	
	val1, _ := m1.get("key1")
	val2, _ := m2.get("key1")
	if val1 != val2 || val1 != 10 {
		t.Errorf("Expected both maps to have key1=10, got %d and %d", val1, val2)
	}
	
	// Verify they share the same data pointer (reference count should be 2)
	if m1.data.refCount.Load() != 2 {
		t.Errorf("Expected refCount=2 for shared data, got %d", m1.data.refCount.Load())
	}
	
	if m1.data != m2.data {
		t.Error("Expected cloned maps to share the same data pointer")
	}
}

func TestCowMap_CopyOnWrite(t *testing.T) {
	m1 := newCowMap[string, int]()
	m1.set("key1", 10)
	m1.set("key2", 20)
	
	// Clone the map
	m2 := m1.clone()
	
	// Verify they initially share data
	if m1.data != m2.data {
		t.Error("Expected cloned maps to initially share data")
	}
	
	// Modify m2 - should trigger copy-on-write
	m2.set("key3", 30)
	
	// Verify they now have different data pointers
	if m1.data == m2.data {
		t.Error("Expected maps to have separate data after write")
	}
	
	// Verify m1 is unchanged
	if m1.len() != 2 {
		t.Errorf("Expected m1 len to remain 2, got %d", m1.len())
	}
	_, ok := m1.get("key3")
	if ok {
		t.Error("Expected m1 to not have key3")
	}
	
	// Verify m2 has the new key
	if m2.len() != 3 {
		t.Errorf("Expected m2 len to be 3, got %d", m2.len())
	}
	val, ok := m2.get("key3")
	if !ok || val != 30 {
		t.Errorf("Expected m2 to have key3=30, got %v, %v", val, ok)
	}
	
	// Verify m2 still has the original keys
	val, ok = m2.get("key1")
	if !ok || val != 10 {
		t.Errorf("Expected m2 to have key1=10, got %v, %v", val, ok)
	}
}

func TestCowMap_CopyOnDelete(t *testing.T) {
	m1 := newCowMap[string, int]()
	m1.set("key1", 10)
	m1.set("key2", 20)
	
	// Clone the map
	m2 := m1.clone()
	
	// Delete from m2 - should trigger copy-on-write
	m2.delete("key1")
	
	// Verify they now have different data pointers
	if m1.data == m2.data {
		t.Error("Expected maps to have separate data after delete")
	}
	
	// Verify m1 is unchanged
	if m1.len() != 2 {
		t.Errorf("Expected m1 len to remain 2, got %d", m1.len())
	}
	val, ok := m1.get("key1")
	if !ok || val != 10 {
		t.Errorf("Expected m1 to still have key1=10, got %v, %v", val, ok)
	}
	
	// Verify m2 has key1 deleted
	if m2.len() != 1 {
		t.Errorf("Expected m2 len to be 1, got %d", m2.len())
	}
	_, ok = m2.get("key1")
	if ok {
		t.Error("Expected m2 to not have key1")
	}
}

func TestCowMap_CopyTo(t *testing.T) {
	m := newCowMap[string, int]()
	m.set("key1", 10)
	m.set("key2", 20)
	m.set("key3", 30)
	
	// Copy to a regular map
	dest := make(map[string]int)
	m.copyTo(dest)
	
	if len(dest) != 3 {
		t.Errorf("Expected dest to have 3 elements, got %d", len(dest))
	}
	
	if dest["key1"] != 10 || dest["key2"] != 20 || dest["key3"] != 30 {
		t.Errorf("Expected dest to have correct values, got %v", dest)
	}
	
	// Modify dest and verify it doesn't affect the cowMap
	dest["key1"] = 100
	val, _ := m.get("key1")
	if val != 10 {
		t.Errorf("Expected cowMap to be unaffected by dest modification, got key1=%d", val)
	}
}

func TestCowMap_MultipleClones(t *testing.T) {
	m1 := newCowMap[string, int]()
	m1.set("key1", 10)
	
	m2 := m1.clone()
	m3 := m1.clone()
	
	// All should share data initially
	if m1.data.refCount.Load() != 3 {
		t.Errorf("Expected refCount=3, got %d", m1.data.refCount.Load())
	}
	
	// Modify m2 - should only split m2 from the group
	m2.set("key2", 20)
	
	// m1 and m3 should still share data
	if m1.data != m3.data {
		t.Error("Expected m1 and m3 to still share data")
	}
	
	if m1.data.refCount.Load() != 2 {
		t.Errorf("Expected refCount=2 for m1/m3, got %d", m1.data.refCount.Load())
	}
	
	// m2 should have its own data
	if m2.data.refCount.Load() != 1 {
		t.Errorf("Expected refCount=1 for m2, got %d", m2.data.refCount.Load())
	}
	
	// Verify m1 and m3 are unchanged
	if m1.len() != 1 || m3.len() != 1 {
		t.Errorf("Expected m1 and m3 len to be 1, got %d and %d", m1.len(), m3.len())
	}
	
	// Verify m2 has the new key
	if m2.len() != 2 {
		t.Errorf("Expected m2 len to be 2, got %d", m2.len())
	}
}

func TestCowMap_ConcurrentReads(t *testing.T) {
	m := newCowMap[string, int]()
	for i := 0; i < 100; i++ {
		m.set(string(rune('a'+i%26)), i)
	}
	
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				m.get(string(rune('a' + j%26)))
			}
		}()
	}
	wg.Wait()
}

func TestCowMap_ConcurrentClones(t *testing.T) {
	m := newCowMap[string, int]()
	for i := 0; i < 10; i++ {
		m.set(string(rune('a'+i)), i)
	}
	
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			clone := m.clone()
			if clone.len() != 10 {
				t.Errorf("Expected clone to have 10 elements")
			}
		}()
	}
	wg.Wait()
}
