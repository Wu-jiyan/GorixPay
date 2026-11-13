-- 000015: buyer identity forwarded by the V1/V2 create endpoints.
-- Upstream stores the payer's openid in pre_order.buyer at creation time
-- (Epay/includes/lib/api/Pay.php:425: $buyer = $sub_openid) and every channel
-- plugin reads it from the order when it starts the payment. Without the column a
-- JSAPI payment started from the cashier has no openid to hand the channel.

ALTER TABLE payment_orders ADD COLUMN buyer VARCHAR(128);

CREATE INDEX IF NOT EXISTS idx_payment_orders_buyer ON payment_orders(buyer);
