-- 000013: payment order kind (sale / recharge / deposit).
-- Created by the Lead so the payment model change is migrated before the
-- funding package lands; the funding work reuses this migration.

ALTER TABLE payment_orders ADD COLUMN kind VARCHAR(20) NOT NULL DEFAULT 'sale';

CREATE INDEX IF NOT EXISTS idx_payment_orders_kind ON payment_orders(kind);
