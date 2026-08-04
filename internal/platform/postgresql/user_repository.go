package platform

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"gopherai/internal/user"
)

type UserRepository struct {
	pool *pgxpool.Pool
}

func NewUserRepository(pool *pgxpool.Pool) *UserRepository {
	return &UserRepository{pool: pool}
}

func (r *UserRepository) Create(ctx context.Context, newUser *user.User) error {
	err := r.pool.QueryRow(ctx, InsertUser, newUser.Email, newUser.PasswordHash).Scan(&newUser.ID, &newUser.CreatedAt, &newUser.UpdatedAt)
	var pgErr *pgconn.PgError

	if errors.As(err, &pgErr) && pgErr.Code == "23505" &&
		pgErr.ConstraintName == "users_email_unique" {
		return user.ErrEmailAlreadyExists
	}

	if err != nil {
		return fmt.Errorf("insert user: %w", err)
	}

	return nil
}

func (r *UserRepository) GetByEmail(ctx context.Context, email string) (*user.User, error) {
	queryUser := user.NewUser()
	err := r.pool.QueryRow(ctx, QueryUserByEmail, email).
		Scan(&queryUser.ID, &queryUser.Email, &queryUser.PasswordHash,
			&queryUser.CreatedAt, &queryUser.UpdatedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, user.ErrUserNotFound
	} else if err != nil {
		return nil, fmt.Errorf("query user error: %w", err)
	}

	return queryUser, nil
}

func (r *UserRepository) GetByID(ctx context.Context, userID uint64) (*user.User, error) {
	queryUser := user.NewUser()
	err := r.pool.QueryRow(ctx, QueryUserByID, userID).
		Scan(&queryUser.ID, &queryUser.Email, &queryUser.PasswordHash,
			&queryUser.CreatedAt, &queryUser.UpdatedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, user.ErrUserNotFound
	} else if err != nil {
		return nil, fmt.Errorf("query user error: %w", err)
	}

	return queryUser, nil
}
