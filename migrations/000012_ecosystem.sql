-- 000012: merchant ecosystem (certification, payment domains, invite rebates, risk control).
-- Only portable SQL is used so the migration runs on SQLite, MySQL and PostgreSQL.

ALTER TABLE merchants ADD COLUMN invited_by VARCHAR(36);
ALTER TABLE merchants ADD COLUMN invite_code VARCHAR(32);
CREATE UNIQUE INDEX idx_merchants_invite_code ON merchants(invite_code);

CREATE TABLE IF NOT EXISTS merchant_certifications (
    id VARCHAR(36) PRIMARY KEY,
    merchant_id VARCHAR(36) NOT NULL UNIQUE,
    cert_type VARCHAR(10) NOT NULL,
    real_name_encrypted TEXT NOT NULL,
    id_no_encrypted TEXT NOT NULL,
    corp_name VARCHAR(128),
    corp_no VARCHAR(64),
    status VARCHAR(20) NOT NULL,
    fee_cents BIGINT NOT NULL DEFAULT 0,
    rejection_reason VARCHAR(255),
    reviewer_id VARCHAR(36),
    submitted_at TIMESTAMP NOT NULL,
    reviewed_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS merchant_domains (
    id VARCHAR(36) PRIMARY KEY,
    merchant_id VARCHAR(36) NOT NULL,
    domain VARCHAR(191) NOT NULL,
    status VARCHAR(20) NOT NULL,
    rejection_reason VARCHAR(255),
    reviewer_id VARCHAR(36),
    created_at TIMESTAMP NOT NULL,
    reviewed_at TIMESTAMP,
    UNIQUE (merchant_id, domain)
);

CREATE TABLE IF NOT EXISTS invite_rebates (
    id VARCHAR(36) PRIMARY KEY,
    inviter_id VARCHAR(36) NOT NULL,
    invitee_id VARCHAR(36) NOT NULL,
    order_id VARCHAR(36) NOT NULL UNIQUE,
    trade_no VARCHAR(32) NOT NULL,
    order_amount BIGINT NOT NULL,
    rebate_amount BIGINT NOT NULL,
    rate_bp INT NOT NULL,
    created_at TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS blacklist_entries (
    id VARCHAR(36) PRIMARY KEY,
    entry_type VARCHAR(20) NOT NULL,
    value VARCHAR(191) NOT NULL,
    remark VARCHAR(191),
    created_at TIMESTAMP NOT NULL,
    UNIQUE (entry_type, value)
);

CREATE TABLE IF NOT EXISTS risk_events (
    id VARCHAR(36) PRIMARY KEY,
    merchant_id VARCHAR(36),
    rule_code VARCHAR(50) NOT NULL,
    detail VARCHAR(500) NOT NULL,
    client_ip VARCHAR(45),
    created_at TIMESTAMP NOT NULL
);
