package inspectserver

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/cmd/inspector/graph"
	"github.com/xiaonanln/goverse/cmd/inspector/models"
)

// TestSSEEndpoint_InitialState tests that the SSE endpoint sends initial state
func TestSSEEndpoint_InitialState(t *testing.T) {
	pg := graph.NewGoverseGraph(0)

	// Add some initial data
	node := models.GoverseNode{ID: "node1", Label: "Node 1"}
	pg.AddOrUpdateNode(node)
	obj := models.GoverseObject{ID: "obj1", Label: "Object 1", GoverseNodeID: "node1"}
	pg.AddOrUpdateObject(obj)

	server := New(pg, Config{
		GRPCAddr:  ":0",
		HTTPAddr:  ":0",
		StaticDir: ".",
	})

	handler := server.createHTTPHandler()

	// Create a test request
	req := httptest.NewRequest(http.MethodGet, "/events/stream", nil)

	// Create a pipe to capture streaming response
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req = req.WithContext(ctx)

	// Use a recorder with custom flusher
	rr := &flushableRecorder{
		ResponseRecorder: httptest.NewRecorder(),
		done:             make(chan struct{}),
	}

	// Run the handler in a goroutine
	go func() {
		handler.ServeHTTP(rr, req)
		close(rr.done)
	}()

	// Wait a bit for the handler to write the initial event
	time.Sleep(100 * time.Millisecond)

	// Cancel the context to stop the handler
	cancel()

	// Wait for handler to finish
	select {
	case <-rr.done:
	case <-time.After(1 * time.Second):
		t.Fatal("Handler did not finish in time")
	}

	// Parse the response
	body := rr.Body.String()
	if body == "" {
		t.Fatal("Expected SSE response body, got empty")
	}

	// Check for initial event
	if !strings.Contains(body, "event: initial") {
		t.Fatalf("Expected 'event: initial' in response, got: %s", body)
	}

	if !strings.Contains(body, "data:") {
		t.Fatalf("Expected 'data:' in response, got: %s", body)
	}

	// Parse the JSON data
	lines := strings.Split(body, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "data:") {
			jsonData := strings.TrimPrefix(line, "data:")
			jsonData = strings.TrimSpace(jsonData)

			var data struct {
				Type           string                 `json:"type"`
				GoverseNodes   []models.GoverseNode   `json:"goverse_nodes"`
				GoverseObjects []models.GoverseObject `json:"goverse_objects"`
			}
			if err := json.Unmarshal([]byte(jsonData), &data); err != nil {
				t.Fatalf("Failed to parse JSON data: %v, data: %s", err, jsonData)
			}

			if data.Type != "initial" {
				t.Fatalf("Expected type 'initial', got '%s'", data.Type)
			}

			if len(data.GoverseNodes) != 1 {
				t.Fatalf("Expected 1 node, got %d", len(data.GoverseNodes))
			}

			if len(data.GoverseObjects) != 1 {
				t.Fatalf("Expected 1 object, got %d", len(data.GoverseObjects))
			}

			break
		}
	}
}

// TestSSEEndpoint_PushUpdates tests that the SSE endpoint pushes updates
func TestSSEEndpoint_PushUpdates(t *testing.T) {
	pg := graph.NewGoverseGraph(0)

	server := New(pg, Config{
		GRPCAddr:  ":0",
		HTTPAddr:  ":0",
		StaticDir: ".",
	})

	handler := server.createHTTPHandler()

	// Create a test request with a longer timeout
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/events/stream", nil)
	req = req.WithContext(ctx)

	rr := &flushableRecorder{
		ResponseRecorder: httptest.NewRecorder(),
		done:             make(chan struct{}),
	}

	// Run the handler in a goroutine
	go func() {
		handler.ServeHTTP(rr, req)
		close(rr.done)
	}()

	// Wait for initial event
	time.Sleep(100 * time.Millisecond)

	// Add a node (should trigger node_added event)
	node := models.GoverseNode{ID: "node1", Label: "Node 1"}
	pg.AddOrUpdateNode(node)

	// Wait for the event to be processed
	time.Sleep(100 * time.Millisecond)

	// Cancel and wait for handler
	cancel()
	select {
	case <-rr.done:
	case <-time.After(1 * time.Second):
		t.Fatal("Handler did not finish in time")
	}

	body := rr.Body.String()

	// Check for node_added event
	if !strings.Contains(body, "event: node_added") {
		t.Fatalf("Expected 'event: node_added' in response, got: %s", body)
	}
}

