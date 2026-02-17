package module

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/CrisisTextLine/modular"
)

// AWSAPIGateway is a stub module that syncs workflow HTTP routes to
// AWS API Gateway. Actual AWS SDK integration is future work.
type AWSAPIGateway struct {
	name   string
	region string
	apiID  string
	stage  string
	logger *slog.Logger
}

// NewAWSAPIGateway creates a new AWS API Gateway sync module.
func NewAWSAPIGateway(name string) *AWSAPIGateway {
	return &AWSAPIGateway{
		name:   name,
		logger: slog.Default(),
	}
}

// SetConfig configures the AWS API Gateway module.
func (a *AWSAPIGateway) SetConfig(region, apiID, stage string) {
	a.region = region
	a.apiID = apiID
	a.stage = stage
}

// Name returns the module name.
func (a *AWSAPIGateway) Name() string { return a.name }

// Init initializes the module.
func (a *AWSAPIGateway) Init(_ modular.Application) error { return nil }

// Start logs that the module would sync routes (stub).
func (a *AWSAPIGateway) Start(_ context.Context) error {
	a.logger.Info("AWS API Gateway sync started (stub)",
		"region", a.region,
		"api_id", a.apiID,
		"stage", a.stage,
	)
	return nil
}

// Stop is a no-op.
func (a *AWSAPIGateway) Stop(_ context.Context) error { return nil }

// ProvidesServices returns the services provided by this module.
func (a *AWSAPIGateway) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        a.name,
			Description: "AWS API Gateway Sync (stub)",
			Instance:    a,
		},
	}
}

// RequiresServices returns no dependencies.
func (a *AWSAPIGateway) RequiresServices() []modular.ServiceDependency { return nil }

// SyncRoutes would sync the given routes to AWS API Gateway.
// This is a stub that only logs what it would do.
func (a *AWSAPIGateway) SyncRoutes(routes []GatewayRoute) error {
	if a.apiID == "" {
		return fmt.Errorf("aws_api_gateway %q: api_id is required", a.name)
	}

	for _, route := range routes {
		a.logger.Info("Would sync route to AWS API Gateway (stub)",
			"prefix", route.PathPrefix,
			"backend", route.Backend,
			"methods", route.Methods,
			"stage", a.stage,
		)
	}
	return nil
}

// Region returns the configured AWS region.
func (a *AWSAPIGateway) Region() string { return a.region }

// APIID returns the configured API ID.
func (a *AWSAPIGateway) APIID() string { return a.apiID }

// Stage returns the configured deployment stage.
func (a *AWSAPIGateway) Stage() string { return a.stage }
