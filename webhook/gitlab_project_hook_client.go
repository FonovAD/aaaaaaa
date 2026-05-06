package webhook

import (
	"context"
	"errors"

	gitlab "gitlab.com/gitlab-org/api/client-go"
)

var (
	ErrNilGitLabClient    = errors.New("gitlab client is nil")
	ErrNilProjectsService = errors.New("gitlab projects service is nil")
)

// GitLabProjectHookClient реализует ProjectHookClient через Projects API GitLab.
type GitLabProjectHookClient struct {
	projects gitlab.ProjectsServiceInterface
}

// NewGitLabProjectHookClient создаёт клиент на основе oauth2/gitlab Client.
func NewGitLabProjectHookClient(c *gitlab.Client) (*GitLabProjectHookClient, error) {
	if c == nil {
		return nil, ErrNilGitLabClient
	}
	return &GitLabProjectHookClient{projects: c.Projects}, nil
}

// NewGitLabProjectHookClientWithProjects подставляет сервис Projects (удобно для тестов со сгенерированным моком GitLab).
func NewGitLabProjectHookClientWithProjects(projects gitlab.ProjectsServiceInterface) (*GitLabProjectHookClient, error) {
	if projects == nil {
		return nil, ErrNilProjectsService
	}
	return &GitLabProjectHookClient{projects: projects}, nil
}

func (c *GitLabProjectHookClient) ListProjectHooks(ctx context.Context, projectID int64) ([]*gitlab.ProjectHook, error) {
	const perPage int64 = 100
	var all []*gitlab.ProjectHook
	var page int64 = 1
	for {
		hooks, resp, err := c.projects.ListProjectHooks(
			projectID,
			&gitlab.ListProjectHooksOptions{
				ListOptions: gitlab.ListOptions{Page: page, PerPage: perPage},
			},
			gitlab.WithContext(ctx),
		)
		if err != nil {
			return nil, err
		}
		all = append(all, hooks...)
		if len(hooks) < int(perPage) || resp == nil || resp.NextPage == 0 {
			break
		}
		page++
	}
	return all, nil
}

func (c *GitLabProjectHookClient) AddProjectHook(ctx context.Context, projectID int64, opt *gitlab.AddProjectHookOptions) (*gitlab.ProjectHook, error) {
	hook, _, err := c.projects.AddProjectHook(projectID, opt, gitlab.WithContext(ctx))
	return hook, err
}

func (c *GitLabProjectHookClient) EditProjectHook(ctx context.Context, projectID int64, hookID int64, opt *gitlab.EditProjectHookOptions) (*gitlab.ProjectHook, error) {
	hook, _, err := c.projects.EditProjectHook(projectID, hookID, opt, gitlab.WithContext(ctx))
	return hook, err
}
