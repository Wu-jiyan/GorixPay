-- 000029: indexes for the queries the gateway actually runs on every request,
-- every second or every report. 000003 created payment_orders without any index
-- besides the trade_no/out_trade_no uniques, so every merchant order list, every
-- admin order list and every statistics report scanned and sorted the whole
-- table; outbox_messages had no index at all while the worker polls it once per
-- second.
--
-- Everything here is IF NOT EXISTS so the migration is safe to re-run and safe on
-- a database that already carries the GORM-declared index names.

-- Merchant order list: WHERE merchant_id = ? ORDER BY created_at DESC.
CREATE INDEX IF NOT EXISTS idx_payment_orders_merchant_created
    ON payment_orders(merchant_id, created_at);

-- Admin order list and the expiry sweeper: WHERE status = ? ORDER BY created_at.
CREATE INDEX IF NOT EXISTS idx_payment_orders_status_created
    ON payment_orders(status, created_at);

-- Cashier expiry check and any "expired but still open" scan.
CREATE INDEX IF NOT EXISTS idx_payment_orders_expires_at
    ON payment_orders(expires_at);

-- Reporting windows: paid orders between two timestamps.
CREATE INDEX IF NOT EXISTS idx_payment_orders_paid_at
    ON payment_orders(paid_at);

-- Reporting/export by kind (recharge vs sale) with a date window.
CREATE INDEX IF NOT EXISTS idx_payment_orders_kind_created
    ON payment_orders(kind, created_at);

-- outbox worker: (status = 'pending' AND available_at <= now) OR
-- (status = 'processing' AND lease_until <= now).
CREATE INDEX IF NOT EXISTS idx_outbox_messages_available
    ON outbox_messages(status, available_at);
CREATE INDEX IF NOT EXISTS idx_outbox_messages_lease
    ON outbox_messages(status, lease_until);
