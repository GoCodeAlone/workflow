package module

import (
	"fmt"
	"os"
	"strings"

	"github.com/CrisisTextLine/modular"
)

// GitLabClientModule is a workflow module that creates a GitLabClient and
// registers it in the service registry under its module name.
//
// Config:
//
//   - name: gitlab-client
//     type: gitlab.client
//     config:
//     url: "https://gitlab.com"   # or self-hosted URL; use "mock://" for testing
//     token: "${GITLAB_TOKEN}"
type GitLabClientModule struct {
	name   string
	config map[string]any
	client *GitLabClient
}

// NewGitLabClientModule creates a new gitlab.client module.
func NewGitLabClientModule(name string, cfg map[string]any) *GitLabClientModule {
	return &GitLabClientModule{name: name, config: cfg}
}

// Name returns the module name.
func (m *GitLabClientModule) Name() string { return m.name }

// Init resolves configuration, creates the client, and registers it as a service.
func (m *GitLabClientModule) Init(app modular.Application) error {
	rawURL, _ := m.config["url"].(string)
	if rawURL == "" {
		rawURL = "https://gitlab.com"
	}

	token, _ := m.config["token"].(string)
	token = expandEnvVars(token)

	m.client = NewGitLabClient(rawURL, token)

	return app.RegisterService(m.name, m.client)
}

// ProvidesServices declares the service provided by this module.
func (m *GitLabClientModule) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        m.name,
			Description: "GitLab API client: " + m.name,
			Instance:    m.client,
		},
	}
}

// expandEnvVars replaces ${VAR} and $VAR references with environment values.
func expandEnvVars(s string) string {
	if s == "" {
		return s
	}
	if strings.HasPrefix(s, "${") && strings.HasSuffix(s, "}") {
		key := s[2 : len(s)-1]
		if v := os.Getenv(key); v != "" {
			return v
		}
	}
	return os.ExpandEnv(s)
}

// gitLabClientFromService looks up a *GitLabClient from the application service registry.
func gitLabClientFromService(app modular.Application, clientName string) (*GitLabClient, error) {
	var raw any
	if err := app.GetService(clientName, &raw); err != nil {
		return nil, fmt.Errorf("gitlab: service %q not found: %w", clientName, err)
	}
	client, ok := raw.(*GitLabClient)
	if !ok {
		return nil, fmt.Errorf("gitlab: service %q is not a *GitLabClient", clientName)
	}
	return client, nil
}
