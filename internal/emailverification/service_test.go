package emailverification

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeCodeStore struct {
	saveCalls     int
	saveCtx       context.Context
	savedEmail    string
	savedCode     string
	savedTTL      time.Duration
	savedCooldown time.Duration
	saveErr       error
	verifyCalls   int
	verifyCtx     context.Context
	verifiedEmail string
	verifiedCode  string
	verifiedMax   int
	verifyErr     error
	deleteCalls   int
	deleteCtx     context.Context
	deleteCtxErr  error
	deletedEmail  string
	deletedCode   string
	deleteErr     error
}

func (f *fakeCodeStore) Save(ctx context.Context, email, code string, ttl, cooldown time.Duration) error {
	f.saveCalls++
	f.saveCtx = ctx
	f.savedEmail = email
	f.savedCode = code
	f.savedTTL = ttl
	f.savedCooldown = cooldown
	return f.saveErr
}

func (f *fakeCodeStore) VerifyAndConsume(ctx context.Context, email, code string, maxAttempts int) error {
	f.verifyCalls++
	f.verifyCtx = ctx
	f.verifiedEmail = email
	f.verifiedCode = code
	f.verifiedMax = maxAttempts
	return f.verifyErr
}

func (f *fakeCodeStore) Delete(ctx context.Context, email, code string) error {
	f.deleteCalls++
	f.deleteCtx = ctx
	f.deleteCtxErr = ctx.Err()
	f.deletedEmail = email
	f.deletedCode = code
	return f.deleteErr
}

type fakeSender struct {
	calls  int
	ctx    context.Context
	email  string
	code   string
	err    error
	onSend func()
}

func (f *fakeSender) SendVerificationCode(ctx context.Context, email, code string) error {
	f.calls++
	f.ctx = ctx
	f.email = email
	f.code = code
	if f.onSend != nil {
		f.onSend()
	}
	return f.err
}

