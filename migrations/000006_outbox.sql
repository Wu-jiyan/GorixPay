CREATE TABLE IF NOT EXISTS outbox_messages (
    id VARCHAR(36) PRIMARY KEY,
    topic VARCHAR(100) NOT NULL,
    aggregate_id VARCHAR(36) NOT NULL,
    idempotency_key VARCHAR(191) NOT NULL UNIQUE,
    payload TEXT NOT NULL,
    status VARCHAR(20) NOT NULL,
    attempts INTEGER NOT NULL,
    available_at TIMESTAMP NOT NULL,
    lease_until TIMESTAMP,
    last_error VARCHAR(500),
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);
