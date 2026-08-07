CREATE TABLE file_shares (
    token TEXT PRIMARY KEY NOT NULL CHECK ( length(token) > 0 ),
    user_file_id INTEGER NOT NULL,
    password_hash TEXT,
    expires_at DATETIME,
    max_downloads INTEGER CHECK (
        max_downloads IS NULL OR max_downloads > 0
    ),
    download_count INTEGER NOT NULL DEFAULT 0 CHECK ( download_count >= 0 ),
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK ( max_downloads IS NULL OR download_count <= max_downloads ),
    FOREIGN KEY (user_file_id) REFERENCES user_files(id)
);

CREATE INDEX idx_file_shares_user_file ON file_shares(user_file_id);
CREATE INDEX idx_file_shares_expires_at ON file_shares(expires_at);