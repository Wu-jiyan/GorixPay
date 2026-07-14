CREATE TABLE IF NOT EXISTS payment_callbacks (
    id VARCHAR(36) PRIMARY KEY,
    order_id VARCHAR(36) NOT NULL,
    channel_id VARCHAR(36) NOT NULL,
    channel_trade_no VARCHAR(128) NOT NULL,
    digest VARCHAR(64) NOT NULL UNIQUE,
    amount BIGINT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    UNIQUE (channel_id, channel_trade_no),
    FOREIGN KEY (order_id) REFERENCES payment_orders(id)
);
