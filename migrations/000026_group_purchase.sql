-- 000026: paid merchant-group purchases.
--
-- Only additive statements so SQLite, MySQL 5.7 and PostgreSQL all accept the
-- migration. The pair (merchant_id, group_id) is unique because a merchant buys
-- a given group at most once per lifetime; a refunded purchase is reactivated in
-- place, and the attempt counter keeps the ledger idempotency key distinct.

ALTER TABLE merchant_groups ADD COLUMN price_cents BIGINT NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS group_purchases (
    id VARCHAR(36) PRIMARY KEY,
    merchant_id VARCHAR(36) NOT NULL,
    group_id VARCHAR(36) NOT NULL,
    price_cents BIGINT NOT NULL,
    status VARCHAR(20) NOT NULL,
    attempts INT NOT NULL DEFAULT 0,
    ledger_transaction_id VARCHAR(36),
    expires_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    UNIQUE (merchant_id, group_id)
);

CREATE INDEX IF NOT EXISTS idx_group_purchases_merchant ON group_purchases(merchant_id, created_at);
CREATE INDEX IF NOT EXISTS idx_group_purchases_status ON group_purchases(status, created_at);
