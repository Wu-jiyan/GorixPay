-- 000027: registration invite codes and merchant notification preferences.

CREATE TABLE IF NOT EXISTS invite_codes (
    id VARCHAR(36) PRIMARY KEY,
    code VARCHAR(32) NOT NULL UNIQUE,
    owner_merchant_id VARCHAR(36),
    max_uses INTEGER NOT NULL DEFAULT 0,
    used_count INTEGER NOT NULL DEFAULT 0,
    expires_at TIMESTAMP,
    status VARCHAR(20) NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_invite_codes_status ON invite_codes(status);

CREATE TABLE IF NOT EXISTS merchant_notification_prefs (
    merchant_id VARCHAR(36) PRIMARY KEY,
    notify_payment BOOLEAN NOT NULL DEFAULT TRUE,
    notify_settlement BOOLEAN NOT NULL DEFAULT TRUE,
    notify_complaint BOOLEAN NOT NULL DEFAULT TRUE,
    notify_channel VARCHAR(20) NOT NULL DEFAULT 'mail',
    updated_at TIMESTAMP NOT NULL
);
