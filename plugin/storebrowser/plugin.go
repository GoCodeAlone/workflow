package storebrowser

import (
	"database/sql"
	"net/http"

	"github.com/GoCodeAlone/workflow/plugin"
	"github.com/GoCodeAlone/workflow/store"
)

func init() {
	plugin.RegisterNativePluginFactory(func(db *sql.DB, deps map[string]any) plugin.NativePlugin {
		if db == nil {
			return nil
		}
		var eventStore store.EventStore
		if es, ok := deps["eventStore"].(store.EventStore); ok {
			eventStore = es
		}
		var dlqStore store.DLQStore
		if ds, ok := deps["dlqStore"].(store.DLQStore); ok {
			dlqStore = ds
		}
		return New(db, eventStore, dlqStore)
	})
}

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

func (p *Plugin) Dependencies() []plugin.PluginDependency {
	return nil
}

func (p *Plugin) UIPages() []plugin.UIPageDef {
	return []plugin.UIPageDef{
		{ID: "store-browser", Label: "Store Browser", Icon: "database", Category: "tools"},
	}
}

func (p *Plugin) RegisterRoutes(mux *http.ServeMux) {
	h := &handler{db: p.db, eventStore: p.eventStore, dlqStore: p.dlqStore}
	h.registerRoutes(mux)
}

func (p *Plugin) OnEnable(_ plugin.PluginContext) error  { return nil }
func (p *Plugin) OnDisable(_ plugin.PluginContext) error { return nil }
