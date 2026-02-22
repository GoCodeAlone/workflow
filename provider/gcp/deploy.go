package gcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/GoCodeAlone/workflow/provider"
)

// cloudRunServiceRequest is the request body for creating or updating a Cloud Run service.
type cloudRunServiceRequest struct {
	Template cloudRunTemplate `json:"template"`
	Traffic  []trafficTarget  `json:"traffic,omitempty"`
}

// cloudRunTemplate defines the container template for a Cloud Run service.
type cloudRunTemplate struct {
	Containers []cloudRunContainer `json:"containers"`
}

// cloudRunContainer defines a container within a Cloud Run template.
type cloudRunContainer struct {
	Image string           `json:"image"`
	Ports []cloudRunPort   `json:"ports,omitempty"`
	Env   []cloudRunEnvVar `json:"env,omitempty"`
}

// cloudRunPort defines a port for a Cloud Run container.
type cloudRunPort struct {
	ContainerPort int    `json:"containerPort"`
	Name          string `json:"name,omitempty"`
}

// cloudRunEnvVar defines an environment variable for a Cloud Run container.
type cloudRunEnvVar struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// trafficTarget defines a traffic allocation for a Cloud Run service.
type trafficTarget struct {
	Type     string `json:"type"`
	Percent  int    `json:"percent"`
	Revision string `json:"revision,omitempty"`
}

// deployGKE handles deployment to Google Kubernetes Engine.
func (p *GCPProvider) deployGKE(_ context.Context, req provider.DeployRequest) (*provider.DeployResult, error) {
	return nil, fmt.Errorf("gcp: GKE deployment not yet implemented (project=%s, image=%s, env=%s)",
		p.config.ProjectID, req.Image, req.Environment)
}

// deployCloudRun creates or updates a Google Cloud Run service with the given container image.
// It attempts to update an existing service (PATCH) and falls back to creating a new one (POST)
// when the service does not yet exist.
func (p *GCPProvider) deployCloudRun(ctx context.Context, req provider.DeployRequest) (*provider.DeployResult, error) {
	serviceName, _ := req.Config["service_name"].(string)
	if serviceName == "" {
		serviceName = req.Environment
	}
	region := p.config.Region
	if region == "" {
		region = "us-central1"
	}

	startedAt := time.Now()
	baseURL := fmt.Sprintf("https://run.googleapis.com/v2/projects/%s/locations/%s/services",
		p.config.ProjectID, region)

	svcReq := cloudRunServiceRequest{
		Template: cloudRunTemplate{
			Containers: []cloudRunContainer{{Image: req.Image}},
		},
		Traffic: []trafficTarget{{
			Type:    "TRAFFIC_TARGET_ALLOCATION_TYPE_LATEST",
			Percent: 100,
		}},
	}
	body, err := json.Marshal(svcReq)
	if err != nil {
		return nil, fmt.Errorf("gcp: failed to marshal Cloud Run service request: %w", err)
	}

	// Try to update an existing service (PATCH).
	patchURL := fmt.Sprintf("%s/%s?updateMask=template,traffic", baseURL, serviceName)
	resp, err := p.doRequest(ctx, http.MethodPatch, patchURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("gcp: Cloud Run PATCH failed: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		// Service does not exist; create it.
		createURL := fmt.Sprintf("%s?serviceId=%s", baseURL, serviceName)
		resp, err = p.doRequest(ctx, http.MethodPost, createURL, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("gcp: Cloud Run create failed: %w", err)
		}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("gcp: failed to read Cloud Run deployment response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gcp: Cloud Run deployment returned HTTP %d: %s",
			resp.StatusCode, string(respBody))
	}

	deployID := fmt.Sprintf("projects/%s/locations/%s/services/%s",
		p.config.ProjectID, region, serviceName)
	return &provider.DeployResult{
		DeployID:  deployID,
		Status:    "in_progress",
		Message:   fmt.Sprintf("Cloud Run service %q deployment initiated", serviceName),
		StartedAt: startedAt,
	}, nil
}
