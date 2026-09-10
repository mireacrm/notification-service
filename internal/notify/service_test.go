package notify

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type fakeContacts struct {
	contacts ClientContacts
	err      error
}

func (f fakeContacts) ForClient(context.Context, uuid.UUID) (ClientContacts, error) {
	return f.contacts, f.err
}

type fakeSender struct {
	sent []Notification
	err  error
}

func (f *fakeSender) Send(_ context.Context, n Notification) error {
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, n)
	return nil
}

func setup(contacts ClientContacts) (*Service, *fakeSender) {
	sender := &fakeSender{}
	return New(fakeContacts{contacts: contacts}, sender, NewHistory(10)), sender
}

func full() ClientContacts {
	return ClientContacts{
		FullName: "Мария", Phone: "+79001234567",
		Email: "maria@example.com", Telegram: "@maria",
		Preferred: ChannelTelegram,
	}
}

func TestPreferredChannelUsed(t *testing.T) {
	service, sender := setup(full())
	id := uuid.New()

	_, err := service.NotifyClient(context.Background(),
		Message{ClientID: &id, Subject: "Тест", Body: "Тело"})
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}

	if sender.sent[0].Channel != ChannelTelegram {
		t.Fatalf("канал %q вместо telegram", sender.sent[0].Channel)
	}
	if sender.sent[0].Recipient != "@maria" {
		t.Fatalf("получатель %q", sender.sent[0].Recipient)
	}
}

func TestFallsBackWhenPreferredEmpty(t *testing.T) {
	// Пустой телеграм — не повод не отправить ничего.
	contacts := full()
	contacts.Telegram = ""
	service, sender := setup(contacts)
	id := uuid.New()

	_, _ = service.NotifyClient(context.Background(),
		Message{ClientID: &id, Subject: "Тест", Body: "Тело"})

	if sender.sent[0].Channel != ChannelEmail {
		t.Fatalf("ожидался откат на email, получено %q", sender.sent[0].Channel)
	}
}

func TestFailedSendRecordedNotRaised(t *testing.T) {
	// Неудачная отправка не должна ронять обработку события: поток событий
	// важнее одного письма.
	sender := &fakeSender{err: errors.New("smtp недоступен")}
	service := New(fakeContacts{contacts: full()}, sender, NewHistory(10))
	id := uuid.New()

	notification, err := service.NotifyClient(context.Background(),
		Message{ClientID: &id, Subject: "Тест", Body: "Тело"})

	if err != nil {
		t.Fatalf("ошибка не должна подниматься наружу: %v", err)
	}
	if notification.Status != StatusFailed || notification.Reason == "" {
		t.Fatalf("отправка должна быть помечена как неудачная: %+v", notification)
	}
}

func TestContactsFailurePropagates(t *testing.T) {
	// А вот недоступность client-service — другое дело: без контактов
	// отправлять некуда, событие должно уйти в повтор.
	service := New(fakeContacts{err: errors.New("client недоступен")}, &fakeSender{}, NewHistory(10))
	id := uuid.New()

	if _, err := service.NotifyClient(context.Background(),
		Message{ClientID: &id, Subject: "Тест", Body: "Тело"}); err == nil {
		t.Fatal("ожидалась ошибка")
	}
}

func TestBranchAlertGoesToDashboard(t *testing.T) {
	service, sender := setup(full())
	branch := uuid.New()

	service.NotifyBranch(context.Background(),
		Message{BranchID: &branch, Subject: "Материал", Body: "Заканчивается"})

	if sender.sent[0].Channel != ChannelDashboard {
		t.Fatalf("канал %q вместо dashboard", sender.sent[0].Channel)
	}
	if sender.sent[0].Recipient != branch.String() {
		t.Fatal("получателем должен быть филиал")
	}
}

func TestHistoryIsBounded(t *testing.T) {
	service := New(fakeContacts{contacts: full()}, &fakeSender{}, NewHistory(3))
	branch := uuid.New()

	for range 5 {
		service.NotifyBranch(context.Background(),
			Message{BranchID: &branch, Subject: "S", Body: "B"})
	}

	if got := len(service.History()); got != 3 {
		t.Fatalf("в истории %d записей вместо 3", got)
	}
}

func TestHistoryLookup(t *testing.T) {
	service, _ := setup(full())
	branch := uuid.New()

	notification := service.NotifyBranch(context.Background(),
		Message{BranchID: &branch, Subject: "S", Body: "B"})

	found, ok := service.Get(notification.ID)
	if !ok || found.ID != notification.ID {
		t.Fatal("отправка не найдена в истории")
	}
	if _, ok := service.Get(uuid.New()); ok {
		t.Fatal("найдена несуществующая отправка")
	}
}

func TestRenderSubstitutes(t *testing.T) {
	subject, body, ok := Render("appointment.created", map[string]string{
		"when": FormatWhen(time.Date(2026, 9, 15, 14, 30, 0, 0, time.UTC)),
	})

	if !ok {
		t.Fatal("шаблон не найден")
	}
	if subject != "Вы записаны" {
		t.Fatalf("тема %q", subject)
	}
	if !strings.Contains(body, "15.09.2026 в 14:30") {
		t.Fatalf("дата не подставлена: %q", body)
	}
}

func TestRenderDropsUnfilledPlaceholders(t *testing.T) {
	// Плейсхолдер без значения не должен утекать в текст пользователю.
	_, body, _ := Render("appointment.cancelled", map[string]string{})

	if strings.Contains(body, "{{") {
		t.Fatalf("плейсхолдер остался в тексте: %q", body)
	}
}

func TestRenderUnknownTemplate(t *testing.T) {
	if _, _, ok := Render("нет.такого", nil); ok {
		t.Fatal("несуществующий шаблон не должен находиться")
	}
}
