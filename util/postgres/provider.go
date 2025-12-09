package postgres

import (
	"context"

	"github.com/xiaonanln/goverse/object"
)

// Ensure PostgresPersistenceProvider implements object.PersistenceProvider
var _ object.PersistenceProvider = (*PostgresPersistenceProvider)(nil)

// PostgresPersistenceProvider adapts the postgres DB to the object.PersistenceProvider interface
type PostgresPersistenceProvider struct {
	db *DB
}

// NewPostgresPersistenceProvider creates a new PostgreSQL-backed persistence provider
func NewPostgresPersistenceProvider(db *DB) *PostgresPersistenceProvider {
	return &PostgresPersistenceProvider{
		db: db,
	}
}

// SaveObject saves an object to the PostgreSQL database
func (p *PostgresPersistenceProvider) SaveObject(ctx context.Context, objectID, objectType string, data []byte, nextRcseq int64) error {
	return p.db.SaveObject(ctx, objectID, objectType, data, nextRcseq)
}

// LoadObject loads an object from the PostgreSQL database
func (p *PostgresPersistenceProvider) LoadObject(ctx context.Context, objectID string) ([]byte, int64, error) {
	return p.db.LoadObject(ctx, objectID)
}

// DeleteObject deletes an object from the PostgreSQL database
func (p *PostgresPersistenceProvider) DeleteObject(ctx context.Context, objectID string) error {
	return p.db.DeleteObject(ctx, objectID)
}

// InsertOrGetReliableCall atomically inserts a new reliable call or returns the existing one
func (p *PostgresPersistenceProvider) InsertOrGetReliableCall(ctx context.Context, requestID string, objectID string, objectType string, methodName string, requestData []byte) (*object.ReliableCall, error) {
	return p.db.InsertOrGetReliableCall(ctx, requestID, objectID, objectType, methodName, requestData)
}

// UpdateReliableCallStatus updates the status and result of a reliable call
func (p *PostgresPersistenceProvider) UpdateReliableCallStatus(ctx context.Context, seq int64, status string, resultData []byte, errorMessage string) error {
	return p.db.UpdateReliableCallStatus(ctx, seq, status, resultData, errorMessage)
}

// GetPendingReliableCalls retrieves pending reliable calls for an object
func (p *PostgresPersistenceProvider) GetPendingReliableCalls(ctx context.Context, objectID string, nextRcseq int64) ([]*object.ReliableCall, error) {
	return p.db.GetPendingReliableCalls(ctx, objectID, nextRcseq)
}

// GetReliableCall retrieves a reliable call by its request ID
func (p *PostgresPersistenceProvider) GetReliableCall(ctx context.Context, requestID string) (*object.ReliableCall, error) {
	return p.db.GetReliableCall(ctx, requestID)
}
