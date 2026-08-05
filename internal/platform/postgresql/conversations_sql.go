package platform

const InsertConversation = `
INSERT INTO conversations (user_id, title)
VALUES ($1, $2)
RETURNING id, created_at, updated_at
`

const QueryConversationByUserID = `
SELECT id, user_id, title, created_at, updated_at
FROM conversations
WHERE user_id = $1
ORDER BY created_at DESC, id DESC
LIMIT $2 OFFSET $3`

const GetConversationByIDAndUserID = `
SELECT id, user_id, title, created_at, updated_at
FROM conversations
WHERE id = $1 AND user_id = $2
`
const DeleteConversationByIDAndUserID = `
DELETE FROM conversations
WHERE id = $1 AND user_id = $2
RETURNING id
`
