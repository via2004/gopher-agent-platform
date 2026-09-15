package smtp

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"strings"
	"testing"
	"time"

	gomail "github.com/wneessen/go-mail"
)

type fakeMailer struct {
	calls    int
	ctx      context.Context
	messages []*gomail.Msg
	err      error
	onSend   func(context.Context)
}

func (f *fakeMailer) DialAndSendWithContext(ctx context.Context, messages ...*gomail.Msg) error {
	f.calls++
	f.ctx = ctx
	f.messages = messages
	if f.onSend != nil {
		f.onSend(ctx)
	}
	if f.err != nil {
		return f.err
	}
	return ctx.Err()
}

func validConfig() Config {
	return Config{
		Host:     "smtp.example.com",
		Port:     587,
		Username: "sender@example.com",
		Password: "authorization-code",
		From:     "sender@example.com",
		FromName: "GopherAI",
		Timeout:  time.Second,
	}
}

func TestNewSenderValidatesConfiguration(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr error
	}{
		{name: "empty host", mutate: func(c *Config) { c.Host = " " }, wantErr: ErrHostInvalid},
		{name: "host with path", mutate: func(c *Config) { c.Host = "smtp.example.com/path" }, wantErr: ErrHostInvalid},
		{name: "zero port", mutate: func(c *Config) { c.Port = 0 }, wantErr: ErrPortInvalid},
		{name: "large port", mutate: func(c *Config) { c.Port = 65536 }, wantErr: ErrPortInvalid},
		{name: "empty username", mutate: func(c *Config) { c.Username = " " }, wantErr: ErrUsernameInvalid},
		{name: "username newline", mutate: func(c *Config) { c.Username = "sender\nother" }, wantErr: ErrUsernameInvalid},
		{name: "empty password", mutate: func(c *Config) { c.Password = " " }, wantErr: ErrPasswordInvalid},
		{name: "password newline", mutate: func(c *Config) { c.Password = "secret\nvalue" }, wantErr: ErrPasswordInvalid},
		{name: "invalid from", mutate: func(c *Config) { c.From = "not-an-email" }, wantErr: ErrFromInvalid},
		{name: "display from", mutate: func(c *Config) { c.From = "Name <sender@example.com>" }, wantErr: ErrFromInvalid},
		{name: "empty from name", mutate: func(c *Config) { c.FromName = " " }, wantErr: ErrFromNameInvalid},
		{name: "long from name", mutate: func(c *Config) { c.FromName = strings.Repeat("界", maxFromNameRunes+1) }, wantErr: ErrFromNameInvalid},
		{name: "from name newline", mutate: func(c *Config) { c.FromName = "GopherAI\nBcc" }, wantErr: ErrFromNameInvalid},
		{name: "zero timeout", mutate: func(c *Config) { c.Timeout = 0 }, wantErr: ErrTimeoutInvalid},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := validConfig()
			test.mutate(&config)
			sender, err := NewSender(config)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("NewSender() error = %v, want %v", err, test.wantErr)
			}
			if sender != nil {
				t.Fatalf("NewSender() sender = %#v, want nil", sender)
			}
		})
	}
}

func TestNewSenderConfiguresMandatorySTARTTLS(t *testing.T) {
	config := validConfig()
	config.Host = " smtp.example.com "
	config.Username = " sender@example.com "
	config.From = " SENDER@EXAMPLE.COM "
	config.FromName = " GopherAI "
	sender, err := NewSender(config)
	if err != nil {
		t.Fatalf("NewSender() error = %v", err)
	}
	client, ok := sender.client.(*gomail.Client)
	if !ok {
		t.Fatalf("client = %T, want *mail.Client", sender.client)
	}
	if client.TLSPolicy() != gomail.TLSMandatory.String() {
		t.Fatalf("TLS policy = %q, want %q", client.TLSPolicy(), gomail.TLSMandatory.String())
	}
	if client.ServerAddr() != "smtp.example.com:587" {
		t.Fatalf("server address = %q", client.ServerAddr())
	}
	if sender.from != "sender@example.com" || sender.fromName != "GopherAI" || sender.timeout != time.Second {
		t.Fatalf("sender = %#v", sender)
	}
}

