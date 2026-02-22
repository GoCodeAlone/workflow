package gcp

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/GoCodeAlone/workflow/plugin"
	"github.com/GoCodeAlone/workflow/provider"
	"golang.org/x/oauth2/google"
)

func init() {
	plugin.RegisterNativePluginFactory(func(_ *sql.DB, _ map[string]any) plugin.NativePlugin {
		return NewGCPProvider(GCPConfig{})
	})
}

// GCPConfig holds configuration for the GCP cloud provider.
type GCPConfig struct {
	ProjectID       string `json:"project_id" yaml:"project_id"`
	CredentialsJSON string `json:"credentials_json" yaml:"credentials_json"`
	Region          string `json:"region" yaml:"region"`
}

// HTTPDoer is an interface for making HTTP requests (allows testing).
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// GCPProvider implements CloudProvider for Google Cloud Platform.
type GCPProvider struct {
	config     GCPConfig
	httpClient HTTPDoer
	tokenFunc  func(ctx context.Context) (string, error)
}

// Compile-time interface check.
var _ provider.CloudProvider = (*GCPProvider)(nil)

// NewGCPProvider creates a new GCPProvider with the given configuration.
func NewGCPProvider(config GCPConfig) *GCPProvider {
	p := &GCPProvider{
		config:     config,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
	p.tokenFunc = p.defaultTokenFunc
	return p
}

// NewGCPProviderWithClient creates a GCPProvider with an injectable HTTP client and token
// function. This constructor is intended for testing.
func NewGCPProviderWithClient(config GCPConfig, client HTTPDoer, tokenFunc func(ctx context.Context) (string, error)) *GCPProvider {
	return &GCPProvider{
		config:     config,
		httpClient: client,
		tokenFunc:  tokenFunc,
	}
}

// defaultTokenFunc retrieves a GCP OAuth2 access token using Application Default Credentials
// or the configured service account JSON (which must be a service account key file).
func (p *GCPProvider) defaultTokenFunc(ctx context.Context) (string, error) {
	scopes := []string{"https://www.googleapis.com/auth/cloud-platform"}
	var (
		creds *google.Credentials
		err   error
	)
	if p.config.CredentialsJSON != "" {
		params := google.CredentialsParams{Scopes: scopes}
		creds, err = google.CredentialsFromJSONWithTypeAndParams(ctx, []byte(p.config.CredentialsJSON), google.ServiceAccount, params)
	} else {
		creds, err = google.FindDefaultCredentials(ctx, scopes...)
	}
	if err != nil {
		return "", fmt.Errorf("gcp: failed to load credentials: %w", err)
	}
	tok, err := creds.TokenSource.Token()
	if err != nil {
		return "", fmt.Errorf("gcp: failed to obtain access token: %w", err)
	}
	return tok.AccessToken, nil
}

// doRequest makes an authenticated GCP REST API call with a Bearer token.
func (p *GCPProvider) doRequest(ctx context.Context, method, endpoint string, body io.Reader) (*http.Response, error) {
	token, err := p.tokenFunc(ctx)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("gcp: failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return p.httpClient.Do(req)
}

func (p *GCPProvider) Name() string        { return "gcp" }
func (p *GCPProvider) Version() string     { return "1.0.0" }
func (p *GCPProvider) Description() string { return "GCP Cloud Provider (GKE, Cloud Run, GCR)" }

func (p *GCPProvider) UIPages() []plugin.UIPageDef {
	return []plugin.UIPageDef{
		{
			ID:       "gcp-settings",
			Label:    "GCP Settings",
			Icon:     "cloud",
			Category: "cloud-providers",
		},
	}
}

func (p *GCPProvider) Dependencies() []plugin.PluginDependency { return nil }
func (p *GCPProvider) OnEnable(_ plugin.PluginContext) error   { return nil }
func (p *GCPProvider) OnDisable(_ plugin.PluginContext) error  { return nil }

func (p *GCPProvider) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/providers/gcp/status", p.handleStatus)
	mux.HandleFunc("/api/v1/providers/gcp/regions", p.handleListRegions)
}

func (p *GCPProvider) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"provider":"gcp","status":"available","version":"1.0.0"}`))
}

func (p *GCPProvider) handleListRegions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"regions":["us-central1","us-east1","europe-west1","asia-east1"]}`))
}

func (p *GCPProvider) Deploy(ctx context.Context, req provider.DeployRequest) (*provider.DeployResult, error) {
	serviceType, _ := req.Config["service_type"].(string)
	switch serviceType {
	case "cloud-run":
		return p.deployCloudRun(ctx, req)
	default:
		return p.deployGKE(ctx, req)
	}
}

