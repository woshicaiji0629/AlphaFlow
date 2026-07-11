CREATE TABLE audit_logs (
    id UUID PRIMARY KEY,
    user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    event_type TEXT NOT NULL,
    outcome TEXT NOT NULL CHECK (outcome IN ('success', 'failure', 'blocked')),
    subject TEXT NOT NULL DEFAULT '',
    ip_address INET,
    user_agent TEXT NOT NULL DEFAULT '',
    request_id TEXT NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX audit_logs_user_created_idx ON audit_logs(user_id, created_at DESC);
CREATE INDEX audit_logs_event_created_idx ON audit_logs(event_type, created_at DESC);
