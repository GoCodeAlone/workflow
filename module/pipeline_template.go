package module

import (
	"github.com/GoCodeAlone/workflow/pipeline"
)

// TemplateEngine resolves {{ .field }} expressions against a PipelineContext.
// Aliased from pipeline.TemplateEngine for backwards compatibility.
type TemplateEngine = pipeline.TemplateEngine

// NewTemplateEngine creates a new TemplateEngine.
// Delegates to pipeline.NewTemplateEngine.
var NewTemplateEngine = pipeline.NewTemplateEngine

// preprocessTemplate is kept as a package-level alias for tests in module/ that
// reference the unexported function directly. External callers should use
// pipeline.PreprocessTemplate instead.
var preprocessTemplate = pipeline.PreprocessTemplate

// templateFuncMap returns the function map available in pipeline templates.
// Delegates to pipeline.TemplateFuncMap for backwards compatibility.
var templateFuncMap = pipeline.TemplateFuncMap
