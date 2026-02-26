package module

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/CrisisTextLine/modular"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
	apigwv2types "github.com/aws/aws-sdk-go-v2/service/apigatewayv2/types"
)

// AWSAPIGateway is a module that syncs workflow HTTP routes to
// AWS API Gateway v2 (HTTP API) using aws-sdk-go-v2.
type AWSAPIGateway struct {
	name     string
	region   string
	apiID    string
	stage    string
	provider CloudCredentialProvider
	logger   *slog.Logger
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

// SetProvider sets the cloud credential provider for AWS API calls.
func (a *AWSAPIGateway) SetProvider(p CloudCredentialProvider) {
	a.provider = p
}

// Name returns the module name.
func (a *AWSAPIGateway) Name() string { return a.name }

// Init initializes the module.
func (a *AWSAPIGateway) Init(_ modular.Application) error { return nil }

// Start logs that the module has started.
func (a *AWSAPIGateway) Start(_ context.Context) error {
	a.logger.Info("AWS API Gateway sync module started",
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
			Description: "AWS API Gateway Sync",
			Instance:    a,
		},
	}
}

// RequiresServices returns no dependencies.
func (a *AWSAPIGateway) RequiresServices() []modular.ServiceDependency { return nil }

// SyncRoutes syncs the given routes to AWS API Gateway v2.
// For each route it upserts an HTTP_PROXY integration and route in the HTTP API.
func (a *AWSAPIGateway) SyncRoutes(routes []GatewayRoute) error {
	if a.apiID == "" {
		return fmt.Errorf("aws_api_gateway %q: api_id is required", a.name)
	}

	ctx := context.Background()

	// Build API Gateway client — prefer cloud account credentials, fall back to default chain.
	var apiCfg aws.Config
	var cfgErr error

	awsProv, hasAWS := awsProviderFrom(a.provider)
	if hasAWS {
		apiCfg, cfgErr = awsProv.AWSConfig(ctx)
	} else {
		var opts []func(*config.LoadOptions) error
		if a.region != "" {
			opts = append(opts, config.WithRegion(a.region))
		}
		apiCfg, cfgErr = config.LoadDefaultConfig(ctx, opts...)
	}
	if cfgErr != nil {
		return fmt.Errorf("aws_api_gateway %q: loading AWS config: %w", a.name, cfgErr)
	}

	client := apigatewayv2.NewFromConfig(apiCfg)

	// Fetch existing integrations and routes to enable idempotent upserts.
	existingIntegrations, err := a.listIntegrations(ctx, client)
	if err != nil {
		return fmt.Errorf("aws_api_gateway %q: listing integrations: %w", a.name, err)
	}
	existingRoutes, err := a.listRoutes(ctx, client)
	if err != nil {
		return fmt.Errorf("aws_api_gateway %q: listing routes: %w", a.name, err)
	}

	for _, route := range routes {
		integrationID, err := a.ensureIntegration(ctx, client, existingIntegrations, route)
		if err != nil {
			return fmt.Errorf("aws_api_gateway %q: ensuring integration for %q: %w", a.name, route.PathPrefix, err)
		}
		if err := a.upsertRoutes(ctx, client, existingRoutes, route, integrationID); err != nil {
			return fmt.Errorf("aws_api_gateway %q: upserting route %q: %w", a.name, route.PathPrefix, err)
		}
	}

	return nil
}

// listIntegrations fetches all integrations for the API, returning a map from
// integration URI to integration ID.
func (a *AWSAPIGateway) listIntegrations(ctx context.Context, client *apigatewayv2.Client) (map[string]string, error) {
	result := make(map[string]string)
	var nextToken *string
	for {
		out, err := client.GetIntegrations(ctx, &apigatewayv2.GetIntegrationsInput{
			ApiId:     aws.String(a.apiID),
			NextToken: nextToken,
		})
		if err != nil {
			return nil, fmt.Errorf("GetIntegrations: %w", err)
		}
		for _, item := range out.Items {
			if item.IntegrationUri != nil && item.IntegrationId != nil {
				result[aws.ToString(item.IntegrationUri)] = aws.ToString(item.IntegrationId)
			}
		}
		if out.NextToken == nil {
			break
		}
		nextToken = out.NextToken
	}
	return result, nil
}

