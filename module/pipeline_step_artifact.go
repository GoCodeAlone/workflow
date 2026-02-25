package module

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/CrisisTextLine/modular"
)

// resolveArtifactStore retrieves the named ArtifactStore from the service registry.
func resolveArtifactStore(app modular.Application, storeName, stepName string) (ArtifactStore, error) {
	if app == nil {
		return nil, fmt.Errorf("%s: no application context", stepName)
	}
	svc, ok := app.SvcRegistry()[storeName]
	if !ok {
		return nil, fmt.Errorf("%s: artifact store %q not found in service registry", stepName, storeName)
	}
	store, ok := svc.(ArtifactStore)
	if !ok {
		return nil, fmt.Errorf("%s: service %q does not implement ArtifactStore", stepName, storeName)
	}
	return store, nil
}

// ─── step.artifact_upload ───────────────────────────────────────────────────

// ArtifactUploadStep uploads a local file to a named ArtifactStore.
type ArtifactUploadStep struct {
	name     string
	store    string
	key      string
	source   string
	metadata map[string]string
	app      modular.Application
	tmpl     *TemplateEngine
}

// NewArtifactUploadStepFactory returns a StepFactory for step.artifact_upload.
func NewArtifactUploadStepFactory() StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		store, _ := config["store"].(string)
		if store == "" {
			return nil, fmt.Errorf("artifact_upload step %q: 'store' is required", name)
		}
		key, _ := config["key"].(string)
		if key == "" {
			return nil, fmt.Errorf("artifact_upload step %q: 'key' is required", name)
		}
		source, _ := config["source"].(string)
		if source == "" {
			return nil, fmt.Errorf("artifact_upload step %q: 'source' is required", name)
		}

		md := map[string]string{}
		if raw, ok := config["metadata"].(map[string]any); ok {
			for k, v := range raw {
				md[k] = fmt.Sprintf("%v", v)
			}
		}

		return &ArtifactUploadStep{
			name:     name,
			store:    store,
			key:      key,
			source:   source,
			metadata: md,
			app:      app,
			tmpl:     NewTemplateEngine(),
		}, nil
	}
}

func (s *ArtifactUploadStep) Name() string { return s.name }

func (s *ArtifactUploadStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	store, err := resolveArtifactStore(s.app, s.store, fmt.Sprintf("artifact_upload step %q", s.name))
	if err != nil {
		return nil, err
	}

	key, err := s.tmpl.Resolve(s.key, pc)
	if err != nil {
		return nil, fmt.Errorf("artifact_upload step %q: key template: %w", s.name, err)
	}

	source, err := s.tmpl.Resolve(s.source, pc)
	if err != nil {
		return nil, fmt.Errorf("artifact_upload step %q: source template: %w", s.name, err)
	}

	// Resolve metadata templates.
	md := make(map[string]string, len(s.metadata))
	for k, v := range s.metadata {
		resolved, err := s.tmpl.Resolve(v, pc)
		if err != nil {
			return nil, fmt.Errorf("artifact_upload step %q: metadata[%s] template: %w", s.name, k, err)
		}
		md[k] = resolved
	}

	f, err := os.Open(source)
	if err != nil {
		return nil, fmt.Errorf("artifact_upload step %q: failed to open source %q: %w", s.name, source, err)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("artifact_upload step %q: failed to stat source %q: %w", s.name, source, err)
	}

	if err := store.Upload(ctx, key, f, md); err != nil {
		return nil, fmt.Errorf("artifact_upload step %q: %w", s.name, err)
	}

	return &StepResult{Output: map[string]any{
		"key":   key,
		"store": s.store,
		"size":  stat.Size(),
	}}, nil
}

// ─── step.artifact_download ─────────────────────────────────────────────────

// ArtifactDownloadStep downloads an artifact from a named ArtifactStore to a local path.
type ArtifactDownloadStep struct {
	name  string
	store string
	key   string
	dest  string
	app   modular.Application
	tmpl  *TemplateEngine
}

// NewArtifactDownloadStepFactory returns a StepFactory for step.artifact_download.
func NewArtifactDownloadStepFactory() StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		store, _ := config["store"].(string)
		if store == "" {
			return nil, fmt.Errorf("artifact_download step %q: 'store' is required", name)
		}
		key, _ := config["key"].(string)
		if key == "" {
			return nil, fmt.Errorf("artifact_download step %q: 'key' is required", name)
		}
		dest, _ := config["dest"].(string)
		if dest == "" {
			return nil, fmt.Errorf("artifact_download step %q: 'dest' is required", name)
		}

		return &ArtifactDownloadStep{
			name:  name,
			store: store,
			key:   key,
			dest:  dest,
			app:   app,
			tmpl:  NewTemplateEngine(),
		}, nil
	}
}

func (s *ArtifactDownloadStep) Name() string { return s.name }

