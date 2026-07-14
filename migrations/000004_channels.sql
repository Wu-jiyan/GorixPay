CREATE TABLE IF NOT EXISTS payment_channels (
    id VARCHAR(36) PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    plugin_code VARCHAR(64) NOT NULL,
    payment_type VARCHAR(32) NOT NULL,
    priority INTEGER NOT NULL,
    weight INTEGER NOT NULL,
    status VARCHAR(20) NOT NULL,
    config_encrypted TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);
