-- 000030: complaint query indexes and the second factor replay guard.
--
-- complaints had no index at all. The automatic reply rule scans
-- "status = ? AND deadline_at IS NOT NULL AND deadline_at < ?" every minute, and
-- the merchant/admin lists read "merchant_id = ? ORDER BY created_at DESC", both
-- of which were full table scans; complaint_messages was read per complaint with
-- no index either.

CREATE INDEX IF NOT EXISTS idx_complaints_status_deadline
    ON complaints(status, deadline_at);
CREATE INDEX IF NOT EXISTS idx_complaints_merchant_created
    ON complaints(merchant_id, created_at);
CREATE INDEX IF NOT EXISTS idx_complaints_channel_created
    ON complaints(channel, created_at);
CREATE INDEX IF NOT EXISTS idx_complaint_messages_complaint_created
    ON complaint_messages(complaint_id, created_at);

-- TOTP replay guard: the accepted time step is stored so the same six digits
-- cannot be replayed inside the +-1 step window (RFC 6238 section 5.2).
ALTER TABLE admin_totp ADD COLUMN last_used_step BIGINT NOT NULL DEFAULT 0;
