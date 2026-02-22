package gcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/provider"
)

// TestDeployCloudRun_Create verifies that deployCloudRun falls back to POST when PATCH
// returns 404 (service does not yet exist).
func TestDeployCloudRun_Create(t *testing.T) {
	var patchCalled, postCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.Method {
		case http.MethodPatch:
			patchCalled = true
			// Signal that the service does not exist yet.
			w.WriteHeader(http.StatusNotFound)
		case http.MethodPost:
			postCalled = true
			w.Header().Set("Content-Type", "application/json")
			// Return a minimal Cloud Run Operation response.
			fmt.Fprint(w, `{"name":"projects/proj/locations/us-central1/operations/op1"}`)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

	p := newTestProvider(server, GCPConfig{ProjectID: "proj", Region: "us-central1"})
	result, err := p.Deploy(context.Background(), provider.DeployRequest{
		Environment: "staging",
		Image:       "us-central1-docker.pkg.dev/proj/repo/app:v1",
		Config:      map[string]any{"service_type": "cloud-run", "service_name": "my-svc"},
	})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if !patchCalled {
		t.Error("expected PATCH to be called first")
	}
	if !postCalled {
		t.Error("expected POST to be called as fallback")
	}
	if result.Status != "in_progress" {
		t.Errorf("expected status 'in_progress', got %q", result.Status)
	}
	if !strings.Contains(result.DeployID, "my-svc") {
		t.Errorf("expected deployID to contain 'my-svc', got %q", result.DeployID)
	}
}

// TestDeployCloudRun_Update verifies that deployCloudRun uses PATCH when the service exists.
func TestDeployCloudRun_Update(t *testing.T) {
	var patchCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			patchCalled = true
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"name":"projects/proj/locations/us-central1/operations/op2"}`)
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
	defer server.Close()

	p := newTestProvider(server, GCPConfig{ProjectID: "proj", Region: "us-central1"})
	result, err := p.Deploy(context.Background(), provider.DeployRequest{
		Environment: "prod",
		Image:       "us-central1-docker.pkg.dev/proj/repo/app:v2",
		Config:      map[string]any{"service_type": "cloud-run", "service_name": "existing-svc"},
	})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if !patchCalled {
		t.Error("expected PATCH to be called for existing service")
	}
	if result.Status != "in_progress" {
		t.Errorf("expected status 'in_progress', got %q", result.Status)
	}
}

// TestDeployCloudRun_Error verifies that a non-200/non-404 PATCH response returns an error.
func TestDeployCloudRun_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error":"internal server error"}`)
	}))
	defer server.Close()

	p := newTestProvider(server, GCPConfig{ProjectID: "proj", Region: "us-central1"})
	_, err := p.Deploy(context.Background(), provider.DeployRequest{
		Environment: "prod",
		Image:       "img:v1",
		Config:      map[string]any{"service_type": "cloud-run"},
	})
	if err == nil {
		t.Fatal("expected error for HTTP 500 response")
	}
	if !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("error should mention HTTP 500, got: %v", err)
	}
}

