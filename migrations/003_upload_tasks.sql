CREATE TABLE upload_tasks (
    id TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL,
    original_name TEXT NOT NULL,
    content_type TEXT NOT NULL,
    file_size INTEGER NOT NULL CHECK ( file_size > 0 ),
    chunk_size INTEGER NOT NULL CHECK ( chunk_size > 0 ),
    total_chunks INTEGER NOT NULL CHECK ( total_chunks > 0 ),
    file_hash TEXT,
    status TEXT NOT NULL DEFAULT 'uploading' CHECK ( status IN ('uploading', 'completing', 'completed', 'failed')),
    temp_dir TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id)
);

CREATE INDEX  idx_upload_tasks_user_status ON upload_tasks(user_id, status);

CREATE TABLE  upload_chunks (
    upload_id TEXT NOT NULL,
    chunk_number INTEGER NOT NULL CHECK ( chunk_number >= 0 ),
    size INTEGER NOT NULL CHECK ( size >= 0 ),
    chunck_hash TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (upload_id, chunk_number),
    FOREIGN KEY (upload_id) REFERENCES uplaod_tasks(id)
);