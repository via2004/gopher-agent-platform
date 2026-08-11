ALTER TABLE messages
    ADD CONSTRAINT messages_id_conversation_unique UNIQUE (id, conversation_id);

CREATE TABLE model_calls (
    id                    BIGSERIAL PRIMARY KEY,
    conversation_id       BIGINT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    request_message_id    BIGINT NOT NULL,
    assistant_message_id  BIGINT,
    provider              VARCHAR(50) NOT NULL,
    requested_model       VARCHAR(200),
    actual_model          VARCHAR(200),
    provider_response_id  VARCHAR(255),
    status                VARCHAR(20) NOT NULL CHECK (
        status IN ('running', 'completed', 'failed', 'cancelled', 'timed_out', 'incomplete')
    ),
    input_tokens          BIGINT CHECK (input_tokens IS NULL OR input_tokens >= 0),
    output_tokens         BIGINT CHECK (output_tokens IS NULL OR output_tokens >= 0),
    total_tokens          BIGINT CHECK (total_tokens IS NULL OR total_tokens >= 0),
    error_code            VARCHAR(100),
    started_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at           TIMESTAMPTZ,

    CONSTRAINT model_calls_request_message_fk
        FOREIGN KEY (request_message_id, conversation_id)
        REFERENCES messages(id, conversation_id)
        ON DELETE CASCADE,
    CONSTRAINT model_calls_assistant_message_fk
        FOREIGN KEY (assistant_message_id, conversation_id)
        REFERENCES messages(id, conversation_id)
        ON DELETE CASCADE,
    CONSTRAINT model_calls_lifecycle_check CHECK (
        (status = 'running' AND finished_at IS NULL)
        OR
        (status <> 'running' AND finished_at IS NOT NULL)
    ),
    CONSTRAINT model_calls_assistant_status_check CHECK (
        assistant_message_id IS NULL OR status = 'completed'
    ),
    CONSTRAINT model_calls_completed_assistant_check CHECK (
        status <> 'completed' OR assistant_message_id IS NOT NULL
    )
);

CREATE UNIQUE INDEX idx_model_calls_assistant_message
    ON model_calls (assistant_message_id)
    WHERE assistant_message_id IS NOT NULL;

CREATE INDEX idx_model_calls_conversation_started_at
    ON model_calls (conversation_id, started_at DESC, id DESC);
