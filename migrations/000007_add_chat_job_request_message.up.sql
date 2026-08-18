ALTER TABLE chat_jobs
    ADD COLUMN request_message_id BIGINT,
    ADD CONSTRAINT chat_jobs_request_message_fk
        FOREIGN KEY (request_message_id, conversation_id)
        REFERENCES messages(id, conversation_id)
        ON DELETE CASCADE;
