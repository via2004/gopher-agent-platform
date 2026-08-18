ALTER TABLE chat_jobs
    ADD COLUMN attempt_count INTEGER NOT NULL DEFAULT 0
        CHECK (attempt_count >= 0);
