-- 000014: protocol version of the order that decides the merchant notification
-- format. Upstream Epay stores `version` at order creation
-- (Epay/includes/lib/api/Pay.php:171: $version = defined('API_INIT') ? 1 : 0) and
-- creat_callback branches on it (Epay/includes/functions.php:503): version 1
-- sends an RSA signed payload with api_trade_no/timestamp, version 0 sends the
-- legacy MD5 payload. Deriving the format from the merchant credential instead
-- would break V1 integrations that also enabled an RSA key.

ALTER TABLE payment_orders ADD COLUMN version INTEGER NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_payment_orders_version ON payment_orders(version);