func TestSenderBuildsVerificationEmail(t *testing.T) {
	client := &fakeMailer{}
	sender := &Sender{
		client:   client,
		from:     "sender@example.com",
		fromName: "GopherAI",
		timeout:  time.Second,
	}
	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("request-id"), "request-1")

	if err := sender.SendVerificationCode(ctx, " User@Example.COM ", " 042915 "); err != nil {
		t.Fatalf("SendVerificationCode() error = %v", err)
	}
	if client.calls != 1 || len(client.messages) != 1 {
		t.Fatalf("DialAndSendWithContext() = %d calls with %d messages", client.calls, len(client.messages))
	}
	if client.ctx.Value(contextKey("request-id")) != "request-1" {
		t.Fatal("SendVerificationCode() did not preserve context values")
	}
	message := client.messages[0]
	from := message.GetFrom()
	to := message.GetTo()
	if len(from) != 1 || from[0].Address != "sender@example.com" || from[0].Name != "GopherAI" {
		t.Fatalf("From = %#v", from)
	}
	if len(to) != 1 || to[0].Address != "user@example.com" {
		t.Fatalf("To = %#v", to)
	}
	subjects := message.GetGenHeader(gomail.HeaderSubject)
	if len(subjects) != 1 {
		t.Fatalf("Subject = %#v", subjects)
	}
	subject, err := new(mime.WordDecoder).DecodeHeader(subjects[0])
	if err != nil || subject != verificationSubject {
		t.Fatalf("decoded Subject = %q, %v; raw = %#v", subject, err, subjects)
	}
	parts := message.GetParts()
	if len(parts) != 1 || parts[0].GetContentType() != gomail.TypeTextPlain {
		t.Fatalf("message parts = %#v", parts)
	}
	body, err := parts[0].GetContent()
	if err != nil {
		t.Fatalf("GetContent() error = %v", err)
	}
	if string(body) != verificationBody("042915") || !strings.Contains(string(body), "042915") {
		t.Fatalf("body = %q", body)
	}
}

func TestSenderRejectsInvalidInputBeforeMailer(t *testing.T) {
	tests := []struct {
		name      string
		recipient string
		code      string
		wantErr   error
	}{
		{name: "empty recipient", code: "123456", wantErr: ErrRecipientInvalid},
		{name: "invalid recipient", recipient: "invalid", code: "123456", wantErr: ErrRecipientInvalid},
		{name: "display recipient", recipient: "Name <user@example.com>", code: "123456", wantErr: ErrRecipientInvalid},
		{name: "short code", recipient: "user@example.com", code: "12345", wantErr: ErrCodeInvalid},
		{name: "non-digit code", recipient: "user@example.com", code: "12345x", wantErr: ErrCodeInvalid},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := &fakeMailer{}
			sender := &Sender{client: client, from: "sender@example.com", fromName: "GopherAI", timeout: time.Second}
			err := sender.SendVerificationCode(context.Background(), test.recipient, test.code)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("SendVerificationCode() error = %v, want %v", err, test.wantErr)
			}
			if client.calls != 0 {
				t.Fatalf("mailer calls = %d, want 0", client.calls)
			}
		})
	}
}

func TestSenderPreservesCancellationAndEnforcesTimeout(t *testing.T) {
	t.Run("cancelled before send", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		client := &fakeMailer{}
		sender := &Sender{client: client, from: "sender@example.com", fromName: "GopherAI", timeout: time.Second}
		err := sender.SendVerificationCode(ctx, "user@example.com", "123456")
		if !errors.Is(err, context.Canceled) || client.calls != 0 {
			t.Fatalf("SendVerificationCode() = %v, calls %d", err, client.calls)
		}
	})

	t.Run("send timeout", func(t *testing.T) {
		client := &fakeMailer{onSend: func(ctx context.Context) { <-ctx.Done() }}
		sender := &Sender{client: client, from: "sender@example.com", fromName: "GopherAI", timeout: 10 * time.Millisecond}
		err := sender.SendVerificationCode(context.Background(), "user@example.com", "123456")
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("SendVerificationCode() error = %v, want deadline exceeded", err)
		}
	})
}

func TestSenderSanitizesDeliveryErrors(t *testing.T) {
	const password = "sensitive-password"
	const code = "123456"
	client := &fakeMailer{err: fmt.Errorf("provider exposed password %s and code %s", password, code)}
	sender := &Sender{client: client, from: "sender@example.com", fromName: "GopherAI", timeout: time.Second}

	err := sender.SendVerificationCode(context.Background(), "user@example.com", code)
	if !errors.Is(err, ErrDeliveryFailed) {
		t.Fatalf("SendVerificationCode() error = %v, want %v", err, ErrDeliveryFailed)
	}
	if strings.Contains(err.Error(), password) || strings.Contains(err.Error(), code) {
		t.Fatalf("SendVerificationCode() error exposes sensitive data: %v", err)
	}
}

func TestNilSenderReturnsDeliveryFailure(t *testing.T) {
	var sender *Sender
	if err := sender.SendVerificationCode(context.Background(), "user@example.com", "123456"); !errors.Is(err, ErrDeliveryFailed) {
		t.Fatalf("SendVerificationCode() error = %v, want %v", err, ErrDeliveryFailed)
	}
}
