package module

import (
	"log/slog"
	"net/http"

	"github.com/CrisisTextLine/modular"
	evstore "github.com/GoCodeAlone/workflow/store"
)

// TimelineServiceModule wraps evstore.TimelineHandler and evstore.ReplayHandler
// as a modular.Module. It provides HTTP muxes for timeline and replay features
// via the service registry.
type TimelineServiceModule struct {
	name            string
	eventStore      evstore.EventStore
	timelineHandler *evstore.TimelineHandler
	replayHandler   *evstore.ReplayHandler
	backfillHandler *evstore.BackfillMockDiffHandler
	timelineMux     *http.ServeMux
	replayMux       *http.ServeMux
	backfillMux     *http.ServeMux
}

// NewTimelineServiceModule creates a new timeline service module.
// It requires a non-nil EventStore to function. Panics if eventStore is nil.
func NewTimelineServiceModule(name string, eventStore evstore.EventStore) *TimelineServiceModule {
	if eventStore == nil {
		panic("NewTimelineServiceModule: eventStore must not be nil")
	}
	logger := slog.Default()

	timelineHandler := evstore.NewTimelineHandler(eventStore, logger)
	timelineMux := http.NewServeMux()
	timelineHandler.RegisterRoutes(timelineMux)

	replayHandler := evstore.NewReplayHandler(eventStore, logger)
	replayMux := http.NewServeMux()
	replayHandler.RegisterRoutes(replayMux)

	backfillStore := evstore.NewInMemoryBackfillStore()
	mockStore := evstore.NewInMemoryStepMockStore()
	diffCalc := evstore.NewDiffCalculator(eventStore)
	backfillHandler := evstore.NewBackfillMockDiffHandler(backfillStore, mockStore, diffCalc, logger)
	backfillMux := http.NewServeMux()
	backfillHandler.RegisterRoutes(backfillMux)

	logger.Info("Created timeline, replay, and backfill/mock/diff handlers", "module", name)

	return &TimelineServiceModule{
		name:            name,
		eventStore:      eventStore,
		timelineHandler: timelineHandler,
		replayHandler:   replayHandler,
		backfillHandler: backfillHandler,
		timelineMux:     timelineMux,
		replayMux:       replayMux,
		backfillMux:     backfillMux,
	}
}

// Name implements modular.Module.
func (m *TimelineServiceModule) Name() string { return m.name }

// Init implements modular.Module.
func (m *TimelineServiceModule) Init(_ modular.Application) error { return nil }

// ProvidesServices implements modular.Module. Registers the timeline, replay,
// and backfill muxes as services so the server can delegate routes to them.
func (m *TimelineServiceModule) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        m.name,
			Description: "Timeline service: " + m.name,
			Instance:    m.timelineMux,
		},
		{
			Name:        m.name + ".timeline",
			Description: "Timeline handler mux: " + m.name,
			Instance:    http.Handler(m.timelineMux),
		},
		{
			Name:        m.name + ".replay",
			Description: "Replay handler mux: " + m.name,
			Instance:    http.Handler(m.replayMux),
		},
		{
			Name:        m.name + ".backfill",
			Description: "Backfill/mock/diff handler mux: " + m.name,
			Instance:    http.Handler(m.backfillMux),
		},
	}
}

// RequiresServices implements modular.Module.
func (m *TimelineServiceModule) RequiresServices() []modular.ServiceDependency {
	return nil
}

// TimelineMux returns the HTTP mux for timeline endpoints.
func (m *TimelineServiceModule) TimelineMux() http.Handler { return m.timelineMux }

// ReplayMux returns the HTTP mux for replay endpoints.
func (m *TimelineServiceModule) ReplayMux() http.Handler { return m.replayMux }

// BackfillMux returns the HTTP mux for backfill/mock/diff endpoints.
func (m *TimelineServiceModule) BackfillMux() http.Handler { return m.backfillMux }
