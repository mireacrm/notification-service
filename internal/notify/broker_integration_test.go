//go:build integration

package notify_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonv1 "github.com/mireacrm/contracts-go/mirea/common/v1"
	eventsv1 "github.com/mireacrm/contracts-go/mirea/events/v1"
	realtimev1 "github.com/mireacrm/contracts-go/mirea/realtime/v1"
	"github.com/mireacrm/go-common/infra"
	"github.com/mireacrm/notification-service/internal/notify"
)

// Сервис без базы, поэтому интеграция у него другая: настоящие брокеры.
// Проверяется то, что фейками не проверить, — что конверт доезжает, разбирается
// и доходит до обработчика через настоящую очередь и настоящую тему.
//
//	go test -tags=integration ./...

const (
	exchange  = "mirea.events"
	alertSubj = "mirea.branch.*.alerts"
)

// В конвейере брокеры подняты заведомо, поэтому пропуск теста там означал бы
// не «нечего проверять», а незамеченную поломку.
func brokerRequired() bool {
	return os.Getenv("NOTIFICATION_REQUIRE_BROKER") != ""
}

func unavailable(t *testing.T, what string, err error) {
	t.Helper()
	if brokerRequired() {
		t.Fatalf("%s недоступен: %v", what, err)
	}
	t.Skipf("%s недоступен: %v", what, err)
}

func amqpURL() string {
	if url := os.Getenv("NOTIFICATION_TEST_AMQP_URL"); url != "" {
		return url
	}
	return "amqp://guest:guest@localhost:5672/"
}

func natsURL() string {
	if url := os.Getenv("NOTIFICATION_TEST_NATS_URL"); url != "" {
		return url
	}
	return "nats://localhost:4222"
}

type stubContacts struct{}

func (stubContacts) ForClient(context.Context, uuid.UUID) (notify.ClientContacts, error) {
	return notify.ClientContacts{
		FullName: "Пётр Клиентов", Phone: "+79990000001", Preferred: notify.ChannelSMS,
	}, nil
}

type recordingSender struct {
	sent chan notify.Notification
}

func (r *recordingSender) Send(_ context.Context, notification notify.Notification) error {
	r.sent <- notification
	return nil
}

func newService() (*notify.Service, *recordingSender) {
	sender := &recordingSender{sent: make(chan notify.Notification, 8)}
	return notify.New(stubContacts{}, sender, notify.NewHistory(16)), sender
}

// ownQueue заводит собственную очередь на время теста: очередь сервиса читает
// запущенный контейнер, и тест отбирал бы у него сообщения.
func ownQueue(t *testing.T, keys ...string) (*amqp.Connection, string) {
	t.Helper()

	conn, err := amqp.Dial(amqpURL())
	if err != nil {
		unavailable(t, "RabbitMQ", err)
	}
	channel, err := conn.Channel()
	if err != nil {
		t.Fatalf("канал: %v", err)
	}
	defer channel.Close()

	// Обменник объявляется теми же параметрами, что в deploy/rabbitmq: в
	// конвейере брокер поднимается пустым, и объявлять его некому.
	if err := channel.ExchangeDeclare(exchange, "topic", true, false, false, false, nil); err != nil {
		t.Fatalf("обменник: %v", err)
	}

	// Очередь не эксклюзивная: потребитель подключается своим соединением,
	// а к эксклюзивной очереди доступ есть только у объявившего.
	name := "test-notification-" + uuid.NewString()[:8]
	if _, err := channel.QueueDeclare(name, false, true, false, false, nil); err != nil {
		t.Fatalf("очередь: %v", err)
	}
	for _, key := range keys {
		if err := channel.QueueBind(name, key, exchange, false, nil); err != nil {
			t.Fatalf("привязка %s: %v", key, err)
		}
	}
	t.Cleanup(func() {
		if cleanup, err := conn.Channel(); err == nil {
			_, _ = cleanup.QueueDelete(name, false, false, false)
			_ = cleanup.Close()
		}
		_ = conn.Close()
	})
	return conn, name
}

func publish(t *testing.T, conn *amqp.Connection, key string, envelope *eventsv1.EventEnvelope) {
	t.Helper()

	envelope.EventId = uuid.NewString()
	envelope.RoutingKey = key
	envelope.OccurredAt = timestamppb.New(time.Now().UTC())
	envelope.Producer = "тест"

	body, err := protojson.Marshal(envelope)
	if err != nil {
		t.Fatalf("сериализация: %v", err)
	}

	channel, err := conn.Channel()
	if err != nil {
		t.Fatalf("канал: %v", err)
	}
	defer channel.Close()

	err = channel.PublishWithContext(context.Background(), exchange, key, false, false,
		amqp.Publishing{ContentType: "application/json", Type: key, Body: body})
	if err != nil {
		t.Fatalf("публикация: %v", err)
	}
}

