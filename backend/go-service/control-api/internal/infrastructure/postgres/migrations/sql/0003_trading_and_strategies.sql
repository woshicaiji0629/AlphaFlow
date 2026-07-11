CREATE TABLE trading_accounts (
    id UUID PRIMARY KEY,
    owner_user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    mode TEXT NOT NULL CHECK (mode IN ('paper', 'testnet', 'live')),
    exchange TEXT NOT NULL DEFAULT '',
    account_key TEXT NOT NULL,
    display_name TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    trading_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (owner_user_id, mode, account_key)
);

CREATE INDEX trading_accounts_owner_idx ON trading_accounts(owner_user_id, status);

CREATE TABLE trading_account_credentials (
    account_id UUID PRIMARY KEY REFERENCES trading_accounts(id) ON DELETE CASCADE,
    encrypted_payload BYTEA NOT NULL,
    encryption_key_version TEXT NOT NULL,
    last_verified_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE user_permissions (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    permission TEXT NOT NULL,
    granted_by UUID REFERENCES users(id) ON DELETE SET NULL,
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, permission)
);

CREATE TABLE user_risk_profiles (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    live_trading_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    max_accounts INTEGER NOT NULL DEFAULT 3 CHECK (max_accounts > 0),
    max_leverage NUMERIC(18,8) NOT NULL DEFAULT 1 CHECK (max_leverage >= 1),
    max_margin_ratio NUMERIC(18,8) NOT NULL DEFAULT 0.1 CHECK (max_margin_ratio > 0 AND max_margin_ratio <= 1),
    max_position_notional NUMERIC(30,8) NOT NULL DEFAULT 0 CHECK (max_position_notional >= 0),
    short_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    allowed_exchanges JSONB NOT NULL DEFAULT '[]'::jsonb,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE account_risk_limits (
    trading_account_id UUID PRIMARY KEY REFERENCES trading_accounts(id) ON DELETE CASCADE,
    max_leverage NUMERIC(18,8) NOT NULL DEFAULT 1 CHECK (max_leverage >= 1),
    max_position_notional NUMERIC(30,8) NOT NULL DEFAULT 0 CHECK (max_position_notional >= 0),
    daily_loss_limit NUMERIC(30,8) NOT NULL DEFAULT 0 CHECK (daily_loss_limit >= 0),
    max_open_positions INTEGER NOT NULL DEFAULT 1 CHECK (max_open_positions > 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE strategies (
    id UUID PRIMARY KEY,
    code TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    version TEXT NOT NULL,
    parameters JSONB NOT NULL DEFAULT '{}'::jsonb,
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published', 'disabled')),
    visibility TEXT NOT NULL DEFAULT 'public' CHECK (visibility IN ('public', 'restricted', 'admin_only')),
    risk_level TEXT NOT NULL DEFAULT 'high' CHECK (risk_level IN ('low', 'medium', 'high')),
    paper_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    live_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (code, version)
);

CREATE TABLE strategy_entitlements (
    strategy_id UUID NOT NULL REFERENCES strategies(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    paper_allowed BOOLEAN NOT NULL DEFAULT TRUE,
    live_allowed BOOLEAN NOT NULL DEFAULT FALSE,
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (strategy_id, user_id)
);

CREATE TABLE strategy_subscriptions (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    trading_account_id UUID NOT NULL REFERENCES trading_accounts(id) ON DELETE RESTRICT,
    strategy_id UUID NOT NULL REFERENCES strategies(id) ON DELETE RESTRICT,
    strategy_version TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'paused' CHECK (status IN ('active', 'paused', 'stopped')),
    capital_ratio NUMERIC(18,8) NOT NULL DEFAULT 0.1 CHECK (capital_ratio > 0 AND capital_ratio <= 1),
    requested_leverage NUMERIC(18,8) NOT NULL DEFAULT 1 CHECK (requested_leverage >= 1),
    effective_leverage NUMERIC(18,8) NOT NULL DEFAULT 1 CHECK (effective_leverage >= 1),
    risk_parameters JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (trading_account_id, strategy_id)
);

CREATE TABLE strategy_performance_publications (
    id UUID PRIMARY KEY,
    strategy_id UUID NOT NULL REFERENCES strategies(id) ON DELETE RESTRICT,
    strategy_version TEXT NOT NULL,
    source_run_id TEXT NOT NULL,
    exchange TEXT NOT NULL,
    market TEXT NOT NULL,
    symbol_scope JSONB NOT NULL,
    interval TEXT NOT NULL,
    period_start TIMESTAMPTZ NOT NULL,
    period_end TIMESTAMPTZ NOT NULL,
    metrics JSONB NOT NULL,
    equity_points JSONB NOT NULL DEFAULT '[]'::jsonb,
    published_by UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    published_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (period_end > period_start)
);

CREATE INDEX strategy_performance_strategy_idx ON strategy_performance_publications(strategy_id, published_at DESC);
