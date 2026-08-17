CREATE TABLE chat_jobs (
    id                    BIGSERIAL PRIMARY KEY,
    conversation_id       BIGINT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    content               TEXT NOT NULL CHECK (content <> ''),
    status                VARCHAR(20) NOT NULL DEFAULT 'pending' CHECK (
        status IN ('pending', 'processing', 'completed', 'failed')
    ),
    assistant_message_id  BIGINT,
    error_code            VARCHAR(100),
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at            TIMESTAMPTZ,
    finished_at           TIMESTAMPTZ,

    CONSTRAINT chat_jobs_assistant_message_fk
        FOREIGN KEY (assistant_message_id, conversation_id)
        REFERENCES messages(id, conversation_id)
        ON DELETE CASCADE,
    CONSTRAINT chat_jobs_lifecycle_check CHECK (
        (status = 'pending'
            AND started_at IS NULL
            AND finished_at IS NULL)
        OR
        (status = 'processing'
            AND started_at IS NOT NULL
            AND finished_at IS NULL)
        OR
        (status IN ('completed', 'failed')
            AND started_at IS NOT NULL
            AND finished_at IS NOT NULL)
    ),
    CONSTRAINT chat_jobs_result_check CHECK (
        (status = 'completed'
            AND assistant_message_id IS NOT NULL
            AND error_code IS NULL)
        OR
        (status = 'failed'
            AND assistant_message_id IS NULL
            AND error_code IS NOT NULL
            AND error_code <> '')
        OR
        (status IN ('pending', 'processing')
            AND assistant_message_id IS NULL
            AND error_code IS NULL)
    )
);

CREATE UNIQUE INDEX idx_chat_jobs_assistant_message
    ON chat_jobs (assistant_message_id)
    WHERE assistant_message_id IS NOT NULL;

CREATE INDEX idx_chat_jobs_conversation_created_at
    ON chat_jobs (conversation_id, created_at DESC, id DESC);
