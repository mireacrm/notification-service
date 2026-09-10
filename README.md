# Сервис уведомлений

Рассылает уведомления по доменным событиям: слушает RabbitMQ для надёжной
доставки и NATS для эфемерного real-time. Собственной базы не имеет —
состояние живёт в очередях, а адаптеры каналов пишут в журнал.

## Зависимости

| Модуль | Роль |
|---|---|
| [`mireacrm/contracts-go`](https://github.com/mireacrm/contracts-go) | сообщения и стабы gRPC |
| [`mireacrm/go-common`](https://github.com/mireacrm/go-common) | транспорт, трассировка, метрики, каркас процесса |

Приезжают из сети по версии из `go.mod`. Повышение версии обвяза — ручное
и осознанное: см. [`mireacrm/proto`](https://github.com/mireacrm/proto).

## Локально

```
go test ./...                    # модульные
go test -tags integration ./...  # нужны RabbitMQ и NATS
docker build -t notification-service .
```

Систему целиком поднимает [`mireacrm/deploy`](https://github.com/mireacrm/deploy).
