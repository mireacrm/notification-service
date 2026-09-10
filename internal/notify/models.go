package notify

import (
	"time"

	"github.com/google/uuid"
)

type Channel string

const (
	ChannelSMS      Channel = "sms"
	ChannelEmail    Channel = "email"
	ChannelTelegram Channel = "telegram"
	// ChannelDashboard — экран администратора филиала: у него нет личных
	// контактов, сообщение просто показывается на месте.
	ChannelDashboard Channel = "dashboard"
)

type Status string

const (
	StatusSent   Status = "sent"
	StatusFailed Status = "failed"
)

// Notification — одна отправка. Своего состояния сервис не имеет: это лог
// того, что ушло, а не источник правды.
type Notification struct {
	ID        uuid.UUID `json:"id"`
	Channel   Channel   `json:"channel"`
	Recipient string    `json:"recipient"`
	Subject   string    `json:"subject"`
	Body      string    `json:"body"`
	Status    Status    `json:"status"`
	Reason    string    `json:"reason,omitempty"`
	SentAt    time.Time `json:"sent_at"`
}

// Message — что и кому отправить, до выбора канала.
type Message struct {
	ClientID *uuid.UUID
	BranchID *uuid.UUID
	Subject  string
	Body     string
}
