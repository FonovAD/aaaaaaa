package webhook

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	gitlab "gitlab.com/gitlab-org/api/client-go"
	"go.uber.org/mock/gomock"
)

func TestNewHandler(t *testing.T) {
	t.Parallel()

	_, err := NewHandler(nil)
	if !errors.Is(err, ErrNilParser) {
		t.Fatalf("expected ErrNilParser, got %v", err)
	}

	ctrl := gomock.NewController(t)
	parser := NewMockParser(ctrl)
	got, err := NewHandler(parser)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected handler, got nil")
	}
}

func TestEventHandlerFunc_Handle(t *testing.T) {
	t.Parallel()

	called := false
	f := EventHandlerFunc(func(_ context.Context, event any) error {
		called = true
		if event != "payload" {
			t.Fatalf("unexpected event: %v", event)
		}
		return nil
	})

	if err := f.Handle(context.Background(), "payload"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("expected handler function to be called")
	}
}

func TestRegisterAndServeHTTP_DispatchesByEventType(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	const eventType = gitlab.EventTypePush
	event := &gitlab.PushEvent{Ref: "refs/heads/main"}
	payload := `{"dummy":"payload"}`

	parser := NewMockParser(ctrl)
	h, err := NewHandler(parser)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	handlerA := NewMockEventHandler(ctrl)
	handlerB := NewMockEventHandler(ctrl)
	otherEventHandler := NewMockEventHandler(ctrl)

	if err := h.Register(eventType, handlerA); err != nil {
		t.Fatalf("unexpected register error: %v", err)
	}
	if err := h.Register(eventType, handlerB); err != nil {
		t.Fatalf("unexpected register error: %v", err)
	}

	if err := h.Register(gitlab.EventTypeMergeRequest, otherEventHandler); err != nil {
		t.Fatalf("unexpected register error: %v", err)
	}

	parser.EXPECT().EventType(gomock.Any()).Return(eventType)
	parser.EXPECT().Parse(eventType, []byte(payload)).Return(event, nil)
	handlerA.EXPECT().Handle(gomock.Any(), event).Return(nil)
	handlerB.EXPECT().Handle(gomock.Any(), event).Return(nil)

	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(payload))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected %d, got %d", http.StatusNoContent, rec.Code)
	}
}

func TestRegister_RejectsNilHandler(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	h, err := NewHandler(NewMockParser(ctrl))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := h.Register(gitlab.EventTypePush, nil); !errors.Is(err, ErrNilHandler) {
		t.Fatalf("expected ErrNilHandler, got %v", err)
	}
	if err := h.RegisterFunc(gitlab.EventTypePush, nil); !errors.Is(err, ErrNilHandler) {
		t.Fatalf("expected ErrNilHandler, got %v", err)
	}
}

func TestRegisterFunc_Success(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	parser := NewMockParser(ctrl)
	handlerMock := NewMockEventHandler(ctrl)

	h, err := NewHandler(parser)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := h.RegisterFunc(gitlab.EventTypePush, handlerMock.Handle); err != nil {
		t.Fatalf("unexpected register error: %v", err)
	}

	payload := `{"x":1}`
	event := &gitlab.PushEvent{Ref: "refs/heads/main"}
	parser.EXPECT().EventType(gomock.Any()).Return(gitlab.EventTypePush)
	parser.EXPECT().Parse(gitlab.EventTypePush, []byte(payload)).Return(event, nil)
	handlerMock.EXPECT().Handle(gomock.Any(), event).Return(nil)

	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(payload))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected %d, got %d", http.StatusNoContent, rec.Code)
	}
}

func TestServeHTTP_ParseError(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	parser := NewMockParser(ctrl)
	h, err := NewHandler(parser)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	payload := `{"x":1}`
	parser.EXPECT().EventType(gomock.Any()).Return(gitlab.EventTypePush)
	parser.EXPECT().Parse(gitlab.EventTypePush, []byte(payload)).Return(nil, errors.New("boom"))

	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(payload))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestServeHTTP_HandlerError(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	parser := NewMockParser(ctrl)
	h, err := NewHandler(parser)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	handler := NewMockEventHandler(ctrl)
	if err := h.Register(gitlab.EventTypePush, handler); err != nil {
		t.Fatalf("unexpected register error: %v", err)
	}

	event := &gitlab.PushEvent{Ref: "refs/heads/main"}
	payload := `{"x":1}`
	parser.EXPECT().EventType(gomock.Any()).Return(gitlab.EventTypePush)
	parser.EXPECT().Parse(gitlab.EventTypePush, []byte(payload)).Return(event, nil)
	handler.EXPECT().Handle(gomock.Any(), event).Return(errors.New("failed"))

	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(payload))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
	}
}

func TestServeHTTP_EmptyPayload(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	parser := NewMockParser(ctrl)

	h, err := NewHandler(parser)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(""))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestServeHTTP_NoRegisteredHandlers(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	parser := NewMockParser(ctrl)

	h, err := NewHandler(parser)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	payload := `{"x":1}`
	event := &gitlab.PushEvent{Ref: "refs/heads/main"}

	parser.EXPECT().EventType(gomock.Any()).Return(gitlab.EventTypePush)
	parser.EXPECT().Parse(gitlab.EventTypePush, []byte(payload)).Return(event, nil)

	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(payload))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected %d, got %d", http.StatusNoContent, rec.Code)
	}
}

