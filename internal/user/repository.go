package user

import "context"

// UserRepository defines the persistence required by the user module.
type UserRepository interface {
	Create(ctx context.Context, user *User) error
	GetByEmail(ctx context.Context, email string) (*User, error)
	GetByID(ctx context.Context, userID uint64) (*User, error)
}

type EmailVerifier interface {
	VerifyAndConsume(ctx context.Context, email, code string) error
}
