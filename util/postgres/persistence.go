package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/xiaonanln/goverse/object"
)

// ObjectData represents a persisted object in the database
type ObjectData struct {
	ObjectID   string
	ObjectType string
	Data       []byte
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// SaveObject saves an object to the database
// If the object already exists, it updates the data and updated_at timestamp
func (db *DB) SaveObject(ctx context.Context, objectID, objectType string, data []byte) error {
	if objectID == "" {
		return fmt.Errorf("object_id cannot be empty")
	}
	if objectType == "" {
		return fmt.Errorf("object_type cannot be empty")
	}
	if data == nil {
		data = []byte("{}")
	}

	query := `
		INSERT INTO goverse_objects (object_id, object_type, data, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (object_id) DO UPDATE
		SET data = $3, updated_at = $5, object_type = $2
	`

	now := time.Now()
	_, err := db.conn.ExecContext(ctx, query, objectID, objectType, data, now, now)
	if err != nil {
		return fmt.Errorf("failed to save object: %w", err)
	}

	return nil
}

// LoadObject retrieves an object from the database by its ID
func (db *DB) LoadObject(ctx context.Context, objectID string) ([]byte, error) {
	if objectID == "" {
		return nil, fmt.Errorf("object_id cannot be empty")
	}

	query := `
		SELECT data
		FROM goverse_objects
		WHERE object_id = $1
	`

	var jsonData []byte
	err := db.conn.QueryRowContext(ctx, query, objectID).Scan(&jsonData)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("object not found: %s", objectID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to load object: %w", err)
	}

	return jsonData, nil
}

// DeleteObject deletes an object from the database
func (db *DB) DeleteObject(ctx context.Context, objectID string) error {
	if objectID == "" {
		return fmt.Errorf("object_id cannot be empty")
	}

	query := `DELETE FROM goverse_objects WHERE object_id = $1`
	result, err := db.conn.ExecContext(ctx, query, objectID)
	if err != nil {
		return fmt.Errorf("failed to delete object: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("object not found: %s", objectID)
	}

	return nil
}

// ObjectExists checks if an object exists in the database
func (db *DB) ObjectExists(ctx context.Context, objectID string) (bool, error) {
	if objectID == "" {
		return false, fmt.Errorf("object_id cannot be empty")
	}

	query := `SELECT EXISTS(SELECT 1 FROM goverse_objects WHERE object_id = $1)`
	var exists bool
	err := db.conn.QueryRowContext(ctx, query, objectID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check object existence: %w", err)
	}

	return exists, nil
}

// ListObjectsByType retrieves all objects of a specific type
func (db *DB) ListObjectsByType(ctx context.Context, objectType string) ([]*ObjectData, error) {
	if objectType == "" {
		return nil, fmt.Errorf("object_type cannot be empty")
	}

	query := `
		SELECT object_id, object_type, data, created_at, updated_at
		FROM goverse_objects
		WHERE object_type = $1
		ORDER BY created_at DESC
	`

	rows, err := db.conn.QueryContext(ctx, query, objectType)
	if err != nil {
		return nil, fmt.Errorf("failed to list objects: %w", err)
	}
	defer rows.Close()

	var objects []*ObjectData
	for rows.Next() {
		var objData ObjectData

		err := rows.Scan(
			&objData.ObjectID,
			&objData.ObjectType,
			&objData.Data,
			&objData.CreatedAt,
			&objData.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		objects = append(objects, &objData)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return objects, nil
}

// InsertOrGetReliableCall atomically inserts a new reliable call or returns the existing one
// Uses INSERT ... ON CONFLICT to ensure atomicity and handle duplicates
func (db *DB) InsertOrGetReliableCall(ctx context.Context, requestID string, objectID string, objectType string, methodName string, requestData []byte) (*object.ReliableCall, error) {
	if requestID == "" {
		return nil, fmt.Errorf("request_id cannot be empty")
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

	query := `
		INSERT INTO goverse_requests (request_id, object_id, object_type, method_name, request_data, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, 'pending', $6, $6)
		ON CONFLICT (request_id) DO NOTHING
		RETURNING id, request_id, object_id, object_type, method_name, request_data, result_data, error_message, status, created_at, updated_at
	`

	now := time.Now()
	var rc object.ReliableCall
	var resultData sql.NullString
	var errorMessage sql.NullString

	err := db.conn.QueryRowContext(ctx, query, requestID, objectID, objectType, methodName, requestData, now).Scan(
		&rc.ID,
		&rc.RequestID,
		&rc.ObjectID,
		&rc.ObjectType,
		&rc.MethodName,
		&rc.RequestData,
		&resultData,
		&errorMessage,
		&rc.Status,
		&rc.CreatedAt,
		&rc.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		// Conflict occurred, fetch the existing record
		return db.GetReliableCall(ctx, requestID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to insert or get reliable call: %w", err)
	}

	if resultData.Valid {
		rc.ResultData = []byte(resultData.String)
	}
	if errorMessage.Valid {
		rc.Error = errorMessage.String
	}

	return &rc, nil
}

// UpdateReliableCallStatus updates the status and result of a reliable call
func (db *DB) UpdateReliableCallStatus(ctx context.Context, id int64, status string, resultData []byte, errorMessage string) error {
	if id <= 0 {
		return fmt.Errorf("id must be positive")
	}
	if status == "" {
		return fmt.Errorf("status cannot be empty")
	}

	query := `
		UPDATE goverse_requests
		SET status = $1, result_data = $2, error_message = $3
		WHERE id = $4
	`

	result, err := db.conn.ExecContext(ctx, query, status, resultData, errorMessage, id)
	if err != nil {
		return fmt.Errorf("failed to update reliable call status: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("reliable call not found: %d", id)
	}

	return nil
}

// GetPendingReliableCalls retrieves pending reliable calls for an object, ordered by id
// Only returns calls with id > nextRcid to support incremental processing
func (db *DB) GetPendingReliableCalls(ctx context.Context, objectID string, nextRcid int64) ([]*object.ReliableCall, error) {
	if objectID == "" {
		return nil, fmt.Errorf("object_id cannot be empty")
	}

	query := `
		SELECT id, request_id, object_id, object_type, method_name, request_data, result_data, error_message, status, created_at, updated_at
		FROM goverse_requests
		WHERE object_id = $1 AND id > $2 AND status = 'pending'
		ORDER BY id ASC
	`

	rows, err := db.conn.QueryContext(ctx, query, objectID, nextRcid)
	if err != nil {
		return nil, fmt.Errorf("failed to get pending reliable calls: %w", err)
	}
	defer rows.Close()

	var calls []*object.ReliableCall
	for rows.Next() {
		var rc object.ReliableCall
		var resultData sql.NullString
		var errorMessage sql.NullString

		err := rows.Scan(
			&rc.ID,
			&rc.RequestID,
			&rc.ObjectID,
			&rc.ObjectType,
			&rc.MethodName,
			&rc.RequestData,
			&resultData,
			&errorMessage,
			&rc.Status,
			&rc.CreatedAt,
			&rc.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		if resultData.Valid {
			rc.ResultData = []byte(resultData.String)
		}
		if errorMessage.Valid {
			rc.Error = errorMessage.String
		}

		calls = append(calls, &rc)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return calls, nil
}

// GetReliableCall retrieves a reliable call by its request ID
func (db *DB) GetReliableCall(ctx context.Context, requestID string) (*object.ReliableCall, error) {
	if requestID == "" {
		return nil, fmt.Errorf("request_id cannot be empty")
	}

	query := `
		SELECT id, request_id, object_id, object_type, method_name, request_data, result_data, error_message, status, created_at, updated_at
		FROM goverse_requests
		WHERE request_id = $1
	`

	var rc object.ReliableCall
	var resultData sql.NullString
	var errorMessage sql.NullString

	err := db.conn.QueryRowContext(ctx, query, requestID).Scan(
		&rc.ID,
		&rc.RequestID,
		&rc.ObjectID,
		&rc.ObjectType,
		&rc.MethodName,
		&rc.RequestData,
		&resultData,
		&errorMessage,
		&rc.Status,
		&rc.CreatedAt,
		&rc.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("reliable call not found: %s", requestID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get reliable call: %w", err)
	}

	if resultData.Valid {
		rc.ResultData = []byte(resultData.String)
	}
	if errorMessage.Valid {
		rc.Error = errorMessage.String
	}

	return &rc, nil
}
