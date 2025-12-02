// Package goverseclient provides a client library for connecting to Goverse gate servers.
// It handles connection management, automatic failover between multiple gate addresses,
// and provides a user-friendly API for interacting with Goverse services.
package goverseclient

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	gate_pb "github.com/xiaonanln/goverse/client/proto"
	"github.com/xiaonanln/goverse/util/logger"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

const (
	// DefaultConnectionTimeout is the default timeout for gRPC connection establishment.
	DefaultConnectionTimeout = 30 * time.Second

	// DefaultCallTimeout is the default timeout for RPC calls.
	DefaultCallTimeout = 30 * time.Second

	// DefaultReconnectInterval is the default interval between reconnection attempts.
	DefaultReconnectInterval = 5 * time.Second

	// messageChannelBufferSize is the buffer size for the message channel.
	messageChannelBufferSize = 100
)

var (
	// ErrNoAddresses is returned when no gate addresses are provided.
	ErrNoAddresses = errors.New("no gate addresses provided")

	// ErrNotConnected is returned when trying to call methods while not connected.
	ErrNotConnected = errors.New("client is not connected")

	// ErrClientClosed is returned when trying to use a closed client.
	ErrClientClosed = errors.New("client is closed")

	// ErrConnectionFailed is returned when connection to all gates fails.
	ErrConnectionFailed = errors.New("failed to connect to any gate server")
)

// MessageHandler is a function type for handling pushed messages.
type MessageHandler func(msg proto.Message)

// Client is the main client for interacting with Goverse services via a gate.
// It manages connections to gate servers, handles automatic reconnection,
// and provides methods for object operations.
type Client struct {
	addresses []string
	options   *Options

	conn     *grpc.ClientConn
	client   gate_pb.GateServiceClient
	stream   gate_pb.GateService_RegisterClient
	clientID string

	messageChan chan proto.Message
	logger      *logger.Logger

	mu           sync.RWMutex
	connected    bool
	closed       bool
	currentAddr  string
	cancel       context.CancelFunc
	streamCancel context.CancelFunc
}

// Options holds configuration options for the client.
type Options struct {
	// ConnectionTimeout is the timeout for establishing a connection.
	ConnectionTimeout time.Duration

	// CallTimeout is the default timeout for RPC calls.
	CallTimeout time.Duration

	// ReconnectInterval is the interval between reconnection attempts.
	ReconnectInterval time.Duration

	// GRPCDialOptions are additional gRPC dial options.
	GRPCDialOptions []grpc.DialOption

	// Logger is the logger to use. If nil, a default logger is created.
	Logger *logger.Logger

	// OnConnect is called when a connection is established.
	OnConnect func(clientID string)

	// OnDisconnect is called when the connection is lost.
	OnDisconnect func(err error)

	// OnMessage is called when a push message is received.
	OnMessage MessageHandler
}

// Option is a function that configures Options.
type Option func(*Options)

// WithConnectionTimeout sets the connection timeout.
func WithConnectionTimeout(timeout time.Duration) Option {
	return func(o *Options) {
		o.ConnectionTimeout = timeout
	}
}

// WithCallTimeout sets the default call timeout.
func WithCallTimeout(timeout time.Duration) Option {
	return func(o *Options) {
		o.CallTimeout = timeout
	}
}

// WithReconnectInterval sets the reconnection interval.
func WithReconnectInterval(interval time.Duration) Option {
	return func(o *Options) {
		o.ReconnectInterval = interval
	}
}

// WithGRPCDialOptions adds additional gRPC dial options.
func WithGRPCDialOptions(opts ...grpc.DialOption) Option {
	return func(o *Options) {
		o.GRPCDialOptions = append(o.GRPCDialOptions, opts...)
	}
}

// WithLogger sets the logger to use.
func WithLogger(l *logger.Logger) Option {
	return func(o *Options) {
		o.Logger = l
	}
}

// WithOnConnect sets the callback for when a connection is established.
func WithOnConnect(callback func(clientID string)) Option {
	return func(o *Options) {
		o.OnConnect = callback
	}
}

// WithOnDisconnect sets the callback for when the connection is lost.
func WithOnDisconnect(callback func(err error)) Option {
	return func(o *Options) {
		o.OnDisconnect = callback
	}
}

// WithOnMessage sets the callback for when a push message is received.
func WithOnMessage(handler MessageHandler) Option {
	return func(o *Options) {
		o.OnMessage = handler
	}
}

// NewClient creates a new Goverse client with the given gate addresses and options.
// The client will try to connect to the gates in order and maintain the connection.
func NewClient(addresses []string, opts ...Option) (*Client, error) {
	if len(addresses) == 0 {
		return nil, ErrNoAddresses
	}

	options := &Options{
		ConnectionTimeout: DefaultConnectionTimeout,
		CallTimeout:       DefaultCallTimeout,
		ReconnectInterval: DefaultReconnectInterval,
	}

	for _, opt := range opts {
		opt(options)
	}

	if options.Logger == nil {
		options.Logger = logger.NewLogger("GoverseClient")
	}

	// Copy addresses to prevent external modification
	addrCopy := make([]string, len(addresses))
	copy(addrCopy, addresses)

	return &Client{
		addresses:   addrCopy,
		options:     options,
		messageChan: make(chan proto.Message, messageChannelBufferSize),
		logger:      options.Logger,
	}, nil
}

