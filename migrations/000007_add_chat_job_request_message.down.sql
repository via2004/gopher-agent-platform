ALTER TABLE chat_jobs
    DROP CONSTRAINT chat_jobs_request_message_fk,
    DROP COLUMN request_message_id;