// listRoutes fetches all routes for the API, returning a map from route key
// (e.g. "GET /foo") to route ID.
func (a *AWSAPIGateway) listRoutes(ctx context.Context, client *apigatewayv2.Client) (map[string]string, error) {
	result := make(map[string]string)
	var nextToken *string
	for {
		out, err := client.GetRoutes(ctx, &apigatewayv2.GetRoutesInput{
			ApiId:     aws.String(a.apiID),
			NextToken: nextToken,
		})
		if err != nil {
			return nil, fmt.Errorf("GetRoutes: %w", err)
		}
		for _, item := range out.Items {
			if item.RouteKey != nil && item.RouteId != nil {
				result[aws.ToString(item.RouteKey)] = aws.ToString(item.RouteId)
			}
		}
		if out.NextToken == nil {
			break
		}
		nextToken = out.NextToken
	}
	return result, nil
}

// ensureIntegration finds an existing HTTP_PROXY integration for the route's backend
// URI, or creates a new one. Returns the integration ID.
func (a *AWSAPIGateway) ensureIntegration(
	ctx context.Context,
	client *apigatewayv2.Client,
	existing map[string]string,
	route GatewayRoute,
) (string, error) {
	integrationURI := route.Backend
	if !strings.HasPrefix(integrationURI, "http://") && !strings.HasPrefix(integrationURI, "https://") {
		integrationURI = "http://" + integrationURI
	}

	if id, ok := existing[integrationURI]; ok {
		return id, nil
	}

	out, err := client.CreateIntegration(ctx, &apigatewayv2.CreateIntegrationInput{
		ApiId:                aws.String(a.apiID),
		IntegrationType:      apigwv2types.IntegrationTypeHttpProxy,
		IntegrationUri:       aws.String(integrationURI),
		IntegrationMethod:    aws.String("ANY"),
		PayloadFormatVersion: aws.String("1.0"),
	})
	if err != nil {
		return "", fmt.Errorf("CreateIntegration: %w", err)
	}
	id := aws.ToString(out.IntegrationId)
	existing[integrationURI] = id
	a.logger.Info("Created API Gateway integration",
		"api_id", a.apiID, "uri", integrationURI, "integration_id", id)
	return id, nil
}

// upsertRoutes creates or updates routes in API Gateway for a workflow route.
// One route is created per HTTP method (or a single ANY route if none specified).
func (a *AWSAPIGateway) upsertRoutes(
	ctx context.Context,
	client *apigatewayv2.Client,
	existing map[string]string,
	route GatewayRoute,
	integrationID string,
) error {
	target := fmt.Sprintf("integrations/%s", integrationID)
	path := route.PathPrefix
	if path == "" {
		path = "/"
	}

	methods := route.Methods
	if len(methods) == 0 {
		methods = []string{"ANY"}
	}

	for _, method := range methods {
		routeKey := fmt.Sprintf("%s %s", strings.ToUpper(method), path)

		if existingID, ok := existing[routeKey]; ok {
			if _, err := client.UpdateRoute(ctx, &apigatewayv2.UpdateRouteInput{
				ApiId:   aws.String(a.apiID),
				RouteId: aws.String(existingID),
				Target:  aws.String(target),
			}); err != nil {
				return fmt.Errorf("UpdateRoute %q: %w", routeKey, err)
			}
			a.logger.Info("Updated API Gateway route", "api_id", a.apiID, "route_key", routeKey)
		} else {
			out, err := client.CreateRoute(ctx, &apigatewayv2.CreateRouteInput{
				ApiId:    aws.String(a.apiID),
				RouteKey: aws.String(routeKey),
				Target:   aws.String(target),
			})
			if err != nil {
				return fmt.Errorf("CreateRoute %q: %w", routeKey, err)
			}
			existing[routeKey] = aws.ToString(out.RouteId)
			a.logger.Info("Created API Gateway route",
				"api_id", a.apiID, "route_key", routeKey, "route_id", aws.ToString(out.RouteId))
		}
	}

	return nil
}

// Region returns the configured AWS region.
func (a *AWSAPIGateway) Region() string { return a.region }

// APIID returns the configured API ID.
func (a *AWSAPIGateway) APIID() string { return a.apiID }

// Stage returns the configured deployment stage.
func (a *AWSAPIGateway) Stage() string { return a.stage }