// TestDeployCloudRun_DefaultServiceName verifies that the environment is used as the service
// name when no "service_name" key is present in Config.
func TestDeployCloudRun_DefaultServiceName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"name":"op"}`)
	}))
	defer server.Close()

	p := newTestProvider(server, GCPConfig{ProjectID: "proj", Region: "us-central1"})
	result, err := p.Deploy(context.Background(), provider.DeployRequest{
		Environment: "my-env",
		Image:       "img:v1",
		Config:      map[string]any{"service_type": "cloud-run"},
	})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if !strings.Contains(result.DeployID, "my-env") {
		t.Errorf("expected deployID to use environment name 'my-env', got %q", result.DeployID)
	}
}

// TestGetDeploymentStatus_Succeeded verifies status mapping when Cloud Run reports Ready=SUCCEEDED.
func TestGetDeploymentStatus_Succeeded(t *testing.T) {
	svc := cloudRunService{
		Name:       "projects/proj/locations/us-central1/services/svc",
		Generation: 2,
		Conditions: []cloudRunCondition{
			{Type: "Ready", State: "CONDITION_SUCCEEDED", Message: "all ok"},
		},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(svc)
	}))
	defer server.Close()

	p := newTestProvider(server, GCPConfig{ProjectID: "proj", Region: "us-central1"})
	deployID := "projects/proj/locations/us-central1/services/svc"
	status, err := p.GetDeploymentStatus(context.Background(), deployID)
	if err != nil {
		t.Fatalf("GetDeploymentStatus: %v", err)
	}
	if status.Status != "succeeded" {
		t.Errorf("expected 'succeeded', got %q", status.Status)
	}
	if status.Progress != 100 {
		t.Errorf("expected progress 100, got %d", status.Progress)
	}
	if status.Message != "all ok" {
		t.Errorf("expected message 'all ok', got %q", status.Message)
	}
}

// TestGetDeploymentStatus_Failed verifies status mapping when Cloud Run reports Ready=FAILED.
func TestGetDeploymentStatus_Failed(t *testing.T) {
	svc := cloudRunService{
		Conditions: []cloudRunCondition{
			{Type: "Ready", State: "CONDITION_FAILED", Message: "image not found"},
		},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(svc)
	}))
	defer server.Close()

	p := newTestProvider(server, GCPConfig{ProjectID: "proj", Region: "us-central1"})
	status, err := p.GetDeploymentStatus(context.Background(), "projects/proj/locations/us-central1/services/svc")
	if err != nil {
		t.Fatalf("GetDeploymentStatus: %v", err)
	}
	if status.Status != "failed" {
		t.Errorf("expected 'failed', got %q", status.Status)
	}
	if status.Progress != 0 {
		t.Errorf("expected progress 0, got %d", status.Progress)
	}
}

// TestGetDeploymentStatus_InProgress verifies status mapping when there is no Ready condition.
func TestGetDeploymentStatus_InProgress(t *testing.T) {
	svc := cloudRunService{
		Conditions: []cloudRunCondition{
			{Type: "ConfigurationsReady", State: "CONDITION_SUCCEEDED"},
		},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(svc)
	}))
	defer server.Close()

	p := newTestProvider(server, GCPConfig{ProjectID: "proj", Region: "us-central1"})
	status, err := p.GetDeploymentStatus(context.Background(), "projects/proj/locations/us-central1/services/svc")
	if err != nil {
		t.Fatalf("GetDeploymentStatus: %v", err)
	}
	if status.Status != "in_progress" {
		t.Errorf("expected 'in_progress', got %q", status.Status)
	}
	if status.Progress != 50 {
		t.Errorf("expected progress 50, got %d", status.Progress)
	}
}

// TestGetDeploymentStatus_Error verifies that a non-200 response returns an error.
func TestGetDeploymentStatus_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"error":"not found"}`)
	}))
	defer server.Close()

	p := newTestProvider(server, GCPConfig{ProjectID: "proj"})
	_, err := p.GetDeploymentStatus(context.Background(), "projects/proj/locations/us-central1/services/svc")
	if err == nil {
		t.Fatal("expected error for HTTP 404")
	}
}

// TestRollback_Success verifies that Rollback performs GET then PATCH.
func TestRollback_Success(t *testing.T) {
	var patchCalled bool
	svc := cloudRunService{
		LatestReadyRevision: "projects/proj/locations/us-central1/services/svc/revisions/svc-00001",
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			json.NewEncoder(w).Encode(svc)
		case http.MethodPatch:
			patchCalled = true
			fmt.Fprint(w, `{"name":"op"}`)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

	p := newTestProvider(server, GCPConfig{ProjectID: "proj", Region: "us-central1"})
	err := p.Rollback(context.Background(), "projects/proj/locations/us-central1/services/svc")
	if err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if !patchCalled {
		t.Error("expected PATCH to be called during rollback")
	}
}

// TestRollback_NoReadyRevision verifies that Rollback returns an error when there is no
// ready revision to roll back to.
func TestRollback_NoReadyRevision(t *testing.T) {
	svc := cloudRunService{LatestReadyRevision: ""}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(svc)
	}))
	defer server.Close()

	p := newTestProvider(server, GCPConfig{ProjectID: "proj", Region: "us-central1"})
	err := p.Rollback(context.Background(), "projects/proj/locations/us-central1/services/svc")
	if err == nil {
		t.Fatal("expected error when no ready revision is available")
	}
	if !strings.Contains(err.Error(), "no ready revision") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestRollback_PatchError verifies that a failed PATCH returns an error.
