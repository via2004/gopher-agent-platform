//go:build integration

package platform

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"gopherai/internal/conversation"
)

func TestConversationRepositoryCreate(t *testing.T) {
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

	var userID uint64
	email := fmt.Sprintf("conversation-repository-test-%d@example.com", time.Now().UnixNano())
	if err := pool.QueryRow(ctx,
		"INSERT INTO users (email, password_hash) VALUES ($1, $2) RETURNING id",
		email,
		"test-password-hash",
	).Scan(&userID); err != nil {
		t.Fatalf("create test user: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", userID); err != nil {
			t.Errorf("clean up user %d: %v", userID, err)
		}
	})

	repo := NewConversationRepository(pool)
	created := &conversation.Conversation{UserID: userID, Title: "Go and AI"}
	if err := repo.Create(ctx, created); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.ID == 0 || created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Errorf("Create() did not populate generated fields: %#v", created)
	}

	var storedUserID uint64
	var storedTitle string
	if err := pool.QueryRow(ctx,
		"SELECT user_id, title FROM conversations WHERE id = $1",
		created.ID,
	).Scan(&storedUserID, &storedTitle); err != nil {
		t.Fatalf("query created conversation: %v", err)
	}
	if storedUserID != userID || storedTitle != "Go and AI" {
		t.Errorf("stored conversation = (user ID %d, title %q), want (%d, %q)", storedUserID, storedTitle, userID, "Go and AI")
	}
}
