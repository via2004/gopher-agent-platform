package health

import (
	"context"
	"fmt"
)

type CheckFunc func(context.Context) error

type Checker struct {
	database CheckFunc
	redis    CheckFunc
}

func NewChecker(database CheckFunc, redis CheckFunc) *Checker {
	return &Checker{
		database: database,
		redis:    redis,
	}
}

func (c *Checker) Check(ctx context.Context) error {
	if err := c.database(ctx); err != nil {
		return fmt.Errorf("postgresql: %w", err)
	}

	if err := c.redis(ctx); err != nil {
		return fmt.Errorf("redis: %w", err)
	}

	return nil
}
