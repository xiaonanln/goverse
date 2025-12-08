# PostgreSQL Setup for Goverse Persistence

This guide explains how to set up PostgreSQL for object persistence in Goverse.

## Overview

Goverse supports optional object persistence using PostgreSQL with JSONB storage. This allows distributed objects to maintain state across server restarts and enables features like:

- Persistent user profiles and game state
- Durable chat history and room configurations
- Long-term analytics and audit logs
- Cross-cluster object migration

## Quick Start

### 1. Install PostgreSQL

#### Ubuntu/Debian
```bash
sudo apt-get update
sudo apt-get install postgresql postgresql-contrib
```

#### macOS (Homebrew)
```bash
brew install postgresql@16
brew services start postgresql@16
```

#### Windows
Download and install from [PostgreSQL Downloads](https://www.postgresql.org/download/windows/)

### 2. Create Database and User

Start PostgreSQL and create the Goverse database:

```bash
# Connect to PostgreSQL as the postgres user
sudo -u postgres psql

# In the PostgreSQL prompt, run:
CREATE DATABASE goverse;
CREATE USER goverse WITH PASSWORD 'goverse';
GRANT ALL PRIVILEGES ON DATABASE goverse TO goverse;
\q
```

### 3. Initialize Database Schema

Use the `pgadmin` tool to initialize the database schema:

```bash
# Initialize using command-line flags
go run ./cmd/pgadmin --host localhost --user goverse --password goverse --database goverse init

# Or use a configuration file
go run ./cmd/pgadmin --config config.yml init
```

The tool will create all required tables and indexes. See [cmd/pgadmin/README.md](../cmd/pgadmin/README.md) for more details.

### 4. Verify Connection

Test the database connection and schema:

```bash
# Using pgadmin tool (recommended)
go run ./cmd/pgadmin --host localhost --user goverse --password goverse --database goverse verify

# Or use psql directly
psql -h localhost -U goverse -d goverse -c "SELECT version();"
```

### 5. Run the Example

```bash
cd examples/persistence
go run main.go
```

## Database Schema

Goverse creates the following schema automatically when you call `db.InitSchema()`:

```sql
CREATE TABLE IF NOT EXISTS goverse_objects (
    object_id VARCHAR(255) PRIMARY KEY,
    object_type VARCHAR(255) NOT NULL,
    data JSONB NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_goverse_objects_type ON goverse_objects(object_type);
CREATE INDEX IF NOT EXISTS idx_goverse_objects_updated_at ON goverse_objects(updated_at);
```

### Schema Fields

- **object_id**: Unique identifier for the object (matches Goverse object ID)
- **object_type**: Type name of the object (e.g., "UserProfile", "ChatRoom")
- **data**: JSONB field containing the serialized object state
- **created_at**: Timestamp when the object was first created
- **updated_at**: Timestamp when the object was last modified

## Configuration

### Database Configuration

Create a configuration object to connect to PostgreSQL:

```go
import "github.com/xiaonanln/goverse/util/postgres"

config := &postgres.Config{
    Host:     "localhost",
    Port:     5432,
    User:     "goverse",
    Password: "goverse",
    Database: "goverse",
    SSLMode:  "disable", // Use "require" in production
}

db, err := postgres.NewDB(config)
if err != nil {
    log.Fatal(err)
}
defer db.Close()
```

### SSL Mode Options

- `disable` - No SSL (development only)
- `require` - Require SSL without certificate verification
- `verify-ca` - Require SSL with CA verification
- `verify-full` - Require SSL with full verification

## Creating Persistent Objects

### Thread-Safety Requirements ⚠️

**IMPORTANT**: All persistent objects MUST implement thread-safe `ToData()` and `FromData()` methods because:
- Periodic persistence runs in a background goroutine
- Objects may be processing requests while being persisted
- Multiple save operations can overlap during shutdown

**Always use a mutex** to protect field access:

```go
import (
    "sync"
    "github.com/xiaonanln/goverse/object"
    "google.golang.org/protobuf/proto"
    "google.golang.org/protobuf/types/known/structpb"
)

type UserProfile struct {
    object.BaseObject
    mu       sync.Mutex  // Required for thread-safety
    Username string
    Email    string
    Score    int
}
```

### 1. Define Your Object

Extend `BaseObject` and implement thread-safe serialization methods:

```go
func (u *UserProfile) OnCreated() {
    u.Logger.Infof("UserProfile created: %s", u.Id())
}

// Serialize object state with proper locking
func (u *UserProfile) ToData() (proto.Message, error) {
    u.mu.Lock()
    defer u.mu.Unlock()
    
    data, err := structpb.NewStruct(map[string]interface{}{
        "username": u.Username,
        "email":    u.Email,
        "score":    u.Score,
    })
    return data, err
}

// Deserialize object state with proper locking
func (u *UserProfile) FromData(data proto.Message) error {
    structData, ok := data.(*structpb.Struct)
    if !ok {
        return nil
    }
    
    u.mu.Lock()
    defer u.mu.Unlock()
    
    if username, ok := structData.Fields["username"]; ok {
        u.Username = username.GetStringValue()
    }
    if email, ok := structData.Fields["email"]; ok {
        u.Email = email.GetStringValue()
    }
    if score, ok := structData.Fields["score"]; ok {
        u.Score = int(score.GetNumberValue())
    }
    return nil
}
```

### Common Pitfalls to Avoid

❌ **Don't forget the mutex:**
```go
// UNSAFE - No mutex protection
func (u *UserProfile) ToData() (proto.Message, error) {
    return structpb.NewStruct(map[string]interface{}{
        "username": u.Username,  // Race condition!
    })
}
```

✅ **Always lock before accessing fields:**
```go
// SAFE - Mutex protects concurrent access
func (u *UserProfile) ToData() (proto.Message, error) {
    u.mu.Lock()
    defer u.mu.Unlock()
    return structpb.NewStruct(map[string]interface{}{
        "username": u.Username,  // Protected!
    })
}
```

### 2. Save and Load Objects

```go
import (
    "context"
    "github.com/xiaonanln/goverse/object"
    "github.com/xiaonanln/goverse/util/postgres"
)

// Create persistence provider
provider := postgres.NewPostgresPersistenceProvider(db)
ctx := context.Background()

// Create and save
user := &UserProfile{}
user.OnInit(user, "user-123", nil)
user.// No SetPersistent needed - persistence is type-based
user.Username = "alice"
user.Email = "alice@example.com"
user.Score = 100

err := object.SaveObject(ctx, provider, user)

// Load existing object
loadedUser := &UserProfile{}
loadedUser.OnInit(loadedUser, "user-123", nil)
err = object.LoadObject(ctx, provider, loadedUser, "user-123")
```

## Managing the Database

### Using the pgadmin Tool

GoVerse includes a `pgadmin` command-line tool for database management tasks:

#### Initialize Schema
```bash
go run ./cmd/pgadmin --config config.yml init
```

#### Verify Connection and Schema
```bash
go run ./cmd/pgadmin --config config.yml verify
```

#### Check Database Status
```bash
go run ./cmd/pgadmin --config config.yml status
```

This shows connection status, table row counts, database size, and more.

#### Reset Schema (Warning: Deletes All Data)
```bash
go run ./cmd/pgadmin --config config.yml reset
```

This will prompt for confirmation before dropping and recreating all tables.

For complete documentation, see [cmd/pgadmin/README.md](../cmd/pgadmin/README.md).

## Production Considerations

### Security

1. **Use SSL**: Always set `SSLMode: "require"` or higher in production
2. **Strong Passwords**: Use strong passwords for database users
3. **Limited Permissions**: Grant only necessary permissions to the Goverse user
4. **Network Security**: Use firewall rules to restrict database access

### Performance

1. **Connection Pooling**: The default pool (25 max open, 5 idle) works for most cases
2. **Indexes**: Add additional indexes based on your query patterns
3. **JSONB Queries**: Use PostgreSQL's JSONB operators for efficient queries
4. **Monitoring**: Monitor query performance and connection pool usage

### Backup and Recovery

```bash
# Backup
pg_dump -h localhost -U goverse goverse > goverse_backup.sql

# Restore
psql -h localhost -U goverse goverse < goverse_backup.sql
```

## Advanced Usage

### Custom Queries

Access the underlying database connection for custom queries:

```go
db := postgresDB.Connection()
rows, err := db.QueryContext(ctx, 
    "SELECT data->>'username' as username FROM goverse_objects WHERE object_type = $1",
    "UserProfile")
```

### JSONB Queries

PostgreSQL's JSONB operators allow efficient queries on object data:

```sql
-- Find users with score > 100
SELECT object_id, data 
FROM goverse_objects 
WHERE object_type = 'UserProfile' 
  AND (data->>'score')::int > 100;

-- Search by nested field
SELECT object_id 
FROM goverse_objects 
WHERE data @> '{"username": "alice"}';
```

### Batch Operations

For bulk operations, use transactions:

```go
tx, err := db.Connection().BeginTx(ctx, nil)
if err != nil {
    return err
}
defer tx.Rollback()

// Perform multiple operations...

tx.Commit()
```

## Troubleshooting

### Connection Refused

```
Error: failed to ping database: dial tcp [::1]:5432: connect: connection refused
```

**Solution**: Ensure PostgreSQL is running:
```bash
sudo systemctl status postgresql
sudo systemctl start postgresql
```

### Authentication Failed

```
Error: pq: password authentication failed for user "goverse"
```

**Solution**: 
1. Verify password is correct
2. Check `pg_hba.conf` for authentication method
3. Reload PostgreSQL configuration: `sudo systemctl reload postgresql`

### Permission Denied

```
Error: pq: permission denied for table goverse_objects
```

**Solution**: Grant permissions:
```sql
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO goverse;
```

## Examples

See the complete working example in `examples/persistence/main.go`.

## References

- [PostgreSQL Documentation](https://www.postgresql.org/docs/)
- [JSONB Functions](https://www.postgresql.org/docs/current/functions-json.html)
- [Go PostgreSQL Driver (pq)](https://github.com/lib/pq)
