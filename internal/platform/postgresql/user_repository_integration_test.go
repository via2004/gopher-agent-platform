//go:build integration

package platform

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"gopherai/internal/user"
)

func TestUserRepositoryCreate(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("TEST_DATABASE_URL is required for PostgreSQL integration tests")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("create PostgreSQL pool: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping PostgreSQL: %v", err)
	}

	repo := NewUserRepository(pool)
	email := fmt.Sprintf("repository-test-%d@example.com", time.Now().UnixNano())
	created := &user.User{
		Email:        email,
		PasswordHash: "test-password-hash",
	}

	if err := repo.Create(ctx, created); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", created.ID); err != nil {
			t.Errorf("clean up user %d: %v", created.ID, err)
		}
	})

	if created.ID == 0 {
		t.Error("Create() did not populate user ID")
	}
	if created.CreatedAt.IsZero() {
		t.Error("Create() did not populate created_at")
	}
	if created.UpdatedAt.IsZero() {
		t.Error("Create() did not populate updated_at")
	}

	var storedEmail, storedPasswordHash string
	if err := pool.QueryRow(ctx,
		"SELECT email, password_hash FROM users WHERE id = $1",
		created.ID,
	).Scan(&storedEmail, &storedPasswordHash); err != nil {
		t.Fatalf("query created user: %v", err)
	}
	if storedEmail != created.Email {
		t.Errorf("stored email = %q, want %q", storedEmail, created.Email)
	}
	if storedPasswordHash != created.PasswordHash {
		t.Errorf("stored password hash = %q, want %q", storedPasswordHash, created.PasswordHash)
	}

	queried, err := repo.GetByEmail(ctx, email)
	if err != nil {
		t.Fatalf("GetByEmail() error = %v", err)
	}
	if queried.ID != created.ID || queried.Email != created.Email ||
		queried.PasswordHash != created.PasswordHash ||
		!queried.CreatedAt.Equal(created.CreatedAt) ||
		!queried.UpdatedAt.Equal(created.UpdatedAt) {
		t.Errorf("GetByEmail() user = %#v, want %#v", queried, created)
	}

	missingEmail := fmt.Sprintf("missing-%d@example.com", time.Now().UnixNano())
	if _, err := repo.GetByEmail(ctx, missingEmail); !errors.Is(err, user.ErrUserNotFound) {
		t.Fatalf("missing GetByEmail() error = %v, want %v", err, user.ErrUserNotFound)
	}

	duplicate := &user.User{
		Email:        email,
		PasswordHash: "another-password-hash",
	}
	if err := repo.Create(ctx, duplicate); !errors.Is(err, user.ErrEmailAlreadyExists) {
		t.Fatalf("duplicate Create() error = %v, want %v", err, user.ErrEmailAlreadyExists)
	}
}
