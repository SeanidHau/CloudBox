CREATE TABLE IF NOT EXISTS share_collections (
    token TEXT PRIMARY KEY CHECK (char_length(token) > 0),
    owner_user_id BIGINT NOT NULL REFERENCES users(id),
    password_hash TEXT,
    expires_at TIMESTAMPTZ NOT NULL,
    max_downloads INTEGER CHECK (max_downloads IS NULL OR max_downloads > 0),
    download_count INTEGER NOT NULL DEFAULT 0 CHECK (download_count >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (max_downloads IS NULL OR download_count <= max_downloads)
);

CREATE TABLE IF NOT EXISTS share_collection_items (
    collection_token TEXT NOT NULL REFERENCES share_collections(token) ON DELETE CASCADE,
    user_file_id BIGINT NOT NULL REFERENCES user_files(id) ON DELETE CASCADE,
    PRIMARY KEY (collection_token, user_file_id)
);

CREATE INDEX IF NOT EXISTS idx_share_collections_owner ON share_collections(owner_user_id, created_at);
CREATE INDEX IF NOT EXISTS idx_share_collection_items_file ON share_collection_items(user_file_id);

CREATE OR REPLACE FUNCTION remove_collection_item_for_deleted_file() RETURNS TRIGGER AS $$
BEGIN
    IF NEW.status = 'deleted' AND OLD.status <> 'deleted' THEN
        DELETE FROM share_collections WHERE token IN (
            SELECT collection_token FROM share_collection_items WHERE user_file_id = NEW.id
        );
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS remove_collection_item_for_deleted_file ON user_files;
CREATE TRIGGER remove_collection_item_for_deleted_file
AFTER UPDATE OF status ON user_files
FOR EACH ROW EXECUTE FUNCTION remove_collection_item_for_deleted_file();
