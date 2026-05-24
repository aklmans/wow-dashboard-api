package email

import (
	"context"
	"errors"
	"fmt"

	"github.com/wneessen/go-mail"
)

// SMTPSenderConfig describes how to reach an SMTP relay. Empty Host yields
// the LogSender fallback (see New), so a developer with no real provider can
// still exercise the email flows.
type SMTPSenderConfig struct {
	Host        string
	Port        int
	Username    string
	Password    string
	TLSMode     string // one of: none, starttls (default), tls
	FromAddress string
	FromName    string
}

// SMTPSender sends each Message via a real SMTP relay. It opens a fresh
// connection per send — adequate for the low volume an admin app produces.
type SMTPSender struct {
	cfg SMTPSenderConfig
}

// New picks a transport: LogSender when no SMTP host is configured (handy
// for local dev and tests), SMTPSender otherwise.
func New(cfg SMTPSenderConfig) (Sender, error) {
	if cfg.Host == "" {
		return LogSender{}, nil
	}
	return NewSMTPSender(cfg)
}

// NewSMTPSender validates the config and returns a sender that talks SMTP.
func NewSMTPSender(cfg SMTPSenderConfig) (*SMTPSender, error) {
	if cfg.Host == "" {
		return nil, errors.New("email: smtp host is required")
	}
	if cfg.FromAddress == "" {
		return nil, errors.New("email: from address is required")
	}
	if cfg.Port == 0 {
		cfg.Port = defaultPort(cfg.TLSMode)
	}
	if cfg.TLSMode == "" {
		cfg.TLSMode = "starttls"
	}
	switch cfg.TLSMode {
	case "none", "starttls", "tls":
	default:
		return nil, fmt.Errorf("email: invalid TLS mode %q (want none|starttls|tls)", cfg.TLSMode)
	}
	return &SMTPSender{cfg: cfg}, nil
}

func defaultPort(tlsMode string) int {
	switch tlsMode {
	case "none":
		return 25
	case "tls":
		return 465
	default:
		return 587
	}
}

// Send opens a fresh SMTP connection and delivers msg. Authentication uses
// SMTP AUTH PLAIN when Username is set.
func (s *SMTPSender) Send(ctx context.Context, msg Message) error {
	m := mail.NewMsg()
	if s.cfg.FromName != "" {
		if err := m.FromFormat(s.cfg.FromName, s.cfg.FromAddress); err != nil {
			return fmt.Errorf("email: build From: %w", err)
		}
	} else if err := m.From(s.cfg.FromAddress); err != nil {
		return fmt.Errorf("email: build From: %w", err)
	}
	if err := m.To(msg.To); err != nil {
		return fmt.Errorf("email: build To: %w", err)
	}
	m.Subject(msg.Subject)
	m.SetBodyString(mail.TypeTextPlain, msg.Body)

	opts := []mail.Option{mail.WithPort(s.cfg.Port)}
	switch s.cfg.TLSMode {
	case "none":
		opts = append(opts, mail.WithTLSPolicy(mail.NoTLS))
	case "tls":
		opts = append(opts, mail.WithSSL())
	default: // starttls
		opts = append(opts, mail.WithTLSPolicy(mail.TLSMandatory))
	}
	if s.cfg.Username != "" {
		opts = append(opts,
			mail.WithSMTPAuth(mail.SMTPAuthPlain),
			mail.WithUsername(s.cfg.Username),
			mail.WithPassword(s.cfg.Password),
		)
	}

	client, err := mail.NewClient(s.cfg.Host, opts...)
	if err != nil {
		return fmt.Errorf("email: build smtp client: %w", err)
	}
	if err := client.DialAndSendWithContext(ctx, m); err != nil {
		return fmt.Errorf("email: smtp send to %s: %w", msg.To, err)
	}
	return nil
}
