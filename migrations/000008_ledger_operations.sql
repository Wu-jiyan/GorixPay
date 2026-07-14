CREATE TABLE IF NOT EXISTS ledger_operations (
    id VARCHAR(36) PRIMARY KEY,
    idempotency_key VARCHAR(191) NOT NULL UNIQUE,
    request_hash VARCHAR(64) NOT NULL,
    operation_type VARCHAR(20) NOT NULL,
    status VARCHAR(20) NOT NULL,
    account_id VARCHAR(36) NOT NULL,
    counterparty_account_id VARCHAR(36) NOT NULL,
    amount BIGINT NOT NULL CHECK (amount > 0),
    reference_type VARCHAR(64) NOT NULL,
    reference_id VARCHAR(64) NOT NULL,
    description VARCHAR(255) NOT NULL,
    metadata TEXT NOT NULL,
    created_by VARCHAR(36) NOT NULL,
    processed_by VARCHAR(36),
    transaction_id VARCHAR(36),
    rejection_reason VARCHAR(255),
    created_at TIMESTAMP NOT NULL,
    processed_at TIMESTAMP,
    FOREIGN KEY (account_id) REFERENCES ledger_accounts(id),
    FOREIGN KEY (counterparty_account_id) REFERENCES ledger_accounts(id),
    FOREIGN KEY (transaction_id) REFERENCES ledger_transactions(id)
);

CREATE INDEX IF NOT EXISTS idx_ledger_operations_status_created ON ledger_operations(status, created_at);
CREATE INDEX IF NOT EXISTS idx_ledger_operations_account_created ON ledger_operations(account_id, created_at);
