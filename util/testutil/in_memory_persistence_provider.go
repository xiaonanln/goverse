package testutil

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xiaonanln/goverse/object"
)

// InMemoryPersistenceProvider is a thread-safe in-memory implementation of
// object.PersistenceProvider for use in tests that do not have a Postgres
// database available. It faithfully implements the exactly-once ReliableCall
// semantics so that node.ReliableCallObject can be exercised end-to-end.
type InMemoryPersistenceProvider struct {
	mu         sync.Mutex
	objects    map[string][]byte
	rcseqs     map[string]int64
	callsByID  map[string]*object.ReliableCall
	callsBySeq map[int64]*object.ReliableCall
	nextSeq    int64
}

// NewInMemoryPersistenceProvider returns a ready-to-use in-memory provider.
func NewInMemoryPersistenceProvider() *InMemoryPersistenceProvider {
	return &InMemoryPersistenceProvider{
		objects:    make(map[string][]byte),
		rcseqs:     make(map[string]int64),
		callsByID:  make(map[string]*object.ReliableCall),
		callsBySeq: make(map[int64]*object.ReliableCall),
	}
}

func (p *InMemoryPersistenceProvider) SaveObject(ctx context.Context, objectID, objectType string, data []byte, nextRcseq int64) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.objects[objectID] = data
	p.rcseqs[objectID] = nextRcseq
	return nil
}

func (p *InMemoryPersistenceProvider) LoadObject(ctx context.Context, objectID string) ([]byte, int64, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	data, ok := p.objects[objectID]
	if !ok {
		return nil, 0, nil
	}
	return data, p.rcseqs[objectID], nil
}

func (p *InMemoryPersistenceProvider) DeleteObject(ctx context.Context, objectID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.objects, objectID)
	delete(p.rcseqs, objectID)
	return nil
}

// InsertOrGetReliableCall atomically inserts a new pending reliable call or
// returns the existing one when callID has already been seen.
func (p *InMemoryPersistenceProvider) InsertOrGetReliableCall(ctx context.Context, callID, objectID, objectType, methodName string, requestData []byte) (*object.ReliableCall, error) {
	if callID == "" {
		return nil, fmt.Errorf("call_id cannot be empty")
	}
	if objectID == "" {
		return nil, fmt.Errorf("object_id cannot be empty")
	}
	if objectType == "" {
		return nil, fmt.Errorf("object_type cannot be empty")
	}
	if methodName == "" {
		return nil, fmt.Errorf("method_name cannot be empty")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if rc, ok := p.callsByID[callID]; ok {
		return rc, nil
	}

	p.nextSeq++
	rc := &object.ReliableCall{
		Seq:         p.nextSeq,
		CallID:      callID,
		ObjectID:    objectID,
		ObjectType:  objectType,
		MethodName:  methodName,
		RequestData: requestData,
		Status:      "pending",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	p.callsByID[callID] = rc
	p.callsBySeq[rc.Seq] = rc
	return rc, nil
}

func (p *InMemoryPersistenceProvider) UpdateReliableCallStatus(ctx context.Context, seq int64, status string, resultData []byte, errorMessage string) error {
	if seq <= 0 {
		return fmt.Errorf("seq must be positive")
	}
	if status == "" {
		return fmt.Errorf("status cannot be empty")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	rc, ok := p.callsBySeq[seq]
	if !ok {
		return fmt.Errorf("reliable call not found: %d", seq)
	}
	rc.Status = status
	rc.ResultData = resultData
	rc.Error = errorMessage
	rc.UpdatedAt = time.Now()
	return nil
}

// GetPendingReliableCalls returns pending calls for objectID with seq >= nextRcseq, ordered by seq.
func (p *InMemoryPersistenceProvider) GetPendingReliableCalls(ctx context.Context, objectID string, nextRcseq int64) ([]*object.ReliableCall, error) {
	if objectID == "" {
		return nil, fmt.Errorf("object_id cannot be empty")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Collect matching calls from all seqs (seqs are global, not per-object).
	var calls []*object.ReliableCall
	for seq := int64(1); seq <= p.nextSeq; seq++ {
		rc, ok := p.callsBySeq[seq]
		if !ok {
			continue
		}
		if rc.ObjectID == objectID && rc.Seq >= nextRcseq && rc.Status == "pending" {
			calls = append(calls, rc)
		}
	}
	return calls, nil
}

func (p *InMemoryPersistenceProvider) GetReliableCallBySeq(ctx context.Context, seq int64) (*object.ReliableCall, error) {
	if seq <= 0 {
		return nil, fmt.Errorf("seq must be positive")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	rc, ok := p.callsBySeq[seq]
	if !ok {
		return nil, fmt.Errorf("reliable call not found: %d", seq)
	}
	return rc, nil
}
