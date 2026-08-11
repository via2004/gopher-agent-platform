package modelcall

import "context"

type Repository interface {
	Create(
		ctx context.Context,
		userID uint64,
		modelCall *Model,
	) error

	CompleteModelCall(
		ctx context.Context,
		userID uint64,
		modelCall *Model,
	) error

	FinishModelCall(
		ctx context.Context,
		userID uint64,
		modelCall *Model,
	) error

	GetModelCall(
		ctx context.Context,
		modelCallID uint64,
		userID uint64,
	) (*Model, error)
}
