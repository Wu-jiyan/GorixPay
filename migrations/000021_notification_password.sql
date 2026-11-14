-- 000021: notification channels, one-time verification codes and password resets.
--
-- Only additive statements: CREATE TABLE / CREATE INDEX IF NOT EXISTS all run on
-- SQLite, MySQL 5.7 and PostgreSQL (the runner strips the index guard for MySQL).
--
-- Secrets: notification_channels.config_encrypted holds the AES-GCM envelope from
-- internal/platform/secret, never plaintext. verify_codes and password_resets only
-- keep hashes: a database dump cannot be replayed to reset an account.

CREATE TABLE IF NOT EXISTS notification_channels (
    id VARCHAR(36) PRIMARY KEY,
    kind VARCHAR(10) NOT NULL,
    provider VARCHAR(32) NOT NULL,
    config_encrypted TEXT NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS verify_codes (
    id VARCHAR(36) PRIMARY KEY,
    scene VARCHAR(32) NOT NULL,
    target VARCHAR(120) NOT NULL,
    code_hash VARCHAR(64) NOT NULL,
    expires_at TIMESTAMP NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    consumed_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_verify_codes_scene_target ON verify_codes(scene, target);

CREATE TABLE IF NOT EXISTS password_resets (
    id VARCHAR(36) PRIMARY KEY,
    principal_type VARCHAR(20) NOT NULL,
    principal_id VARCHAR(36) NOT NULL,
    token_hash VARCHAR(64) NOT NULL,
    expires_at TIMESTAMP NOT NULL,
    consumed_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_password_resets_token_hash ON password_resets(token_hash);
CREATE INDEX IF NOT EXISTS idx_password_resets_principal ON password_resets(principal_type, principal_id);
