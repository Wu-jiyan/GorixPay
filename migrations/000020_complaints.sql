-- 000020: complaint handling.
-- complaints keeps one row per channel complaint number; complaint_messages is
-- the inbound/outbound conversation attached to it.

CREATE TABLE IF NOT EXISTS complaints (
    id VARCHAR(36) PRIMARY KEY,
    merchant_id VARCHAR(36) NOT NULL,
    order_id VARCHAR(36),
    channel VARCHAR(20) NOT NULL,
    complaint_no VARCHAR(64) NOT NULL,
    status VARCHAR(20) NOT NULL,
    reason VARCHAR(255) NOT NULL,
    amount_cents BIGINT NOT NULL DEFAULT 0,
    deadline_at TIMESTAMP,
    closed_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    UNIQUE (channel, complaint_no)
);

CREATE TABLE IF NOT EXISTS complaint_messages (
    id VARCHAR(36) PRIMARY KEY,
    complaint_id VARCHAR(36) NOT NULL,
    direction VARCHAR(10) NOT NULL,
    content TEXT NOT NULL,
    operator VARCHAR(64),
    created_at TIMESTAMP NOT NULL
);
