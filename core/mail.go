package core

import (
	"context"
	"crypto/tls"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const smtpNetworkTimeout = 15 * time.Second

type MailMessage struct {
	From    string
	To      string
	Subject string
	Text    string
}

type Mailer interface {
	Send(ctx context.Context, msg MailMessage) error
}

type NoopMailer struct{}

func (NoopMailer) Send(context.Context, MailMessage) error { return nil }

type SMTPMailer struct {
	host       string
	port       string
	fromHeader string
	fromAddr   string
	user       string
	password   string
}

func NewMailer(cfg *Config) Mailer {
	if cfg == nil || strings.TrimSpace(cfg.SMTPHost) == "" {
		return NoopMailer{}
	}
	from, err := mail.ParseAddress(cfg.SMTPFrom)
	if err != nil {
		return NoopMailer{}
	}
	return &SMTPMailer{
		host:       strings.TrimSpace(cfg.SMTPHost),
		port:       strings.TrimSpace(cfg.SMTPPort),
		fromHeader: strings.TrimSpace(cfg.SMTPFrom),
		fromAddr:   from.Address,
		user:       strings.TrimSpace(cfg.SMTPUser),
		password:   cfg.SMTPPassword,
	}
}

func (m *SMTPMailer) Send(ctx context.Context, msg MailMessage) error {
	to, err := mail.ParseAddress(msg.To)
	if err != nil {
		return fmt.Errorf("%w: invalid recipient email", ErrValidation)
	}
	fromHeader := msg.From
	fromAddr := m.fromAddr
	if strings.TrimSpace(fromHeader) == "" {
		fromHeader = m.fromHeader
	}
	if parsed, err := mail.ParseAddress(fromHeader); err == nil {
		fromAddr = parsed.Address
	}
	addr := net.JoinHostPort(m.host, m.port)
	conn, err := publicTCPDialer(smtpNetworkTimeout)(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	setSMTPDeadline(ctx, conn)
	client, err := smtp.NewClient(conn, m.host)
	if err != nil {
		conn.Close()
		return err
	}
	defer client.Close()

	if ok, _ := client.Extension("STARTTLS"); ok {
		tlsCfg := &tls.Config{ServerName: m.host, MinVersion: tls.VersionTLS12}
		if err := client.StartTLS(tlsCfg); err != nil {
			return err
		}
	}
	if m.user != "" {
		if err := client.Auth(smtp.PlainAuth("", m.user, m.password, m.host)); err != nil {
			return err
		}
	}
	if err := client.Mail(fromAddr); err != nil {
		return err
	}
	if err := client.Rcpt(to.Address); err != nil {
		return err
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte(formatMailMessage(fromHeader, to.String(), msg))); err != nil {
		w.Close()
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return client.Quit()
}

func BuildAuthTokenEmail(cfg *Config, kind string, projectSlug string, email string, token string) (MailMessage, error) {
	if cfg == nil {
		return MailMessage{}, fmt.Errorf("%w: config is required", ErrValidation)
	}
	email = NormalizeEmail(email)
	if err := ValidateAppUserEmail(email); err != nil {
		return MailMessage{}, err
	}
	projectSlug = NormalizeProjectSlug(projectSlug)
	if err := ValidateProjectSlug(projectSlug); err != nil {
		return MailMessage{}, err
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return MailMessage{}, fmt.Errorf("%w: token is required", ErrValidation)
	}
	switch kind {
	case "verify_email":
		link, err := authActionLink(cfg.AppURL, "/auth/verify", projectSlug, email, token)
		if err != nil {
			return MailMessage{}, err
		}
		return MailMessage{
			From:    cfg.SMTPFrom,
			To:      email,
			Subject: "Verify your email",
			Text: "Verify your email for project " + projectSlug + ".\n\n" +
				"Open this link:\n" + link + "\n\n" +
				"Token:\n" + token + "\n",
		}, nil
	case "password_reset":
		link, err := authActionLink(cfg.AppURL, "/auth/reset-password", projectSlug, email, token)
		if err != nil {
			return MailMessage{}, err
		}
		return MailMessage{
			From:    cfg.SMTPFrom,
			To:      email,
			Subject: "Reset your password",
			Text: "Reset your password for project " + projectSlug + ".\n\n" +
				"Open this link:\n" + link + "\n\n" +
				"Token:\n" + token + "\n",
		}, nil
	default:
		return MailMessage{}, fmt.Errorf("%w: unsupported auth email type", ErrValidation)
	}
}

func authActionLink(appURL string, route string, projectSlug string, email string, token string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(appURL))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("%w: APP_URL must be an absolute URL", ErrValidation)
	}
	u.Path = joinURLPath(u.Path, route)
	u.RawQuery = ""
	u.Fragment = ""
	q := url.Values{}
	q.Set("project", projectSlug)
	q.Set("email", email)
	q.Set("token", token)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func joinURLPath(base string, route string) string {
	base = strings.TrimRight(base, "/")
	route = "/" + strings.TrimLeft(route, "/")
	if base == "" {
		return route
	}
	return base + route
}

func formatMailMessage(from string, to string, msg MailMessage) string {
	var b strings.Builder
	headers := map[string]string{
		"From":         from,
		"To":           to,
		"Subject":      mime.QEncoding.Encode("utf-8", msg.Subject),
		"MIME-Version": "1.0",
		"Content-Type": "text/plain; charset=utf-8",
	}
	for _, key := range []string{"From", "To", "Subject", "MIME-Version", "Content-Type"} {
		b.WriteString(key)
		b.WriteString(": ")
		b.WriteString(sanitizeMailHeader(headers[key]))
		b.WriteString("\r\n")
	}
	b.WriteString("\r\n")
	body := strings.ReplaceAll(msg.Text, "\r\n", "\n")
	body = strings.ReplaceAll(body, "\r", "\n")
	b.WriteString(strings.ReplaceAll(body, "\n", "\r\n"))
	if !strings.HasSuffix(body, "\n") {
		b.WriteString("\r\n")
	}
	return b.String()
}

func sanitizeMailHeader(v string) string {
	v = strings.ReplaceAll(v, "\r", " ")
	v = strings.ReplaceAll(v, "\n", " ")
	return strings.Join(strings.Fields(v), " ")
}

func setSMTPDeadline(ctx context.Context, conn net.Conn) {
	deadline := time.Now().Add(smtpNetworkTimeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	_ = conn.SetDeadline(deadline)
}

func validateSMTPConfig(c *Config) error {
	if strings.TrimSpace(c.SMTPHost) == "" {
		return nil
	}
	port, err := strconv.Atoi(strings.TrimSpace(c.SMTPPort))
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("SMTP_PORT must be between 1 and 65535 (got %q)", c.SMTPPort)
	}
	host := strings.TrimSpace(c.SMTPHost)
	if err := validatePublicOutboundHost(host); err != nil {
		return fmt.Errorf("SMTP_HOST %w", err)
	}
	if strings.TrimSpace(c.SMTPFrom) == "" {
		return fmt.Errorf("SMTP_FROM is required when SMTP_HOST is set")
	}
	if _, err := mail.ParseAddress(strings.TrimSpace(c.SMTPFrom)); err != nil {
		return fmt.Errorf("SMTP_FROM must be a valid email address")
	}
	if (strings.TrimSpace(c.SMTPUser) == "") != (c.SMTPPassword == "") {
		return fmt.Errorf("SMTP_USER and SMTP_PASSWORD must be set together")
	}
	return nil
}
