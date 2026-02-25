package module

import (
	"context"
	"fmt"
	"strconv"

	"github.com/CrisisTextLine/modular"
)

// NewGitLabTriggerPipelineStepFactory returns a StepFactory for step.gitlab_trigger_pipeline.
//
//	- type: step.gitlab_trigger_pipeline
//	  config:
//	    client: gitlab-client     # name of the gitlab.client module
//	    project: "group/project"  # project path or numeric ID
//	    ref: main
//	    variables:
//	      KEY: value
func NewGitLabTriggerPipelineStepFactory() StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		clientName, _ := config["client"].(string)
		if clientName == "" {
			return nil, fmt.Errorf("gitlab_trigger_pipeline step %q: 'client' is required", name)
		}
		project, _ := config["project"].(string)
		if project == "" {
			return nil, fmt.Errorf("gitlab_trigger_pipeline step %q: 'project' is required", name)
		}
		ref, _ := config["ref"].(string)
		if ref == "" {
			ref = "main"
		}

		variables := make(map[string]string)
		if varsRaw, ok := config["variables"].(map[string]any); ok {
			for k, v := range varsRaw {
				variables[k] = fmt.Sprintf("%v", v)
			}
		}

		client, err := gitLabClientFromService(app, clientName)
		if err != nil {
			return nil, fmt.Errorf("gitlab_trigger_pipeline step %q: %w", name, err)
		}

		return &gitLabTriggerPipelineStep{
			name:      name,
			client:    client,
			project:   project,
			ref:       ref,
			variables: variables,
		}, nil
	}
}

type gitLabTriggerPipelineStep struct {
	name      string
	client    *GitLabClient
	project   string
	ref       string
	variables map[string]string
}

func (s *gitLabTriggerPipelineStep) Name() string { return s.name }

func (s *gitLabTriggerPipelineStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	pipeline, err := s.client.TriggerPipeline(s.project, s.ref, s.variables)
	if err != nil {
		return nil, fmt.Errorf("gitlab_trigger_pipeline step %q: %w", s.name, err)
	}

	return &StepResult{Output: map[string]any{
		"pipeline_id": pipeline.ID,
		"status":      pipeline.Status,
		"ref":         pipeline.Ref,
		"sha":         pipeline.SHA,
		"web_url":     pipeline.WebURL,
		"created_at":  pipeline.CreatedAt,
	}}, nil
}

// NewGitLabPipelineStatusStepFactory returns a StepFactory for step.gitlab_pipeline_status.
//
//	- type: step.gitlab_pipeline_status
//	  config:
//	    client: gitlab-client
//	    project: "group/project"
//	    pipeline_id: "42"   # string or int
func NewGitLabPipelineStatusStepFactory() StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		clientName, _ := config["client"].(string)
		if clientName == "" {
			return nil, fmt.Errorf("gitlab_pipeline_status step %q: 'client' is required", name)
		}
		project, _ := config["project"].(string)
		if project == "" {
			return nil, fmt.Errorf("gitlab_pipeline_status step %q: 'project' is required", name)
		}

		var pipelineID int
		switch v := config["pipeline_id"].(type) {
		case int:
			pipelineID = v
		case float64:
			pipelineID = int(v)
		case string:
			n, err := strconv.Atoi(v)
			if err != nil {
				return nil, fmt.Errorf("gitlab_pipeline_status step %q: invalid pipeline_id %q", name, v)
			}
			pipelineID = n
		}

		client, err := gitLabClientFromService(app, clientName)
		if err != nil {
			return nil, fmt.Errorf("gitlab_pipeline_status step %q: %w", name, err)
		}

		return &gitLabPipelineStatusStep{
			name:       name,
			client:     client,
			project:    project,
			pipelineID: pipelineID,
		}, nil
	}
}

type gitLabPipelineStatusStep struct {
	name       string
	client     *GitLabClient
	project    string
	pipelineID int
}

func (s *gitLabPipelineStatusStep) Name() string { return s.name }

func (s *gitLabPipelineStatusStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	pipeline, err := s.client.GetPipeline(s.project, s.pipelineID)
	if err != nil {
		return nil, fmt.Errorf("gitlab_pipeline_status step %q: %w", s.name, err)
	}

	return &StepResult{Output: map[string]any{
		"pipeline_id": pipeline.ID,
		"status":      pipeline.Status,
		"ref":         pipeline.Ref,
		"sha":         pipeline.SHA,
		"web_url":     pipeline.WebURL,
	}}, nil
}

