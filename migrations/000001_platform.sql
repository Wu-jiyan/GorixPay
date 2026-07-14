CREATE TABLE IF NOT EXISTS system_settings (
    setting_key VARCHAR(191) PRIMARY KEY,
    setting_value TEXT NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS audit_events (
    id VARCHAR(36) PRIMARY KEY,
    event_type VARCHAR(100) NOT NULL,
    actor_type VARCHAR(50) NOT NULL,
    actor_id VARCHAR(36),
    payload TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL
);
