package source

import (
	"github.com/GoCodeAlone/workflow/connector"
)

// RegisterBuiltinSources registers all built-in source and sink factories
// with the given connector registry. This is the main entry point for
// bootstrapping the connector system with the standard set of connectors.
func RegisterBuiltinSources(registry *connector.Registry) error {
	sources := map[string]connector.SourceFactory{
		"postgres.cdc": NewPostgresCDCSourceFactory(),
		"redis.stream": NewRedisStreamSourceFactory(),
		"sqs":          NewSQSSourceFactory(),
	}

	for name, factory := range sources {
		if err := registry.RegisterSource(name, factory); err != nil {
			return err
		}
	}

	sinks := map[string]connector.SinkFactory{
		"sqs": NewSQSSinkFactory(),
	}

	for name, factory := range sinks {
		if err := registry.RegisterSink(name, factory); err != nil {
			return err
		}
	}

	return nil
}
