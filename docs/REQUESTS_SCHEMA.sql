-- GoVerse Requests Schema
-- This schema supports exactly-once semantics for inter-object calls in GoVerse.
--
-- Exactly-once semantics ensure that:
-- 1. Each request is processed exactly one time, even with retries or failures
-- 2. Duplicate requests return the same cached result without re-execution
-- 3. Failed requests can be identified and handled appropriately
--
-- Key Design Principles:
-- - request_id is the deduplication key (PRIMARY KEY ensures uniqueness)
-- - Status transitions: pending -> success/failed
-- - result_data and error_message are populated based on final status

CREATE TABLE goverse_requests (
    -- Auto-incrementing integer ID for ordering and reference
    id BIGSERIAL UNIQUE,
    
    -- Unique identifier for the request (client-generated for idempotency)
    request_id VARCHAR(255) PRIMARY KEY,
    
    -- Target object information
    object_id VARCHAR(255) NOT NULL,
    object_type VARCHAR(255) NOT NULL,
    method_name VARCHAR(255) NOT NULL,
    
    -- Request payload (serialized protobuf or other format)
    request_data BYTEA NOT NULL,
    
    -- Response data (populated when status = 'success')
    result_data BYTEA,
    
    -- Error message (populated when status = 'failed')
    error_message TEXT,
    
    -- Processing status with state machine enforcement
    -- pending: request received, not yet processing
    -- success: successfully processed, result_data available
    -- failed: processing failed, error_message available
    status VARCHAR(50) NOT NULL DEFAULT 'pending' 
        CHECK (status IN ('pending', 'success', 'failed')),
    
    -- Timestamps for lifecycle tracking
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    
    -- Constraints to enforce data integrity
    -- Ensure success requests always have result data
    CONSTRAINT valid_success_state CHECK (
        status != 'success' OR result_data IS NOT NULL
    ),
    -- Ensure failed requests always have error message
    CONSTRAINT valid_failed_state CHECK (
        status != 'failed' OR error_message IS NOT NULL
    )
);

-- Primary index for finding requests by object and status
-- Used for querying pending/processing requests for a specific object
CREATE INDEX idx_goverse_requests_object_status 
    ON goverse_requests(object_id, status);

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