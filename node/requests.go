package node

import (
	"context"
	"fmt"

	"github.com/xiaonanln/goverse/util/postgres"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

// processPendingRequestsImpl implements ProcessPendingRequests with postgres DB
// This is called from node.go to avoid import cycles
func (node *Node) processPendingRequestsImpl(ctx context.Context, objectID string, dbInterface interface{}) error {
	db, ok := dbInterface.(*postgres.DB)
	if !ok {
		return fmt.Errorf("invalid database interface")
	}
	return node.ProcessPendingRequestsWithDB(ctx, objectID, db)
}

// ProcessPendingRequestsWithDB processes all pending requests for an object sequentially
// using the provided postgres database connection
func (node *Node) ProcessPendingRequestsWithDB(ctx context.Context, objectID string, db *postgres.DB) error {
	node.logger.Infof("Processing pending requests for object %s", objectID)

	// Get the object to ensure it exists
	node.objectsMu.RLock()
	_, exists := node.objects[objectID]
	node.objectsMu.RUnlock()

	if !exists {
		node.logger.Warnf("Object %s not found, cannot process pending requests", objectID)
		return fmt.Errorf("object %s not found", objectID)
	}

	// Get lastProcessedID from the database
	// TODO: Store lastProcessedID on the object itself for better performance
	lastProcessedID, err := db.GetLastProcessedID(ctx, objectID)
	if err != nil {
		return fmt.Errorf("failed to get last processed ID: %w", err)
	}

	node.logger.Infof("Last processed ID for object %s: %d", objectID, lastProcessedID)

	// Get pending requests after the last processed ID
	requests, err := db.GetPendingRequests(ctx, objectID)
	if err != nil {
		return fmt.Errorf("failed to get pending requests: %w", err)
	}

	// Filter requests with ID > lastProcessedID
	var filteredRequests []*postgres.RequestData
	for _, req := range requests {
		if req.ID > lastProcessedID {
			filteredRequests = append(filteredRequests, req)
		}
	}

	if len(filteredRequests) == 0 {
		node.logger.Infof("No new pending requests for object %s after ID %d", objectID, lastProcessedID)
		return nil
	}

	node.logger.Infof("Found %d pending requests for object %s", len(filteredRequests), objectID)

	// Process each request sequentially
	processedCount := 0
	for _, req := range filteredRequests {
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
