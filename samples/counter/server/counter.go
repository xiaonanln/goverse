package main

import (
	"context"
	"sync"

	"github.com/xiaonanln/goverse/goverseapi"
	pb "github.com/xiaonanln/goverse/samples/counter/proto"
)

// Counter is a simple counter object that can be incremented, decremented, get, and reset.
type Counter struct {
	goverseapi.BaseObject

	mu    sync.Mutex
	value int32
}

// OnCreated is called when the Counter object is created.
func (c *Counter) OnCreated() {
	c.Logger.Infof("Counter %s created", c.Id())
	c.value = 0
}

// Increment increases the counter value by the specified amount.
func (c *Counter) Increment(ctx context.Context, req *pb.IncrementRequest) (*pb.CounterResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.value += req.Amount

	return &pb.CounterResponse{
		Name:  c.Id(),
		Value: c.value,
	}, nil
}

// Decrement decreases the counter value by the specified amount.
func (c *Counter) Decrement(ctx context.Context, req *pb.DecrementRequest) (*pb.CounterResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.value -= req.Amount

	return &pb.CounterResponse{
		Name:  c.Id(),
		Value: c.value,
	}, nil
}

// Get returns the current counter value.
func (c *Counter) Get(ctx context.Context, req *pb.GetRequest) (*pb.CounterResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	return &pb.CounterResponse{
		Name:  c.Id(),
		Value: c.value,
	}, nil
}

// Reset sets the counter value to zero.
func (c *Counter) Reset(ctx context.Context, req *pb.ResetRequest) (*pb.CounterResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.value = 0

	return &pb.CounterResponse{
		Name:  c.Id(),
		Value: c.value,
	}, nil
}
