CREATE TABLE goverse_requests (
    request_id VARCHAR(255) PRIMARY KEY,
    object_id VARCHAR(255) NOT NULL,
    object_type VARCHAR(255) NOT NULL,
    method_name VARCHAR(255) NOT NULL,
    request_data BYTEA NOT NULL,
    result_data BYTEA,
    status VARCHAR(50) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'processing', 'completed', 'failed')),
    caller_object_id VARCHAR(255),
    caller_client_id VARCHAR(255),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_goverse_requests_object_status ON goverse_requests(object_id, status);