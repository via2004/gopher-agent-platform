DROP TABLE model_calls;

ALTER TABLE messages
    DROP CONSTRAINT messages_id_conversation_unique;
