package user

import "context"

// UserRepository defines the persistence required by the user module.
type UserRepository interface {
	Create(ctx context.Context, user *User) error
}
