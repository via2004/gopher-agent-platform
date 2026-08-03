package platform

const InsertUser = `INSERT INTO users (email, password_hash)
VALUES ($1, $2)
RETURNING id, created_at, updated_at`