// Connect establishes a connection to a gate server.
// It tries each gate address in order until one succeeds.
// After connecting, it starts a goroutine to maintain the connection
// and receive push messages.
func (c *Client) Connect(ctx context.Context) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return ErrClientClosed
	}
	if c.connected {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()

	// Try to connect to each address
	for _, addr := range c.addresses {
		err := c.connectToAddress(ctx, addr)
		if err == nil {
			return nil
		}
		c.logger.Warnf("Failed to connect to %s: %v", addr, err)
	}

	return ErrConnectionFailed
}

// connectToAddress attempts to connect to a specific gate address.
func (c *Client) connectToAddress(ctx context.Context, addr string) error {
	c.logger.Infof("Connecting to gate at %s", addr)

	// Create gRPC connection options
	connectParams := grpc.ConnectParams{
		Backoff:           backoff.DefaultConfig,
		MinConnectTimeout: c.options.ConnectionTimeout,
	}

	dialOpts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithConnectParams(connectParams),
	}
	dialOpts = append(dialOpts, c.options.GRPCDialOptions...)

	// Create connection
	conn, err := grpc.NewClient(addr, dialOpts...)
	if err != nil {
		return fmt.Errorf("failed to create gRPC client: %w", err)
	}

	// Create gate service client
	client := gate_pb.NewGateServiceClient(conn)

	// Create a context for registration that can be cancelled
	streamCtx, streamCancel := context.WithCancel(context.Background())

	// Register with the gate
	stream, err := client.Register(streamCtx, &gate_pb.Empty{})
	if err != nil {
		streamCancel()
		conn.Close()
		return fmt.Errorf("failed to register with gate: %w", err)
	}

	// Receive the registration response
	msgAny, err := stream.Recv()
	if err != nil {
		streamCancel()
		conn.Close()
		return fmt.Errorf("failed to receive registration response: %w", err)
	}

	msg, err := msgAny.UnmarshalNew()
	if err != nil {
		streamCancel()
		conn.Close()
		return fmt.Errorf("failed to unmarshal registration response: %w", err)
	}

	regResp, ok := msg.(*gate_pb.RegisterResponse)
	if !ok {
		streamCancel()
		conn.Close()
		return fmt.Errorf("unexpected response type: %T", msg)
	}

	// Update client state
	c.mu.Lock()
	if c.closed {
		streamCancel()
		conn.Close()
		c.mu.Unlock()
		return ErrClientClosed
	}

	c.conn = conn
	c.client = client
	c.stream = stream
	c.streamCancel = streamCancel
	c.clientID = regResp.ClientId
	c.currentAddr = addr
	c.connected = true
	c.mu.Unlock()

	c.logger.Infof("Connected to gate at %s, client ID: %s", addr, regResp.ClientId)

	// Notify connection callback
	if c.options.OnConnect != nil {
		c.options.OnConnect(regResp.ClientId)
	}

	// Start message receiver goroutine
	go c.receiveMessages()

	return nil
}

// receiveMessages continuously receives push messages from the gate.
func (c *Client) receiveMessages() {
	for {
		c.mu.RLock()
		stream := c.stream
		closed := c.closed
		connected := c.connected
		c.mu.RUnlock()

		if closed || !connected || stream == nil {
			return
		}

		msgAny, err := stream.Recv()
		if err != nil {
			c.mu.RLock()
			wasClosed := c.closed
			c.mu.RUnlock()

			if wasClosed {
				return
			}

			c.logger.Errorf("Error receiving message from stream: %v", err)

			// Notify disconnect callback
			if c.options.OnDisconnect != nil {
				c.options.OnDisconnect(err)
			}

			c.handleDisconnect()
			return
		}

		msg, err := msgAny.UnmarshalNew()
		if err != nil {
			c.logger.Errorf("Failed to unmarshal pushed message: %v", err)
			continue
		}

		// Call message handler if set
		if c.options.OnMessage != nil {
			c.options.OnMessage(msg)
		}

		// Send to message channel (non-blocking)
		select {
		case c.messageChan <- msg:
		default:
			c.logger.Warnf("Message channel full, dropping message")
		}
	}
}

// handleDisconnect handles a disconnection event.
func (c *Client) handleDisconnect() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return
	}

	c.connected = false

	if c.streamCancel != nil {
		c.streamCancel()
		c.streamCancel = nil
	}

	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}

	c.stream = nil
	c.client = nil
	c.clientID = ""
	c.currentAddr = ""
}

// Close closes the client and releases all resources.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}

	c.closed = true
	c.connected = false

	if c.cancel != nil {
		c.cancel()
	}

	if c.streamCancel != nil {
		c.streamCancel()
	}

	var err error
	if c.conn != nil {
		err = c.conn.Close()
	}

	close(c.messageChan)

	c.logger.Infof("Client closed")
	return err
}

