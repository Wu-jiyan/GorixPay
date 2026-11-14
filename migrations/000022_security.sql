-- 000022: administrator TOTP, login attempts and captcha challenges.

CREATE TABLE IF NOT EXISTS admin_totp (
    id VARCHAR(36) PRIMARY KEY,
    administrator_id VARCHAR(36) NOT NULL UNIQUE,
    secret_encrypted TEXT NOT NULL,
    enabled_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS login_attempts (
    id VARCHAR(36) PRIMARY KEY,
    principal_type VARCHAR(20) NOT NULL,
    identifier VARCHAR(120) NOT NULL,
    client_ip VARCHAR(64),
    success BOOLEAN NOT NULL,
    created_at TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_login_attempts_identifier_created ON login_attempts(identifier, created_at);

CREATE TABLE IF NOT EXISTS captcha_challenges (
    id VARCHAR(36) PRIMARY KEY,
    answer_hash VARCHAR(64) NOT NULL,
    expires_at TIMESTAMP NOT NULL,
    consumed_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL
);
