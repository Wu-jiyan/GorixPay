-- 000019: profit sharing receivers/orders and red packet payouts.
-- Only portable SQL is used so the migration runs on SQLite, MySQL and PostgreSQL.

CREATE TABLE IF NOT EXISTS profit_sharing_receivers (
    id VARCHAR(36) PRIMARY KEY,
    merchant_id VARCHAR(36) NOT NULL,
    receiver_type VARCHAR(20) NOT NULL,
    account VARCHAR(191) NOT NULL,
    name VARCHAR(128) NOT NULL,
    rate_bp INT NOT NULL,
    status VARCHAR(20) NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    UNIQUE (merchant_id, account)
);

CREATE INDEX IF NOT EXISTS idx_profit_sharing_receivers_merchant
    ON profit_sharing_receivers(merchant_id, status);

CREATE TABLE IF NOT EXISTS profit_sharing_orders (
    id VARCHAR(36) PRIMARY KEY,
    order_id VARCHAR(36) NOT NULL,
    merchant_id VARCHAR(36) NOT NULL,
    receiver_id VARCHAR(36) NOT NULL,
    amount BIGINT NOT NULL CHECK (amount > 0),
    status VARCHAR(20) NOT NULL,
    ledger_transaction_id VARCHAR(36),
    channel_batch_no VARCHAR(64),
    error VARCHAR(255),
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    UNIQUE (order_id, receiver_id),
    FOREIGN KEY (order_id) REFERENCES payment_orders(id),
    FOREIGN KEY (receiver_id) REFERENCES profit_sharing_receivers(id),
    FOREIGN KEY (ledger_transaction_id) REFERENCES ledger_transactions(id)
);

CREATE INDEX IF NOT EXISTS idx_profit_sharing_orders_merchant
    ON profit_sharing_orders(merchant_id, created_at);
CREATE INDEX IF NOT EXISTS idx_profit_sharing_orders_status
    ON profit_sharing_orders(status, created_at);

-- Red packet payouts reuse the payout lifecycle; kind separates them from
-- ordinary merchant withdrawals in listings and reports.
ALTER TABLE transfer_orders ADD COLUMN kind VARCHAR(20) NOT NULL DEFAULT 'normal';
-- Red packet batches share one batch_no; remark keeps the batch note. Neither
-- column is used by ordinary withdrawals.
ALTER TABLE transfer_orders ADD COLUMN batch_no VARCHAR(64);
ALTER TABLE transfer_orders ADD COLUMN remark VARCHAR(191);

CREATE INDEX IF NOT EXISTS idx_transfer_orders_kind_created ON transfer_orders(kind, created_at);
CREATE INDEX IF NOT EXISTS idx_transfer_orders_batch ON transfer_orders(batch_no);