// IsConnected returns true if the client is connected to a gate.
func (c *Client) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected && !c.closed
}

// ClientID returns the current client ID assigned by the gate.
// Returns empty string if not connected.
func (c *Client) ClientID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.clientID
}

// CurrentAddress returns the address of the currently connected gate.
// Returns empty string if not connected.
func (c *Client) CurrentAddress() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.currentAddr
}

// MessageChan returns a channel for receiving push messages from objects.
// The channel is closed when the client is closed.
func (c *Client) MessageChan() <-chan proto.Message {
	return c.messageChan
}

// CallObject calls a method on an object and returns the response.
// The request and response are protobuf messages.
func (c *Client) CallObject(ctx context.Context, objectType, objectID, method string, request proto.Message) (proto.Message, error) {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return nil, ErrClientClosed
	}
	if !c.connected {
		c.mu.RUnlock()
		return nil, ErrNotConnected
	}
	client := c.client
	clientID := c.clientID
	c.mu.RUnlock()

	// Apply default timeout if context has no deadline
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.options.CallTimeout)
		defer cancel()
	}

	// Marshal request to Any
	anyReq, err := anypb.New(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Make the call
	req := &gate_pb.CallObjectRequest{
		ClientId: clientID,
		Type:     objectType,
		Id:       objectID,
		Method:   method,
		Request:  anyReq,
	}

	resp, err := client.CallObject(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("CallObject failed: %w", err)
	}

	if resp == nil || resp.Response == nil {
		return nil, nil
	}

	// Unmarshal response
	ret, err := resp.Response.UnmarshalNew()
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return ret, nil
}

// CallObjectAny calls a method on an object using anypb.Any directly.
// This is useful when working with generic protobuf messages.
func (c *Client) CallObjectAny(ctx context.Context, objectType, objectID, method string, request *anypb.Any) (*anypb.Any, error) {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return nil, ErrClientClosed
	}
	if !c.connected {
		c.mu.RUnlock()
		return nil, ErrNotConnected
	}
	client := c.client
	clientID := c.clientID
	c.mu.RUnlock()

	// Apply default timeout if context has no deadline
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.options.CallTimeout)
		defer cancel()
	}

	req := &gate_pb.CallObjectRequest{
		ClientId: clientID,
		Type:     objectType,
		Id:       objectID,
		Method:   method,
		Request:  request,
	}

	resp, err := client.CallObject(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("CallObject failed: %w", err)
	}

	if resp == nil {
		return nil, nil
	}

	return resp.Response, nil
}

// CreateObject creates a new object of the specified type with the given ID.
func (c *Client) CreateObject(ctx context.Context, objectType, objectID string) (string, error) {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return "", ErrClientClosed
	}
	if !c.connected {
		c.mu.RUnlock()
		return "", ErrNotConnected
	}
	client := c.client
	c.mu.RUnlock()

	// Apply default timeout if context has no deadline
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.options.CallTimeout)
		defer cancel()
	}

	req := &gate_pb.CreateObjectRequest{
		Type: objectType,
		Id:   objectID,
	}

	resp, err := client.CreateObject(ctx, req)
	if err != nil {
		return "", fmt.Errorf("CreateObject failed: %w", err)
	}

	return resp.Id, nil
}

// DeleteObject deletes an object by its ID.
func (c *Client) DeleteObject(ctx context.Context, objectID string) error {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return ErrClientClosed
	}
	if !c.connected {
		c.mu.RUnlock()
		return ErrNotConnected
	}
	client := c.client
	c.mu.RUnlock()

	// Apply default timeout if context has no deadline
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.options.CallTimeout)
		defer cancel()
	}

	req := &gate_pb.DeleteObjectRequest{
		Id: objectID,
	}

	_, err := client.DeleteObject(ctx, req)
	if err != nil {
		return fmt.Errorf("DeleteObject failed: %w", err)
	}

	return nil
}

// Reconnect attempts to reconnect to a gate server.
// It first closes any existing connection, then tries to connect to any available gate.
func (c *Client) Reconnect(ctx context.Context) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return ErrClientClosed
	}

	// Clean up existing connection
	if c.streamCancel != nil {
		c.streamCancel()
		c.streamCancel = nil
	}
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
	c.connected = false
	c.stream = nil
	c.client = nil
	c.clientID = ""
	c.mu.Unlock()

	// Try to connect
	return c.Connect(ctx)
}

// WaitForConnection waits until the client is connected or the context is cancelled.
func (c *Client) WaitForConnection(ctx context.Context) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if c.IsConnected() {
				return nil
			}
		}
	}
}

// ConnectionState returns the current gRPC connection state.
// Returns connectivity.Shutdown if not connected.
func (c *Client) ConnectionState() connectivity.State {
	c.mu.RLock()
	conn := c.conn
	c.mu.RUnlock()

	if conn == nil {
		return connectivity.Shutdown
	}

	return conn.GetState()
}
