package platform

const InsertConversation = `INSERT INTO conversations (user_id, title)
VALUES ($1, $2)
RETURNING id, created_at, updated_at`
