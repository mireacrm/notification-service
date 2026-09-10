package notify

import (
	"context"
	"log/slog"
)

// LogSender — заглушка вместо реальной отправки. Подменяется на SMTP или
// Telegram Bot API одной строкой в main: интерфейс Sender для того и нужен.
type LogSender struct{}

func (LogSender) Send(ctx context.Context, notification Notification) error {
	slog.InfoContext(ctx, "уведомление отправлено",
		"channel", notification.Channel,
		"recipient", notification.Recipient,
		"subject", notification.Subject,
		"body", notification.Body,
	)
	return nil
}