// TestSSEEndpoint_MethodNotAllowed tests that non-GET requests are rejected
func TestSSEEndpoint_MethodNotAllowed(t *testing.T) {
	pg := graph.NewGoverseGraph(0)

	server := New(pg, Config{
		GRPCAddr:  ":0",
		HTTPAddr:  ":0",
		StaticDir: ".",
	})

	handler := server.createHTTPHandler()

	// Test POST method
	req := httptest.NewRequest(http.MethodPost, "/events/stream", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("Expected status 405 for POST, got %d", rr.Code)
	}
}

// TestSSEEndpoint_MultipleClients tests multiple SSE clients
func TestSSEEndpoint_MultipleClients(t *testing.T) {
	pg := graph.NewGoverseGraph(0)

	server := New(pg, Config{
		GRPCAddr:  ":0",
		HTTPAddr:  ":0",
		StaticDir: ".",
	})

	handler := server.createHTTPHandler()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Create two clients
	req1 := httptest.NewRequest(http.MethodGet, "/events/stream", nil)
	req1 = req1.WithContext(ctx)
	rr1 := &flushableRecorder{
		ResponseRecorder: httptest.NewRecorder(),
		done:             make(chan struct{}),
	}

	req2 := httptest.NewRequest(http.MethodGet, "/events/stream", nil)
	req2 = req2.WithContext(ctx)
	rr2 := &flushableRecorder{
		ResponseRecorder: httptest.NewRecorder(),
		done:             make(chan struct{}),
	}

	// Start both handlers
	go func() {
		handler.ServeHTTP(rr1, req1)
		close(rr1.done)
	}()
	go func() {
		handler.ServeHTTP(rr2, req2)
		close(rr2.done)
	}()

	// Wait for initial events
	time.Sleep(100 * time.Millisecond)

	// Add a node
	node := models.GoverseNode{ID: "node1", Label: "Node 1"}
	pg.AddOrUpdateNode(node)

	// Wait for events
	time.Sleep(100 * time.Millisecond)

	cancel()

	// Wait for handlers
	select {
	case <-rr1.done:
	case <-time.After(1 * time.Second):
		t.Fatal("Handler 1 did not finish in time")
	}
	select {
	case <-rr2.done:
	case <-time.After(1 * time.Second):
		t.Fatal("Handler 2 did not finish in time")
	}

	// Both should have received the node_added event
	if !strings.Contains(rr1.Body.String(), "event: node_added") {
		t.Fatal("Client 1 did not receive node_added event")
	}
	if !strings.Contains(rr2.Body.String(), "event: node_added") {
		t.Fatal("Client 2 did not receive node_added event")
	}
}

// flushableRecorder wraps httptest.ResponseRecorder with Flush support
type flushableRecorder struct {
	*httptest.ResponseRecorder
	done chan struct{}
}

func (r *flushableRecorder) Flush() {
	// No-op for testing, actual flush happens in the recorder
}

// parseSSEEvents parses SSE events from a reader
func parseSSEEvents(body string) []map[string]string {
	events := []map[string]string{}
	scanner := bufio.NewScanner(strings.NewReader(body))

	currentEvent := make(map[string]string)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if len(currentEvent) > 0 {
				events = append(events, currentEvent)
				currentEvent = make(map[string]string)
			}
			continue
		}
		if strings.HasPrefix(line, "event:") {
			currentEvent["event"] = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			currentEvent["data"] = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		}
	}
	if len(currentEvent) > 0 {
		events = append(events, currentEvent)
	}
	return events
}
