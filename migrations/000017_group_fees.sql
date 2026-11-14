-- 000017: merchant groups and per-group fee rates.
--
-- Upstream keeps the fee rate on the user group (pre_group / pre_user.gid) and
-- derives the merchant payout mode from the group settings. GorixPay had neither,
-- so settlement could not charge a merchant specific rate.
--
-- Only additive statements: SQLite, MySQL 5.7 and PostgreSQL all accept
-- CREATE TABLE IF NOT EXISTS and ALTER TABLE ... ADD COLUMN. The inline
-- REFERENCES on ADD COLUMN is enforced by SQLite and PostgreSQL; MySQL parses the
-- column-level references and ignores it, so group deletion is protected in the
-- service layer (409 when merchants still point at the group) rather than by the
-- database alone.

CREATE TABLE IF NOT EXISTS merchant_groups (
    id VARCHAR(36) PRIMARY KEY,
    name VARCHAR(64) NOT NULL,
    rate_bp INTEGER NOT NULL DEFAULT 0,
    pay_types TEXT,
    settle_mode VARCHAR(20) NOT NULL DEFAULT 'normal',
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    UNIQUE (name)
);

ALTER TABLE merchants ADD COLUMN group_id VARCHAR(36) REFERENCES merchant_groups(id);
ALTER TABLE merchants ADD COLUMN settle_mode VARCHAR(20) NOT NULL DEFAULT 'normal';
