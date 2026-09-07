package emailverification

import (
	"context"
	"time"
)

type CodeStore interface {
	Save(ctx context.Context, email, code string, ttl, cooldown time.Duration) error
	VerifyAndConsume(ctx context.Context, email, code string, maxAttempts int) error
	Delete(ctx context.Context, email, code string) error
}

type Sender interface {
	SendVerificationCode(ctx context.Context, email, code string) error
}
