package email_test

import (
	"testing"

	"github.com/aklmans/wow-dashboard-api/internal/email"
)

func TestNew_LogSenderWhenHostMissing(t *testing.T) {
	sender, err := email.New(email.SMTPSenderConfig{})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	if _, ok := sender.(email.LogSender); !ok {
		t.Fatalf("expected LogSender fallback, got %T", sender)
	}
}

func TestNewSMTPSender_Validation(t *testing.T) {
	cases := []struct {
		name    string
		cfg     email.SMTPSenderConfig
		wantErr bool
	}{
		{
			name:    "missing host",
			cfg:     email.SMTPSenderConfig{FromAddress: "ops@wow-dashboard.test"},
			wantErr: true,
		},
		{
			name:    "missing from",
			cfg:     email.SMTPSenderConfig{Host: "smtp.example.com"},
			wantErr: true,
		},
		{
			name: "invalid TLS mode",
			cfg: email.SMTPSenderConfig{
				Host:        "smtp.example.com",
				FromAddress: "ops@wow-dashboard.test",
				TLSMode:     "bogus",
			},
			wantErr: true,
		},
		{
			name: "valid starttls config",
			cfg: email.SMTPSenderConfig{
				Host:        "smtp.example.com",
				FromAddress: "ops@wow-dashboard.test",
				TLSMode:     "starttls",
			},
			wantErr: false,
		},
		{
			name: "valid defaults",
			cfg: email.SMTPSenderConfig{
				Host:        "smtp.example.com",
				FromAddress: "ops@wow-dashboard.test",
			},
			wantErr: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := email.NewSMTPSender(tc.cfg)
			if (err != nil) != tc.wantErr {
				t.Fatalf("NewSMTPSender err = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}
