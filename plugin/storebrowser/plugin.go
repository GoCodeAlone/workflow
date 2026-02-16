package storebrowser

import (
	"database/sql"
	"net/http"

	"github.com/GoCodeAlone/workflow/plugin"
	"github.com/GoCodeAlone/workflow/store"
)

// Compile-time interface check.
var _ plugin.NativePlugin = (*Plugin)(nil)

// Plugin implements the store-browser native plugin, providing HTTP endpoints
// to browse database tables, execution events, and DLQ entries.
type Plugin struct {
	db         *sql.DB
	eventStore store.EventStore
	dlqStore   store.DLQStore
}

// New creates a new store-browser plugin. Any of the parameters may be nil;
// handlers that depend on a nil dependency will return 503.
func New(db *sql.DB, eventStore store.EventStore, dlqStore store.DLQStore) *Plugin {
	return &Plugin{db: db, eventStore: eventStore, dlqStore: dlqStore}
}

func (p *Plugin) Name() string        { return "store-browser" }
func (p *Plugin) Version() string     { return "1.0.0" }
func (p *Plugin) Description() string { return "Browse database tables, events, and DLQ entries" }

func (p *Plugin) UIPages() []plugin.UIPageDef {
	return []plugin.UIPageDef{
		{ID: "store-browser", Label: "Store Browser", Icon: "database", Category: "tools"},
	}
}

func (p *Plugin) RegisterRoutes(mux *http.ServeMux) {
	h := &handler{db: p.db, eventStore: p.eventStore, dlqStore: p.dlqStore}
	h.registerRoutes(mux)
}
