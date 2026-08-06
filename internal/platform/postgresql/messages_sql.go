package platform

const InsertMessageByConversationIDAndUserID = `
INSERT INTO messages (conversation_id, role, content)
SELECT id, $3, $4
FROM conversations
WHERE user_id = $1 AND id = $2
RETURNING id, created_at
`

const QueryMessagesByConversationIDAndUserID = `
SELECT m.id, m.conversation_id, m.role, m.content, m.created_at
FROM messages AS m
JOIN conversations AS c ON c.id = m.conversation_id
WHERE c.user_id = $1 AND c.id = $2
ORDER BY m.created_at ASC, m.id ASC
LIMIT $3 OFFSET $4
`

const QueryRecentMessagesByConversationIDAndUserID = `
SELECT recent.id, recent.conversation_id, recent.role, recent.content, recent.created_at
FROM (
    SELECT m.id, m.conversation_id, m.role, m.content, m.created_at
    FROM messages AS m
    JOIN conversations AS c ON c.id = m.conversation_id
    WHERE c.user_id = $1 AND c.id = $2
    ORDER BY m.created_at DESC, m.id DESC
    LIMIT $3
) AS recent
ORDER BY recent.created_at ASC, recent.id ASC
`
