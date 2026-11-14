-- 000024: voice devices, cloud printers and their job queues.

CREATE TABLE IF NOT EXISTS voice_devices (
    id VARCHAR(36) PRIMARY KEY,
    merchant_id VARCHAR(36),
    device_no VARCHAR(64) NOT NULL UNIQUE,
    name VARCHAR(64) NOT NULL,
    secret VARCHAR(64) NOT NULL,
    last_seen_at TIMESTAMP,
    status VARCHAR(20) NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS voice_broadcasts (
    id VARCHAR(36) PRIMARY KEY,
    device_id VARCHAR(36) NOT NULL,
    order_id VARCHAR(36),
    text VARCHAR(255) NOT NULL,
    amount_cents BIGINT NOT NULL DEFAULT 0,
    status VARCHAR(20) NOT NULL,
    created_at TIMESTAMP NOT NULL,
    delivered_at TIMESTAMP
);

CREATE TABLE IF NOT EXISTS printers (
    id VARCHAR(36) PRIMARY KEY,
    merchant_id VARCHAR(36) NOT NULL,
    name VARCHAR(64) NOT NULL,
    secret VARCHAR(64) NOT NULL,
    status VARCHAR(20) NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS print_jobs (
    id VARCHAR(36) PRIMARY KEY,
    printer_id VARCHAR(36) NOT NULL,
    order_id VARCHAR(36),
    content TEXT NOT NULL,
    status VARCHAR(20) NOT NULL,
    attempts INT NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL,
    printed_at TIMESTAMP
);
