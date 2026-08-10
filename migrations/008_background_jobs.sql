CREATE TABLE background_jobs (
     id TEXT PRIMARY KEY NOT NULL,
     job_type TEXT NOT NULL CHECK (length(trim(job_type)) > 0),
     payload TEXT NOT NULL DEFAULT '{}',
     status TEXT NOT NULL DEFAULT 'queued'
         CHECK (status IN ('queued', 'running', 'succeeded', 'failed')),
     attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
     max_attempts INTEGER NOT NULL DEFAULT 3 CHECK (max_attempts > 0),
     run_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
     locked_at DATETIME,
     last_error TEXT,
     created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
     updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_background_jobs_poll ON background_jobs (status, run_at, created_at);