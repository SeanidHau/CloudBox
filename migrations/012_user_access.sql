ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'user'
    CHECK (role IN ('user', 'admin'));

ALTER TABLE users ADD COLUMN status TEXT NOT NULL DEFAULT 'active'
    CHECK (status IN ('active', 'disabled'));

ALTER TABLE users ADD COLUMN storage_quota_bytes INTEGER NOT NULL DEFAULT 1073741824
    CHECK (storage_quota_bytes > 0);

ALTER TABLE users ADD COLUMN session_version INTEGER NOT NULL DEFAULT 1
    CHECK (session_version > 0);

ALTER TABLE users ADD COLUMN must_change_password INTEGER NOT NULL DEFAULT 0
    CHECK (must_change_password IN (0, 1));

CREATE TABLE invitations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    code_digest TEXT NOT NULL UNIQUE,
    code_hash TEXT NOT NULL,
    created_by_user_id INTEGER NOT NULL REFERENCES users(id),
    expires_at DATETIME NOT NULL,
    used_by_user_id INTEGER REFERENCES users(id),
    used_at DATETIME,
    revoked_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_invitations_active
ON invitations(expires_at, revoked_at, used_at);
