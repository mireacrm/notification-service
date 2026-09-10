package config

import "github.com/mireacrm/go-common/infra"

type Config struct {
	ServiceName  string
	HTTPPort     int
	GRPCPort     int
	AMQPURL      string
	NATSURL      string
	ClientAddr   string
	OTLPEndpoint string
	Debug        bool
}

func Load() (Config, error) {
	cfg := Config{
		ServiceName: "notification-service",
		AMQPURL:     infra.Env("NOTIFICATION_AMQP_URL", "amqp://guest:guest@localhost:5672/"),
		NATSURL:     infra.Env("NOTIFICATION_NATS_URL", "nats://localhost:4222"),
		ClientAddr:  infra.Env("NOTIFICATION_CLIENT_ADDR", "localhost:9005"),
		// Пустой адрес выключает экспорт трасс.
		OTLPEndpoint: infra.Env("NOTIFICATION_OTLP_ENDPOINT", ""),
		Debug:        infra.Env("NOTIFICATION_DEBUG", "false") == "true",
	}

	var err error
	if cfg.HTTPPort, err = infra.EnvInt("NOTIFICATION_HTTP_PORT", 8007); err != nil {
		return cfg, err
	}
	if cfg.GRPCPort, err = infra.EnvInt("NOTIFICATION_GRPC_PORT", 9007); err != nil {
		return cfg, err
	}
	return cfg, nil
}
