package alert

import (
	"bytes"
	"io"
	"mime"
	"net/mail"
	"strings"
	"testing"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/config"
)

func TestNewSMTPNotifierValidatesHeaderAddresses(t *testing.T) {
	base := config.AlertConfig{
		SMTPHost:      "smtp.example.test",
		SMTPPort:      587,
		SMTPFromEmail: "alerts@example.test",
		SMTPFromName:  "JinkoBridge",
		SMTPToEmails:  []string{"operator@example.test"},
	}
	tests := []struct {
		name   string
		mutate func(*config.AlertConfig)
	}{
		{
			name: "sender header injection",
			mutate: func(cfg *config.AlertConfig) {
				cfg.SMTPFromEmail = "alerts@example.test\r\nBcc: victim@example.test"
			},
		},
		{
			name: "recipient header injection",
			mutate: func(cfg *config.AlertConfig) {
				cfg.SMTPToEmails = []string{"operator@example.test\nCc: victim@example.test"}
			},
		},
		{
			name: "sender display name injection",
			mutate: func(cfg *config.AlertConfig) {
				cfg.SMTPFromName = "JinkoBridge\r\nBcc: victim@example.test"
			},
		},
		{
			name: "sender display name control character",
			mutate: func(cfg *config.AlertConfig) {
				cfg.SMTPFromName = "JinkoBridge\x00hidden"
			},
		},
		{
			name: "multiple recipients in one value",
			mutate: func(cfg *config.AlertConfig) {
				cfg.SMTPToEmails = []string{"first@example.test, second@example.test"}
			},
		},
		{
			name: "display name in envelope address",
			mutate: func(cfg *config.AlertConfig) {
				cfg.SMTPToEmails = []string{"Operator <operator@example.test>"}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := base
			cfg.SMTPToEmails = append([]string(nil), base.SMTPToEmails...)
			tt.mutate(&cfg)
			if _, err := NewSMTPNotifier(cfg); err == nil {
				t.Fatal("NewSMTPNotifier() error = nil, want invalid-header rejection")
			}
		})
	}
}

func TestSMTPMessageEncodesUnicodeAndPreventsSubjectInjection(t *testing.T) {
	notifier, err := NewSMTPNotifier(config.AlertConfig{
		SMTPHost:      "smtp.example.test",
		SMTPPort:      587,
		SMTPFromEmail: "alerts@example.test",
		SMTPFromName:  "Сонячний міст",
		SMTPToEmails:  []string{"operator@example.test"},
	})
	if err != nil {
		t.Fatalf("NewSMTPNotifier() error = %v", err)
	}

	raw := notifier.buildMessage("Помилка\r\nBcc: victim@example.test", "plain body")
	if bytes.Contains(raw, []byte("\r\nBcc:")) {
		t.Fatalf("message contains injected Bcc header:\n%s", raw)
	}
	if !bytes.Contains(raw, []byte("Subject: =?UTF-8?")) {
		t.Fatalf("subject is not RFC 2047 encoded:\n%s", raw)
	}

	message, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("mail.ReadMessage() error = %v", err)
	}
	decodedSubject, err := new(mime.WordDecoder).DecodeHeader(message.Header.Get("Subject"))
	if err != nil {
		t.Fatalf("DecodeHeader(Subject) error = %v", err)
	}
	if decodedSubject != "Помилка  Bcc: victim@example.test" {
		t.Fatalf("decoded Subject = %q", decodedSubject)
	}
	if got := message.Header.Get("Bcc"); got != "" {
		t.Fatalf("injected Bcc header = %q", got)
	}
	body, err := io.ReadAll(message.Body)
	if err != nil {
		t.Fatalf("ReadAll(message body) error = %v", err)
	}
	if strings.TrimSpace(string(body)) != "plain body" {
		t.Fatalf("message body = %q", body)
	}
}

func TestNewSMTPNotifierFormatsIPv6Endpoint(t *testing.T) {
	notifier, err := NewSMTPNotifier(config.AlertConfig{
		SMTPHost:      "[2001:db8::1]",
		SMTPPort:      587,
		SMTPFromEmail: "alerts@example.test",
		SMTPToEmails:  []string{"operator@example.test"},
	})
	if err != nil {
		t.Fatalf("NewSMTPNotifier() error = %v", err)
	}
	if notifier.addr != "[2001:db8::1]:587" {
		t.Fatalf("SMTP endpoint = %q, want bracketed IPv6 endpoint", notifier.addr)
	}
	if notifier.cfg.SMTPHost != "2001:db8::1" {
		t.Fatalf("normalized SMTP host = %q, want unbracketed IPv6 host", notifier.cfg.SMTPHost)
	}
}
