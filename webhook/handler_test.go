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

type newHandlerOut struct {
	T       *testing.T
	Handler *Handler
	Err     error
	WantErr error
}

func TestNewHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		parserFn func(*testing.T) Parser
		wantErr  error
		assert   func(o *newHandlerOut)
	}{
		{
			name:     "nil_parser",
			parserFn: func(*testing.T) Parser { return nil },
			wantErr:  ErrNilParser,
			assert: func(o *newHandlerOut) {
				if !errors.Is(o.Err, o.WantErr) {
					o.T.Fatalf("want error %v, got %v", o.WantErr, o.Err)
				}
			},
		},
		{
			name: "ok",
			parserFn: func(t *testing.T) Parser {
				return NewMockParser(gomock.NewController(t))
			},
			wantErr: nil,
			assert: func(o *newHandlerOut) {
				if !errors.Is(o.Err, o.WantErr) {
					o.T.Fatalf("want error %v, got %v", o.WantErr, o.Err)
				}
				if o.Handler == nil {
					o.T.Fatal("expected non-nil Handler")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := NewHandler(tt.parserFn(t))
			o := &newHandlerOut{T: t, Handler: got, Err: err, WantErr: tt.wantErr}
			tt.assert(o)
		})
	}
}

type eventHandlerFuncOut struct {
	T      *testing.T
	Err    error
	Called bool
}

func TestEventHandlerFunc_Handle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		event  any
		assert func(o *eventHandlerFuncOut)
	}{
		{
			name:  "forwards_payload",
			event: "payload",
			assert: func(o *eventHandlerFuncOut) {
				if o.Err != nil {
					o.T.Fatalf("unexpected error: %v", o.Err)
				}
				if !o.Called {
					o.T.Fatal("expected handler to run")
				}
			},
		},
		{
			name:  "wrong_event_returns_error_via_func_body",
			event: "other",
			assert: func(o *eventHandlerFuncOut) {
				if o.Err == nil {
					o.T.Fatal("expected error, got nil")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var called bool
			f := EventHandlerFunc(func(_ context.Context, event any) error {
				called = true
				if event != "payload" {
					return errors.New("bad event")
				}
				return nil
			})

			err := f.Handle(context.Background(), tt.event)

			o := &eventHandlerFuncOut{T: t, Err: err, Called: called}
			tt.assert(o)
		})
	}
}

type handlerRegisterNilOut struct {
	T   *testing.T
	Err error
}