func (s *ArtifactDownloadStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	store, err := resolveArtifactStore(s.app, s.store, fmt.Sprintf("artifact_download step %q", s.name))
	if err != nil {
		return nil, err
	}

	key, err := s.tmpl.Resolve(s.key, pc)
	if err != nil {
		return nil, fmt.Errorf("artifact_download step %q: key template: %w", s.name, err)
	}

	dest, err := s.tmpl.Resolve(s.dest, pc)
	if err != nil {
		return nil, fmt.Errorf("artifact_download step %q: dest template: %w", s.name, err)
	}

	reader, md, err := store.Download(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("artifact_download step %q: %w", s.name, err)
	}
	defer reader.Close()

	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		return nil, fmt.Errorf("artifact_download step %q: failed to create destination directory: %w", s.name, err)
	}

	f, err := os.Create(dest)
	if err != nil {
		return nil, fmt.Errorf("artifact_download step %q: failed to create dest file %q: %w", s.name, dest, err)
	}
	defer f.Close()

	written, err := io.Copy(f, reader)
	if err != nil {
		return nil, fmt.Errorf("artifact_download step %q: failed to write: %w", s.name, err)
	}

	return &StepResult{Output: map[string]any{
		"key":      key,
		"dest":     dest,
		"size":     written,
		"metadata": md,
	}}, nil
}

// ─── step.artifact_list ─────────────────────────────────────────────────────

// ArtifactListStep lists artifacts in a named ArtifactStore.
type ArtifactListStep struct {
	name   string
	store  string
	prefix string
	output string
	app    modular.Application
	tmpl   *TemplateEngine
}

// NewArtifactListStepFactory returns a StepFactory for step.artifact_list.
func NewArtifactListStepFactory() StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		store, _ := config["store"].(string)
		if store == "" {
			return nil, fmt.Errorf("artifact_list step %q: 'store' is required", name)
		}

		prefix, _ := config["prefix"].(string)

		output, _ := config["output"].(string)
		if output == "" {
			output = "artifacts"
		}

		return &ArtifactListStep{
			name:   name,
			store:  store,
			prefix: prefix,
			output: output,
			app:    app,
			tmpl:   NewTemplateEngine(),
		}, nil
	}
}

func (s *ArtifactListStep) Name() string { return s.name }

func (s *ArtifactListStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	store, err := resolveArtifactStore(s.app, s.store, fmt.Sprintf("artifact_list step %q", s.name))
	if err != nil {
		return nil, err
	}

	prefix, err := s.tmpl.Resolve(s.prefix, pc)
	if err != nil {
		return nil, fmt.Errorf("artifact_list step %q: prefix template: %w", s.name, err)
	}
	prefix = strings.TrimPrefix(prefix, "/")

	artifacts, err := store.List(ctx, prefix)
	if err != nil {
		return nil, fmt.Errorf("artifact_list step %q: %w", s.name, err)
	}

	// Serialize to JSON-compatible slice for pipeline context.
	items := make([]map[string]any, 0, len(artifacts))
	for _, a := range artifacts {
		items = append(items, map[string]any{
			"key":      a.Key,
			"size":     a.Size,
			"modified": a.Modified.Format("2006-01-02T15:04:05Z"),
			"metadata": a.Metadata,
		})
	}

	return &StepResult{Output: map[string]any{
		s.output: items,
		"count":   len(items),
	}}, nil
}

// ─── step.artifact_delete ───────────────────────────────────────────────────

// ArtifactDeleteStep removes an artifact from a named ArtifactStore.
type ArtifactDeleteStep struct {
	name  string
	store string
	key   string
	app   modular.Application
	tmpl  *TemplateEngine
}

// NewArtifactDeleteStepFactory returns a StepFactory for step.artifact_delete.
func NewArtifactDeleteStepFactory() StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		store, _ := config["store"].(string)
		if store == "" {
			return nil, fmt.Errorf("artifact_delete step %q: 'store' is required", name)
		}
		key, _ := config["key"].(string)
		if key == "" {
			return nil, fmt.Errorf("artifact_delete step %q: 'key' is required", name)
		}

		return &ArtifactDeleteStep{
			name:  name,
			store: store,
			key:   key,
			app:   app,
			tmpl:  NewTemplateEngine(),
		}, nil
	}
}

func (s *ArtifactDeleteStep) Name() string { return s.name }

func (s *ArtifactDeleteStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	store, err := resolveArtifactStore(s.app, s.store, fmt.Sprintf("artifact_delete step %q", s.name))
	if err != nil {
		return nil, err
	}

	key, err := s.tmpl.Resolve(s.key, pc)
	if err != nil {
		return nil, fmt.Errorf("artifact_delete step %q: key template: %w", s.name, err)
	}

	if err := store.Delete(ctx, key); err != nil {
		return nil, fmt.Errorf("artifact_delete step %q: %w", s.name, err)
	}

	return &StepResult{Output: map[string]any{
		"key":     key,
		"deleted": true,
	}}, nil
}

