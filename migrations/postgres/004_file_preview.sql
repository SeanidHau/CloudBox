CREATE TABLE file_previews (
    file_object_id BIGINT PRIMARY KEY REFERENCES file_objects(id) ON DELETE CASCADE,
    storage_path TEXT NOT NULL,
    size BIGINT NOT NULL CHECK (size >= 0),
    content_type TEXT NOT NULL,
    width INTEGER NOT NULL CHECK (width > 0),
    height INTEGER NOT NULL CHECK (height > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);