// NewGitLabCreateMRStepFactory returns a StepFactory for step.gitlab_create_mr.
//
//	- type: step.gitlab_create_mr
//	  config:
//	    client: gitlab-client
//	    project: "group/project"
//	    source_branch: feature-x
//	    target_branch: main
//	    title: "Feature X"
//	    description: "Optional description"
func NewGitLabCreateMRStepFactory() StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		clientName, _ := config["client"].(string)
		if clientName == "" {
			return nil, fmt.Errorf("gitlab_create_mr step %q: 'client' is required", name)
		}
		project, _ := config["project"].(string)
		if project == "" {
			return nil, fmt.Errorf("gitlab_create_mr step %q: 'project' is required", name)
		}
		sourceBranch, _ := config["source_branch"].(string)
		if sourceBranch == "" {
			return nil, fmt.Errorf("gitlab_create_mr step %q: 'source_branch' is required", name)
		}
		targetBranch, _ := config["target_branch"].(string)
		if targetBranch == "" {
			targetBranch = "main"
		}
		title, _ := config["title"].(string)
		if title == "" {
			title = sourceBranch
		}
		description, _ := config["description"].(string)

		client, err := gitLabClientFromService(app, clientName)
		if err != nil {
			return nil, fmt.Errorf("gitlab_create_mr step %q: %w", name, err)
		}

		return &gitLabCreateMRStep{
			name:         name,
			client:       client,
			project:      project,
			sourceBranch: sourceBranch,
			targetBranch: targetBranch,
			title:        title,
			description:  description,
		}, nil
	}
}

type gitLabCreateMRStep struct {
	name         string
	client       *GitLabClient
	project      string
	sourceBranch string
	targetBranch string
	title        string
	description  string
}

func (s *gitLabCreateMRStep) Name() string { return s.name }

func (s *gitLabCreateMRStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	mr, err := s.client.CreateMergeRequest(s.project, MROptions{
		SourceBranch: s.sourceBranch,
		TargetBranch: s.targetBranch,
		Title:        s.title,
		Description:  s.description,
	})
	if err != nil {
		return nil, fmt.Errorf("gitlab_create_mr step %q: %w", s.name, err)
	}

	return &StepResult{Output: map[string]any{
		"mr_id":         mr.ID,
		"mr_iid":        mr.IID,
		"title":         mr.Title,
		"state":         mr.State,
		"source_branch": mr.SourceBranch,
		"target_branch": mr.TargetBranch,
		"web_url":       mr.WebURL,
	}}, nil
}

// NewGitLabMRCommentStepFactory returns a StepFactory for step.gitlab_mr_comment.
//
//	- type: step.gitlab_mr_comment
//	  config:
//	    client: gitlab-client
//	    project: "group/project"
//	    mr_iid: 42
//	    body: "Pipeline passed!"
func NewGitLabMRCommentStepFactory() StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		clientName, _ := config["client"].(string)
		if clientName == "" {
			return nil, fmt.Errorf("gitlab_mr_comment step %q: 'client' is required", name)
		}
		project, _ := config["project"].(string)
		if project == "" {
			return nil, fmt.Errorf("gitlab_mr_comment step %q: 'project' is required", name)
		}
		body, _ := config["body"].(string)
		if body == "" {
			return nil, fmt.Errorf("gitlab_mr_comment step %q: 'body' is required", name)
		}

		var mrIID int
		switch v := config["mr_iid"].(type) {
		case int:
			mrIID = v
		case float64:
			mrIID = int(v)
		case string:
			n, err := strconv.Atoi(v)
			if err != nil {
				return nil, fmt.Errorf("gitlab_mr_comment step %q: invalid mr_iid %q", name, v)
			}
			mrIID = n
		}

		client, err := gitLabClientFromService(app, clientName)
		if err != nil {
			return nil, fmt.Errorf("gitlab_mr_comment step %q: %w", name, err)
		}

		return &gitLabMRCommentStep{
			name:    name,
			client:  client,
			project: project,
			mrIID:   mrIID,
			body:    body,
		}, nil
	}
}

type gitLabMRCommentStep struct {
	name    string
	client  *GitLabClient
	project string
	mrIID   int
	body    string
}

func (s *gitLabMRCommentStep) Name() string { return s.name }

func (s *gitLabMRCommentStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	if err := s.client.CommentOnMR(s.project, s.mrIID, s.body); err != nil {
		return nil, fmt.Errorf("gitlab_mr_comment step %q: %w", s.name, err)
	}

	return &StepResult{Output: map[string]any{
		"commented": true,
		"project":   s.project,
		"mr_iid":    s.mrIID,
	}}, nil
}
