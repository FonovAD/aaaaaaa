package webhook

import (
	"context"
	"errors"
	"fmt"
	"strings"

	gitlab "gitlab.com/gitlab-org/api/client-go"
)

var (
	ErrNilProjectSource = errors.New("project source is nil")
	ErrNilHookClient    = errors.New("hook client is nil")
	ErrEmptyHookURL     = errors.New("hook url is empty")
)

type ProjectSource interface {
	ListProjectIDs(ctx context.Context) ([]int64, error)
}

type ProjectHookClient interface {
	ListProjectHooks(ctx context.Context, projectID int64) ([]*gitlab.ProjectHook, error)
	AddProjectHook(ctx context.Context, projectID int64, opt *gitlab.AddProjectHookOptions) (*gitlab.ProjectHook, error)
	EditProjectHook(ctx context.Context, projectID int64, hookID int64, opt *gitlab.EditProjectHookOptions) (*gitlab.ProjectHook, error)
}

type HookSpec struct {
	URL                    string
	Token                  string
	Name                   string
	Description            string
	EnableSSLVerification  bool
	PushEvents             bool
	PushEventsBranchFilter string
	MergeRequestsEvents    bool
	TagPushEvents          bool
	NoteEvents             bool
	JobEvents              bool
	PipelineEvents         bool
	WikiPageEvents         bool
	DeploymentEvents       bool
	ReleasesEvents         bool
}

type ReconcileReport struct {
	TotalProjects int
	Created       int
	Updated       int
	Unchanged     int
	Failed        int
}

type ReconcileError struct {
	Failures map[int64]error
}

func (e *ReconcileError) Error() string {
	return fmt.Sprintf("reconcile failed for %d project(s)", len(e.Failures))
}

type HookRegistrar struct {
	source ProjectSource
	client ProjectHookClient
	spec   HookSpec
}

func NewHookRegistrar(source ProjectSource, client ProjectHookClient, spec HookSpec) (*HookRegistrar, error) {
	if source == nil {
		return nil, ErrNilProjectSource
	}
	if client == nil {
		return nil, ErrNilHookClient
	}
	if strings.TrimSpace(spec.URL) == "" {
		return nil, ErrEmptyHookURL
	}

	return &HookRegistrar{
		source: source,
		client: client,
		spec:   spec,
	}, nil
}

func (r *HookRegistrar) Reconcile(ctx context.Context) (ReconcileReport, error) {
	projectIDs, err := r.source.ListProjectIDs(ctx)
	if err != nil {
		return ReconcileReport{}, err
	}

	report := ReconcileReport{TotalProjects: len(projectIDs)}
	failures := make(map[int64]error)

	for _, projectID := range projectIDs {
		if err := r.reconcileProject(ctx, projectID, &report); err != nil {
			report.Failed++
			failures[projectID] = err
		}
	}

	if len(failures) > 0 {
		return report, &ReconcileError{Failures: failures}
	}

	return report, nil
}

func (r *HookRegistrar) reconcileProject(ctx context.Context, projectID int64, report *ReconcileReport) error {
	hooks, err := r.client.ListProjectHooks(ctx, projectID)
	if err != nil {
		return err
	}

	existing := r.findManagedHook(hooks)
	if existing == nil {
		_, err := r.client.AddProjectHook(ctx, projectID, r.newAddOptions())
		if err != nil {
			return err
		}
		report.Created++
		return nil
	}

	if r.matchesSpec(existing) {
		report.Unchanged++
		return nil
	}

	_, err = r.client.EditProjectHook(ctx, projectID, existing.ID, r.newEditOptions())
	if err != nil {
		return err
	}
	report.Updated++
	return nil
}

func (r *HookRegistrar) findManagedHook(hooks []*gitlab.ProjectHook) *gitlab.ProjectHook {
	for _, hook := range hooks {
		if hook == nil {
			continue
		}
		if hook.URL != r.spec.URL {
			continue
		}
		if r.spec.Name != "" && hook.Name != r.spec.Name {
			continue
		}
		return hook
	}
	return nil
}

