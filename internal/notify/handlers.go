package notify

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"

	eventsv1 "github.com/mireacrm/contracts-go/mirea/events/v1"
	realtimev1 "github.com/mireacrm/contracts-go/mirea/realtime/v1"
)

// Обработчики доменных событий. Разбор payload — работа транспорта: домен
// получает уже готовое сообщение.

func (s *Service) OnAppointmentCreated(
	ctx context.Context, envelope *eventsv1.EventEnvelope,
) error {
	payload := envelope.GetAppointmentCreated()
	if payload == nil {
		return errors.New("appointment.created без payload")
	}

	clientID, err := uuid.Parse(payload.GetClientId())
	if err != nil {
		return err
	}

	subject, body, _ := Render("appointment.created", map[string]string{
		"when": FormatWhen(payload.GetPeriod().GetStartAt().AsTime()),
	})
	_, err = s.NotifyClient(ctx, Message{ClientID: &clientID, Subject: subject, Body: body})
	return err
}

func (s *Service) OnAppointmentCancelled(
	ctx context.Context, envelope *eventsv1.EventEnvelope,
) error {
	payload := envelope.GetAppointmentCancelled()
	if payload == nil {
		return errors.New("appointment.cancelled без payload")
	}

	clientID, err := uuid.Parse(payload.GetClientId())
	if err != nil {
		return err
	}

	reason := payload.GetReason()
	if reason != "" {
		reason = "Причина: " + reason + "."
	}
	subject, body, _ := Render("appointment.cancelled", map[string]string{"reason": reason})
	_, err = s.NotifyClient(ctx, Message{ClientID: &clientID, Subject: subject, Body: body})
	return err
}

func (s *Service) OnInvoiceIssued(ctx context.Context, envelope *eventsv1.EventEnvelope) error {
	payload := envelope.GetInvoiceIssued()
	if payload == nil {
		return errors.New("invoice.issued без payload")
	}

	clientID, err := uuid.Parse(payload.GetClientId())
	if err != nil {
		return err
	}

	subject, body, _ := Render("invoice.issued", map[string]string{
		"amount": FormatMoney(payload.GetTotal().GetAmountKopecks()),
	})
	_, err = s.NotifyClient(ctx, Message{ClientID: &clientID, Subject: subject, Body: body})
	return err
}

func (s *Service) OnStockLow(ctx context.Context, envelope *eventsv1.EventEnvelope) error {
	payload := envelope.GetStockLow()
	if payload == nil {
		return errors.New("stock.low без payload")
	}

	branchID, err := uuid.Parse(payload.GetBranchId())
	if err != nil {
		return err
	}

	subject, body, _ := Render("stock.low", map[string]string{
		"name":      payload.GetName(),
		"unit":      payload.GetUnit(),
		"remaining": FormatMoney(int64(payload.GetRemaining() * 100)),
		"threshold": FormatMoney(int64(payload.GetThreshold() * 100)),
	})
	s.NotifyBranch(ctx, Message{BranchID: &branchID, Subject: subject, Body: body})
	return nil
}

// OnBranchAlert — то же самое, но из NATS: мгновенный показ на экране.
// Тот же факт приходит и через RabbitMQ письмом; NATS нужен только для скорости,
// поэтому потеря здесь безвредна и ошибка не возвращается наружу.
func (s *Service) OnBranchAlert(ctx context.Context, alert *realtimev1.BranchAlert) {
	branchID, err := uuid.Parse(alert.GetBranchId())
	if err != nil {
		slog.WarnContext(ctx, "алерт с некорректным branch_id", "error", err)
		return
	}

	s.NotifyBranch(ctx, Message{
		BranchID: &branchID,
		Subject:  alert.GetTitle(),
		Body:     alert.GetBody(),
	})
}
