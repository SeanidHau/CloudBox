CREATE TABLE file_scans (
    file_object_id INTEGER PRIMARY KEY
        REFERENCES file_objects(id) ON DELETE CASCADE,
    status TEXT NOT NULL
        CHECK (status IN ('pending', 'scanning', 'clean', 'infected', 'failed')),
    signature TEXT,
    scanned_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_file_scans_status ON file_scans(status);