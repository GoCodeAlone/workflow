package tracing

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

// Config holds configuration for the TracerProvider setup.
type Config struct {
	// Endpoint is the OTLP HTTP endpoint (e.g., "localhost:4318").
	Endpoint string
	// ServiceName is the service name reported in traces.
	ServiceName string
	// ServiceVersion is the optional service version.
	ServiceVersion string
	// Insecure disables TLS for the OTLP exporter.
	Insecure bool
	// SampleRate controls the trace sampling ratio (0.0 to 1.0). 0 means default (always sample).
	SampleRate float64
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Endpoint:    "localhost:4318",
		ServiceName: "workflow",
		Insecure:    true,
		SampleRate:  1.0,
	}
}

// Provider wraps an OpenTelemetry TracerProvider and handles lifecycle.
type Provider struct {
	tp     *sdktrace.TracerProvider
	tracer trace.Tracer
}

// NewProvider creates a new TracerProvider from the given config and sets it as the global provider.
func NewProvider(ctx context.Context, cfg Config) (*Provider, error) {
	opts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(cfg.Endpoint),
	}
	if cfg.Insecure {
		opts = append(opts, otlptracehttp.WithInsecure())
	}

	exporter, err := otlptracehttp.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("create OTLP exporter: %w", err)
	}

	attrs := []resource.Option{
		resource.WithAttributes(semconv.ServiceNameKey.String(cfg.ServiceName)),
	}
	if cfg.ServiceVersion != "" {
		attrs = append(attrs, resource.WithAttributes(semconv.ServiceVersionKey.String(cfg.ServiceVersion)))
	}

	res, err := resource.New(ctx, attrs...)
	if err != nil {
		return nil, fmt.Errorf("create resource: %w", err)
	}

	var sampler sdktrace.Sampler
	switch {
	case cfg.SampleRate <= 0:
		sampler = sdktrace.AlwaysSample()
	case cfg.SampleRate >= 1.0:
		sampler = sdktrace.AlwaysSample()
	default:
		sampler = sdktrace.TraceIDRatioBased(cfg.SampleRate)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return &Provider{
		tp:     tp,
		tracer: tp.Tracer(cfg.ServiceName),
	}, nil
}

// Tracer returns the named tracer from the provider.
func (p *Provider) Tracer() trace.Tracer {
	return p.tracer
}

// TracerProvider returns the underlying SDK TracerProvider.
func (p *Provider) TracerProvider() *sdktrace.TracerProvider {
	return p.tp
}

// Shutdown gracefully shuts down the tracer provider, flushing pending spans.
func (p *Provider) Shutdown(ctx context.Context) error {
	if p.tp != nil {
		return p.tp.Shutdown(ctx)
	}
	return nil
}
