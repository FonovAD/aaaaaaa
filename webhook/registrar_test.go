package webhook

import (
	"context"
	"errors"
	"testing"

	gitlab "gitlab.com/gitlab-org/api/client-go"
	"go.uber.org/mock/gomock"
)

func baseSpec() HookSpec {
	return HookSpec{
		URL:                    "https://example.com/gitlab/webhook",
		Token:                  "secret",
		Name:                   "managed-hook",
		Description:            "managed by service",
		EnableSSLVerification:  true,
		PushEvents:             true,
		PushEventsBranchFilter: "main",
		MergeRequestsEvents:    true,
		TagPushEvents:          false,
		NoteEvents:             false,
		JobEvents:              true,
		PipelineEvents:         true,
		WikiPageEvents:         false,
		DeploymentEvents:       true,
		ReleasesEvents:         false,
	}
}

func hookFromSpec(id int64, s HookSpec) *gitlab.ProjectHook {
	return &gitlab.ProjectHook{
		ID:                     id,
		URL:                    s.URL,
		Name:                   s.Name,
		Description:            s.Description,
		EnableSSLVerification:  s.EnableSSLVerification,
		PushEvents:             s.PushEvents,
		PushEventsBranchFilter: s.PushEventsBranchFilter,
		MergeRequestsEvents:    s.MergeRequestsEvents,
		TagPushEvents:          s.TagPushEvents,
		NoteEvents:             s.NoteEvents,
		JobEvents:              s.JobEvents,
		PipelineEvents:         s.PipelineEvents,
		WikiPageEvents:         s.WikiPageEvents,
		DeploymentEvents:       s.DeploymentEvents,
		ReleasesEvents:         s.ReleasesEvents,
	}
}

func TestNewHookRegistrar(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	source := NewMockProjectSource(ctrl)
	client := NewMockProjectHookClient(ctrl)

	if _, err := NewHookRegistrar(nil, client, baseSpec()); !errors.Is(err, ErrNilProjectSource) {
		t.Fatalf("expected ErrNilProjectSource, got %v", err)
	}
	if _, err := NewHookRegistrar(source, nil, baseSpec()); !errors.Is(err, ErrNilHookClient) {
		t.Fatalf("expected ErrNilHookClient, got %v", err)
	}

	spec := baseSpec()
	spec.URL = "   "
	if _, err := NewHookRegistrar(source, client, spec); !errors.Is(err, ErrEmptyHookURL) {
		t.Fatalf("expected ErrEmptyHookURL, got %v", err)
	}
}

func TestReconcile_CreatesMissingHook(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	source := NewMockProjectSource(ctrl)
	client := NewMockProjectHookClient(ctrl)
	spec := baseSpec()

	r, err := NewHookRegistrar(source, client, spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	source.EXPECT().ListProjectIDs(gomock.Any()).Return([]int64{101}, nil)
	client.EXPECT().ListProjectHooks(gomock.Any(), int64(101)).Return([]*gitlab.ProjectHook{}, nil)
	client.EXPECT().AddProjectHook(gomock.Any(), int64(101), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ int64, opt *gitlab.AddProjectHookOptions) (*gitlab.ProjectHook, error) {
			if opt == nil || opt.URL == nil || *opt.URL != spec.URL {
				t.Fatalf("unexpected add options: %#v", opt)
			}
			return &gitlab.ProjectHook{ID: 1}, nil
		})

	report, err := r.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Created != 1 || report.Unchanged != 0 || report.Updated != 0 || report.Failed != 0 {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestReconcile_UpdatesOutdatedHook(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	source := NewMockProjectSource(ctrl)
	client := NewMockProjectHookClient(ctrl)
	spec := baseSpec()

	r, err := NewHookRegistrar(source, client, spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	outdated := hookFromSpec(11, spec)
	outdated.PipelineEvents = false

	source.EXPECT().ListProjectIDs(gomock.Any()).Return([]int64{202}, nil)
	client.EXPECT().ListProjectHooks(gomock.Any(), int64(202)).Return([]*gitlab.ProjectHook{outdated}, nil)
	client.EXPECT().EditProjectHook(gomock.Any(), int64(202), int64(11), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ int64, _ int64, opt *gitlab.EditProjectHookOptions) (*gitlab.ProjectHook, error) {
			if opt == nil || opt.PipelineEvents == nil || !*opt.PipelineEvents {
				t.Fatalf("unexpected edit options: %#v", opt)
			}
			return hookFromSpec(11, spec), nil
		})

	report, err := r.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Updated != 1 || report.Created != 0 || report.Unchanged != 0 || report.Failed != 0 {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestReconcile_LeavesUpToDateHook(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	source := NewMockProjectSource(ctrl)
	client := NewMockProjectHookClient(ctrl)
	spec := baseSpec()

	r, err := NewHookRegistrar(source, client, spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	source.EXPECT().ListProjectIDs(gomock.Any()).Return([]int64{303}, nil)
	client.EXPECT().ListProjectHooks(gomock.Any(), int64(303)).Return([]*gitlab.ProjectHook{hookFromSpec(21, spec)}, nil)

	report, err := r.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Unchanged != 1 || report.Created != 0 || report.Updated != 0 || report.Failed != 0 {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestReconcile_ReturnsPartialFailure(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	source := NewMockProjectSource(ctrl)
	client := NewMockProjectHookClient(ctrl)
	spec := baseSpec()

	r, err := NewHookRegistrar(source, client, spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	source.EXPECT().ListProjectIDs(gomock.Any()).Return([]int64{1, 2}, nil)
	client.EXPECT().ListProjectHooks(gomock.Any(), int64(1)).Return(nil, errors.New("list failed"))
	client.EXPECT().ListProjectHooks(gomock.Any(), int64(2)).Return([]*gitlab.ProjectHook{}, nil)
	client.EXPECT().AddProjectHook(gomock.Any(), int64(2), gomock.Any()).Return(&gitlab.ProjectHook{ID: 2}, nil)

	report, err := r.Reconcile(context.Background())
	if err == nil {
		t.Fatal("expected reconcile error, got nil")
	}

	var recErr *ReconcileError
	if !errors.As(err, &recErr) {
		t.Fatalf("expected ReconcileError, got %T", err)
	}
	if len(recErr.Failures) != 1 || recErr.Failures[1] == nil {
		t.Fatalf("unexpected failures map: %#v", recErr.Failures)
	}
	if report.Failed != 1 || report.Created != 1 || report.TotalProjects != 2 {
		t.Fatalf("unexpected report: %+v", report)
	}
}

