CREATE TABLE IF NOT EXISTS payment_orders (
    id VARCHAR(36) PRIMARY KEY,
    trade_no VARCHAR(32) NOT NULL UNIQUE,
    merchant_id VARCHAR(36) NOT NULL,
    out_trade_no VARCHAR(64) NOT NULL,
    payment_type VARCHAR(32) NOT NULL,
    name VARCHAR(255) NOT NULL,
    amount BIGINT NOT NULL,
    currency CHAR(3) NOT NULL,
    status VARCHAR(20) NOT NULL,
    notify_url TEXT NOT NULL,
    return_url TEXT,
    client_ip VARCHAR(45),
    attach TEXT,
    channel_trade_no VARCHAR(128),
    paid_at TIMESTAMP,
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    UNIQUE (merchant_id, out_trade_no),
    FOREIGN KEY (merchant_id) REFERENCES merchants(id)
);

CREATE TABLE IF NOT EXISTS payment_attempts (
    id VARCHAR(36) PRIMARY KEY,
    order_id VARCHAR(36) NOT NULL,
    channel_id VARCHAR(36),
    attempt_no INTEGER NOT NULL,
    status VARCHAR(20) NOT NULL,
    channel_trade_no VARCHAR(128),
    error_code VARCHAR(100),
    error_message VARCHAR(500),
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    UNIQUE (order_id, attempt_no),
    FOREIGN KEY (order_id) REFERENCES payment_orders(id)
);