func (r *HookRegistrar) matchesSpec(hook *gitlab.ProjectHook) bool {
	return hook.URL == r.spec.URL &&
		hook.Name == r.spec.Name &&
		hook.Description == r.spec.Description &&
		hook.EnableSSLVerification == r.spec.EnableSSLVerification &&
		hook.PushEvents == r.spec.PushEvents &&
		hook.PushEventsBranchFilter == r.spec.PushEventsBranchFilter &&
		hook.MergeRequestsEvents == r.spec.MergeRequestsEvents &&
		hook.TagPushEvents == r.spec.TagPushEvents &&
		hook.NoteEvents == r.spec.NoteEvents &&
		hook.JobEvents == r.spec.JobEvents &&
		hook.PipelineEvents == r.spec.PipelineEvents &&
		hook.WikiPageEvents == r.spec.WikiPageEvents &&
		hook.DeploymentEvents == r.spec.DeploymentEvents &&
		hook.ReleasesEvents == r.spec.ReleasesEvents
}

func (r *HookRegistrar) newAddOptions() *gitlab.AddProjectHookOptions {
	return &gitlab.AddProjectHookOptions{
		URL:                    gitlab.Ptr(r.spec.URL),
		Token:                  gitlab.Ptr(r.spec.Token),
		Name:                   gitlab.Ptr(r.spec.Name),
		Description:            gitlab.Ptr(r.spec.Description),
		EnableSSLVerification:  gitlab.Ptr(r.spec.EnableSSLVerification),
		PushEvents:             gitlab.Ptr(r.spec.PushEvents),
		PushEventsBranchFilter: gitlab.Ptr(r.spec.PushEventsBranchFilter),
		MergeRequestsEvents:    gitlab.Ptr(r.spec.MergeRequestsEvents),
		TagPushEvents:          gitlab.Ptr(r.spec.TagPushEvents),
		NoteEvents:             gitlab.Ptr(r.spec.NoteEvents),
		JobEvents:              gitlab.Ptr(r.spec.JobEvents),
		PipelineEvents:         gitlab.Ptr(r.spec.PipelineEvents),
		WikiPageEvents:         gitlab.Ptr(r.spec.WikiPageEvents),
		DeploymentEvents:       gitlab.Ptr(r.spec.DeploymentEvents),
		ReleasesEvents:         gitlab.Ptr(r.spec.ReleasesEvents),
	}
}

func (r *HookRegistrar) newEditOptions() *gitlab.EditProjectHookOptions {
	return &gitlab.EditProjectHookOptions{
		URL:                    gitlab.Ptr(r.spec.URL),
		Token:                  gitlab.Ptr(r.spec.Token),
		Name:                   gitlab.Ptr(r.spec.Name),
		Description:            gitlab.Ptr(r.spec.Description),
		EnableSSLVerification:  gitlab.Ptr(r.spec.EnableSSLVerification),
		PushEvents:             gitlab.Ptr(r.spec.PushEvents),
		PushEventsBranchFilter: gitlab.Ptr(r.spec.PushEventsBranchFilter),
		MergeRequestsEvents:    gitlab.Ptr(r.spec.MergeRequestsEvents),
		TagPushEvents:          gitlab.Ptr(r.spec.TagPushEvents),
		NoteEvents:             gitlab.Ptr(r.spec.NoteEvents),
		JobEvents:              gitlab.Ptr(r.spec.JobEvents),
		PipelineEvents:         gitlab.Ptr(r.spec.PipelineEvents),
		WikiPageEvents:         gitlab.Ptr(r.spec.WikiPageEvents),
		DeploymentEvents:       gitlab.Ptr(r.spec.DeploymentEvents),
		ReleasesEvents:         gitlab.Ptr(r.spec.ReleasesEvents),
	}
}

