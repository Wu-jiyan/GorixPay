-- 000010_epay_v2.sql
-- epay V2 replay protection store. Platform V2 RSA key material lives in
-- system_settings (epay.v2.platform_private_key encrypted, epay.v2.platform_public_key
-- PEM) and needs no table. Merchant RSA credentials reuse merchant_credentials
-- with credential_type='rsa'.
-- The UNIQUE constraint is declared inline so the statement stays valid on
-- SQLite, MySQL and PostgreSQL without database specific syntax.

CREATE TABLE IF NOT EXISTS epay_request_nonces (
    id VARCHAR(36) PRIMARY KEY,
    merchant_id VARCHAR(36) NOT NULL,
    sign_hash VARCHAR(64) NOT NULL,
    method VARCHAR(32) NOT NULL,
    created_at TIMESTAMP NOT NULL,
    expires_at TIMESTAMP NOT NULL,
    UNIQUE (sign_hash)
);
