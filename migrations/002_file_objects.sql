CREATE TABLE file_objects (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    file_hash TEXT NOT NULL UNIQUE,
    storage_path TEXT NOT NULL,
    size INTEGER NOT NULL,
    content_type TEXT NOT NULL,
    reference_count INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

ALTER TABLE user_files ADD COLUMN object_id INTEGER REFERENCES file_objects(id);

CREATE INDEX idx_user_files_object_id
ON user_files(object_id);

INSERT INTO file_objects (file_hash, storage_path, size, content_type, reference_count)
SELECT 'legacy-' || id, storage_path, size, content_type, 1
FROM user_files
WHERE object_id IS NULL;

UPDATE user_files
SET object_id = (
    SELECT file_objects.id
    FROM file_objects
    WHERE file_objects.file_hash = 'legacy-' || user_files.id
    )
WHERE object_id IS NULL;