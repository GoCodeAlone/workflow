package module

import (
	"net/http"
	"strconv"
	"time"

	"github.com/CrisisTextLine/modular"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// MetricsCollector wraps Prometheus metrics for the workflow engine.
// It registers as service "metrics.collector" and provides pre-defined metric vectors.
type MetricsCollector struct {
	name     string
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
	reg := prometheus.NewRegistry()

	workflowExecutions := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "workflow_executions_total",
		Help: "Total number of workflow executions",
	}, []string{"workflow_type", "action", "status"})

	workflowDuration := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "workflow_duration_seconds",
		Help:    "Duration of workflow executions in seconds",
		Buckets: prometheus.DefBuckets,
	}, []string{"workflow_type", "action"})

	httpRequestsTotal := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Total number of HTTP requests",
	}, []string{"method", "path", "status_code"})

	httpRequestDuration := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "Duration of HTTP requests in seconds",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "path"})

	moduleOperations := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "module_operations_total",
		Help: "Total number of module operations",
	}, []string{"module", "operation", "status"})

	activeWorkflows := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "active_workflows",
		Help: "Number of currently active workflows",
	}, []string{"workflow_type"})

	reg.MustRegister(workflowExecutions)
	reg.MustRegister(workflowDuration)
	reg.MustRegister(httpRequestsTotal)
	reg.MustRegister(httpRequestDuration)
	reg.MustRegister(moduleOperations)
	reg.MustRegister(activeWorkflows)

	return &MetricsCollector{
		name:                name,
		registry:            reg,
		WorkflowExecutions:  workflowExecutions,
		WorkflowDuration:    workflowDuration,
		HTTPRequestsTotal:   httpRequestsTotal,
		HTTPRequestDuration: httpRequestDuration,
		ModuleOperations:    moduleOperations,
		ActiveWorkflows:     activeWorkflows,
	}
}

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
	m.WorkflowExecutions.WithLabelValues(workflowType, action, status).Inc()
}

// RecordWorkflowDuration records the duration of a workflow execution.
func (m *MetricsCollector) RecordWorkflowDuration(workflowType, action string, duration time.Duration) {
	m.WorkflowDuration.WithLabelValues(workflowType, action).Observe(duration.Seconds())
}

// RecordHTTPRequest records an HTTP request metric.
func (m *MetricsCollector) RecordHTTPRequest(method, path string, statusCode int, duration time.Duration) {
	m.HTTPRequestsTotal.WithLabelValues(method, path, strconv.Itoa(statusCode)).Inc()
	m.HTTPRequestDuration.WithLabelValues(method, path).Observe(duration.Seconds())
}

// RecordModuleOperation records a module operation metric.
func (m *MetricsCollector) RecordModuleOperation(module, operation, status string) {
	m.ModuleOperations.WithLabelValues(module, operation, status).Inc()
}

// SetActiveWorkflows sets the gauge for active workflows of a given type.
func (m *MetricsCollector) SetActiveWorkflows(workflowType string, count float64) {
	m.ActiveWorkflows.WithLabelValues(workflowType).Set(count)
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