func newTestService(t *testing.T, store CodeStore, sender Sender) *Service {
	t.Helper()
	service, err := NewService(store, sender, Config{
		CodeTTL:        2 * time.Minute,
		ResendInterval: 30 * time.Second,
		MaxAttempts:    3,
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service
}

func TestNewServiceValidatesDependenciesAndConfig(t *testing.T) {
	store := &fakeCodeStore{}
	sender := &fakeSender{}

	unavailable, err := NewService(nil, nil, Config{})
	if err != nil || unavailable == nil {
		t.Fatalf("NewService(nil, nil) = %#v, %v", unavailable, err)
	}
	if _, err := NewService(nil, sender, DefaultConfig()); !errors.Is(err, ErrInvalidCodeStore) {
		t.Fatalf("missing store error = %v, want %v", err, ErrInvalidCodeStore)
	}
	if _, err := NewService(store, nil, DefaultConfig()); !errors.Is(err, ErrInvalidSender) {
		t.Fatalf("missing sender error = %v, want %v", err, ErrInvalidSender)
	}

	invalidConfigs := []Config{
		{ResendInterval: time.Minute, MaxAttempts: 5},
		{CodeTTL: time.Minute, MaxAttempts: 5},
		{CodeTTL: time.Minute, ResendInterval: time.Minute},
		{CodeTTL: time.Minute, ResendInterval: 2 * time.Minute, MaxAttempts: 5},
	}
	for _, config := range invalidConfigs {
		if _, err := NewService(store, sender, config); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("NewService() config %#v error = %v, want %v", config, err, ErrInvalidConfig)
		}
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()
	if config.CodeTTL != 10*time.Minute || config.ResendInterval != time.Minute || config.MaxAttempts != 5 {
		t.Fatalf("DefaultConfig() = %#v", config)
	}
}

func TestServiceSend(t *testing.T) {
	store := &fakeCodeStore{}
	sender := &fakeSender{}
	service := newTestService(t, store, sender)
	service.generateCode = func() (string, error) { return "042915", nil }
	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("request-id"), "request-1")

	if err := service.Send(ctx, "  User@Example.COM "); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if store.saveCalls != 1 || store.savedEmail != "user@example.com" || store.savedCode != "042915" ||
		store.savedTTL != 2*time.Minute || store.savedCooldown != 30*time.Second {
		t.Fatalf("Save() = %d calls with email %q, code %q, TTL %s, cooldown %s",
			store.saveCalls, store.savedEmail, store.savedCode, store.savedTTL, store.savedCooldown)
	}
	if store.saveCtx != ctx {
		t.Fatal("Send() did not pass its context to Save()")
	}
	if sender.calls != 1 || sender.email != "user@example.com" || sender.code != "042915" || sender.ctx != ctx {
		t.Fatalf("SendVerificationCode() = %d calls with email %q and code %q", sender.calls, sender.email, sender.code)
	}
	if store.deleteCalls != 0 {
		t.Fatalf("Delete() calls = %d, want 0", store.deleteCalls)
	}
}

func TestGenerateNumericCode(t *testing.T) {
	for i := 0; i < 100; i++ {
		code, err := generateNumericCode()
		if err != nil {
			t.Fatalf("generateNumericCode() error = %v", err)
		}
		if !validCode(code) {
			t.Fatalf("generateNumericCode() = %q, want six digits", code)
		}
	}
}

func TestServiceSendRejectsInvalidEmailBeforeDependencies(t *testing.T) {
	tests := []string{"", "not-an-email", "Name <user@example.com>", "user@example.com extra"}
	for _, email := range tests {
		t.Run(email, func(t *testing.T) {
			store := &fakeCodeStore{}
			sender := &fakeSender{}
			service := newTestService(t, store, sender)
			if err := service.Send(context.Background(), email); !errors.Is(err, ErrInvalidEmail) {
				t.Fatalf("Send() error = %v, want %v", err, ErrInvalidEmail)
			}
			if store.saveCalls != 0 || sender.calls != 0 {
				t.Fatalf("dependency calls = store %d, sender %d", store.saveCalls, sender.calls)
			}
		})
	}
}

func TestServiceSendReturnsGenerationAndStoreErrors(t *testing.T) {
	generationErr := errors.New("entropy unavailable")
	service := newTestService(t, &fakeCodeStore{}, &fakeSender{})
	service.generateCode = func() (string, error) { return "", generationErr }
	if err := service.Send(context.Background(), "user@example.com"); !errors.Is(err, ErrGenerateCodeFailed) || !errors.Is(err, generationErr) {
		t.Fatalf("generation error = %v", err)
	}

	service.generateCode = func() (string, error) { return "invalid", nil }
	if err := service.Send(context.Background(), "user@example.com"); !errors.Is(err, ErrGenerateCodeFailed) {
		t.Fatalf("invalid generated code error = %v", err)
	}

	storeErr := errors.New("redis unavailable")
	store := &fakeCodeStore{saveErr: storeErr}
	sender := &fakeSender{}
	service = newTestService(t, store, sender)
	service.generateCode = func() (string, error) { return "123456", nil }
	if err := service.Send(context.Background(), "user@example.com"); !errors.Is(err, ErrStoreFailed) || !errors.Is(err, storeErr) {
		t.Fatalf("store error = %v", err)
	}
	if sender.calls != 0 {
		t.Fatalf("sender calls = %d, want 0", sender.calls)
	}

	store.saveErr = ErrSendTooFrequent
	if err := service.Send(context.Background(), "user@example.com"); !errors.Is(err, ErrSendTooFrequent) || errors.Is(err, ErrStoreFailed) {
		t.Fatalf("cooldown error = %v", err)
	}
}

func TestServiceSendCompensatesSenderFailure(t *testing.T) {
	sendErr := errors.New("SMTP unavailable")
	store := &fakeCodeStore{}
	ctx, cancel := context.WithCancel(context.Background())
	sender := &fakeSender{err: sendErr, onSend: cancel}
	service := newTestService(t, store, sender)
	service.generateCode = func() (string, error) { return "123456", nil }

	err := service.Send(ctx, "user@example.com")
	if !errors.Is(err, ErrSendFailed) || !errors.Is(err, sendErr) {
		t.Fatalf("Send() error = %v", err)
	}
	if store.deleteCalls != 1 || store.deletedEmail != "user@example.com" || store.deletedCode != "123456" {
		t.Fatalf("Delete() = %d calls with email %q and code %q", store.deleteCalls, store.deletedEmail, store.deletedCode)
	}
	if store.deleteCtx == nil || store.deleteCtxErr != nil {
		t.Fatalf("Delete() context = %#v, error during call = %v", store.deleteCtx, store.deleteCtxErr)
	}
}

func TestServiceSendJoinsCompensationError(t *testing.T) {
	sendErr := errors.New("SMTP unavailable")
	deleteErr := errors.New("redis delete unavailable")
	store := &fakeCodeStore{deleteErr: deleteErr}
	service := newTestService(t, store, &fakeSender{err: sendErr})
	service.generateCode = func() (string, error) { return "123456", nil }

	err := service.Send(context.Background(), "user@example.com")
	if !errors.Is(err, ErrSendFailed) || !errors.Is(err, ErrStoreFailed) ||
		!errors.Is(err, sendErr) || !errors.Is(err, deleteErr) {
		t.Fatalf("Send() error = %v", err)
	}
}

func TestServiceVerifyAndConsume(t *testing.T) {
	store := &fakeCodeStore{}
	service := newTestService(t, store, &fakeSender{})
	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("request-id"), "request-1")

	if err := service.VerifyAndConsume(ctx, " User@Example.COM ", " 042915 "); err != nil {
		t.Fatalf("VerifyAndConsume() error = %v", err)
	}
	if store.verifyCalls != 1 || store.verifiedEmail != "user@example.com" ||
		store.verifiedCode != "042915" || store.verifiedMax != 3 || store.verifyCtx != ctx {
		t.Fatalf("VerifyAndConsume() = %d calls with email %q, code %q, max %d",
			store.verifyCalls, store.verifiedEmail, store.verifiedCode, store.verifiedMax)
	}
}

