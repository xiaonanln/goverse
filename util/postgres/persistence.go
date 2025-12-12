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
	NextRcseq  int64
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// SaveObject saves an object to the database
// If the object already exists, it updates the data, next_rcseq and updated_at timestamp
func (db *DB) SaveObject(ctx context.Context, objectID, objectType string, data []byte, nextRcseq int64) error {
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
		INSERT INTO goverse_objects (object_id, object_type, data, next_rcseq, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (object_id) DO UPDATE
		SET data = $3, next_rcseq = $4, updated_at = $6, object_type = $2
	`

	now := time.Now()
	_, err := db.conn.ExecContext(ctx, query, objectID, objectType, data, nextRcseq, now, now)
	if err != nil {
		return fmt.Errorf("failed to save object: %w", err)
	}

	return nil
}

// LoadObject retrieves an object from the database by its ID
func (db *DB) LoadObject(ctx context.Context, objectID string) ([]byte, int64, error) {
	if objectID == "" {
		return nil, 0, fmt.Errorf("object_id cannot be empty")
	}

	query := `
		SELECT data, next_rcseq
		FROM goverse_objects
		WHERE object_id = $1
	`

	var jsonData []byte
	var nextRcseq int64
	err := db.conn.QueryRowContext(ctx, query, objectID).Scan(&jsonData, &nextRcseq)

	if err == sql.ErrNoRows {
		return nil, 0, fmt.Errorf("object not found: %s", objectID)
	}
	if err != nil {
		return nil, 0, fmt.Errorf("failed to load object: %w", err)
	}

	return jsonData, nextRcseq, nil
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
		SELECT object_id, object_type, data, next_rcseq, created_at, updated_at
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
			&objData.NextRcseq,
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

	query := `
		INSERT INTO goverse_reliable_calls (call_id, object_id, object_type, method_name, request_data, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, 'pending', $6, $6)
		ON CONFLICT (call_id) DO NOTHING
		RETURNING seq, call_id, object_id, object_type, method_name, request_data, result_data, error_message, status, created_at, updated_at
	`

	// Debug: Log the insert parameters
	fmt.Printf("[DEBUG InsertOrGetReliableCall] call_id=%s, object_id=%s, object_type=%s, method_name=%s\n", requestID, objectID, objectType, methodName)

	now := time.Now()
	var rc object.ReliableCall
	var resultData sql.NullString
	var errorMessage sql.NullString

	err := db.conn.QueryRowContext(ctx, query, requestID, objectID, objectType, methodName, requestData, now).Scan(
		&rc.Seq,
		&rc.CallID,
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
		fmt.Printf("[DEBUG InsertOrGetReliableCall] Conflict occurred, fetching existing record for call_id=%s\n", requestID)
		return db.GetReliableCall(ctx, requestID)
	}
	if err != nil {
		fmt.Printf("[DEBUG InsertOrGetReliableCall] ERROR: %v\n", err)
		return nil, fmt.Errorf("failed to insert or get reliable call: %w", err)
	}

	if resultData.Valid {
		rc.ResultData = []byte(resultData.String)
	}
	if errorMessage.Valid {
		rc.Error = errorMessage.String
	}

	// Debug: Log the inserted record
	fmt.Printf("[DEBUG InsertOrGetReliableCall] Inserted: seq=%d, call_id=%s, object_id=%s, status=%s\n", rc.Seq, rc.CallID, rc.ObjectID, rc.Status)

	return &rc, nil
}

// UpdateReliableCallStatus updates the status and result of a reliable call
func (db *DB) UpdateReliableCallStatus(ctx context.Context, id int64, status string, resultData []byte, errorMessage string) error {
	if id <= 0 {
		return fmt.Errorf("seq must be positive")
	}
	if status == "" {
		return fmt.Errorf("status cannot be empty")
	}

	query := `
		UPDATE goverse_reliable_calls
		SET status = $1, result_data = $2, error_message = $3
		WHERE seq = $4
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

// GetPendingReliableCalls retrieves pending reliable calls for an object, ordered by seq
// Only returns calls with seq > nextRcseq to support incremental processing
func (db *DB) GetPendingReliableCalls(ctx context.Context, objectID string, nextRcseq int64) ([]*object.ReliableCall, error) {
	if objectID == "" {
		return nil, fmt.Errorf("object_id cannot be empty")
	}

	query := `
		SELECT seq, call_id, object_id, object_type, method_name, request_data, result_data, error_message, status, created_at, updated_at
		FROM goverse_reliable_calls
		WHERE object_id = $1 AND seq > $2 AND status = 'pending'
		ORDER BY seq ASC
	`

	// Debug: Log the query parameters
	fmt.Printf("[DEBUG GetPendingReliableCalls] object_id=%s, nextRcseq=%d\n", objectID, nextRcseq)

	// Debug: Check what's actually in the database
	var count int
	debugQuery := `SELECT COUNT(*) FROM goverse_reliable_calls WHERE object_id = $1`
	db.conn.QueryRowContext(ctx, debugQuery, objectID).Scan(&count)
	fmt.Printf("[DEBUG GetPendingReliableCalls] Total rows for object_id=%s: %d\n", objectID, count)

	debugQuery2 := `SELECT COUNT(*) FROM goverse_reliable_calls WHERE object_id = $1 AND seq > $2`
	db.conn.QueryRowContext(ctx, debugQuery2, objectID, nextRcseq).Scan(&count)
	fmt.Printf("[DEBUG GetPendingReliableCalls] Rows with seq > %d: %d\n", nextRcseq, count)

	debugQuery3 := `SELECT COUNT(*) FROM goverse_reliable_calls WHERE object_id = $1 AND seq > $2 AND status = 'pending'`
	db.conn.QueryRowContext(ctx, debugQuery3, objectID, nextRcseq).Scan(&count)
	fmt.Printf("[DEBUG GetPendingReliableCalls] Rows with seq > %d AND status='pending': %d\n", nextRcseq, count)

	rows, err := db.conn.QueryContext(ctx, query, objectID, nextRcseq)
	if err != nil {
		return nil, fmt.Errorf("failed to get pending reliable calls: %w", err)
	}
	defer rows.Close()

	var calls []*object.ReliableCall
	rowCount := 0
	for rows.Next() {
		rowCount++
		var rc object.ReliableCall
		var resultData sql.NullString
		var errorMessage sql.NullString

		err := rows.Scan(
			&rc.Seq,
			&rc.CallID,
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

	fmt.Printf("[DEBUG GetPendingReliableCalls] Found %d rows\n", rowCount)

	return calls, nil
}

// GetReliableCall retrieves a reliable call by its request ID
func (db *DB) GetReliableCall(ctx context.Context, requestID string) (*object.ReliableCall, error) {
	if requestID == "" {
		return nil, fmt.Errorf("call_id cannot be empty")
	}

	query := `
		SELECT seq, call_id, object_id, object_type, method_name, request_data, result_data, error_message, status, created_at, updated_at
		FROM goverse_reliable_calls
		WHERE call_id = $1
	`

	var rc object.ReliableCall
	var resultData sql.NullString
	var errorMessage sql.NullString

	err := db.conn.QueryRowContext(ctx, query, requestID).Scan(
		&rc.Seq,
		&rc.CallID,
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