func TestRollback_PatchError(t *testing.T) {
	svc := cloudRunService{LatestReadyRevision: "rev-001"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(svc)
			return
		}
		// PATCH fails.
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error":"oops"}`)
	}))
	defer server.Close()

	p := newTestProvider(server, GCPConfig{ProjectID: "proj", Region: "us-central1"})
	err := p.Rollback(context.Background(), "projects/proj/locations/us-central1/services/svc")
	if err == nil {
		t.Fatal("expected error for failed PATCH")
	}
}

// TestTestConnection_Success verifies that TestConnection returns Success=true on HTTP 200.
func TestTestConnection_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if !strings.Contains(r.URL.Path, "my-project") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"projectId":"my-project","name":"My Project","lifecycleState":"ACTIVE"}`)
	}))
	defer server.Close()

	p := newTestProvider(server, GCPConfig{ProjectID: "my-project"})
	result, err := p.TestConnection(context.Background(), nil)
	if err != nil {
		t.Fatalf("TestConnection returned unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected Success=true, got false (message: %s)", result.Message)
	}
	if result.Latency == 0 {
		t.Error("expected non-zero latency")
	}
	if result.Details["project_id"] != "my-project" {
		t.Errorf("expected project_id in details, got %v", result.Details)
	}
}

// TestTestConnection_Failure verifies that TestConnection returns Success=false on HTTP 403.
func TestTestConnection_Failure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"error":{"code":403,"message":"permission denied"}}`)
	}))
	defer server.Close()

	p := newTestProvider(server, GCPConfig{ProjectID: "my-project"})
	result, err := p.TestConnection(context.Background(), nil)
	if err != nil {
		t.Fatalf("TestConnection: %v", err)
	}
	if result.Success {
		t.Error("expected Success=false for HTTP 403")
	}
	if !strings.Contains(result.Message, "HTTP 403") {
		t.Errorf("expected message to mention HTTP 403, got: %s", result.Message)
	}
}

// TestGetMetrics_Success verifies that GetMetrics parses a Cloud Monitoring response correctly.
func TestGetMetrics_Success(t *testing.T) {
	reqCount := "42"
	latency := 125.5
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		// First call = request_count, second call = request_latencies.
		var resp monitoringTimeSeriesResponse
		if strings.Contains(r.URL.RawQuery, "request_count") {
			resp = monitoringTimeSeriesResponse{
				TimeSeries: []monitoringTimeSeries{
					{Points: []monitoringDataPoint{{Value: monitoringTypedValue{Int64Value: &reqCount}}}},
				},
			}
		} else {
			resp = monitoringTimeSeriesResponse{
				TimeSeries: []monitoringTimeSeries{
					{Points: []monitoringDataPoint{{Value: monitoringTypedValue{DoubleValue: &latency}}}},
				},
			}
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := newTestProvider(server, GCPConfig{ProjectID: "proj"})
	metrics, err := p.GetMetrics(context.Background(), "projects/proj/locations/us-central1/services/svc", 5*time.Minute)
	if err != nil {
		t.Fatalf("GetMetrics: %v", err)
	}
	if metrics.RequestCount != 42 {
		t.Errorf("expected RequestCount=42, got %d", metrics.RequestCount)
	}
	if metrics.Latency != time.Duration(125)*time.Millisecond {
		t.Errorf("expected Latency=125ms, got %v", metrics.Latency)
	}
	if callCount != 2 {
		t.Errorf("expected 2 monitoring API calls, got %d", callCount)
	}
}

// TestGetMetrics_EmptyResponse verifies that GetMetrics returns zero values when there is no data.
func TestGetMetrics_EmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(monitoringTimeSeriesResponse{})
	}))
	defer server.Close()

	p := newTestProvider(server, GCPConfig{ProjectID: "proj"})
	metrics, err := p.GetMetrics(context.Background(), "svc", time.Minute)
	if err != nil {
		t.Fatalf("GetMetrics: %v", err)
	}
	if metrics.RequestCount != 0 {
		t.Errorf("expected RequestCount=0, got %d", metrics.RequestCount)
	}
}

// TestGetMetrics_Error verifies that GetMetrics returns an error on a non-200 response.
func TestGetMetrics_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"error":"permission denied"}`)
	}))
	defer server.Close()

	p := newTestProvider(server, GCPConfig{ProjectID: "proj"})
	_, err := p.GetMetrics(context.Background(), "svc", time.Minute)
	if err == nil {
		t.Fatal("expected error for HTTP 403 monitoring response")
	}
}
