package platform

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrTxBeginFailed  = errors.New("tx begin failed")
	ErrTxCommitFailed = errors.New("tx commit failed")
)

type TxManager struct {
	pool *pgxpool.Pool
}

func NewTxManager(pool *pgxpool.Pool) *TxManager {
	return &TxManager{
		pool: pool,
	}
}

func (m *TxManager) WithinTx(ctx context.Context, fn func(db DBTX) error) (errs error) {
	tx, err := m.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrTxBeginFailed, err)
	}
	defer func() {
		if err := tx.Rollback(context.WithoutCancel(ctx)); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			errs = errors.Join(errs, err)
		}
	}()

	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("%w: %w", ErrTxCommitFailed, err)
	}

	return nil
}
