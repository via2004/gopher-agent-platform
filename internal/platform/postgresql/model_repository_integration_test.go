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
	"gopherai/internal/modelcall"
)

func TestModelRepository(t *testing.T) {
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

	ownerID := createModelCallTestUser(t, ctx, pool, "owner")
	otherUserID := createModelCallTestUser(t, ctx, pool, "other")
	t.Cleanup(func() {
		for _, id := range []uint64{ownerID, otherUserID} {
			if _, err := pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", id); err != nil {
				t.Errorf("clean up user %d: %v", id, err)
			}
		}
	})

	conversationRepo := NewConversationRepository(pool)
	ownedConversation := &conversation.Conversation{UserID: ownerID, Title: "Model call conversation"}
	if err := conversationRepo.Create(ctx, ownedConversation); err != nil {
		t.Fatalf("create owned conversation: %v", err)
	}
	otherConversation := &conversation.Conversation{UserID: otherUserID, Title: "Other conversation"}
	if err := conversationRepo.Create(ctx, otherConversation); err != nil {
		t.Fatalf("create other conversation: %v", err)
	}

	messageRepo := NewMessageRepository(pool)
	request := &message.Message{ConversationID: ownedConversation.ID, Role: message.RoleUser, Content: "Explain interfaces"}
	if err := messageRepo.Create(ctx, ownerID, request); err != nil {
		t.Fatalf("create request message: %v", err)
	}
	assistant := &message.Message{ConversationID: ownedConversation.ID, Role: message.RoleAssistant, Content: "An interface is a method set."}
	if err := messageRepo.Create(ctx, ownerID, assistant); err != nil {
		t.Fatalf("create assistant message: %v", err)
	}
	secondAssistant := &message.Message{ConversationID: ownedConversation.ID, Role: message.RoleAssistant, Content: "Second response"}
	if err := messageRepo.Create(ctx, ownerID, secondAssistant); err != nil {
		t.Fatalf("create second assistant message: %v", err)
	}
	otherRequest := &message.Message{ConversationID: otherConversation.ID, Role: message.RoleUser, Content: "Other request"}
	if err := messageRepo.Create(ctx, otherUserID, otherRequest); err != nil {
		t.Fatalf("create other request message: %v", err)
	}

	repo := NewModelRepository(pool)
	requestedModel := "gpt-5.6-sol"
	completedCall := &modelcall.Model{
		ConversationID:   ownedConversation.ID,
		RequestMessageID: request.ID,
		Provider:         "OpenAI",
		RequestedModel:   &requestedModel,
	}
	if err := repo.Create(ctx, ownerID, completedCall); err != nil {
		t.Fatalf("Create() completed call error = %v", err)
	}
	if completedCall.ID == 0 || completedCall.StartedAt.IsZero() {
		t.Fatalf("Create() did not populate generated fields: %#v", completedCall)
	}

	// Multiple attempts for one user message are valid and receive distinct IDs.
	failedCall := &modelcall.Model{
		ConversationID:   ownedConversation.ID,
		RequestMessageID: request.ID,
		Provider:         "OpenAI",
		RequestedModel:   &requestedModel,
	}
	if err := repo.Create(ctx, ownerID, failedCall); err != nil {
		t.Fatalf("Create() second attempt error = %v", err)
	}
	if failedCall.ID == 0 || failedCall.ID == completedCall.ID {
		t.Fatalf("second attempt ID = %d, first attempt ID = %d", failedCall.ID, completedCall.ID)
	}

	created, err := repo.GetModelCall(ctx, completedCall.ID, ownerID)
	if err != nil {
		t.Fatalf("GetModelCall() running call error = %v", err)
	}
	if created.ID != completedCall.ID || created.ConversationID != ownedConversation.ID ||
		created.RequestMessageID != request.ID || created.Provider != "OpenAI" ||
		created.Status != modelcall.StatusRunning || created.FinishedAt != nil {
		t.Fatalf("running model call = %#v", created)
	}
	if _, err := repo.GetModelCall(ctx, completedCall.ID, otherUserID); !errors.Is(err, modelcall.ErrModelCallNotFound) {
		t.Fatalf("other user's GetModelCall() error = %v, want %v", err, modelcall.ErrModelCallNotFound)
	}

	unauthorized := &modelcall.Model{
		ConversationID:   ownedConversation.ID,
		RequestMessageID: request.ID,
		Provider:         "OpenAI",
	}
	if err := repo.Create(ctx, otherUserID, unauthorized); !errors.Is(err, modelcall.ErrModelCallNotFound) {
		t.Fatalf("other user's Create() error = %v, want %v", err, modelcall.ErrModelCallNotFound)
	}
	if unauthorized.ID != 0 || !unauthorized.StartedAt.IsZero() {
		t.Fatalf("other user's Create() populated fields: %#v", unauthorized)
	}

	mismatchedRequest := &modelcall.Model{
		ConversationID:   ownedConversation.ID,
		RequestMessageID: otherRequest.ID,
		Provider:         "OpenAI",
	}
	if err := repo.Create(ctx, ownerID, mismatchedRequest); !errors.Is(err, modelcall.ErrModelCallNotFound) {
		t.Fatalf("Create() with message from another conversation error = %v, want %v", err, modelcall.ErrModelCallNotFound)
	}

	actualModel := "gpt-5.6-sol-2026-08-01"
	providerResponseID := "resp_completed"
	inputTokens := int64(21)
	outputTokens := int64(8)
	totalTokens := int64(29)
	completedCall.AssistantMessageID = &assistant.ID
	completedCall.ActualModel = &actualModel
	completedCall.ProviderResponseID = &providerResponseID
	completedCall.InputTokens = &inputTokens
	completedCall.OutputTokens = &outputTokens
	completedCall.TotalTokens = &totalTokens
	if err := repo.CompleteModelCall(ctx, ownerID, completedCall); err != nil {
		t.Fatalf("CompleteModelCall() error = %v", err)
	}
	if completedCall.Status != modelcall.StatusCompleted || completedCall.FinishedAt == nil ||
		completedCall.AssistantMessageID == nil || *completedCall.AssistantMessageID != assistant.ID {
		t.Fatalf("completed model call = %#v", completedCall)
	}

	errorCode := "provider_error"
	failedCall.Status = modelcall.StatusFailed
	failedCall.ActualModel = &actualModel
	failedCall.ProviderResponseID = nil
	failedCall.InputTokens = &inputTokens
	failedCall.OutputTokens = nil
	failedCall.TotalTokens = nil
	failedCall.ErrorCode = &errorCode
	if err := repo.FinishModelCall(ctx, ownerID, failedCall); err != nil {
		t.Fatalf("FinishModelCall() error = %v", err)
	}
	if failedCall.Status != modelcall.StatusFailed || failedCall.FinishedAt == nil ||
		failedCall.ErrorCode == nil || *failedCall.ErrorCode != errorCode {
		t.Fatalf("failed model call = %#v", failedCall)
	}

	storedCompleted, err := repo.GetModelCall(ctx, completedCall.ID, ownerID)
	if err != nil {
		t.Fatalf("GetModelCall() completed call error = %v", err)
	}
	if storedCompleted.Status != modelcall.StatusCompleted || storedCompleted.FinishedAt == nil ||
		storedCompleted.AssistantMessageID == nil || *storedCompleted.AssistantMessageID != assistant.ID ||
		storedCompleted.TotalTokens == nil || *storedCompleted.TotalTokens != totalTokens {
		t.Fatalf("stored completed call = %#v", storedCompleted)
	}
	storedFailed, err := repo.GetModelCall(ctx, failedCall.ID, ownerID)
	if err != nil {
		t.Fatalf("GetModelCall() failed call error = %v", err)
	}
	if storedFailed.Status != modelcall.StatusFailed || storedFailed.FinishedAt == nil ||
		storedFailed.ErrorCode == nil || *storedFailed.ErrorCode != errorCode {
		t.Fatalf("stored failed call = %#v", storedFailed)
	}

	protectedCall := &modelcall.Model{
		ConversationID:   ownedConversation.ID,
		RequestMessageID: request.ID,
		Provider:         "OpenAI",
	}
	if err := repo.Create(ctx, ownerID, protectedCall); err != nil {
		t.Fatalf("Create() protected call error = %v", err)
	}
	protectedCall.AssistantMessageID = &secondAssistant.ID
	if err := repo.CompleteModelCall(ctx, otherUserID, protectedCall); !errors.Is(err, modelcall.ErrModelCallNotFound) {
		t.Fatalf("other user's CompleteModelCall() error = %v, want %v", err, modelcall.ErrModelCallNotFound)
	}
	protectedCall.Status = modelcall.StatusCancelled
	if err := repo.FinishModelCall(ctx, otherUserID, protectedCall); !errors.Is(err, modelcall.ErrModelCallNotFound) {
		t.Fatalf("other user's FinishModelCall() error = %v, want %v", err, modelcall.ErrModelCallNotFound)
	}
	stillRunning, err := repo.GetModelCall(ctx, protectedCall.ID, ownerID)
	if err != nil {
		t.Fatalf("GetModelCall() protected call error = %v", err)
	}
	if stillRunning.Status != modelcall.StatusRunning || stillRunning.FinishedAt != nil {
		t.Fatalf("protected model call after unauthorized updates = %#v", stillRunning)
	}

	wrongAssistantCall := &modelcall.Model{
		ConversationID:   ownedConversation.ID,
		RequestMessageID: request.ID,
		Provider:         "OpenAI",
	}
	if err := repo.Create(ctx, ownerID, wrongAssistantCall); err != nil {
		t.Fatalf("Create() wrong-assistant call error = %v", err)
	}
	wrongAssistantCall.AssistantMessageID = &request.ID
	if err := repo.CompleteModelCall(ctx, ownerID, wrongAssistantCall); !errors.Is(err, modelcall.ErrModelCallNotFound) {
		t.Fatalf("CompleteModelCall() with user message error = %v, want %v", err, modelcall.ErrModelCallNotFound)
	}
	wrongAssistantCall.Status = modelcall.StatusIncomplete
	if err := repo.FinishModelCall(ctx, ownerID, wrongAssistantCall); err != nil {
		t.Fatalf("FinishModelCall() after rejected completion error = %v", err)
	}

	if err := conversationRepo.DeleteByIDAndUserID(ctx, ownerID, ownedConversation.ID); err != nil {
		t.Fatalf("delete conversation: %v", err)
	}
	var remaining int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM model_calls WHERE conversation_id = $1", ownedConversation.ID).Scan(&remaining); err != nil {
		t.Fatalf("count model calls after conversation delete: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("model calls after conversation delete = %d, want 0", remaining)
	}
}

func createModelCallTestUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool, label string) uint64 {
	t.Helper()

	email := fmt.Sprintf("model-call-repository-%s-%d@example.com", label, time.Now().UnixNano())
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
