-- 000018: automated settlement rules.
-- settle_rules drives the scheduled auto settlement; settlement_orders keeps the
-- automation outcome next to the finance columns it already had.

CREATE TABLE IF NOT EXISTS settle_rules (
    id VARCHAR(36) PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    merchant_id VARCHAR(36),
    min_amount_cents BIGINT NOT NULL DEFAULT 0,
    fee_bp INT NOT NULL DEFAULT 0,
    period VARCHAR(20) NOT NULL DEFAULT 'daily',
    auto_transfer BOOLEAN NOT NULL DEFAULT FALSE,
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    last_run_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

ALTER TABLE settlement_orders ADD COLUMN source VARCHAR(20) NOT NULL DEFAULT 'manual';
ALTER TABLE settlement_orders ADD COLUMN fee_cents BIGINT NOT NULL DEFAULT 0;
ALTER TABLE settlement_orders ADD COLUMN transfer_status VARCHAR(20) NOT NULL DEFAULT 'none';
ALTER TABLE settlement_orders ADD COLUMN transfer_at TIMESTAMP;
