package webhook

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"

	gitlab "gitlab.com/gitlab-org/api/client-go"
)

var (
	ErrNilParser    = errors.New("webhook parser is nil")
	ErrNilHandler   = errors.New("event handler is nil")
	ErrEmptyPayload = errors.New("webhook payload is empty")
)

type Parser interface {
	EventType(r *http.Request) gitlab.EventType
	Parse(eventType gitlab.EventType, payload []byte) (any, error)
}

type EventHandler interface {
	Handle(ctx context.Context, event any) error
}

type EventHandlerFunc func(ctx context.Context, event any) error

func (f EventHandlerFunc) Handle(ctx context.Context, event any) error {
	return f(ctx, event)
}

type Handler struct {
	parser   Parser
	mu       sync.RWMutex
	handlers map[gitlab.EventType][]EventHandler
}

func NewHandler(parser Parser) (*Handler, error) {
	if parser == nil {
		return nil, ErrNilParser
	}

	return &Handler{
		parser:   parser,
		handlers: make(map[gitlab.EventType][]EventHandler),
	}, nil
}

func (h *Handler) Register(eventType gitlab.EventType, handler EventHandler) error {
	if handler == nil {
		return ErrNilHandler
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	h.handlers[eventType] = append(h.handlers[eventType], handler)
	return nil
}

func (h *Handler) RegisterFunc(eventType gitlab.EventType, handler EventHandlerFunc) error {
	if handler == nil {
		return ErrNilHandler
	}

	return h.Register(eventType, handler)
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	payload, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read webhook payload", http.StatusBadRequest)
		return
	}
	if len(payload) == 0 {
		http.Error(w, ErrEmptyPayload.Error(), http.StatusBadRequest)
		return
	}

	eventType := h.parser.EventType(r)
	event, err := h.parser.Parse(eventType, payload)
	if err != nil {
		http.Error(w, "failed to parse webhook payload", http.StatusBadRequest)
		return
	}

	h.mu.RLock()
	handlers := append([]EventHandler(nil), h.handlers[eventType]...)
	h.mu.RUnlock()

	for _, handler := range handlers {
		if err := handler.Handle(r.Context(), event); err != nil {
			http.Error(w, "handler execution failed", http.StatusInternalServerError)
			return
		}
	}

	w.WriteHeader(http.StatusNoContent)
}
