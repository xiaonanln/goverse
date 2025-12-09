package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// RequestStatus represents the status of a request
type RequestStatus string

const (
	RequestStatusPending    RequestStatus = "pending"
	RequestStatusProcessing RequestStatus = "processing"
	RequestStatusCompleted  RequestStatus = "completed"
	RequestStatusFailed     RequestStatus = "failed"
)

// RequestData represents a request in the database
type RequestData struct {
	ID           int64
	RequestID    string
	ObjectID     string
	ObjectType   string
	MethodName   string
	RequestData  []byte
	ResultData   []byte
	ErrorMessage string
	Status       RequestStatus
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// InsertRequest inserts a new request into the database
// Returns the auto-generated ID and any error
func (db *DB) InsertRequest(ctx context.Context, requestID, objectID, objectType, methodName string, requestData []byte) (int64, error) {
	if requestID == "" {
		return 0, fmt.Errorf("request_id cannot be empty")
	}
	if objectID == "" {
		return 0, fmt.Errorf("object_id cannot be empty")
	}
	if objectType == "" {
		return 0, fmt.Errorf("object_type cannot be empty")
	}
	if methodName == "" {
		return 0, fmt.Errorf("method_name cannot be empty")
	}
	if requestData == nil {
		requestData = []byte{}
	}

	query := `
		INSERT INTO goverse_requests (request_id, object_id, object_type, method_name, request_data, status)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id
	`

	var id int64
	err := db.conn.QueryRowContext(ctx, query, requestID, objectID, objectType, methodName, requestData, RequestStatusPending).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("failed to insert request: %w", err)
	}

	return id, nil
}

// GetRequest retrieves a request by its request_id
func (db *DB) GetRequest(ctx context.Context, requestID string) (*RequestData, error) {
	if requestID == "" {
		return nil, fmt.Errorf("request_id cannot be empty")
	}

	query := `
		SELECT id, request_id, object_id, object_type, method_name, request_data, 
		       result_data, error_message, status, created_at, updated_at
		FROM goverse_requests
		WHERE request_id = $1
	`

	var req RequestData
	err := db.conn.QueryRowContext(ctx, query, requestID).Scan(
		&req.ID,
		&req.RequestID,
		&req.ObjectID,
		&req.ObjectType,
		&req.MethodName,
		&req.RequestData,
		&req.ResultData,
		&req.ErrorMessage,
		&req.Status,
		&req.CreatedAt,
		&req.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil // Request not found is not an error, just return nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get request: %w", err)
	}

	return &req, nil
}

// UpdateRequestStatus updates the status of a request
// For completed status, resultData must be provided
// For failed status, errorMessage must be provided
func (db *DB) UpdateRequestStatus(ctx context.Context, requestID string, status RequestStatus, resultData []byte, errorMessage string) error {
	if requestID == "" {
		return fmt.Errorf("request_id cannot be empty")
	}

	// Validate status-specific requirements
	if status == RequestStatusCompleted && resultData == nil {
		return fmt.Errorf("result_data is required for completed status")
	}
	if status == RequestStatusFailed && errorMessage == "" {
		return fmt.Errorf("error_message is required for failed status")
	}

	query := `
		UPDATE goverse_requests
		SET status = $1, result_data = $2, error_message = $3
		WHERE request_id = $4
	`

	result, err := db.conn.ExecContext(ctx, query, status, resultData, errorMessage, requestID)
	if err != nil {
		return fmt.Errorf("failed to update request status: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("request not found: %s", requestID)
	}

	return nil
}

// GetPendingRequests retrieves all pending requests for an object, ordered by id
func (db *DB) GetPendingRequests(ctx context.Context, objectID string) ([]*RequestData, error) {
	if objectID == "" {
		return nil, fmt.Errorf("object_id cannot be empty")
	}

	query := `
		SELECT id, request_id, object_id, object_type, method_name, request_data,
		       result_data, error_message, status, created_at, updated_at
		FROM goverse_requests
		WHERE object_id = $1 AND status = $2
		ORDER BY id ASC
	`

	rows, err := db.conn.QueryContext(ctx, query, objectID, RequestStatusPending)
	if err != nil {
		return nil, fmt.Errorf("failed to get pending requests: %w", err)
	}
	defer rows.Close()

	var requests []*RequestData
	for rows.Next() {
		var req RequestData
		err := rows.Scan(
			&req.ID,
			&req.RequestID,
			&req.ObjectID,
			&req.ObjectType,
			&req.MethodName,
			&req.RequestData,
			&req.ResultData,
			&req.ErrorMessage,
			&req.Status,
			&req.CreatedAt,
			&req.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan request row: %w", err)
		}
		requests = append(requests, &req)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating request rows: %w", err)
	}

	return requests, nil
}

// GetLastProcessedID retrieves the last processed request ID for an object
// This is stored in a separate column in the goverse_objects table
// Returns 0 if no requests have been processed yet
func (db *DB) GetLastProcessedID(ctx context.Context, objectID string) (int64, error) {
	if objectID == "" {
		return 0, fmt.Errorf("object_id cannot be empty")
	}

	// We'll store last_processed_id in the object data as a simple approach
	// Alternatively, we could extend the goverse_objects table with a last_processed_request_id column
	// For now, we'll query the maximum completed request ID for this object
	query := `
		SELECT COALESCE(MAX(id), 0)
		FROM goverse_requests
		WHERE object_id = $1 AND status IN ($2, $3)
	`

	var lastProcessedID int64
	err := db.conn.QueryRowContext(ctx, query, objectID, RequestStatusCompleted, RequestStatusFailed).Scan(&lastProcessedID)
	if err != nil {
		return 0, fmt.Errorf("failed to get last processed ID: %w", err)
	}

	return lastProcessedID, nil
}

// WaitForRequestCompletion polls the database for a request to complete
// Returns the completed request data or an error if timeout is reached
func (db *DB) WaitForRequestCompletion(ctx context.Context, requestID string, pollInterval time.Duration, timeout time.Duration) (*RequestData, error) {
	if requestID == "" {
		return nil, fmt.Errorf("request_id cannot be empty")
	}

	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		// Check if context is done
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			// Check timeout
			if time.Now().After(deadline) {
				return nil, errors.New("timeout waiting for request completion")
			}

			// Check request status
			req, err := db.GetRequest(ctx, requestID)
			if err != nil {
				return nil, err
			}

			if req == nil {
				return nil, fmt.Errorf("request not found: %s", requestID)
			}

			// If completed or failed, return the result
			if req.Status == RequestStatusCompleted || req.Status == RequestStatusFailed {
				return req, nil
			}

			// Otherwise, continue polling
		}
	}
}
