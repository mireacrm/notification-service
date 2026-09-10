package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/mireacrm/go-common/infra"
	"github.com/mireacrm/notification-service/internal/notify"
)

type Handler struct {
	service *notify.Service
}

type SendRequest struct {
	ClientID uuid.UUID `json:"client_id"`
	Subject  string    `json:"subject"`
	Body     string    `json:"body"`
}

func NewRouter(
	service *notify.Service, serviceName string, probes ...infra.Probe,
) http.Handler {
	handler := &Handler{service: service}

	router := chi.NewRouter()
	router.Use(infra.TraceMiddleware, infra.IdentityMiddleware,
		infra.MetricsMiddleware(serviceName))

	router.Post("/notifications", handler.send)
	router.Get("/notifications/{notificationID}", handler.get)
	router.Get("/templates", handler.templates)

	router.Get("/metrics", infra.MetricsHandler().ServeHTTP)
	router.Get("/healthz", infra.LivenessHandler())
	router.Get("/readyz", infra.ReadinessHandler(probes...))
	return router
}

// send отправляет произвольное уведомление клиенту. Внутри идёт gRPC-запрос
// в client-service за контактами.
func (h *Handler) send(w http.ResponseWriter, r *http.Request) {
	var request SendRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		infra.WriteError(w, r, infra.InvalidArgument("тело запроса: %v", err))
		return
	}
	if request.Subject == "" || request.Body == "" {
		infra.WriteError(w, r, infra.InvalidArgument("subject и body обязательны"))
		return
	}

	notification, err := h.service.NotifyClient(r.Context(), notify.Message{
		ClientID: &request.ClientID,
		Subject:  request.Subject,
		Body:     request.Body,
	})
	if err != nil {
		infra.WriteError(w, r, err)
		return
	}
	infra.WriteJSON(w, http.StatusAccepted, notification)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "notificationID"))
	if err != nil {
		infra.WriteError(w, r, infra.InvalidArgument("notificationID: невалидный UUID"))
		return
	}

	notification, ok := h.service.Get(id)
	if !ok {
		infra.WriteError(w, r, infra.NotFound("notification", id))
		return
	}
	infra.WriteJSON(w, http.StatusOK, notification)
}

func (h *Handler) templates(w http.ResponseWriter, r *http.Request) {
	infra.WriteJSON(w, http.StatusOK, notify.Templates())
}
