CREATE TABLE folders (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    parent_id INTEGER,
    name TEXT NOT NULL CHECK ( length(trim(name)) > 0 ),
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id),
    FOREIGN KEY (parent_id) REFERENCES folders(id)
);

CREATE UNIQUE INDEX idx_folders_user_parent_name ON folders(user_id, COALESCE(parent_id, 0), name);

CREATE INDEX idx_folders_user_parent ON folders(user_id, parent_id);

ALTER TABLE user_files ADD COLUMN parent_id INTEGER REFERENCES folders(id);

CREATE INDEX idx_user_files_user_parent_status ON user_files(user_id, parent_id, status);