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

	"gopherai/internal/conversation"
	"gopherai/internal/message"
)

func TestMessageRepository(t *testing.T) {
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

	userID := createMessageTestUser(t, ctx, pool, "owner")
	otherUserID := createMessageTestUser(t, ctx, pool, "other")
	t.Cleanup(func() {
		for _, id := range []uint64{userID, otherUserID} {
			if _, err := pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", id); err != nil {
				t.Errorf("clean up user %d: %v", id, err)
			}
		}
	})

	conversationRepo := NewConversationRepository(pool)
	ownedConversation := &conversation.Conversation{UserID: userID, Title: "Owned conversation"}
	if err := conversationRepo.Create(ctx, ownedConversation); err != nil {
		t.Fatalf("create owned conversation: %v", err)
	}
	emptyConversation := &conversation.Conversation{UserID: userID, Title: "Empty conversation"}
	if err := conversationRepo.Create(ctx, emptyConversation); err != nil {
		t.Fatalf("create empty conversation: %v", err)
	}

	repo := NewMessageRepository(pool)
	first := &message.Message{ConversationID: ownedConversation.ID, Role: message.RoleUser, Content: "hello"}
	if err := repo.Create(ctx, userID, first); err != nil {
		t.Fatalf("Create() first message error = %v", err)
	}
	second := &message.Message{ConversationID: ownedConversation.ID, Role: message.RoleAssistant, Content: "hi"}
	if err := repo.Create(ctx, userID, second); err != nil {
		t.Fatalf("Create() second message error = %v", err)
	}
	if first.ID == 0 || first.CreatedAt.IsZero() || second.ID == 0 || second.CreatedAt.IsZero() {
		t.Fatalf("Create() did not populate generated fields: first = %#v, second = %#v", first, second)
	}

	unauthorized := &message.Message{ConversationID: ownedConversation.ID, Role: message.RoleUser, Content: "must not be inserted"}
	if err := repo.Create(ctx, otherUserID, unauthorized); !errors.Is(err, conversation.ErrConversationNotFound) {
		t.Fatalf("other user's Create() error = %v, want %v", err, conversation.ErrConversationNotFound)
	}
	if unauthorized.ID != 0 || !unauthorized.CreatedAt.IsZero() {
		t.Fatalf("other user's Create() populated fields: %#v", unauthorized)
	}

	firstPage, err := repo.ListByConversationID(ctx, userID, ownedConversation.ID, 1, 0)
	if err != nil {
		t.Fatalf("ListByConversationID() first page error = %v", err)
	}
	if len(firstPage) != 1 || firstPage[0].ID != first.ID || firstPage[0].Role != message.RoleUser {
		t.Fatalf("first page = %#v, want message %d", firstPage, first.ID)
	}
	secondPage, err := repo.ListByConversationID(ctx, userID, ownedConversation.ID, 1, 1)
	if err != nil {
		t.Fatalf("ListByConversationID() second page error = %v", err)
	}
	if len(secondPage) != 1 || secondPage[0].ID != second.ID || secondPage[0].Role != message.RoleAssistant {
		t.Fatalf("second page = %#v, want message %d", secondPage, second.ID)
	}
	recent, err := repo.ListRecentByConversationID(ctx, userID, ownedConversation.ID, 1)
	if err != nil {
		t.Fatalf("ListRecentByConversationID() one-message error = %v", err)
	}
	if len(recent) != 1 || recent[0].ID != second.ID {
		t.Fatalf("recent messages = %#v, want latest message %d", recent, second.ID)
	}
	recent, err = repo.ListRecentByConversationID(ctx, userID, ownedConversation.ID, 2)
	if err != nil {
		t.Fatalf("ListRecentByConversationID() two-message error = %v", err)
	}
	if len(recent) != 2 || recent[0].ID != first.ID || recent[1].ID != second.ID {
		t.Fatalf("recent messages order = %v, want [%d %d]", messageIDs(recent), first.ID, second.ID)
	}

	empty, err := repo.ListByConversationID(ctx, userID, emptyConversation.ID, 20, 0)
	if err != nil {
		t.Fatalf("ListByConversationID() empty conversation error = %v", err)
	}
	if empty == nil || len(empty) != 0 {
		t.Fatalf("empty conversation messages = %#v, want non-nil empty slice", empty)
	}
	if _, err := repo.ListByConversationID(ctx, otherUserID, ownedConversation.ID, 20, 0); !errors.Is(err, conversation.ErrConversationNotFound) {
		t.Fatalf("other user's ListByConversationID() error = %v, want %v", err, conversation.ErrConversationNotFound)
	}

	if err := conversationRepo.DeleteByIDAndUserID(ctx, userID, ownedConversation.ID); err != nil {
		t.Fatalf("delete conversation: %v", err)
	}
	var remaining int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM messages WHERE conversation_id = $1", ownedConversation.ID).Scan(&remaining); err != nil {
		t.Fatalf("count messages after conversation delete: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("messages after conversation delete = %d, want 0", remaining)
	}
}

func createMessageTestUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool, label string) uint64 {
	t.Helper()

	email := fmt.Sprintf("message-repository-%s-%d@example.com", label, time.Now().UnixNano())
	var userID uint64
	if err := pool.QueryRow(ctx,
		"INSERT INTO users (email, password_hash) VALUES ($1, $2) RETURNING id",
		email,
		"test-password-hash",
	).Scan(&userID); err != nil {
		t.Fatalf("create %s test user: %v", label, err)
	}
	return userID
}

func messageIDs(messages []*message.Message) []uint64 {
	ids := make([]uint64, 0, len(messages))
	for _, item := range messages {
		ids = append(ids, item.ID)
	}
	return ids
}
