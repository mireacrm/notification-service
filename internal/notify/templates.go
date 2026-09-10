package notify

import (
	"fmt"
	"strings"
	"time"
)

// Шаблоны сообщений. Отдельного хранилища не заводим: текстов немного,
// и меняются они вместе с кодом, а не администратором.

type Template struct {
	Key     string `json:"key"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

var templates = map[string]Template{
	"appointment.created": {
		Key:     "appointment.created",
		Subject: "Вы записаны",
		Body:    "Ждём вас {{when}}. Если планы изменятся, отмените запись заранее.",
	},
	"appointment.cancelled": {
		Key:     "appointment.cancelled",
		Subject: "Запись отменена",
		Body:    "Ваша запись отменена. {{reason}}",
	},
	"invoice.issued": {
		Key:     "invoice.issued",
		Subject: "Счёт за визит",
		Body:    "К оплате {{amount}} руб.",
	},
	"stock.low": {
		Key:     "stock.low",
		Subject: "Материал заканчивается",
		Body:    "{{name}}: осталось {{remaining}} {{unit}} при пороге {{threshold}}.",
	},
}

func Templates() []Template {
	out := make([]Template, 0, len(templates))
	for _, item := range templates {
		out = append(out, item)
	}
	return out
}

// Render подставляет значения в шаблон. Плейсхолдеры без значения остаются
// пустыми, а не ломают отправку: письмо с пропуском лучше неотправленного.
func Render(key string, values map[string]string) (string, string, bool) {
	template, ok := templates[key]
	if !ok {
		return "", "", false
	}

	body := template.Body
	for name, value := range values {
		body = strings.ReplaceAll(body, "{{"+name+"}}", value)
	}
	for _, name := range placeholders(body) {
		body = strings.ReplaceAll(body, name, "")
	}
	return template.Subject, strings.TrimSpace(collapseSpaces(body)), true
}

func placeholders(body string) []string {
	var found []string
	for {
		start := strings.Index(body, "{{")
		if start < 0 {
			return found
		}
		end := strings.Index(body[start:], "}}")
		if end < 0 {
			return found
		}
		found = append(found, body[start:start+end+2])
		body = body[start+end+2:]
	}
}

func collapseSpaces(value string) string {
	for strings.Contains(value, "  ") {
		value = strings.ReplaceAll(value, "  ", " ")
	}
	return value
}

func FormatMoney(kopecks int64) string {
	return fmt.Sprintf("%.2f", float64(kopecks)/100)
}

func FormatWhen(value time.Time) string {
	return value.Format("02.01.2006 в 15:04")
}
