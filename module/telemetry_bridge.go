package module

import (
	"context"
	"sync"
	"time"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/telemetry"
)

const defaultTelemetryBridgeInterval = 30 * time.Second

type TelemetryBridgeConfig struct {
	Interval time.Duration `yaml:"interval" json:"interval"`
	Timeout  time.Duration `yaml:"timeout" json:"timeout"`
}

type TelemetryBridge struct {
	name   string
	config TelemetryBridgeConfig
	bridge *telemetry.Bridge
	app    modular.Application
	logger modular.Logger

	mu     sync.Mutex
	cancel context.CancelFunc
}

func NewTelemetryBridge(name string, sink telemetry.TelemetrySink, config TelemetryBridgeConfig) *TelemetryBridge {
	if config.Interval <= 0 {
		config.Interval = defaultTelemetryBridgeInterval
	}
	return &TelemetryBridge{
		name:   name,
		config: config,
		bridge: telemetry.NewBridge(sink, telemetry.BridgeConfig{Timeout: config.Timeout}),
	}
}

func (m *TelemetryBridge) Name() string {
	return m.name
}

func (m *TelemetryBridge) Init(app modular.Application) error {
	m.app = app
	m.logger = app.Logger()
	return app.RegisterService("telemetry.bridge", m)
}

func (m *TelemetryBridge) Start(ctx context.Context) error {
	m.mu.Lock()
	if m.cancel != nil {
		m.mu.Unlock()
		return nil
	}
	runCtx, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	m.mu.Unlock()

	m.collect(runCtx)
	go m.run(runCtx)
	return nil
}

func (m *TelemetryBridge) Stop(context.Context) error {
	m.mu.Lock()
	cancel := m.cancel
	m.cancel = nil
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

func (m *TelemetryBridge) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        "telemetry.bridge",
			Description: "Host-side telemetry bridge for neutral Workflow emitters",
			Instance:    m,
		},
	}
}

func (m *TelemetryBridge) RequiresServices() []modular.ServiceDependency {
	return nil
}

func (m *TelemetryBridge) run(ctx context.Context) {
	ticker := time.NewTicker(m.config.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.collect(ctx)
		}
	}
}

func (m *TelemetryBridge) collect(ctx context.Context) {
	if err := m.bridge.Collect(ctx, m.app); err != nil && m.logger != nil {
		m.logger.Warn("telemetry bridge collection failed", "error", err)
	}
}