// cloudRunService represents relevant fields of the Cloud Run v2 service resource.
type cloudRunService struct {
	Name                  string              `json:"name"`
	UID                   string              `json:"uid"`
	Generation            int64               `json:"generation"`
	Conditions            []cloudRunCondition `json:"conditions"`
	LatestCreatedRevision string              `json:"latestCreatedRevision"`
	LatestReadyRevision   string              `json:"latestReadyRevision"`
}

// cloudRunCondition represents a condition on a Cloud Run service.
type cloudRunCondition struct {
	Type    string `json:"type"`
	State   string `json:"state"`
	Message string `json:"message"`
}

// GetDeploymentStatus queries the Cloud Run v2 API for the status of a service.
// deployID must be the full Cloud Run service resource name:
// "projects/{project}/locations/{region}/services/{service}".
func (p *GCPProvider) GetDeploymentStatus(ctx context.Context, deployID string) (*provider.DeployStatus, error) {
	endpoint := fmt.Sprintf("https://run.googleapis.com/v2/%s", deployID)
	resp, err := p.doRequest(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("gcp: GetDeploymentStatus request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("gcp: failed to read deployment status response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gcp: GetDeploymentStatus returned HTTP %d for %q: %s",
			resp.StatusCode, deployID, string(body))
	}
	var svc cloudRunService
	if err := json.Unmarshal(body, &svc); err != nil {
		return nil, fmt.Errorf("gcp: failed to parse Cloud Run service response: %w", err)
	}
	return cloudRunServiceToDeployStatus(deployID, &svc), nil
}

// cloudRunServiceToDeployStatus maps a Cloud Run service to a provider.DeployStatus.
func cloudRunServiceToDeployStatus(deployID string, svc *cloudRunService) *provider.DeployStatus {
	status := "in_progress"
	message := ""
	progress := 50
	for _, c := range svc.Conditions {
		if c.Type == "Ready" {
			switch c.State {
			case "CONDITION_SUCCEEDED":
				status = "succeeded"
				progress = 100
			case "CONDITION_FAILED":
				status = "failed"
				progress = 0
			}
			message = c.Message
		}
	}
	return &provider.DeployStatus{
		DeployID: deployID,
		Status:   status,
		Progress: progress,
		Message:  message,
	}
}

