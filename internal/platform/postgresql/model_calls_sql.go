package platform

// InsertRunningModelCall parameters: conversation ID, user ID, request message
// ID, provider, and requested model.
const InsertRunningModelCall = `
INSERT INTO model_calls (
    conversation_id,
    request_message_id,
    provider,
    requested_model,
    status
)
SELECT c.id, m.id, $4, $5, 'running'
FROM conversations AS c
JOIN messages AS m
  ON m.id = $3
 AND m.conversation_id = c.id
 AND m.role = 'user'
WHERE c.id = $1
  AND c.user_id = $2
RETURNING id, started_at
`

const CompleteModelCall = `
UPDATE model_calls AS mc
SET status = 'completed',
    assistant_message_id = $3,
    actual_model = $4,
    provider_response_id = $5,
    input_tokens = $6,
    output_tokens = $7,
    total_tokens = $8,
    error_code = NULL,
    finished_at = NOW()
FROM conversations AS c
WHERE mc.id = $1
  AND c.id = mc.conversation_id
  AND c.user_id = $2
  AND mc.status = 'running'
  AND EXISTS (
      SELECT 1
      FROM messages AS m
      WHERE m.id = $3
        AND m.conversation_id = mc.conversation_id
        AND m.role = 'assistant'
  )
RETURNING mc.id, mc.status, mc.assistant_message_id, mc.actual_model,
          mc.provider_response_id, mc.input_tokens, mc.output_tokens,
          mc.total_tokens, mc.started_at, mc.finished_at
`

// FinishModelCall parameters: call ID, user ID, terminal status, actual
// model, provider response ID, optional token counts, and error code.
const FinishModelCall = `
UPDATE model_calls AS mc
SET status = $3,
    actual_model = $4,
    provider_response_id = $5,
    input_tokens = $6,
    output_tokens = $7,
    total_tokens = $8,
    error_code = $9,
    finished_at = NOW()
FROM conversations AS c
WHERE mc.id = $1
  AND c.id = mc.conversation_id
  AND c.user_id = $2
  AND mc.status = 'running'
RETURNING mc.id, mc.status, mc.actual_model, mc.provider_response_id,
          mc.input_tokens, mc.output_tokens, mc.total_tokens,
          mc.error_code, mc.started_at, mc.finished_at
`

const GetModelCallByIDAndUserID = `
SELECT mc.id, mc.conversation_id, mc.request_message_id,
       mc.assistant_message_id, mc.provider, mc.requested_model,
       mc.actual_model, mc.provider_response_id, mc.status,
       mc.input_tokens, mc.output_tokens, mc.total_tokens,
       mc.error_code, mc.started_at, mc.finished_at
FROM model_calls AS mc
JOIN conversations AS c ON c.id = mc.conversation_id
WHERE mc.id = $1
  AND c.user_id = $2
`
