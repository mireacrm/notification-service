package notify

import (
	"context"
	"log/slog"
	"sync"

	"github.com/google/uuid"
)

// Contacts — контакты клиента из client-service. Здесь они не дублируются:
// персональные данные живут в одном месте, что заметно упрощает ПР9.
type Contacts interface {
	ForClient(ctx context.Context, clientID uuid.UUID) (ClientContacts, error)
}

type ClientContacts struct {
	FullName  string
	Phone     string
	Email     string
	Telegram  string
	Preferred Channel
}

// Sender отправляет готовое сообщение в конкретный канал.
type Sender interface {
	Send(ctx context.Context, notification Notification) error
}

type Service struct {
	contacts Contacts
	sender   Sender
	history  *History
}

func New(contacts Contacts, sender Sender, history *History) *Service {
	return &Service{contacts: contacts, sender: sender, history: history}
}

// NotifyClient выбирает канал по предпочтению клиента и отправляет.
func (s *Service) NotifyClient(ctx context.Context, message Message) (Notification, error) {
	contacts, err := s.contacts.ForClient(ctx, *message.ClientID)
	if err != nil {
		return Notification{}, err
	}

	channel, recipient := pickChannel(contacts)
	return s.deliver(ctx, channel, recipient, message), nil
}

// NotifyBranch шлёт администратору филиала: личных контактов у него нет,
// сообщение уходит на экран.
func (s *Service) NotifyBranch(ctx context.Context, message Message) Notification {
	return s.deliver(ctx, ChannelDashboard, message.BranchID.String(), message)
}

func (s *Service) History() []Notification { return s.history.All() }

func (s *Service) Get(id uuid.UUID) (Notification, bool) { return s.history.Get(id) }

func (s *Service) deliver(
	ctx context.Context, channel Channel, recipient string, message Message,
) Notification {
	notification := Notification{
		ID:        uuid.New(),
		Channel:   channel,
		Recipient: recipient,
		Subject:   message.Subject,
		Body:      message.Body,
		Status:    StatusSent,
		SentAt:    now(),
	}

	if err := s.sender.Send(ctx, notification); err != nil {
		// Неудачная отправка не роняет обработку события: письмо можно
		// повторить, а поток событий останавливать нельзя.
		notification.Status = StatusFailed
		notification.Reason = err.Error()
		slog.WarnContext(ctx, "уведомление не отправлено",
			"channel", channel, "error", err)
	}

	s.history.Add(notification)
	return notification
}

// pickChannel уважает предпочтение клиента, но откатывается на то, что есть:
// пустой телеграм не повод не отправить ничего.
func pickChannel(contacts ClientContacts) (Channel, string) {
	options := []struct {
		channel Channel
		value   string
	}{
		{ChannelTelegram, contacts.Telegram},
		{ChannelEmail, contacts.Email},
		{ChannelSMS, contacts.Phone},
	}

	for _, option := range options {
		if option.channel == contacts.Preferred && option.value != "" {
			return option.channel, option.value
		}
	}
	for _, option := range options {
		if option.value != "" {
			return option.channel, option.value
		}
	}
	return ChannelSMS, ""
}

// History — кольцевой буфер последних отправок. Базы у сервиса нет: своего
// состояния он не имеет, а история нужна только для отладки и проверки.
type History struct {
	mu       sync.RWMutex
	capacity int
	items    []Notification
	index    map[uuid.UUID]Notification
}

func NewHistory(capacity int) *History {
	return &History{capacity: capacity, index: map[uuid.UUID]Notification{}}
}

func (h *History) Add(notification Notification) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.items = append(h.items, notification)
	h.index[notification.ID] = notification
	if len(h.items) > h.capacity {
		oldest := h.items[0]
		h.items = h.items[1:]
		delete(h.index, oldest.ID)
	}
}

func (h *History) Get(id uuid.UUID) (Notification, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	notification, ok := h.index[id]
	return notification, ok
}

func (h *History) All() []Notification {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]Notification, len(h.items))
	copy(out, h.items)
	return out
}
