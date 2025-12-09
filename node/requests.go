package node

import (
	"context"
	"fmt"

	"github.com/xiaonanln/goverse/util/postgres"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

// ProcessPendingRequestsWithDB processes all pending requests for an object sequentially
// using the provided postgres database connection
func (node *Node) ProcessPendingRequestsWithDB(ctx context.Context, objectID string, db *postgres.DB) error {
	node.logger.Infof("Processing pending requests for object %s", objectID)

	// Get all pending requests for this object, ordered by ID
	requests, err := db.GetPendingRequests(ctx, objectID)
	if err != nil {
		return fmt.Errorf("failed to get pending requests: %w", err)
	}

	if len(requests) == 0 {
		node.logger.Infof("No pending requests for object %s", objectID)
		return nil
	}

	node.logger.Infof("Found %d pending requests for object %s", len(requests), objectID)

	// Get the last processed ID to skip already executed calls
	lastProcessedID, err := db.GetLastProcessedID(ctx, objectID)
	if err != nil {
		return fmt.Errorf("failed to get last processed ID: %w", err)
	}

	node.logger.Infof("Last processed ID for object %s: %d", objectID, lastProcessedID)

	// Process each request sequentially
	processedCount := 0
	for _, req := range requests {
		// Skip requests that have already been processed
		if req.ID <= lastProcessedID {
			node.logger.Infof("Skipping already processed request %s (ID: %d)", req.RequestID, req.ID)
			continue
		}

		// Update status to processing
		err = db.UpdateRequestStatus(ctx, req.RequestID, postgres.RequestStatusProcessing, nil, "")
		if err != nil {
			node.logger.Errorf("Failed to update request %s to processing: %v", req.RequestID, err)
			continue
		}

		// Unmarshal the request data
		// The request data is stored as a marshaled anypb.Any
		var requestProto proto.Message
		if len(req.RequestData) > 0 {
			requestAny := &anypb.Any{}
			if err := proto.Unmarshal(req.RequestData, requestAny); err != nil {
				node.logger.Errorf("Failed to unmarshal request Any for %s: %v", req.RequestID, err)
				// Mark as failed
				_ = db.UpdateRequestStatus(ctx, req.RequestID, postgres.RequestStatusFailed, nil, fmt.Sprintf("failed to unmarshal request: %v", err))
				continue
			}

			// Unmarshal the Any to get the actual request proto
			requestProto, err = requestAny.UnmarshalNew()
			if err != nil {
				node.logger.Errorf("Failed to unmarshal request proto from Any for %s: %v", req.RequestID, err)
				// Mark as failed
				_ = db.UpdateRequestStatus(ctx, req.RequestID, postgres.RequestStatusFailed, nil, fmt.Sprintf("failed to unmarshal request proto: %v", err))
				continue
			}
		}

		// Execute the method on the object
		node.logger.Infof("Executing method %s on object %s for request %s", req.MethodName, objectID, req.RequestID)
		response, err := node.CallObject(ctx, req.ObjectType, objectID, req.MethodName, requestProto)
		if err != nil {
			node.logger.Errorf("Failed to execute method %s on object %s: %v", req.MethodName, objectID, err)
			// Mark as failed
			_ = db.UpdateRequestStatus(ctx, req.RequestID, postgres.RequestStatusFailed, nil, fmt.Sprintf("method execution failed: %v", err))
			continue
		}

		// Marshal the response
		var responseData []byte
		if response != nil {
			responseAny, err := anypb.New(response)
			if err != nil {
				node.logger.Errorf("Failed to marshal response for request %s: %v", req.RequestID, err)
				// Mark as failed
				_ = db.UpdateRequestStatus(ctx, req.RequestID, postgres.RequestStatusFailed, nil, fmt.Sprintf("failed to marshal response: %v", err))
				continue
			}

			responseData, err = proto.Marshal(responseAny)
			if err != nil {
				node.logger.Errorf("Failed to marshal response Any for request %s: %v", req.RequestID, err)
				// Mark as failed
				_ = db.UpdateRequestStatus(ctx, req.RequestID, postgres.RequestStatusFailed, nil, fmt.Sprintf("failed to marshal response: %v", err))
				continue
			}
		}

		// Mark as completed
		err = db.UpdateRequestStatus(ctx, req.RequestID, postgres.RequestStatusCompleted, responseData, "")
		if err != nil {
			node.logger.Errorf("Failed to update request %s to completed: %v", req.RequestID, err)
			continue
		}

		processedCount++
		node.logger.Infof("Successfully processed request %s (ID: %d) for object %s", req.RequestID, req.ID, objectID)
	}

	node.logger.Infof("Processed %d requests for object %s", processedCount, objectID)
	return nil
}
