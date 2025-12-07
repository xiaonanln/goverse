-- GoVerse Requests Schema
-- This schema supports exactly-once semantics for inter-object calls in GoVerse.
--
-- Exactly-once semantics ensure that:
-- 1. Each request is processed exactly one time, even with retries or failures
-- 2. Duplicate requests return the same cached result without re-execution
-- 3. Failed requests can be identified and handled appropriately
-- 4. Old requests are automatically cleaned up to prevent unbounded growth
--
-- Key Design Principles:
-- - request_id is the deduplication key (PRIMARY KEY ensures uniqueness)
-- - Status transitions: pending -> processing -> completed/failed
-- - result_data and error_message are populated based on final status
-- - node_id provides distributed locking (only the processing node can update)
-- - expires_at enables automatic cleanup of old requests (TTL)

CREATE TABLE goverse_requests (
    -- Unique identifier for the request (client-generated for idempotency)
    request_id VARCHAR(255) PRIMARY KEY,
    
    -- Target object information
    object_id VARCHAR(255) NOT NULL,
    object_type VARCHAR(255) NOT NULL,
    method_name VARCHAR(255) NOT NULL,
    
    -- Request payload (serialized protobuf or other format)
    request_data BYTEA NOT NULL,
    
    -- Response data (populated when status = 'completed')
    result_data BYTEA,
    
    -- Error message (populated when status = 'failed')
    error_message TEXT,
    
    -- Processing status with state machine enforcement
    -- pending: request received, not yet processing
    -- processing: actively being processed by a node
    -- completed: successfully processed, result_data available
    -- failed: processing failed, error_message available
    status VARCHAR(50) NOT NULL DEFAULT 'pending' 
        CHECK (status IN ('pending', 'processing', 'completed', 'failed')),
    
    -- Node ID currently processing this request (for distributed locking)
    -- Only the node with matching node_id can update the request
    node_id VARCHAR(255),
    
    -- Caller information for tracing and debugging
    caller_object_id VARCHAR(255),
    caller_client_id VARCHAR(255),
    
    -- Number of retry attempts (for monitoring and debugging)
    retry_count INTEGER NOT NULL DEFAULT 0,
    
    -- Timestamps for lifecycle tracking
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    processed_at TIMESTAMP, -- When processing completed (status = completed/failed)
    expires_at TIMESTAMP, -- Automatic cleanup time (optional, for TTL)
    
    -- Constraints to enforce data integrity
    -- Ensure completed requests always have result data
    CONSTRAINT valid_completed_state CHECK (
        status != 'completed' OR result_data IS NOT NULL
    ),
    -- Ensure failed requests always have error message
    CONSTRAINT valid_failed_state CHECK (
        status != 'failed' OR error_message IS NOT NULL
    ),
    -- Ensure terminal states always have processed timestamp
    CONSTRAINT valid_processed_at CHECK (
        status NOT IN ('completed', 'failed') OR processed_at IS NOT NULL
    )
);

-- Primary index for finding requests by object and status
-- Used for querying pending/processing requests for a specific object
CREATE INDEX idx_goverse_requests_object_status 
    ON goverse_requests(object_id, status);

-- Index for cleanup operations (find expired requests)
-- Used by background jobs to delete old completed/failed requests
CREATE INDEX idx_goverse_requests_expires_at 
    ON goverse_requests(expires_at) 
    WHERE expires_at IS NOT NULL;

-- Index for finding old requests by status and creation time
-- Useful for monitoring and debugging stuck requests
CREATE INDEX idx_goverse_requests_status_created 
    ON goverse_requests(status, created_at);

-- Index for finding requests by node (distributed lock queries)
-- Used when a node crashes to recover in-flight requests
CREATE INDEX idx_goverse_requests_node_status 
    ON goverse_requests(node_id, status) 
    WHERE node_id IS NOT NULL;

-- Optional: Partial index for active requests only (performance optimization)
CREATE INDEX idx_goverse_requests_active 
    ON goverse_requests(request_id, object_id) 
    WHERE status IN ('pending', 'processing');

-- Example trigger to automatically update updated_at timestamp
-- This ensures updated_at is always current whenever a row changes
CREATE OR REPLACE FUNCTION update_goverse_requests_timestamp()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trigger_update_goverse_requests_timestamp
    BEFORE UPDATE ON goverse_requests
    FOR EACH ROW
    EXECUTE FUNCTION update_goverse_requests_timestamp();

-- Example cleanup function to delete expired requests
-- Should be called periodically by a background job
CREATE OR REPLACE FUNCTION cleanup_expired_requests()
RETURNS INTEGER AS $$
DECLARE
    deleted_count INTEGER;
BEGIN
    DELETE FROM goverse_requests
    WHERE expires_at IS NOT NULL AND expires_at < CURRENT_TIMESTAMP;
    
    GET DIAGNOSTICS deleted_count = ROW_COUNT;
    RETURN deleted_count;
END;
$$ LANGUAGE plpgsql;

-- Example function to mark stuck requests as failed
-- Should be called periodically to recover from node failures
-- Note: updated_at is automatically updated by the trigger, no need to set it manually
CREATE OR REPLACE FUNCTION mark_stuck_requests_as_failed(
    stuck_duration INTERVAL DEFAULT '5 minutes'
)
RETURNS INTEGER AS $$
DECLARE
    affected_count INTEGER;
BEGIN
    UPDATE goverse_requests
    SET status = 'failed',
        error_message = 'Request stuck in processing state (node may have crashed)',
        processed_at = CURRENT_TIMESTAMP
    WHERE status = 'processing' 
      AND updated_at < (CURRENT_TIMESTAMP - stuck_duration);
    
    GET DIAGNOSTICS affected_count = ROW_COUNT;
    RETURN affected_count;
END;
$$ LANGUAGE plpgsql;