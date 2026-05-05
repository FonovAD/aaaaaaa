package webhook

import (
	"net/http"

	gitlab "gitlab.com/gitlab-org/api/client-go"
)

type GitLabParser struct{}

func NewGitLabParser() *GitLabParser {
	return &GitLabParser{}
}

func (p *GitLabParser) EventType(r *http.Request) gitlab.EventType {
	return gitlab.WebhookEventType(r)
}

func (p *GitLabParser) Parse(eventType gitlab.EventType, payload []byte) (any, error) {
	return gitlab.ParseWebhook(eventType, payload)
}
