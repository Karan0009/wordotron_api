// Package mailer defines the outbound email contract. The default
// implementation logs instead of sending, which keeps local development and
// tests free of external dependencies; swap in an SMTP or provider-backed
// implementation without touching the service layer.
package mailer

import (
	"context"
	"log/slog"
)

// Mailer sends transactional email.
type Mailer interface {
	SendPasswordReset(ctx context.Context, to, resetURL string) error
	SendWelcome(ctx context.Context, to, fullName string) error
}

// LogMailer writes the message that would have been sent to the application
// log. Reset links are logged at debug level so they never end up in
// production log aggregation by default.
type LogMailer struct {
	log *slog.Logger
}

var _ Mailer = (*LogMailer)(nil)

// NewLogMailer returns a Mailer backed by the application logger.
func NewLogMailer(log *slog.Logger) *LogMailer {
	return &LogMailer{log: log.With(slog.String("component", "mailer"))}
}

func (m *LogMailer) SendPasswordReset(ctx context.Context, to, resetURL string) error {
	m.log.DebugContext(ctx, "password reset email",
		slog.String("to", to),
		slog.String("reset_url", resetURL),
	)
	m.log.InfoContext(ctx, "password reset email queued", slog.String("to", to))
	return nil
}

func (m *LogMailer) SendWelcome(ctx context.Context, to, fullName string) error {
	m.log.InfoContext(ctx, "welcome email queued",
		slog.String("to", to),
		slog.String("full_name", fullName),
	)
	return nil
}
