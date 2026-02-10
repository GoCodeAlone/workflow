package module

import (
	"context"
	"fmt"

	"github.com/CrisisTextLine/modular"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// OTelTracing provides OpenTelemetry distributed tracing.
// It implements the modular.Module interface.
type OTelTracing struct {
	name           string
	endpoint       string
	serviceName    string
	tracerProvider *sdktrace.TracerProvider
	logger         modular.Logger
}

// NewOTelTracing creates a new OpenTelemetry tracing module.
func NewOTelTracing(name string) *OTelTracing {
	return &OTelTracing{
		name:        name,
		endpoint:    "localhost:4318",
		serviceName: "workflow",
		logger:      &noopLogger{},
	}
}

// Name returns the module name.
func (o *OTelTracing) Name() string {
	return o.name
}

// Init initializes the module with the application context.
func (o *OTelTracing) Init(app modular.Application) error {
	o.logger = app.Logger()
	return nil
}

// ProvidesServices returns the services provided by this module.
func (o *OTelTracing) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        o.name,
			Description: "OpenTelemetry Tracing",
			Instance:    o,
		},
	}
}

// RequiresServices returns the services required by this module.
func (o *OTelTracing) RequiresServices() []modular.ServiceDependency {
	return nil
}

// SetEndpoint sets the OTLP endpoint.
func (o *OTelTracing) SetEndpoint(endpoint string) {
	o.endpoint = endpoint
}

// SetServiceName sets the service name used in traces.
func (o *OTelTracing) SetServiceName(serviceName string) {
	o.serviceName = serviceName
}

// Start initializes the OTLP exporter and TracerProvider.
func (o *OTelTracing) Start(ctx context.Context) error {
	exporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpoint(o.endpoint),
		otlptracehttp.WithInsecure(),
	)
	if err != nil {
		return fmt.Errorf("failed to create OTLP exporter: %w", err)
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(o.serviceName),
		),
	)
	if err != nil {
		return fmt.Errorf("failed to create resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)

	otel.SetTracerProvider(tp)
	o.tracerProvider = tp

	o.logger.Info("OpenTelemetry tracing started", "endpoint", o.endpoint, "service", o.serviceName)
	return nil
}

// Stop shuts down the TracerProvider gracefully.
func (o *OTelTracing) Stop(ctx context.Context) error {
	if o.tracerProvider != nil {
		if err := o.tracerProvider.Shutdown(ctx); err != nil {
			return fmt.Errorf("failed to shutdown tracer provider: %w", err)
		}
	}
	o.logger.Info("OpenTelemetry tracing stopped")
	return nil
}