// Rollback directs 100% of traffic to the latest ready revision of the Cloud Run service.
// deployID must be the full Cloud Run service resource name.
func (p *GCPProvider) Rollback(ctx context.Context, deployID string) error {
	// Fetch the service to find the latest ready revision.
	endpoint := fmt.Sprintf("https://run.googleapis.com/v2/%s", deployID)
	resp, err := p.doRequest(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("gcp: Rollback failed to get service: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("gcp: failed to read service response for rollback: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("gcp: Rollback get service returned HTTP %d: %s", resp.StatusCode, string(body))
	}
	var svc cloudRunService
	if err := json.Unmarshal(body, &svc); err != nil {
		return fmt.Errorf("gcp: failed to parse service for rollback: %w", err)
	}
	if svc.LatestReadyRevision == "" {
		return fmt.Errorf("gcp: no ready revision found for rollback of %q", deployID)
	}

	// Patch the service to route 100% of traffic to the latest ready revision.
	patch := map[string]any{
		"traffic": []map[string]any{
			{
				"type":     "TRAFFIC_TARGET_ALLOCATION_TYPE_REVISION",
				"revision": svc.LatestReadyRevision,
				"percent":  100,
			},
		},
	}
	patchBody, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("gcp: failed to marshal rollback patch: %w", err)
	}
	patchURL := fmt.Sprintf("https://run.googleapis.com/v2/%s?updateMask=traffic", deployID)
	resp2, err := p.doRequest(ctx, http.MethodPatch, patchURL, bytes.NewReader(patchBody))
	if err != nil {
		return fmt.Errorf("gcp: Rollback PATCH request failed: %w", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		rollbackBody, _ := io.ReadAll(resp2.Body)
		return fmt.Errorf("gcp: Rollback returned HTTP %d: %s", resp2.StatusCode, string(rollbackBody))
	}
	return nil
}

// TestConnection verifies that the configured GCP project is accessible via the
// Cloud Resource Manager API.
func (p *GCPProvider) TestConnection(ctx context.Context, _ map[string]any) (*provider.ConnectionResult, error) {
	start := time.Now()
	endpoint := fmt.Sprintf("https://cloudresourcemanager.googleapis.com/v1/projects/%s", p.config.ProjectID)
	resp, err := p.doRequest(ctx, http.MethodGet, endpoint, nil)
	latency := time.Since(start)
	if err != nil {
		return &provider.ConnectionResult{
			Success: false,
			Message: fmt.Sprintf("gcp: connection test failed: %v", err),
			Latency: latency,
		}, nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return &provider.ConnectionResult{
			Success: false,
			Message: fmt.Sprintf("gcp: project %q returned HTTP %d", p.config.ProjectID, resp.StatusCode),
			Latency: latency,
		}, nil
	}
	return &provider.ConnectionResult{
		Success: true,
		Message: fmt.Sprintf("gcp: project %q is accessible", p.config.ProjectID),
		Latency: latency,
		Details: map[string]any{"project_id": p.config.ProjectID},
	}, nil
}

// monitoringTimeSeriesResponse is the response from the Cloud Monitoring v3 timeSeries list API.
type monitoringTimeSeriesResponse struct {
	TimeSeries []monitoringTimeSeries `json:"timeSeries"`
}

// monitoringTimeSeries represents a single time series in a monitoring response.
type monitoringTimeSeries struct {
	Points []monitoringDataPoint `json:"points"`
}

// monitoringDataPoint holds a single data point with its typed value.
type monitoringDataPoint struct {
	Value monitoringTypedValue `json:"value"`
}

// monitoringTypedValue holds a Cloud Monitoring metric value.
// GCP returns int64 values as JSON strings.
type monitoringTypedValue struct {
	Int64Value  *string  `json:"int64Value"`
	DoubleValue *float64 `json:"doubleValue"`
}

// GetMetrics fetches Cloud Monitoring metrics (request count and latency) for a Cloud Run service.
// deployID may be the full resource name ("projects/.../services/{service}") or just the service name.
func (p *GCPProvider) GetMetrics(ctx context.Context, deployID string, window time.Duration) (*provider.Metrics, error) {
	serviceName := deployID
	if idx := strings.LastIndex(deployID, "/"); idx >= 0 {
		serviceName = deployID[idx+1:]
	}
	endTime := time.Now().UTC()
	startTime := endTime.Add(-window)

	requestCount, err := p.fetchMetric(ctx, "run.googleapis.com/request_count", serviceName, startTime, endTime)
	if err != nil {
		return nil, fmt.Errorf("gcp: failed to fetch request_count metric: %w", err)
	}
	latencyMs, err := p.fetchMetric(ctx, "run.googleapis.com/request_latencies", serviceName, startTime, endTime)
	if err != nil {
		return nil, fmt.Errorf("gcp: failed to fetch request_latencies metric: %w", err)
	}
	return &provider.Metrics{
		RequestCount: int64(requestCount),
		Latency:      time.Duration(latencyMs) * time.Millisecond,
		CustomMetrics: map[string]any{
			"service": serviceName,
			"window":  window.String(),
		},
	}, nil
}

// fetchMetric queries Cloud Monitoring for a single metric type and returns the aggregated value.
func (p *GCPProvider) fetchMetric(ctx context.Context, metricType, serviceName string, startTime, endTime time.Time) (float64, error) {
	windowSec := int(endTime.Sub(startTime).Seconds())
	if windowSec < 60 {
		windowSec = 60
	}
	filter := fmt.Sprintf(
		`resource.type="cloud_run_revision" AND metric.type=%q AND resource.labels.service_name=%q`,
		metricType, serviceName,
	)
	params := url.Values{
		"filter":                          {filter},
		"interval.startTime":              {startTime.Format(time.RFC3339)},
		"interval.endTime":                {endTime.Format(time.RFC3339)},
		"aggregation.alignmentPeriod":     {fmt.Sprintf("%ds", windowSec)},
		"aggregation.perSeriesAligner":    {"ALIGN_MEAN"},
		"aggregation.crossSeriesReducer":  {"REDUCE_SUM"},
		"view":                            {"FULL"},
	}
	endpoint := fmt.Sprintf("https://monitoring.googleapis.com/v3/projects/%s/timeSeries?%s",
		p.config.ProjectID, params.Encode())

	resp, err := p.doRequest(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, fmt.Errorf("gcp: metrics request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("gcp: failed to read metrics response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("gcp: metrics API returned HTTP %d: %s", resp.StatusCode, string(body))
	}
	var result monitoringTimeSeriesResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, fmt.Errorf("gcp: failed to parse metrics response: %w", err)
	}
	var total float64
	for _, ts := range result.TimeSeries {
		for _, pt := range ts.Points {
			if pt.Value.DoubleValue != nil {
				total += *pt.Value.DoubleValue
			} else if pt.Value.Int64Value != nil {
				var v int64
				fmt.Sscanf(*pt.Value.Int64Value, "%d", &v) //nolint:errcheck // best-effort metric parsing
				total += float64(v)
			}
		}
	}
	return total, nil
}