func TestHandler_Register_rejects_nil(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		call   func(*Handler, gitlab.EventType) error
		assert func(o *handlerRegisterNilOut)
	}{
		{
			name: "Register",
			call: func(h *Handler, et gitlab.EventType) error { return h.Register(et, nil) },
			assert: func(o *handlerRegisterNilOut) {
				if !errors.Is(o.Err, ErrNilHandler) {
					o.T.Fatalf("want ErrNilHandler, got %v", o.Err)
				}
			},
		},
		{
			name: "RegisterFunc",
			call: func(h *Handler, et gitlab.EventType) error { return h.RegisterFunc(et, nil) },
			assert: func(o *handlerRegisterNilOut) {
				if !errors.Is(o.Err, ErrNilHandler) {
					o.T.Fatalf("want ErrNilHandler, got %v", o.Err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			h, err := NewHandler(NewMockParser(ctrl))
			if err != nil {
				t.Fatalf("NewHandler: %v", err)
			}

			err = tt.call(h, gitlab.EventTypePush)
			o := &handlerRegisterNilOut{T: t, Err: err}
			tt.assert(o)
		})
	}
}

type handlerServeHTTPOut struct {
	T        *testing.T
	Recorder *httptest.ResponseRecorder
	WantCode int
}

func assertHandlerServeHTTPStatus(o *handlerServeHTTPOut) {
	if o.Recorder.Code != o.WantCode {
		o.T.Fatalf("status: want %d, got %d", o.WantCode, o.Recorder.Code)
	}
}

func TestHandler_ServeHTTP(t *testing.T) {
	t.Parallel()

	const pushPayload = `{"dummy":"payload"}`
	pushEvent := &gitlab.PushEvent{Ref: "refs/heads/main"}

	tests := []struct {
		name   string
		body   string
		setup  func(t *testing.T, ctrl *gomock.Controller, payload string, h *Handler)
		want   int
		assert func(o *handlerServeHTTPOut)
	}{
		{
			name: "dispatches_registered_handlers_same_event_type",
			body: pushPayload,
			setup: func(t *testing.T, ctrl *gomock.Controller, payload string, h *Handler) {
				handlerA := NewMockEventHandler(ctrl)
				handlerB := NewMockEventHandler(ctrl)
				otherHandler := NewMockEventHandler(ctrl)
				if err := h.Register(gitlab.EventTypePush, handlerA); err != nil {
					t.Fatalf("Register: %v", err)
				}
				if err := h.Register(gitlab.EventTypePush, handlerB); err != nil {
					t.Fatalf("Register: %v", err)
				}
				if err := h.Register(gitlab.EventTypeMergeRequest, otherHandler); err != nil {
					t.Fatalf("Register: %v", err)
				}

				parser := h.parser.(*MockParser)
				parser.EXPECT().EventType(gomock.Any()).Return(gitlab.EventTypePush)
				parser.EXPECT().Parse(gitlab.EventTypePush, []byte(payload)).Return(pushEvent, nil)
				handlerA.EXPECT().Handle(gomock.Any(), pushEvent).Return(nil)
				handlerB.EXPECT().Handle(gomock.Any(), pushEvent).Return(nil)
			},
			want:   http.StatusNoContent,
			assert: assertHandlerServeHTTPStatus,
		},
		{
			name: "register_func_delegate",
			body: `{"x":1}`,
			setup: func(t *testing.T, ctrl *gomock.Controller, payload string, h *Handler) {
				handlerMock := NewMockEventHandler(ctrl)
				if err := h.RegisterFunc(gitlab.EventTypePush, handlerMock.Handle); err != nil {
					t.Fatalf("RegisterFunc: %v", err)
				}
				ev := &gitlab.PushEvent{Ref: "refs/heads/main"}
				parser := h.parser.(*MockParser)
				parser.EXPECT().EventType(gomock.Any()).Return(gitlab.EventTypePush)
				parser.EXPECT().Parse(gitlab.EventTypePush, []byte(payload)).Return(ev, nil)
				handlerMock.EXPECT().Handle(gomock.Any(), ev).Return(nil)
			},
			want:   http.StatusNoContent,
			assert: assertHandlerServeHTTPStatus,
		},
		{
			name: "parse_error_bad_request",
			body: `{"x":1}`,
			setup: func(t *testing.T, ctrl *gomock.Controller, payload string, h *Handler) {
				parser := h.parser.(*MockParser)
				parser.EXPECT().EventType(gomock.Any()).Return(gitlab.EventTypePush)
				parser.EXPECT().Parse(gitlab.EventTypePush, []byte(payload)).Return(nil, errors.New("boom"))
			},
			want:   http.StatusBadRequest,
			assert: assertHandlerServeHTTPStatus,
		},
		{
			name: "handler_error_internal_server_error",
			body: `{"x":1}`,
			setup: func(t *testing.T, ctrl *gomock.Controller, payload string, h *Handler) {
				handlerMock := NewMockEventHandler(ctrl)
				if err := h.Register(gitlab.EventTypePush, handlerMock); err != nil {
					t.Fatalf("Register: %v", err)
				}
				ev := &gitlab.PushEvent{Ref: "refs/heads/main"}
				parser := h.parser.(*MockParser)
				parser.EXPECT().EventType(gomock.Any()).Return(gitlab.EventTypePush)
				parser.EXPECT().Parse(gitlab.EventTypePush, []byte(payload)).Return(ev, nil)
				handlerMock.EXPECT().Handle(gomock.Any(), ev).Return(errors.New("failed"))
			},
			want:   http.StatusInternalServerError,
			assert: assertHandlerServeHTTPStatus,
		},
		{
			name:   "empty_payload_bad_request",
			body:   "",
			setup:  func(t *testing.T, ctrl *gomock.Controller, payload string, h *Handler) {},
			want:   http.StatusBadRequest,
			assert: assertHandlerServeHTTPStatus,
		},
		{
			name: "no_handlers_still_no_content",
			body: `{"x":1}`,
			setup: func(t *testing.T, ctrl *gomock.Controller, payload string, h *Handler) {
				ev := &gitlab.PushEvent{Ref: "refs/heads/main"}
				parser := h.parser.(*MockParser)
				parser.EXPECT().EventType(gomock.Any()).Return(gitlab.EventTypePush)
				parser.EXPECT().Parse(gitlab.EventTypePush, []byte(payload)).Return(ev, nil)
			},
			want:   http.StatusNoContent,
			assert: assertHandlerServeHTTPStatus,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			parser := NewMockParser(ctrl)
			h, err := NewHandler(parser)
			if err != nil {
				t.Fatalf("NewHandler: %v", err)
			}

			tt.setup(t, ctrl, tt.body, h)

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(tt.body))
			h.ServeHTTP(rec, req)

			o := &handlerServeHTTPOut{T: t, Recorder: rec, WantCode: tt.want}
			tt.assert(o)
		})
	}
}
