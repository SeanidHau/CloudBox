ALTER TABLE background_jobs ADD COLUMN user_id INTEGER REFERENCES users(id) ON DELETE SET NULL;

CREATE INDEX idx_background_jobs_user_created ON background_jobs (user_id, created_at);