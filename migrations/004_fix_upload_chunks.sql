ALTER TABLE upload_chunks RENAME TO upload_chunks_legacy;

CREATE TABLE upload_chunks (
                               upload_id TEXT NOT NULL,
                               chunk_number INTEGER NOT NULL CHECK (chunk_number >= 0),
                               size INTEGER NOT NULL CHECK (size >= 0),
    chunk_hash TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (upload_id, chunk_number),
    FOREIGN KEY (upload_id) REFERENCES upload_tasks(id)
);

INSERT INTO upload_chunks (
    upload_id,
    chunk_number,
    size,
    chunk_hash,
    created_at
)
SELECT
    upload_id,
    chunk_number,
    size,
    chunck_hash,
    created_at
FROM upload_chunks_legacy;

DROP TABLE upload_chunks_legacy;