func TestServiceVerifyRejectsInvalidInputBeforeStore(t *testing.T) {
	tests := []struct {
		name    string
		email   string
		code    string
		wantErr error
	}{
		{name: "invalid email", email: "invalid", code: "123456", wantErr: ErrInvalidEmail},
		{name: "empty code", email: "user@example.com", wantErr: ErrInvalidCode},
		{name: "short code", email: "user@example.com", code: "12345", wantErr: ErrInvalidCode},
		{name: "long code", email: "user@example.com", code: "1234567", wantErr: ErrInvalidCode},
		{name: "non-digit code", email: "user@example.com", code: "12a456", wantErr: ErrInvalidCode},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &fakeCodeStore{}
			service := newTestService(t, store, &fakeSender{})
			if err := service.VerifyAndConsume(context.Background(), test.email, test.code); !errors.Is(err, test.wantErr) {
				t.Fatalf("VerifyAndConsume() error = %v, want %v", err, test.wantErr)
			}
			if store.verifyCalls != 0 {
				t.Fatalf("VerifyAndConsume() store calls = %d, want 0", store.verifyCalls)
			}
		})
	}
}

func TestServiceVerifyPreservesBusinessAndWrapsStoreErrors(t *testing.T) {
	for _, wantErr := range []error{ErrCodeInvalidOrExpired, ErrTooManyAttempts} {
		store := &fakeCodeStore{verifyErr: wantErr}
		err := newTestService(t, store, &fakeSender{}).VerifyAndConsume(context.Background(), "user@example.com", "123456")
		if !errors.Is(err, wantErr) || errors.Is(err, ErrStoreFailed) {
			t.Fatalf("VerifyAndConsume() error = %v, want %v", err, wantErr)
		}
	}

	storeErr := errors.New("redis unavailable")
	store := &fakeCodeStore{verifyErr: storeErr}
	err := newTestService(t, store, &fakeSender{}).VerifyAndConsume(context.Background(), "user@example.com", "123456")
	if !errors.Is(err, ErrStoreFailed) || !errors.Is(err, storeErr) {
		t.Fatalf("VerifyAndConsume() error = %v", err)
	}
}

func TestServiceReturnsNotConfigured(t *testing.T) {
	service, err := NewService(nil, nil, Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Send(context.Background(), "user@example.com"); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("Send() error = %v, want %v", err, ErrNotConfigured)
	}
	if err := service.VerifyAndConsume(context.Background(), "user@example.com", "123456"); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("VerifyAndConsume() error = %v, want %v", err, ErrNotConfigured)
	}
}

func TestServicePreservesCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	store := &fakeCodeStore{}
	sender := &fakeSender{}
	service := newTestService(t, store, sender)

	if err := service.Send(ctx, "user@example.com"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Send() error = %v, want context canceled", err)
	}
	if err := service.VerifyAndConsume(ctx, "user@example.com", "123456"); !errors.Is(err, context.Canceled) {
		t.Fatalf("VerifyAndConsume() error = %v, want context canceled", err)
	}
	if store.saveCalls != 0 || store.verifyCalls != 0 || sender.calls != 0 {
		t.Fatalf("dependency calls = save %d, verify %d, send %d", store.saveCalls, store.verifyCalls, sender.calls)
	}
}

func TestNormalizeEmail(t *testing.T) {
	got, err := normalizeEmail("  User@Example.COM ")
	if err != nil || got != "user@example.com" {
		t.Fatalf("normalizeEmail() = %q, %v", got, err)
	}
	longEmail := strings.Repeat("x", 243) + "@example.com"
	if _, err := normalizeEmail(longEmail); !errors.Is(err, ErrInvalidEmail) {
		t.Fatalf("normalizeEmail() error = %v, want %v", err, ErrInvalidEmail)
	}
}
