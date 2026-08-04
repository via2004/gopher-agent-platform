package platform

const InsertUser = `INSERT INTO users (email, password_hash)
VALUES ($1, $2)
RETURNING id, created_at, updated_at`

const QueryUserByEmail = `SELECT id, email, password_hash, created_at, updated_at
FROM users
WHERE email = $1`

const QueryUserByID = `SELECT id, email, password_hash, created_at, updated_at
FROM users
WHERE id = $1`
