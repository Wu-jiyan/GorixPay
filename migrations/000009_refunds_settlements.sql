CREATE TABLE IF NOT EXISTS refund_orders (
    id VARCHAR(36) PRIMARY KEY,
    refund_no VARCHAR(32) NOT NULL UNIQUE,
    merchant_id VARCHAR(36) NOT NULL,
    payment_order_id VARCHAR(36) NOT NULL,
    out_refund_no VARCHAR(64) NOT NULL,
    amount BIGINT NOT NULL CHECK (amount > 0),
    currency CHAR(3) NOT NULL,
    reason VARCHAR(255) NOT NULL,
    status VARCHAR(20) NOT NULL,
    ledger_transaction_id VARCHAR(36),
    rejection_reason VARCHAR(255),
    processed_by VARCHAR(36),
    processed_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    UNIQUE (merchant_id, out_refund_no),
    FOREIGN KEY (merchant_id) REFERENCES merchants(id),
    FOREIGN KEY (payment_order_id) REFERENCES payment_orders(id),
    FOREIGN KEY (ledger_transaction_id) REFERENCES ledger_transactions(id)
);

CREATE INDEX IF NOT EXISTS idx_refund_orders_merchant_created ON refund_orders(merchant_id, created_at);
CREATE INDEX IF NOT EXISTS idx_refund_orders_payment_order ON refund_orders(payment_order_id);
CREATE INDEX IF NOT EXISTS idx_refund_orders_status_created ON refund_orders(status, created_at);

CREATE TABLE IF NOT EXISTS settlement_orders (
    id VARCHAR(36) PRIMARY KEY,
    settlement_no VARCHAR(32) NOT NULL UNIQUE,
    merchant_id VARCHAR(36) NOT NULL,
    out_settlement_no VARCHAR(64) NOT NULL,
    amount BIGINT NOT NULL CHECK (amount > 0),
    currency CHAR(3) NOT NULL,
    destination VARCHAR(255) NOT NULL,
    status VARCHAR(20) NOT NULL,
    ledger_transaction_id VARCHAR(36),
    rejection_reason VARCHAR(255),
    processed_by VARCHAR(36),
    processed_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    UNIQUE (merchant_id, out_settlement_no),
    FOREIGN KEY (merchant_id) REFERENCES merchants(id),
    FOREIGN KEY (ledger_transaction_id) REFERENCES ledger_transactions(id)
);

CREATE INDEX IF NOT EXISTS idx_settlement_orders_merchant_created ON settlement_orders(merchant_id, created_at);
CREATE INDEX IF NOT EXISTS idx_settlement_orders_status_created ON settlement_orders(status, created_at);