func createdEvent(after time.Duration) *eventsv1.EventEnvelope {
	start := time.Now().Add(after)
	return &eventsv1.EventEnvelope{
		Payload: &eventsv1.EventEnvelope_AppointmentCreated{
			AppointmentCreated: &eventsv1.AppointmentCreated{
				AppointmentId: uuid.NewString(),
				BranchId:      uuid.NewString(),
				ClientId:      uuid.NewString(),
				EmployeeId:    uuid.NewString(),
				ServiceId:     uuid.NewString(),
				Period: &commonv1.TimeRange{
					StartAt: timestamppb.New(start),
					EndAt:   timestamppb.New(start.Add(time.Hour)),
				},
			},
		},
	}
}

// runConsumer запускает потребителя и снимает его в конце теста. Порядок важен:
// сначала Run обязан выйти, и только потом закрывается канал. Обе фазы под
// таймаутом — зависание здесь иначе упирается в общий лимит go test и
// диагностируется десятиминутной паникой.
func runConsumer(t *testing.T, consumer *infra.Consumer) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		_ = consumer.Run(ctx)
	}()

	t.Cleanup(func() {
		cancel()
		waitFor(t, stopped, "потребитель не остановился")

		closed := make(chan struct{})
		go func() {
			defer close(closed)
			consumer.Close()
		}()
		waitFor(t, closed, "закрытие потребителя зависло")
	})
}

func waitFor(t *testing.T, done <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Errorf("%s за 5 секунд", what)
	}
}

func TestEventFromBrokerReachesHandler(t *testing.T) {
	conn, queue := ownQueue(t, "appointment.created", "stock.low")

	service, sender := newService()
	consumer, err := infra.NewConsumer(amqpURL(), queue, "notification-test")
	if err != nil {
		t.Fatalf("потребитель: %v", err)
	}

	consumer.Handle("appointment.created", service.OnAppointmentCreated)
	consumer.Handle("stock.low", service.OnStockLow)
	runConsumer(t, consumer)

	publish(t, conn, "appointment.created", createdEvent(24*time.Hour))

	select {
	case notification := <-sender.sent:
		if notification.Channel != notify.ChannelSMS {
			t.Errorf("канал: %v, ожидался предпочитаемый клиентом", notification.Channel)
		}
		if notification.Body == "" {
			t.Error("текст уведомления пуст")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("уведомление не дошло через очередь")
	}
}

func TestUnparsableEventDoesNotStopConsumer(t *testing.T) {
	conn, queue := ownQueue(t, "appointment.created")

	service, sender := newService()
	consumer, err := infra.NewConsumer(amqpURL(), queue, "notification-test")
	if err != nil {
		t.Fatalf("потребитель: %v", err)
	}

	consumer.Handle("appointment.created", service.OnAppointmentCreated)
	runConsumer(t, consumer)

	channel, err := conn.Channel()
	if err != nil {
		t.Fatalf("канал: %v", err)
	}
	err = channel.PublishWithContext(context.Background(), exchange, "appointment.created",
		false, false, amqp.Publishing{ContentType: "application/json", Body: []byte("{не json")})
	if err != nil {
		t.Fatalf("публикация мусора: %v", err)
	}
	_ = channel.Close()

	// Следом — корректное событие. Если потребитель встал на мусоре, оно
	// не дойдёт, и очередь окажется заблокированной одним битым сообщением.
	publish(t, conn, "appointment.created", createdEvent(time.Hour))

	select {
	case <-sender.sent:
	case <-time.After(10 * time.Second):
		t.Fatal("после битого сообщения очередь встала")
	}
}

func TestAlertFromRealtimeReachesHandler(t *testing.T) {
	subscriber, err := infra.NewSubscriber(natsURL(), "notification-test")
	if err != nil {
		unavailable(t, "NATS", err)
	}
	defer subscriber.Close()

	service, sender := newService()
	err = subscriber.Subscribe(alertSubj,
		func() proto.Message { return &realtimev1.BranchAlert{} },
		func(ctx context.Context, message proto.Message) {
			service.OnBranchAlert(ctx, message.(*realtimev1.BranchAlert))
		})
	if err != nil {
		t.Fatalf("подписка: %v", err)
	}

	publisher, err := infra.NewRealtimePublisher(natsURL(), "тест")
	if err != nil {
		t.Fatalf("издатель: %v", err)
	}
	defer publisher.Close()

	branch := uuid.NewString()
	err = publisher.Publish(context.Background(), "mirea.branch."+branch+".alerts",
		&realtimev1.BranchAlert{
			BranchId: branch,
			Severity: realtimev1.BranchAlert_SEVERITY_WARNING,
			Title:    "Материал заканчивается",
			Body:     "краска: осталось 40 г",
			RaisedAt: timestamppb.New(time.Now()),
		})
	if err != nil {
		t.Fatalf("публикация: %v", err)
	}

	select {
	case notification := <-sender.sent:
		if notification.Body == "" {
			t.Error("текст предупреждения пуст")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("предупреждение не дошло через NATS")
	}
}
