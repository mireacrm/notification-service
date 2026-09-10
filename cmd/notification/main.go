package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"google.golang.org/protobuf/proto"

	realtimev1 "github.com/mireacrm/contracts-go/mirea/realtime/v1"

	"github.com/mireacrm/go-common/infra"
	"github.com/mireacrm/notification-service/internal/api"
	"github.com/mireacrm/notification-service/internal/clients"
	"github.com/mireacrm/notification-service/internal/config"
	"github.com/mireacrm/notification-service/internal/notify"
)

const (
	queueName    = "notification-service.events"
	alertSubject = "mirea.branch.*.alerts"
	historySize  = 500
)

func main() {
	// В distroless-образе нет ни shell, ни curl, поэтому healthcheck
	// контейнера выполняет сам бинарник.
	healthcheck := flag.Bool("healthcheck", false, "проверить готовность и выйти")
	flag.Parse()
	if *healthcheck {
		os.Exit(probe())
	}

	if err := run(); err != nil {
		slog.Error("сервис остановлен с ошибкой", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	setupLogging(cfg.Debug)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	shutdownTracing, err := infra.SetupTracing(ctx, cfg.ServiceName, cfg.OTLPEndpoint)
	if err != nil {
		return err
	}
	// Экспортёр копит спаны пачками: без остановки последняя пачка теряется.
	defer func() {
		flush, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdownTracing(flush)
	}()

	contacts, err := clients.DialClients(cfg.ClientAddr)
	if err != nil {
		return err
	}
	defer contacts.Close()

	service := notify.New(contacts, notify.LogSender{}, notify.NewHistory(historySize))

	consumer, err := infra.NewConsumer(cfg.AMQPURL, queueName, cfg.ServiceName)
	if err != nil {
		return err
	}
	defer consumer.Close()

	consumer.Handle("appointment.created", service.OnAppointmentCreated)
	consumer.Handle("appointment.cancelled", service.OnAppointmentCancelled)
	consumer.Handle("invoice.issued", service.OnInvoiceIssued)
	consumer.Handle("stock.low", service.OnStockLow)

	subscriber, err := infra.NewSubscriber(cfg.NATSURL, cfg.ServiceName)
	if err != nil {
		return err
	}
	defer subscriber.Close()

	err = subscriber.Subscribe(alertSubject,
		func() proto.Message { return &realtimev1.BranchAlert{} },
		func(ctx context.Context, message proto.Message) {
			service.OnBranchAlert(ctx, message.(*realtimev1.BranchAlert))
		})
	if err != nil {
		return err
	}

	router := api.NewRouter(service, cfg.ServiceName,
		infra.Probe{Name: "broker", Check: consumer.Ping},
		infra.Probe{Name: "realtime", Check: subscriber.Ping},
	)
	server := &http.Server{
		Addr:    ":" + strconv.Itoa(cfg.HTTPPort),
		Handler: infra.HTTPHandler(router, cfg.ServiceName),
	}

	errc := make(chan error, 2)
	go func() {
		slog.Info("HTTP слушает", "port", cfg.HTTPPort)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
	}()
	go func() {
		if err := consumer.Run(ctx); err != nil {
			errc <- err
		}
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		slog.Info("получен сигнал, останавливаемся")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), infra.ShutdownGrace)
	defer cancel()
	return server.Shutdown(shutdownCtx)
}

func probe() int {
	cfg, err := config.Load()
	if err != nil {
		return 1
	}

	client := http.Client{Timeout: 3 * time.Second}
	response, err := client.Get("http://127.0.0.1:" + strconv.Itoa(cfg.HTTPPort) + "/readyz")
	if err != nil {
		return 1
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

func setupLogging(debug bool) {
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})))
}
