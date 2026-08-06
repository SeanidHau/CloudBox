ALTER TABLE upload_tasks ADD COLUMN parent_id INTEGER REFERENCES folders(id);

CREATE INDEX idx_upload_tasks_user_parent_status ON upload_tasks(user_id, parent_id, status);