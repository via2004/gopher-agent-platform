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

	"gopherai/internal/chatjob"
	"gopherai/internal/conversation"
	"gopherai/internal/message"
	"gopherai/internal/modelcall"
)

func TestChatJobsRepository(t *testing.T) {
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

	ownerID := createChatJobTestUser(t, ctx, pool, "owner")
	otherUserID := createChatJobTestUser(t, ctx, pool, "other")
	t.Cleanup(func() {
		for _, id := range []uint64{ownerID, otherUserID} {
			if _, err := pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", id); err != nil {
				t.Errorf("clean up user %d: %v", id, err)
			}
		}
	})

	conversationRepo := NewConversationRepository(pool)
	ownedConversation := &conversation.Conversation{UserID: ownerID, Title: "Async chat"}
	if err := conversationRepo.Create(ctx, ownedConversation); err != nil {
		t.Fatalf("create owned conversation: %v", err)
	}

	repo := NewChatJobsRepository(pool)
	completedJob, err := repo.Create(ctx, ownerID, ownedConversation.ID, "explain interfaces")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	assertPendingChatJob(t, completedJob, ownedConversation.ID, "explain interfaces")

	if _, err := repo.Create(ctx, otherUserID, ownedConversation.ID, "must not be created"); !errors.Is(err, conversation.ErrConversationNotFound) {
		t.Fatalf("other user's Create() error = %v, want %v", err, conversation.ErrConversationNotFound)
	}

	found, err := repo.GetByID(ctx, ownerID, completedJob.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if found.ID != completedJob.ID || found.Status != chatjob.StatusPending {
		t.Fatalf("GetByID() job = %#v, want pending job %d", found, completedJob.ID)
	}
	if _, err := repo.GetByID(ctx, otherUserID, completedJob.ID); !errors.Is(err, chatjob.ErrJobNotFound) {
		t.Fatalf("other user's GetByID() error = %v, want %v", err, chatjob.ErrJobNotFound)
	}

	if err := repo.Complete(ctx, completedJob.ID, 1); !errors.Is(err, chatjob.ErrJobNotCompletable) {
		t.Fatalf("Complete() pending job error = %v, want %v", err, chatjob.ErrJobNotCompletable)
	}
	if err := repo.Fail(ctx, completedJob.ID, "premature_failure"); !errors.Is(err, chatjob.ErrJobNotProcessing) {
		t.Fatalf("Fail() pending job error = %v, want %v", err, chatjob.ErrJobNotProcessing)
	}

	claimed, claimedUserID, err := repo.ClaimForProcessing(ctx, completedJob.ID)
	if err != nil {
		t.Fatalf("ClaimForProcessing() error = %v", err)
	}
	if claimedUserID != ownerID || claimed.Status != chatjob.StatusProcessing ||
		claimed.AttemptCount != 1 || claimed.StartedAt == nil {
		t.Fatalf("ClaimForProcessing() = (%#v, user %d), want processing job for user %d", claimed, claimedUserID, ownerID)
	}
	if _, _, err := repo.ClaimForProcessing(ctx, completedJob.ID); !errors.Is(err, chatjob.ErrJobNotClaimable) {
		t.Fatalf("repeated ClaimForProcessing() error = %v, want %v", err, chatjob.ErrJobNotClaimable)
	}

	requestMessageID, err := repo.EnsureRequestMessage(ctx, ownerID, completedJob.ID)
	if err != nil || requestMessageID == 0 {
		t.Fatalf("EnsureRequestMessage() = (%d, %v), want a message ID", requestMessageID, err)
	}
	requestMessageIDAgain, err := repo.EnsureRequestMessage(ctx, ownerID, completedJob.ID)
	if err != nil || requestMessageIDAgain != requestMessageID {
		t.Fatalf("repeated EnsureRequestMessage() = (%d, %v), want (%d, nil)",
			requestMessageIDAgain, err, requestMessageID)
	}
	if assistantMessageID, ok, err := repo.FindCompletedAssistantID(ctx, ownerID, completedJob.ID); err != nil || ok || assistantMessageID != 0 {
		t.Fatalf("FindCompletedAssistantID() before model completion = (%d, %t, %v)",
			assistantMessageID, ok, err)
	}

	if err := repo.Retry(ctx, completedJob.ID); err != nil {
		t.Fatalf("Retry() error = %v", err)
	}
	retried, err := repo.GetByID(ctx, ownerID, completedJob.ID)
	if err != nil || retried.Status != chatjob.StatusPending || retried.AttemptCount != 1 || retried.StartedAt != nil {
		t.Fatalf("retried job = %#v, error = %v", retried, err)
	}
	claimed, claimedUserID, err = repo.ClaimForProcessing(ctx, completedJob.ID)
	if err != nil || claimedUserID != ownerID || claimed.AttemptCount != 2 {
		t.Fatalf("second ClaimForProcessing() = (%#v, user %d, %v)", claimed, claimedUserID, err)
	}
	if got, err := repo.EnsureRequestMessage(ctx, ownerID, completedJob.ID); err != nil || got != requestMessageID {
		t.Fatalf("EnsureRequestMessage() after retry = (%d, %v), want (%d, nil)", got, err, requestMessageID)
	}

	messageRepo := NewMessageRepository(pool)
	assistant := &message.Message{
		ConversationID: ownedConversation.ID,
		Role:           message.RoleAssistant,
		Content:        "An interface describes behavior.",
	}
	if err := messageRepo.Create(ctx, ownerID, assistant); err != nil {
		t.Fatalf("create assistant message: %v", err)
	}
	requestedModel := "gpt-test-requested"
	actualModel := "gpt-test-actual"
	modelCall := &modelcall.Model{
		ConversationID:   ownedConversation.ID,
		RequestMessageID: requestMessageID,
		Provider:         "test",
		RequestedModel:   &requestedModel,
	}
	modelRepo := NewModelRepository(pool)
	if err := modelRepo.Create(ctx, ownerID, modelCall); err != nil {
		t.Fatalf("create model call: %v", err)
	}
	modelCall.AssistantMessageID = &assistant.ID
	modelCall.ActualModel = &actualModel
	if err := modelRepo.CompleteModelCall(ctx, ownerID, modelCall); err != nil {
		t.Fatalf("complete model call: %v", err)
	}
	foundAssistantID, ok, err := repo.FindCompletedAssistantID(ctx, ownerID, completedJob.ID)
	if err != nil || !ok || foundAssistantID != assistant.ID {
		t.Fatalf("FindCompletedAssistantID() = (%d, %t, %v), want (%d, true, nil)",
			foundAssistantID, ok, err, assistant.ID)
	}
	if err := repo.Complete(ctx, completedJob.ID, assistant.ID); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	completed, err := repo.GetByID(ctx, ownerID, completedJob.ID)
	if err != nil {
		t.Fatalf("GetByID() completed job error = %v", err)
	}
	if completed.Status != chatjob.StatusCompleted || completed.AssistantMessageID == nil ||
		*completed.AssistantMessageID != assistant.ID || completed.ErrorCode != nil || completed.FinishedAt == nil {
		t.Fatalf("completed job = %#v", completed)
	}

	failedJob, err := repo.Create(ctx, ownerID, ownedConversation.ID, "fail this job")
	if err != nil {
		t.Fatalf("Create() failed-job fixture error = %v", err)
	}
	if _, _, err := repo.ClaimForProcessing(ctx, failedJob.ID); err != nil {
		t.Fatalf("ClaimForProcessing() failed-job fixture error = %v", err)
	}
	if err := repo.Fail(ctx, failedJob.ID, "chat_failed"); err != nil {
		t.Fatalf("Fail() error = %v", err)
	}

	failed, err := repo.GetByID(ctx, ownerID, failedJob.ID)
	if err != nil {
		t.Fatalf("GetByID() failed job error = %v", err)
	}
	if failed.Status != chatjob.StatusFailed || failed.ErrorCode == nil ||
		*failed.ErrorCode != "chat_failed" || failed.AssistantMessageID != nil || failed.FinishedAt == nil {
		t.Fatalf("failed job = %#v", failed)
	}

	if err := conversationRepo.DeleteByIDAndUserID(ctx, ownerID, ownedConversation.ID); err != nil {
		t.Fatalf("delete conversation: %v", err)
	}
	var remaining int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM chat_jobs WHERE conversation_id = $1", ownedConversation.ID).Scan(&remaining); err != nil {
		t.Fatalf("count jobs after conversation delete: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("jobs after conversation delete = %d, want 0", remaining)
	}
}

func createChatJobTestUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool, label string) uint64 {
	t.Helper()

	email := fmt.Sprintf("chat-job-repository-%s-%d@example.com", label, time.Now().UnixNano())
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

func assertPendingChatJob(t *testing.T, job *chatjob.Job, conversationID uint64, content string) {
	t.Helper()

	if job.ID == 0 || job.ConversationID != conversationID || job.Content != content ||
		job.Status != chatjob.StatusPending || job.AttemptCount != 0 || job.AssistantMessageID != nil || job.ErrorCode != nil ||
		job.CreatedAt.IsZero() || job.StartedAt != nil || job.FinishedAt != nil {
		t.Fatalf("pending job = %#v", job)
	}
}
