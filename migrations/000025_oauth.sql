-- 000025: third party login bindings and QR login sessions.
-- oauth_states is not in the module spec but is required for one-time authorize
-- state; parking it in another module's table would couple the packages.

CREATE TABLE IF NOT EXISTS oauth_bindings (
    id VARCHAR(36) PRIMARY KEY,
    principal_type VARCHAR(20) NOT NULL,
    principal_id VARCHAR(36) NOT NULL,
    provider VARCHAR(20) NOT NULL,
    open_id VARCHAR(128) NOT NULL,
    union_id VARCHAR(128),
    nickname VARCHAR(64),
    avatar VARCHAR(255),
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    UNIQUE (provider, open_id)
);

CREATE INDEX IF NOT EXISTS idx_oauth_bindings_principal ON oauth_bindings(principal_type, principal_id);

CREATE TABLE IF NOT EXISTS qr_login_sessions (
    id VARCHAR(36) PRIMARY KEY,
    ticket VARCHAR(64) NOT NULL UNIQUE,
    principal_type VARCHAR(20),
    principal_id VARCHAR(36),
    status VARCHAR(20) NOT NULL,
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_qr_login_sessions_expires ON qr_login_sessions(expires_at);

CREATE TABLE IF NOT EXISTS oauth_states (
    id VARCHAR(36) PRIMARY KEY,
    state_hash VARCHAR(64) NOT NULL UNIQUE,
    provider VARCHAR(20) NOT NULL,
    redirect_path VARCHAR(255) NOT NULL,
    expires_at TIMESTAMP NOT NULL,
    consumed_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_oauth_states_expires ON oauth_states(expires_at);
