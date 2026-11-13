-- 000016: merchant settlement profile.
-- Upstream keeps the payout destination on the merchant row (pre_user.settle_id,
-- pre_user.account, pre_user.username) and republishes it through
-- api.php?act=query (Epay/api.php:24) and merchant/info
-- (Epay/includes/lib/api/Merchant.php:20). GorixPay only had the display name,
-- so those two endpoints could not answer the fields merchants actually read.

ALTER TABLE merchants ADD COLUMN settle_type INTEGER NOT NULL DEFAULT 0;
ALTER TABLE merchants ADD COLUMN settle_account VARCHAR(128);
