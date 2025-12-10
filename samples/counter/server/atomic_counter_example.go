package main

import (
	"context"
	"sync/atomic"

	"github.com/xiaonanln/goverse/goverseapi"
	pb "github.com/xiaonanln/goverse/samples/counter/proto"
)

// AtomicCounter is an alternative Counter implementation using lock-free atomic operations.
//
// When to use atomic operations instead of mutex:
// - Simple read-modify-write operations (increment, decrement, get, set)
// - High-concurrency scenarios where lock contention matters
// - When operations are independent (no need to coordinate multiple fields)
//
// When to use mutex instead:
// - Complex operations involving multiple fields
// - Operations requiring consistency across multiple updates
// - When readability is more important than performance
//
// Performance characteristics:
// - Atomic operations are lock-free and faster than mutex for simple operations
// - No goroutine blocking or context switching
// - Uses CPU atomic instructions (CAS, XADD, etc.)
type AtomicCounter struct {
	goverseapi.BaseObject

	value atomic.Int32 // Lock-free atomic value
}

// OnCreated is called when the AtomicCounter object is created.
func (c *AtomicCounter) OnCreated() {
	c.Logger.Infof("AtomicCounter %s created", c.Id())
	// Note: atomic.Int32 zero-initializes to 0, but we call Store(0) explicitly for clarity
	c.value.Store(0)
}

// Increment increases the counter value by the specified amount.
// Uses atomic.Int32.Add() for lock-free operation.
func (c *AtomicCounter) Increment(ctx context.Context, req *pb.IncrementRequest) (*pb.CounterResponse, error) {
	newValue := c.value.Add(req.Amount)

	return &pb.CounterResponse{
		Name:  c.Id(),
		Value: newValue,
	}, nil
}

// Decrement decreases the counter value by the specified amount.
// Uses atomic.Int32.Add() with negative amount for lock-free operation.
func (c *AtomicCounter) Decrement(ctx context.Context, req *pb.DecrementRequest) (*pb.CounterResponse, error) {
	newValue := c.value.Add(-req.Amount)

	return &pb.CounterResponse{
		Name:  c.Id(),
		Value: newValue,
	}, nil
}

// Get returns the current counter value.
// Uses atomic.Int32.Load() for lock-free read.
func (c *AtomicCounter) Get(ctx context.Context, req *pb.GetRequest) (*pb.CounterResponse, error) {
	currentValue := c.value.Load()

	return &pb.CounterResponse{
		Name:  c.Id(),
		Value: currentValue,
	}, nil
}

// Reset sets the counter value to zero.
// Uses atomic.Int32.Store() for lock-free write.
func (c *AtomicCounter) Reset(ctx context.Context, req *pb.ResetRequest) (*pb.CounterResponse, error) {
	c.value.Store(0)

	return &pb.CounterResponse{
		Name:  c.Id(),
		Value: 0,
	}, nil
}
