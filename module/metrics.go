package module

import (
	"net/http"
	"strconv"
	"time"

	"github.com/CrisisTextLine/modular"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// MetricsCollectorConfig holds configuration for the MetricsCollector module.
type MetricsCollectorConfig struct {
	Namespace      string   `yaml:"namespace" json:"namespace"`
	Subsystem      string   `yaml:"subsystem" json:"subsystem"`
	MetricsPath    string   `yaml:"metricsPath" json:"metricsPath"`
	EnabledMetrics []string `yaml:"enabledMetrics" json:"enabledMetrics"`
}

// DefaultMetricsCollectorConfig returns the default configuration.
func DefaultMetricsCollectorConfig() MetricsCollectorConfig {
	return MetricsCollectorConfig{
		Namespace:      "workflow",
		Subsystem:      "",
		MetricsPath:    "/metrics",
		EnabledMetrics: []string{"workflow", "http", "module", "active_workflows"},
	}
}

func metricsEnabled(enabledList []string, name string) bool {
	for _, e := range enabledList {
		if e == name {
			return true
		}
	}
	return false
}

// MetricsCollector wraps Prometheus metrics for the workflow engine.
// It registers as service "metrics.collector" and provides pre-defined metric vectors.
type MetricsCollector struct {
	name     string
	config   MetricsCollectorConfig
	registry *prometheus.Registry

	WorkflowExecutions  *prometheus.CounterVec
	WorkflowDuration    *prometheus.HistogramVec
	HTTPRequestsTotal   *prometheus.CounterVec
	HTTPRequestDuration *prometheus.HistogramVec
	ModuleOperations    *prometheus.CounterVec
	ActiveWorkflows     *prometheus.GaugeVec
}

// NewMetricsCollector creates a new MetricsCollector with its own Prometheus registry.
func NewMetricsCollector(name string) *MetricsCollector {
	return NewMetricsCollectorWithConfig(name, DefaultMetricsCollectorConfig())
}

// NewMetricsCollectorWithConfig creates a new MetricsCollector with the given config.
func NewMetricsCollectorWithConfig(name string, cfg MetricsCollectorConfig) *MetricsCollector {
	reg := prometheus.NewRegistry()
	enabled := cfg.EnabledMetrics
	ns := cfg.Namespace
	sub := cfg.Subsystem

	mc := &MetricsCollector{
		name:     name,
		config:   cfg,
		registry: reg,
	}

	if metricsEnabled(enabled, "workflow") {
		mc.WorkflowExecutions = prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: ns,
			Subsystem: sub,
			Name:      "workflow_executions_total",
			Help:      "Total number of workflow executions",
		}, []string{"workflow_type", "action", "status"})

		mc.WorkflowDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: ns,
			Subsystem: sub,
			Name:      "workflow_duration_seconds",
			Help:      "Duration of workflow executions in seconds",
			Buckets:   prometheus.DefBuckets,
		}, []string{"workflow_type", "action"})

		reg.MustRegister(mc.WorkflowExecutions)
		reg.MustRegister(mc.WorkflowDuration)
	}

	if metricsEnabled(enabled, "http") {
		mc.HTTPRequestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: ns,
			Subsystem: sub,
			Name:      "http_requests_total",
			Help:      "Total number of HTTP requests",
		}, []string{"method", "path", "status_code"})

		mc.HTTPRequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: ns,
			Subsystem: sub,
			Name:      "http_request_duration_seconds",
			Help:      "Duration of HTTP requests in seconds",
			Buckets:   prometheus.DefBuckets,
		}, []string{"method", "path"})

		reg.MustRegister(mc.HTTPRequestsTotal)
		reg.MustRegister(mc.HTTPRequestDuration)
	}

	if metricsEnabled(enabled, "module") {
		mc.ModuleOperations = prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: ns,
			Subsystem: sub,
			Name:      "module_operations_total",
			Help:      "Total number of module operations",
		}, []string{"module", "operation", "status"})

		reg.MustRegister(mc.ModuleOperations)
	}

	if metricsEnabled(enabled, "active_workflows") {
		mc.ActiveWorkflows = prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: ns,
			Subsystem: sub,
			Name:      "active_workflows",
			Help:      "Number of currently active workflows",
		}, []string{"workflow_type"})

		reg.MustRegister(mc.ActiveWorkflows)
	}

	return mc
}

// MetricsPath returns the configured metrics endpoint path.
func (m *MetricsCollector) MetricsPath() string { return m.config.MetricsPath }

// Name returns the module name.
func (m *MetricsCollector) Name() string {
	return m.name
}

// Init registers the metrics collector as a service.
func (m *MetricsCollector) Init(app modular.Application) error {
	return app.RegisterService("metrics.collector", m)
}

// Handler returns an HTTP handler that serves Prometheus metrics.
func (m *MetricsCollector) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

// RecordWorkflowExecution increments the workflow execution counter.
func (m *MetricsCollector) RecordWorkflowExecution(workflowType, action, status string) {
	if m.WorkflowExecutions != nil {
		m.WorkflowExecutions.WithLabelValues(workflowType, action, status).Inc()
	}
}

// RecordWorkflowDuration records the duration of a workflow execution.
func (m *MetricsCollector) RecordWorkflowDuration(workflowType, action string, duration time.Duration) {
	if m.WorkflowDuration != nil {
		m.WorkflowDuration.WithLabelValues(workflowType, action).Observe(duration.Seconds())
	}
}

// RecordHTTPRequest records an HTTP request metric.
func (m *MetricsCollector) RecordHTTPRequest(method, path string, statusCode int, duration time.Duration) {
	if m.HTTPRequestsTotal != nil {
		m.HTTPRequestsTotal.WithLabelValues(method, path, strconv.Itoa(statusCode)).Inc()
	}
	if m.HTTPRequestDuration != nil {
		m.HTTPRequestDuration.WithLabelValues(method, path).Observe(duration.Seconds())
	}
}

// RecordModuleOperation records a module operation metric.
func (m *MetricsCollector) RecordModuleOperation(module, operation, status string) {
	if m.ModuleOperations != nil {
		m.ModuleOperations.WithLabelValues(module, operation, status).Inc()
	}
}

// SetActiveWorkflows sets the gauge for active workflows of a given type.
func (m *MetricsCollector) SetActiveWorkflows(workflowType string, count float64) {
	if m.ActiveWorkflows != nil {
		m.ActiveWorkflows.WithLabelValues(workflowType).Set(count)
	}
}

// MetricsHTTPHandler adapts an http.Handler to the HTTPHandler interface
type MetricsHTTPHandler struct {
	Handler http.Handler
}

// Handle implements the HTTPHandler interface
func (h *MetricsHTTPHandler) Handle(w http.ResponseWriter, r *http.Request) {
	h.Handler.ServeHTTP(w, r)
}

// ProvidesServices returns the services provided by this module.
func (m *MetricsCollector) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        "metrics.collector",
			Description: "Prometheus metrics collector for workflow engine",
			Instance:    m,
		},
	}
}

// RequiresServices returns services required by this module.
func (m *MetricsCollector) RequiresServices() []modular.ServiceDependency {
	return nil
}
