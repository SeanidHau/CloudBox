CREATE TABLE share_access_audits (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    token TEXT NOT NULL,
    ip_hash TEXT NOT NULL,
    action TEXT NOT NULL CHECK (action IN ('info', 'preview', 'download', 'save')),
    result TEXT NOT NULL CHECK (result IN ('allowed', 'denied', 'rate_limited', 'locked')),
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_share_access_audits_token_created
ON share_access_audits(token, created_at);
