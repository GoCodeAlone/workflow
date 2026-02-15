package module

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/CrisisTextLine/modular"
)

// LogEntry represents a single log message collected from a module.
type LogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Module    string    `json:"module"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
}

// LogEmitter is implemented by modules that produce log entries.
// The log collector auto-discovers services implementing this interface.
type LogEmitter interface {
	DrainLogs() []LogEntry
}

// LogCollectorConfig holds the configuration for the log collector module.
type LogCollectorConfig struct {
	LogLevel      string `yaml:"logLevel" json:"logLevel"`
	OutputFormat  string `yaml:"outputFormat" json:"outputFormat"`
	RetentionDays int    `yaml:"retentionDays" json:"retentionDays"`
}

// LogCollector collects log entries from modules implementing LogEmitter
// and exposes them via a /logs HTTP endpoint.
type LogCollector struct {
	name   string
	config LogCollectorConfig
	app    modular.Application

	mu      sync.RWMutex
	entries []LogEntry
	maxSize int
}

// NewLogCollector creates a new LogCollector module.
func NewLogCollector(name string, cfg LogCollectorConfig) *LogCollector {
	if cfg.LogLevel == "" {
		cfg.LogLevel = "info"
	}
	if cfg.OutputFormat == "" {
		cfg.OutputFormat = "json"
	}
	if cfg.RetentionDays <= 0 {
		cfg.RetentionDays = 7
	}

	return &LogCollector{
		name:    name,
		config:  cfg,
		entries: make([]LogEntry, 0, 1024),
		maxSize: 10000,
	}
}

// Name returns the module name.
func (lc *LogCollector) Name() string {
	return lc.name
}

// Init registers the log collector as a service.
func (lc *LogCollector) Init(app modular.Application) error {
	lc.app = app
	return app.RegisterService("log.collector", lc)
}

// CollectFromEmitters scans the service registry for LogEmitter services
// and drains their log entries.
func (lc *LogCollector) CollectFromEmitters() {
	if lc.app == nil {
		return
	}
	for _, svc := range lc.app.SvcRegistry() {
		if emitter, ok := svc.(LogEmitter); ok {
			entries := emitter.DrainLogs()
			lc.addEntries(entries)
		}
	}
}

// AddEntry adds a single log entry to the collector.
func (lc *LogCollector) AddEntry(entry LogEntry) {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	if !lc.shouldLog(entry.Level) {
		return
	}

	lc.entries = append(lc.entries, entry)
	if len(lc.entries) > lc.maxSize {
		lc.entries = lc.entries[len(lc.entries)-lc.maxSize:]
	}
}

func (lc *LogCollector) addEntries(entries []LogEntry) {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	for _, entry := range entries {
		if lc.shouldLog(entry.Level) {
			lc.entries = append(lc.entries, entry)
		}
	}
	if len(lc.entries) > lc.maxSize {
		lc.entries = lc.entries[len(lc.entries)-lc.maxSize:]
	}
}

func (lc *LogCollector) shouldLog(level string) bool {
	levels := map[string]int{"debug": 0, "info": 1, "warn": 2, "error": 3}
	minLevel, ok := levels[lc.config.LogLevel]
	if !ok {
		minLevel = 1
	}
	entryLevel, ok := levels[level]
	if !ok {
		entryLevel = 1
	}
	return entryLevel >= minLevel
}

// Entries returns a copy of the current log entries.
func (lc *LogCollector) Entries() []LogEntry {
	lc.mu.RLock()
	defer lc.mu.RUnlock()
	out := make([]LogEntry, len(lc.entries))
	copy(out, lc.entries)
	return out
}

// LogHandler returns an HTTP handler that serves collected logs.
func (lc *LogCollector) LogHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Collect from emitters on each request
		lc.CollectFromEmitters()

		entries := lc.Entries()

		// Apply retention filter
		cutoff := time.Now().AddDate(0, 0, -lc.config.RetentionDays)
		filtered := make([]LogEntry, 0, len(entries))
		for _, e := range entries {
			if e.Timestamp.After(cutoff) {
				filtered = append(filtered, e)
			}
		}

		// Optional level query filter
		if levelFilter := r.URL.Query().Get("level"); levelFilter != "" {
			var levelFiltered []LogEntry
			for _, e := range filtered {
				if e.Level == levelFilter {
					levelFiltered = append(levelFiltered, e)
				}
			}
			filtered = levelFiltered
		}

		// Optional module query filter
		if moduleFilter := r.URL.Query().Get("module"); moduleFilter != "" {
			var moduleFiltered []LogEntry
			for _, e := range filtered {
				if e.Module == moduleFilter {
					moduleFiltered = append(moduleFiltered, e)
				}
			}
			filtered = moduleFiltered
		}

		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"count":   len(filtered),
			"entries": filtered,
			"config": map[string]any{
				"logLevel":      lc.config.LogLevel,
				"outputFormat":  lc.config.OutputFormat,
				"retentionDays": lc.config.RetentionDays,
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}
}

// StartCollectionLoop runs a background goroutine that periodically collects
// logs from emitters. Call the returned cancel function to stop.
func (lc *LogCollector) StartCollectionLoop(ctx context.Context, interval time.Duration) context.CancelFunc {
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				lc.CollectFromEmitters()
			}
		}
	}()
	return cancel
}

// ProvidesServices returns the services provided by this module.
func (lc *LogCollector) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        "log.collector",
			Description: "Centralized log collection from all modules",
			Instance:    lc,
		},
	}
}

// RequiresServices returns services required by this module.
func (lc *LogCollector) RequiresServices() []modular.ServiceDependency {
	return nil
}

// LogHTTPHandler adapts an http.HandlerFunc to the HTTPHandler interface.
type LogHTTPHandler struct {
	Handler http.HandlerFunc
}

// Handle implements the HTTPHandler interface.
func (h *LogHTTPHandler) Handle(w http.ResponseWriter, r *http.Request) {
	h.Handler(w, r)
}
