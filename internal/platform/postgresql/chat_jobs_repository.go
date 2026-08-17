package platform

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"gopherai/internal/chatjob"
	"gopherai/internal/conversation"
)

type ChatJobsRepository struct {
	pool *pgxpool.Pool
}

func NewChatJobsRepository(pool *pgxpool.Pool) *ChatJobsRepository {
	return &ChatJobsRepository{
		pool: pool,
	}
}

var _ chatjob.Repository = (*ChatJobsRepository)(nil)

func (r *ChatJobsRepository) Create(ctx context.Context, userID, conversationID uint64, content string) (*chatjob.Job, error) {
	job := &chatjob.Job{}

	err := r.pool.QueryRow(ctx, InsertPendingChatJob, userID, conversationID, content).
		Scan(&job.ID, &job.ConversationID,
			&job.Content, &job.Status, &job.AssistantMessageID,
			&job.ErrorCode, &job.CreatedAt, &job.StartedAt, &job.FinishedAt)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, conversation.ErrConversationNotFound
	case err != nil:
		return nil, fmt.Errorf("%w: %w", ErrInsertChatJobFailed, err)
	default:
		return job, nil
	}
}

func (r *ChatJobsRepository) GetByID(ctx context.Context, userID, jobID uint64) (*chatjob.Job, error) {
	job := &chatjob.Job{}
	err := r.pool.QueryRow(ctx, GetChatJobByIDAndUserID, userID, jobID).Scan(
		&job.ID,
		&job.ConversationID,
		&job.Content,
		&job.Status,
		&job.AssistantMessageID,
		&job.ErrorCode,
		&job.CreatedAt,
		&job.StartedAt,
		&job.FinishedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, chatjob.ErrJobNotFound
	}

	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrGetChatJobsFailed, err)
	}

	return job, nil
}

func (r *ChatJobsRepository) ClaimForProcessing(ctx context.Context, jobID uint64) (*chatjob.Job, uint64, error) {
	var userID uint64
	job := &chatjob.Job{}
	err := r.pool.QueryRow(ctx, ClaimPendingChatJob, jobID).Scan(
		&job.ID,
		&job.ConversationID,
		&job.Content,
		&job.Status,
		&job.AssistantMessageID,
		&job.ErrorCode,
		&job.CreatedAt,
		&job.StartedAt,
		&job.FinishedAt,
		&userID,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, 0, chatjob.ErrJobNotClaimable
	}

	if err != nil {
		return nil, 0, fmt.Errorf("%w: %w", ErrClaimPendingChatJobFailed, err)
	}

	return job, userID, nil
}

func (r *ChatJobsRepository) Complete(ctx context.Context, jobID, assistantMessageID uint64) error {
	err := r.pool.QueryRow(ctx, CompleteChatJob, jobID, assistantMessageID).Scan(&jobID)

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return chatjob.ErrJobNotCompletable
	case err != nil:
		return fmt.Errorf("%w: %w", ErrCompleteChatJobFailed, err)
	default:
		return nil
	}
}

func (r *ChatJobsRepository) Fail(ctx context.Context, jobID uint64, errorCode string) error {
	err := r.pool.QueryRow(ctx, FailChatJob, jobID, errorCode).Scan(&jobID)

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return chatjob.ErrJobNotProcessing
	case err != nil:
		return fmt.Errorf("%w: %w", ErrFailChatJobFailed, err)
	default:
		return nil
	}
}
