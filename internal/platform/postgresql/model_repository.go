package platform

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"gopherai/internal/modelcall"
)

type DBTX interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type ModelRepository struct {
	pool DBTX
}

func NewModelRepository(pool DBTX) *ModelRepository {
	return &ModelRepository{
		pool: pool,
	}
}

func (r *ModelRepository) Create(ctx context.Context, userID uint64,
	model *modelcall.Model) error {
	err := r.pool.QueryRow(ctx, InsertRunningModelCall,
		model.ConversationID, userID, model.RequestMessageID,
		model.Provider, model.RequestedModel).
		Scan(&model.ID, &model.StartedAt)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return modelcall.ErrModelCallNotFound
	case err != nil:
		return fmt.Errorf("%w: %w", modelcall.ErrModelCallRepository, err)
	}
	return nil
}

// LLM 正常返回，并且 assistant message 已成功保存(需要 assistant message)
func (r *ModelRepository) CompleteModelCall(ctx context.Context,
	userID uint64, model *modelcall.Model) error {

	err := r.pool.QueryRow(ctx, CompleteModelCall,
		model.ID, userID, model.AssistantMessageID, model.ActualModel,
		model.ProviderResponseID, model.InputTokens, model.OutputTokens,
		model.TotalTokens).
		Scan(&model.ID, &model.Status, &model.AssistantMessageID, &model.ActualModel,
			&model.ProviderResponseID, &model.InputTokens, &model.OutputTokens,
			&model.TotalTokens, &model.StartedAt, &model.FinishedAt)

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return modelcall.ErrModelCallNotFound
	case err != nil:
		return fmt.Errorf("%w: %w", modelcall.ErrModelCallRepository, err)
	}
	return nil
}

// LLM 调用失败、客户端断开、超时或返回不完整(不需要 assistant message)
// 失败事务也直接调这个方法
func (r *ModelRepository) FinishModelCall(ctx context.Context,
	userID uint64, model *modelcall.Model) error {

	err := r.pool.QueryRow(ctx, FinishModelCall,
		model.ID, userID, model.Status, model.ActualModel,
		model.ProviderResponseID, model.InputTokens, model.OutputTokens,
		model.TotalTokens, model.ErrorCode).
		Scan(&model.ID, &model.Status, &model.ActualModel, &model.ProviderResponseID,
			&model.InputTokens, &model.OutputTokens, &model.TotalTokens,
			&model.ErrorCode, &model.StartedAt, &model.FinishedAt)

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return modelcall.ErrModelCallNotFound
	case err != nil:
		return fmt.Errorf("%w: %w", modelcall.ErrModelCallRepository, err)
	default:
		return nil
	}
}

func (r *ModelRepository) GetModelCall(ctx context.Context,
	modelID, userID uint64) (model *modelcall.Model, err error) {
	model = &modelcall.Model{}
	errs := r.pool.QueryRow(ctx, GetModelCallByIDAndUserID,
		modelID, userID).Scan(&model.ID, &model.ConversationID,
		&model.RequestMessageID, &model.AssistantMessageID,
		&model.Provider, &model.RequestedModel, &model.ActualModel,
		&model.ProviderResponseID, &model.Status, &model.InputTokens,
		&model.OutputTokens, &model.TotalTokens, &model.ErrorCode,
		&model.StartedAt, &model.FinishedAt)

	switch {
	case errors.Is(errs, pgx.ErrNoRows):
		return nil, modelcall.ErrModelCallNotFound
	case errs != nil:
		return nil, fmt.Errorf("%w: %w", modelcall.ErrModelCallRepository, errs)
	default:
		return model, nil
	}
}
