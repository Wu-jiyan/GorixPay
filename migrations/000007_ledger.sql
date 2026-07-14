CREATE TABLE IF NOT EXISTS ledger_accounts (
    id VARCHAR(36) PRIMARY KEY,
    owner_type VARCHAR(32) NOT NULL,
    owner_id VARCHAR(36) NOT NULL,
    purpose VARCHAR(32) NOT NULL,
    category VARCHAR(20) NOT NULL,
    currency CHAR(3) NOT NULL,
    status VARCHAR(20) NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    UNIQUE (owner_type, owner_id, purpose, currency)
);

CREATE TABLE IF NOT EXISTS ledger_transactions (
    id VARCHAR(36) PRIMARY KEY,
    idempotency_key VARCHAR(191) NOT NULL UNIQUE,
    request_hash VARCHAR(64) NOT NULL,
    reference_type VARCHAR(64) NOT NULL,
    reference_id VARCHAR(64) NOT NULL,
    description VARCHAR(255) NOT NULL,
    metadata TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS ledger_entries (
    id VARCHAR(36) PRIMARY KEY,
    transaction_id VARCHAR(36) NOT NULL,
    account_id VARCHAR(36) NOT NULL,
    sequence INTEGER NOT NULL,
    direction VARCHAR(10) NOT NULL,
    bucket VARCHAR(20) NOT NULL,
    amount BIGINT NOT NULL CHECK (amount > 0),
    balance_after BIGINT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    UNIQUE (transaction_id, sequence),
    FOREIGN KEY (transaction_id) REFERENCES ledger_transactions(id),
    FOREIGN KEY (account_id) REFERENCES ledger_accounts(id)
);

CREATE INDEX IF NOT EXISTS idx_ledger_entries_account_created ON ledger_entries(account_id, created_at);
CREATE INDEX IF NOT EXISTS idx_ledger_transactions_reference ON ledger_transactions(reference_type, reference_id);

CREATE TABLE IF NOT EXISTS ledger_balances (
    account_id VARCHAR(36) PRIMARY KEY,
    available BIGINT NOT NULL,
    frozen BIGINT NOT NULL,
    version BIGINT NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    FOREIGN KEY (account_id) REFERENCES ledger_accounts(id)
);

CREATE TABLE IF NOT EXISTS ledger_freezes (
    id VARCHAR(36) PRIMARY KEY,
    account_id VARCHAR(36) NOT NULL,
    business_key VARCHAR(191) NOT NULL UNIQUE,
    amount BIGINT NOT NULL CHECK (amount > 0),
    status VARCHAR(20) NOT NULL,
    freeze_transaction_id VARCHAR(36) NOT NULL,
    release_transaction_id VARCHAR(36),
    created_at TIMESTAMP NOT NULL,
    released_at TIMESTAMP,
    FOREIGN KEY (account_id) REFERENCES ledger_accounts(id),
    FOREIGN KEY (freeze_transaction_id) REFERENCES ledger_transactions(id),
    FOREIGN KEY (release_transaction_id) REFERENCES ledger_transactions(id)
);
