package alert

import (
	"context"
	"crypto/tls"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/config"
	"github.com/rs/zerolog/log"
)

type SMTPNotifier struct {
	cfg      config.AlertConfig
	addr     string
	from     mail.Address
	to       []string
	toHeader string
	auth     smtp.Auth
}

func NewSMTPNotifier(cfg config.AlertConfig) (*SMTPNotifier, error) {
	to := cfg.SMTPToEmails
	if len(to) == 0 && cfg.SMTPFromEmail != "" {
		to = []string{cfg.SMTPFromEmail}
	}
	fromAddress, err := parseSMTPAddress("smtp-from-email", cfg.SMTPFromEmail)
	if err != nil {
		return nil, err
	}
	if containsHeaderControl(cfg.SMTPFromName) {
		return nil, fmt.Errorf("smtp-from-name must not contain control characters")
	}
	if len(to) == 0 {
		return nil, fmt.Errorf("smtp-to-email must contain at least one address")
	}
	recipients := make([]string, 0, len(to))
	for _, value := range to {
		recipient, err := parseSMTPAddress("smtp-to-email", value)
		if err != nil {
			return nil, err
		}
		recipients = append(recipients, recipient)
	}
	cfg.SMTPHost = strings.Trim(strings.TrimSpace(cfg.SMTPHost), "[]")

	n := &SMTPNotifier{
		cfg:  cfg,
		addr: net.JoinHostPort(cfg.SMTPHost, strconv.Itoa(cfg.SMTPPort)),
		from: mail.Address{Name: cfg.SMTPFromName, Address: fromAddress},
		to:   recipients,
	}
	if cfg.SMTPUsername != "" {
		n.auth = smtp.PlainAuth("", cfg.SMTPUsername, cfg.SMTPPassword, cfg.SMTPHost)
	}

	headers := make([]string, 0, len(n.to))
	for _, recipient := range n.to {
		headers = append(headers, (&mail.Address{Address: recipient}).String())
	}
	n.toHeader = strings.Join(headers, ", ")
	return n, nil
}

func (n *SMTPNotifier) Notify(ctx context.Context, subject string, body string) error {
	message := n.buildMessage(subject, body)

	log.Info().
		Str("smtp_host", n.cfg.SMTPHost).
		Int("smtp_port", n.cfg.SMTPPort).
		Int("recipient_count", len(n.to)).
		Msg("sending SMTP alert")

	client, conn, err := n.dial(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	defer func() { _ = client.Close() }()
	if err := conn.SetDeadline(time.Now().Add(n.cfg.Timeout)); err != nil {
		return err
	}

	if err := n.authenticate(client); err != nil {
		return err
	}
	if err := client.Mail(n.from.Address); err != nil {
		return err
	}
	for _, recipient := range n.to {
		if err := client.Rcpt(recipient); err != nil {
			return err
		}
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := writer.Write(message); err != nil {
		_ = writer.Close()
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return client.Quit()
}

func (n *SMTPNotifier) buildMessage(subject string, body string) []byte {
	headers := []string{
		fmt.Sprintf("From: %s", n.from.String()),
		fmt.Sprintf("To: %s", n.toHeader),
		fmt.Sprintf("Subject: %s", encodeSubject(subject)),
		fmt.Sprintf("Date: %s", time.Now().UTC().Format(time.RFC1123Z)),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"",
		body,
	}
	return []byte(strings.Join(headers, "\r\n"))
}

func parseSMTPAddress(field string, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || containsHeaderControl(value) {
		return "", fmt.Errorf("%s must be a valid email address", field)
	}
	address, err := mail.ParseAddress(value)
	if err != nil || address.Name != "" || address.Address != value {
		return "", fmt.Errorf("%s must be a single email address without a display name", field)
	}
	return address.Address, nil
}

func containsHeaderControl(value string) bool {
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

func encodeSubject(subject string) string {
	// Alert subjects can include upstream text. Collapse every ASCII control
	// character before RFC 2047 encoding so no value can create a new header.
	subject = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, subject)
	return mime.QEncoding.Encode("UTF-8", strings.TrimSpace(subject))
}

func (n *SMTPNotifier) dial(ctx context.Context) (*smtp.Client, net.Conn, error) {
	dialer := &net.Dialer{Timeout: n.cfg.Timeout}

	if n.cfg.SMTPUseTLS {
		conn, err := tls.DialWithDialer(dialer, "tcp", n.addr, &tls.Config{
			ServerName: n.cfg.SMTPHost,
			MinVersion: tls.VersionTLS12,
		})
		if err != nil {
			return nil, nil, err
		}
		client, err := smtp.NewClient(conn, n.cfg.SMTPHost)
		if err != nil {
			_ = conn.Close()
			return nil, nil, err
		}
		return client, conn, nil
	}

	conn, err := dialer.DialContext(ctx, "tcp", n.addr)
	if err != nil {
		return nil, nil, err
	}
	client, err := smtp.NewClient(conn, n.cfg.SMTPHost)
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	if n.cfg.SMTPStartTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			_ = client.Close()
			_ = conn.Close()
			return nil, nil, fmt.Errorf("smtp server does not advertise STARTTLS")
		}
		if err := client.StartTLS(&tls.Config{
			ServerName: n.cfg.SMTPHost,
			MinVersion: tls.VersionTLS12,
		}); err != nil {
			_ = client.Close()
			_ = conn.Close()
			return nil, nil, err
		}
	}
	return client, conn, nil
}

func (n *SMTPNotifier) authenticate(client *smtp.Client) error {
	if n.auth == nil {
		return nil
	}

	if ok, _ := client.Extension("AUTH"); !ok {
		return fmt.Errorf("smtp server does not advertise AUTH")
	}
	return client.Auth(n.auth)
}
