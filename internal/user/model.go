package user

import "time"

// User is the user entity used by the business layer.
type User struct {
	ID           uint64
	Email        string
	PasswordHash string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func NewUser() *User {
	return &User{}
}
