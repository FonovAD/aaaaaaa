package webhook

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
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

type registrarMocks struct {
	Source *MockProjectSource
	Client *MockProjectHookClient
}

func newRegistrarMocks(t *testing.T) *registrarMocks {
	ctrl := gomock.NewController(t)
	return &registrarMocks{
		Source: NewMockProjectSource(ctrl),
		Client: NewMockProjectHookClient(ctrl),
	}
}

type hookRegistrarCtorOut struct {
	T   *testing.T
	Err error
}

func TestNewHookRegistrar(t *testing.T) {
	t.Parallel()

	emptyURLSpec := baseSpec()
	emptyURLSpec.URL = "   "

	tests := []struct {
		name   string
		spec   HookSpec
		setup  func(m *registrarMocks) (source ProjectSource, client ProjectHookClient)
		assert func(o *hookRegistrarCtorOut)
	}{
		{
			name: "nil_project_source",
			spec: baseSpec(),
			setup: func(m *registrarMocks) (ProjectSource, ProjectHookClient) {
				return nil, m.Client
			},
			assert: func(o *hookRegistrarCtorOut) {
				require.ErrorIs(o.T, o.Err, ErrNilProjectSource)
			},
		},
		{
			name: "nil_hook_client",
			spec: baseSpec(),
			setup: func(m *registrarMocks) (ProjectSource, ProjectHookClient) {
				return m.Source, nil
			},
			assert: func(o *hookRegistrarCtorOut) {
				require.ErrorIs(o.T, o.Err, ErrNilHookClient)
			},
		},
		{
			name: "empty_hook_url",
			spec: emptyURLSpec,
			setup: func(m *registrarMocks) (ProjectSource, ProjectHookClient) {
				return m.Source, m.Client
			},
			assert: func(o *hookRegistrarCtorOut) {
				require.ErrorIs(o.T, o.Err, ErrEmptyHookURL)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := newRegistrarMocks(t)
			source, client := tt.setup(m)
			_, err := NewHookRegistrar(source, client, tt.spec)

			o := &hookRegistrarCtorOut{T: t, Err: err}
			tt.assert(o)
		})
	}
}

type hookRegistrarReconcileOut struct {
	T   *testing.T
	Err error
}

func TestHookRegistrar_Reconcile(t *testing.T) {
	t.Parallel()

	spec := baseSpec()

	tests := []struct {
		name   string
		setup  func(m *registrarMocks)
		assert func(o *hookRegistrarReconcileOut)
	}{
		{
			name: "creates_missing_hook",
			setup: func(m *registrarMocks) {
				m.Source.EXPECT().ListProjectIDs(gomock.Any()).Return([]int64{101}, nil)
				m.Client.EXPECT().ListProjectHooks(gomock.Any(), int64(101)).Return([]*gitlab.ProjectHook{}, nil)
				m.Client.EXPECT().AddProjectHook(gomock.Any(), int64(101), gomock.Any()).
					Return(&gitlab.ProjectHook{ID: 1}, nil)
			},
			assert: func(o *hookRegistrarReconcileOut) {
				require.NoError(o.T, o.Err)
			},
		},
		{
			name: "updates_outdated_hook",
			setup: func(m *registrarMocks) {
				outdated := hookFromSpec(11, spec)
				outdated.PipelineEvents = false

				m.Source.EXPECT().ListProjectIDs(gomock.Any()).Return([]int64{202}, nil)
				m.Client.EXPECT().ListProjectHooks(gomock.Any(), int64(202)).Return([]*gitlab.ProjectHook{outdated}, nil)
				m.Client.EXPECT().EditProjectHook(gomock.Any(), int64(202), int64(11), gomock.Any()).
					Return(hookFromSpec(11, spec), nil)
			},
			assert: func(o *hookRegistrarReconcileOut) {
				require.NoError(o.T, o.Err)
			},
		},
		{
			name: "leaves_up_to_date_hook",
			setup: func(m *registrarMocks) {
				m.Source.EXPECT().ListProjectIDs(gomock.Any()).Return([]int64{303}, nil)
				m.Client.EXPECT().ListProjectHooks(gomock.Any(), int64(303)).Return([]*gitlab.ProjectHook{hookFromSpec(21, spec)}, nil)
			},
			assert: func(o *hookRegistrarReconcileOut) {
				require.NoError(o.T, o.Err)
			},
		},
		{
			name: "retries_then_succeeds",
			setup: func(m *registrarMocks) {
				m.Source.EXPECT().ListProjectIDs(gomock.Any()).Return([]int64{404}, nil)
				m.Client.EXPECT().ListProjectHooks(gomock.Any(), int64(404)).Return(nil, errors.New("transient"))
				m.Client.EXPECT().ListProjectHooks(gomock.Any(), int64(404)).Return(nil, errors.New("transient"))
				m.Client.EXPECT().ListProjectHooks(gomock.Any(), int64(404)).Return([]*gitlab.ProjectHook{}, nil)
				m.Client.EXPECT().AddProjectHook(gomock.Any(), int64(404), gomock.Any()).
					Return(&gitlab.ProjectHook{ID: 4}, nil)
			},
			assert: func(o *hookRegistrarReconcileOut) {
				require.NoError(o.T, o.Err)
			},
		},
		{
			name: "failure_after_all_retries",
			setup: func(m *registrarMocks) {
				m.Source.EXPECT().ListProjectIDs(gomock.Any()).Return([]int64{1, 2}, nil)
				m.Client.EXPECT().ListProjectHooks(gomock.Any(), int64(1)).Return(nil, errors.New("list failed")).Times(1 + reconcileRetries)
				m.Client.EXPECT().ListProjectHooks(gomock.Any(), int64(2)).Return([]*gitlab.ProjectHook{}, nil)
				m.Client.EXPECT().AddProjectHook(gomock.Any(), int64(2), gomock.Any()).Return(&gitlab.ProjectHook{ID: 2}, nil)
			},
			assert: func(o *hookRegistrarReconcileOut) {
				require.Error(o.T, o.Err)
				var recErr *ReconcileError
				require.ErrorAs(o.T, o.Err, &recErr)
				require.Len(o.T, recErr.Failures, 1)
				require.Error(o.T, recErr.Failures[1])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := newRegistrarMocks(t)
			r, err := NewHookRegistrar(m.Source, m.Client, spec)
			require.NoError(t, err)

			tt.setup(m)

			err = r.Reconcile(context.Background())
			o := &hookRegistrarReconcileOut{T: t, Err: err}
			tt.assert(o)
		})
	}
}
