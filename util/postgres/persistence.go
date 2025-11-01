package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"
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
