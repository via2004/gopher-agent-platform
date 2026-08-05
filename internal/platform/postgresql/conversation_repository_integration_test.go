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

	second := &conversation.Conversation{UserID: userID, Title: "Second"}
	third := &conversation.Conversation{UserID: userID, Title: "Third"}
	if err := repo.Create(ctx, second); err != nil {
		t.Fatalf("create second conversation: %v", err)
	}
	if err := repo.Create(ctx, third); err != nil {
		t.Fatalf("create third conversation: %v", err)
	}

	var otherUserID uint64
	otherEmail := fmt.Sprintf("other-conversation-test-%d@example.com", time.Now().UnixNano())
	if err := pool.QueryRow(ctx,
		"INSERT INTO users (email, password_hash) VALUES ($1, $2) RETURNING id",
		otherEmail,
		"test-password-hash",
	).Scan(&otherUserID); err != nil {
		t.Fatalf("create other test user: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", otherUserID); err != nil {
			t.Errorf("clean up user %d: %v", otherUserID, err)
		}
	})
	if err := repo.Create(ctx, &conversation.Conversation{UserID: otherUserID, Title: "Must not be returned"}); err != nil {
		t.Fatalf("create other user's conversation: %v", err)
	}

	firstPage, err := repo.ListByUserID(ctx, userID, 2, 0)
	if err != nil {
		t.Fatalf("ListByUserID() first page error = %v", err)
	}
	if len(firstPage) != 2 || firstPage[0].ID != third.ID || firstPage[1].ID != second.ID {
		t.Fatalf("first page IDs = %v, want [%d %d]", conversationIDs(firstPage), third.ID, second.ID)
	}
	secondPage, err := repo.ListByUserID(ctx, userID, 2, 2)
	if err != nil {
		t.Fatalf("ListByUserID() second page error = %v", err)
	}
	if len(secondPage) != 1 || secondPage[0].ID != created.ID {
		t.Fatalf("second page IDs = %v, want [%d]", conversationIDs(secondPage), created.ID)
	}
}

func conversationIDs(conversations []*conversation.Conversation) []uint64 {
	ids := make([]uint64, 0, len(conversations))
	for _, item := range conversations {
		ids = append(ids, item.ID)
	}
	return ids
}
