/*
SMTP Server 负责接收并转发邮件
Simple Mail Transfer Protocol
*/
package smtp

import (
	"context"
	"errors"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	gomail "github.com/wneessen/go-mail"
)

const (
	verificationSubject = "GopherAI 邮箱验证码"
	maxEmailBytes       = 254
	maxFromNameRunes    = 100
	verificationCodeLen = 6
)

type Config struct {
	Host     string        // SMTP Port: eg: smtp.qq.com
	Port     int           // SMTP Port: eg: 587
	Username string        // email account for login SMTP Server
	Password string        // the SMTP authenication code, is not necessarily the same as your email login password
	From     string        // the sender's address displayed in the email
	FromName string        // the sender's name displayed in the email
	Timeout  time.Duration // Maximum time for connecting and sending
}

type mailer interface {
	DialAndSendWithContext(context.Context, ...*gomail.Msg) error
}

// Sender 通过要求 STARTTLS 的 SMTP 连接发送纯文本邮箱验证码。
type Sender struct {
	client   mailer
	from     string
	fromName string
	timeout  time.Duration
}

func NewSender(config Config) (*Sender, error) {
	host := strings.TrimSpace(config.Host)
	if host == "" || strings.ContainsAny(host, " \t\r\n/") {
		return nil, ErrHostInvalid
	}
	if config.Port <= 0 || config.Port > 65535 {
		return nil, ErrPortInvalid
	}
	username := strings.TrimSpace(config.Username)
	if username == "" || strings.ContainsAny(username, "\r\n") {
		return nil, ErrUsernameInvalid
	}
	password := config.Password
	if strings.TrimSpace(password) == "" || strings.ContainsAny(password, "\r\n") {
		return nil, ErrPasswordInvalid
	}
	from := strings.ToLower(strings.TrimSpace(config.From))
	if !validAddress(from) {
		return nil, ErrFromInvalid
	}
	fromName := strings.TrimSpace(config.FromName)
	if fromName == "" || utf8.RuneCountInString(fromName) > maxFromNameRunes || strings.ContainsAny(fromName, "\r\n") {
		return nil, ErrFromNameInvalid
	}
	if config.Timeout <= 0 {
		return nil, ErrTimeoutInvalid
	}

	client, err := gomail.NewClient(
		host,                               // connect which SMTP Server
		gomail.WithPort(config.Port),       // use which port
		gomail.WithTimeout(config.Timeout), // set connection timeout
		gomail.WithTLSPortPolicy(gomail.TLSMandatory), // force use STARTTLS
		// automically select the authentication methods supported by the server
		// select the appropriate authentication methods based on the server's declared capabilities
		gomail.WithSMTPAuth(gomail.SMTPAuthAutoDiscover),
		gomail.WithUsername(username), // set username
		gomail.WithPassword(password), // set authentication code
	)
	if err != nil {
		return nil, ErrDeliveryFailed
	}

	return &Sender{
		client:   client,
		from:     from,
		fromName: fromName,
		timeout:  config.Timeout,
	}, nil
}

// SendVerificationCode 构造固定格式邮件，并在独立超时内完成连接和发送。
func (s *Sender) SendVerificationCode(ctx context.Context, recipient, code string) error {
	if s == nil || s.client == nil {
		return ErrDeliveryFailed
	}
	recipient = strings.ToLower(strings.TrimSpace(recipient))
	if !validAddress(recipient) {
		return ErrRecipientInvalid
	}
	code = strings.TrimSpace(code)
	if !validCode(code) {
		return ErrCodeInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	message := gomail.NewMsg()
	if err := message.FromFormat(s.fromName, s.from); err != nil {
		return ErrDeliveryFailed
	}
	if err := message.To(recipient); err != nil {
		return ErrRecipientInvalid
	}
	// set mail subject
	message.Subject(verificationSubject)
	// set mail body
	message.SetBodyString(gomail.TypeTextPlain, verificationBody(code))

	sendCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	if err := s.client.DialAndSendWithContext(sendCtx, message); err != nil {
		if contextErr := sendCtx.Err(); contextErr != nil {
			return contextErr
		}
		if errors.Is(err, context.Canceled) {
			return context.Canceled
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return context.DeadlineExceeded
		}
		return ErrDeliveryFailed
	}
	return nil
}

func verificationBody(code string) string {
	return "你的验证码是：" + code + "\n请在有效期内尽快使用，并且不要将验证码告诉他人。"
}

func validAddress(value string) bool {
	if value == "" || len(value) > maxEmailBytes {
		return false
	}
	address, err := mail.ParseAddress(value)
	return err == nil && address.Address == value
}

func validCode(code string) bool {
	if len(code) != verificationCodeLen {
		return false
	}
	for i := range code {
		if code[i] < '0' || code[i] > '9' {
			return false
		}
	}
	return true
}
