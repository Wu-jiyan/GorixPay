-- 000028: complaint auto reply and timeout close rules.

CREATE TABLE IF NOT EXISTS complaint_auto_rules (
    id VARCHAR(36) PRIMARY KEY,
    channel VARCHAR(20) NOT NULL,
    reply_text TEXT NOT NULL,
    close_after_hours INT NOT NULL DEFAULT 0,
    status VARCHAR(20) NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);
