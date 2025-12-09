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
func (p *PostgresPersistenceProvider) SaveObject(ctx context.Context, objectID, objectType string, data []byte) error {
	return p.db.SaveObject(ctx, objectID, objectType, data)
}

// LoadObject loads an object from the PostgreSQL database
func (p *PostgresPersistenceProvider) LoadObject(ctx context.Context, objectID string) ([]byte, error) {
	return p.db.LoadObject(ctx, objectID)
}

// DeleteObject deletes an object from the PostgreSQL database
func (p *PostgresPersistenceProvider) DeleteObject(ctx context.Context, objectID string) error {
	return p.db.DeleteObject(ctx, objectID)
}

// GetDB returns the underlying database connection
// This is useful for accessing request tracking functionality
func (p *PostgresPersistenceProvider) GetDB() *DB {
	return p.db
}
