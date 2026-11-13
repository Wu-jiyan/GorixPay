-- 000011: transfer orders, announcements and cashier action persistence.

CREATE TABLE IF NOT EXISTS transfer_orders (
    id VARCHAR(36) PRIMARY KEY,
    transfer_no VARCHAR(32) NOT NULL UNIQUE,
    merchant_id VARCHAR(36) NOT NULL,
    out_transfer_no VARCHAR(64) NOT NULL,
    amount BIGINT NOT NULL CHECK (amount > 0),
    currency CHAR(3) NOT NULL,
    account_name VARCHAR(128) NOT NULL,
    account_no VARCHAR(64) NOT NULL,
    bank_name VARCHAR(128) NOT NULL,
    branch_name VARCHAR(191),
    status VARCHAR(20) NOT NULL,
    ledger_transaction_id VARCHAR(36),
    rejection_reason VARCHAR(255),
    processed_by VARCHAR(36),
    processed_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    UNIQUE (merchant_id, out_transfer_no),
    FOREIGN KEY (merchant_id) REFERENCES merchants(id),
    FOREIGN KEY (ledger_transaction_id) REFERENCES ledger_transactions(id)
);

CREATE INDEX IF NOT EXISTS idx_transfer_orders_merchant_created ON transfer_orders(merchant_id, created_at);
CREATE INDEX IF NOT EXISTS idx_transfer_orders_status_created ON transfer_orders(status, created_at);

CREATE TABLE IF NOT EXISTS announcements (
    id VARCHAR(36) PRIMARY KEY,
    title VARCHAR(191) NOT NULL,
    content TEXT NOT NULL,
    status VARCHAR(20) NOT NULL,
    published_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_announcements_status_published ON announcements(status, published_at);
CREATE INDEX IF NOT EXISTS idx_announcements_created ON announcements(created_at);

ALTER TABLE payment_attempts ADD COLUMN action_type VARCHAR(20);
ALTER TABLE payment_attempts ADD COLUMN action_payload TEXT;
