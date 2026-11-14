-- 000023: payment method catalogue, per-day channel rotation and WeChat MP configs.
-- Only portable SQL is used so the migration runs on SQLite, MySQL and PostgreSQL.

CREATE TABLE IF NOT EXISTS pay_types (
    id VARCHAR(36) PRIMARY KEY,
    code VARCHAR(32) NOT NULL UNIQUE,
    name VARCHAR(64) NOT NULL,
    icon VARCHAR(255),
    sort INT NOT NULL DEFAULT 0,
    status VARCHAR(20) NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_pay_types_status_sort ON pay_types(status, sort);

CREATE TABLE IF NOT EXISTS pay_rolls (
    id VARCHAR(36) PRIMARY KEY,
    channel_id VARCHAR(36) NOT NULL,
    roll_date DATE NOT NULL,
    weight INT NOT NULL DEFAULT 1,
    status VARCHAR(20) NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    UNIQUE (channel_id, roll_date)
);

CREATE INDEX IF NOT EXISTS idx_pay_rolls_date ON pay_rolls(roll_date, status);

CREATE TABLE IF NOT EXISTS wechat_mp_configs (
    id VARCHAR(36) PRIMARY KEY,
    merchant_id VARCHAR(36),
    app_id VARCHAR(64) NOT NULL,
    app_secret_encrypted TEXT NOT NULL,
    token VARCHAR(64),
    aes_key_encrypted TEXT,
    status VARCHAR(20) NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_wechat_mp_configs_merchant ON wechat_mp_configs(merchant_id, status);
