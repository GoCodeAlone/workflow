package docmanager

import (
	"database/sql"
	"net/http"

	"github.com/GoCodeAlone/workflow/plugin"
)

// Compile-time interface check.
var _ plugin.NativePlugin = (*Plugin)(nil)

// Plugin implements the doc-manager native plugin, providing HTTP endpoints
// to create and manage markdown documentation for workflows.
type Plugin struct {
	h *handler
}

// New creates a new doc-manager plugin. It eagerly creates the workflow_docs
// table so that initialization happens before the server starts handling
// requests (avoiding SQLITE_BUSY on lazy first-request init).
func New(db *sql.DB) *Plugin {
	return &Plugin{h: newHandler(db)}
}

func (p *Plugin) Name() string    { return "doc-manager" }
func (p *Plugin) Version() string { return "1.0.0" }
func (p *Plugin) Description() string {
	return "Create and manage markdown documentation for workflows"
}

func (p *Plugin) UIPages() []plugin.UIPageDef {
	return []plugin.UIPageDef{
		{ID: "docs", Label: "Documentation", Icon: "book", Category: "docs"},
	}
}

func (p *Plugin) RegisterRoutes(mux *http.ServeMux) {
	p.h.registerRoutes(mux)
}
