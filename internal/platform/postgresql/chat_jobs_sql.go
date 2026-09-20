package platform

// InsertPendingChatJob parameters: user ID, conversation ID, and content.
const InsertPendingChatJob = `
INSERT INTO chat_jobs (conversation_id, content)
SELECT c.id, $3
FROM conversations AS c
WHERE c.user_id = $1
  AND c.id = $2
RETURNING id, conversation_id, content, status, attempt_count,
          assistant_message_id, error_code, created_at,
          started_at, finished_at
`

// GetChatJobByIDAndUserID parameters: user ID and job ID.
const GetChatJobByIDAndUserID = `
SELECT j.id, j.conversation_id, j.content, j.status,
       j.attempt_count, j.assistant_message_id, j.error_code,
       j.created_at, j.started_at, j.finished_at
FROM chat_jobs AS j
JOIN conversations AS c ON c.id = j.conversation_id
WHERE c.user_id = $1
  AND j.id = $2
`

// ClaimPendingChatJob parameters: job ID. The returned user ID belongs to the
// job's conversation and is used by the worker to call the chat service.
const ClaimPendingChatJob = `
UPDATE chat_jobs AS j
SET status = 'processing',
    started_at = NOW(),
    attempt_count = j.attempt_count + 1
FROM conversations AS c
WHERE j.id = $1
  AND j.status = 'pending'
  AND c.id = j.conversation_id
RETURNING j.id, j.conversation_id, j.content, j.status,
          j.attempt_count, j.assistant_message_id, j.error_code,
          j.created_at, j.started_at, j.finished_at, c.user_id
`

// RetryChatJob parameters: job ID.
const RetryChatJob = `
UPDATE chat_jobs
SET status = 'pending',
    started_at = NULL
WHERE id = $1
  AND status = 'processing'
RETURNING id
`

// 一次性生成该任务的用户消息，并在后续调用时返回相同的消息 ID。
const EnsureChatJobRequestMessage = `
WITH target_job AS (
    SELECT j.id, j.conversation_id, j.content, j.request_message_id
    FROM chat_jobs AS j
    JOIN conversations AS c ON c.id = j.conversation_id
    WHERE c.user_id = $1
      AND j.id = $2
      AND j.status = 'processing'
    FOR UPDATE OF j
),
inserted_message AS (
    INSERT INTO messages (conversation_id, role, content)
    SELECT conversation_id, 'user', content
    FROM target_job
    WHERE request_message_id IS NULL
    RETURNING id, conversation_id
),
bound_job AS (
    UPDATE chat_jobs AS j
    SET request_message_id = m.id
    FROM inserted_message AS m
    WHERE j.id = $2
      AND j.conversation_id = m.conversation_id
    RETURNING j.request_message_id
)
SELECT request_message_id
FROM target_job
WHERE request_message_id IS NOT NULL
UNION ALL
SELECT request_message_id
FROM bound_job
LIMIT 1
`

// GetCompletedAssistantMessageIDByChatJob parameters: user ID and job ID.
const GetCompletedAssistantMessageIDByChatJob = `
SELECT mc.assistant_message_id
FROM chat_jobs AS j
JOIN conversations AS c ON c.id = j.conversation_id
JOIN model_calls AS mc
  ON mc.conversation_id = j.conversation_id
 AND mc.request_message_id = j.request_message_id
WHERE c.user_id = $1
  AND j.id = $2
  AND j.status = 'processing'
  AND mc.status = 'completed'
  AND mc.assistant_message_id IS NOT NULL
ORDER BY mc.finished_at DESC, mc.id DESC
LIMIT 1
`

// CompleteChatJob parameters: job ID and assistant message ID.
const CompleteChatJob = `
UPDATE chat_jobs AS j
SET status = 'completed',
    assistant_message_id = $2,
    error_code = NULL,
    finished_at = NOW()
FROM messages AS m
WHERE j.id = $1
  AND j.status = 'processing'
  AND m.id = $2
  AND m.conversation_id = j.conversation_id
  AND m.role = 'assistant'
RETURNING j.id
`

// FailChatJob parameters: job ID and error code.
const FailChatJob = `
UPDATE chat_jobs
SET status = 'failed',
    assistant_message_id = NULL,
    error_code = $2,
    finished_at = NOW()
WHERE id = $1
  AND status = 'processing'
RETURNING id
`
