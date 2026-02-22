package gcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/provider"
)

// staticTokenFunc returns a hard-coded token; used to avoid real GCP credential calls in tests.
func staticTokenFunc(_ context.Context) (string, error) {
	return "test-token", nil
}

// redirectTransport rewrites every outgoing request to point at the given test server,
// preserving the original path and query string.
type redirectTransport struct {
	serverURL string
}

func (t *redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base, err := url.Parse(t.serverURL)
	if err != nil {
		return nil, fmt.Errorf("redirectTransport: invalid server URL: %w", err)
	}
	req2 := req.Clone(req.Context())
	req2.URL.Scheme = base.Scheme
	req2.URL.Host = base.Host
	return http.DefaultTransport.RoundTrip(req2)
}

// newTestHTTPClient returns an HTTPDoer backed by an http.Client whose transport
// redirects every request to the given httptest.Server.
func newTestHTTPClient(server *httptest.Server) HTTPDoer {
	return &http.Client{Transport: &redirectTransport{serverURL: server.URL}}
}

// newTestProvider creates a GCPProvider that sends all HTTP requests to server.
func newTestProvider(server *httptest.Server, cfg GCPConfig) *GCPProvider {
	return NewGCPProviderWithClient(cfg, newTestHTTPClient(server), staticTokenFunc)
}

// TestListImages verifies that ListImages parses a successful Artifact Registry response.
func TestListImages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if !strings.Contains(r.URL.Path, "dockerImages") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		resp := artifactRegistryDockerImagesResponse{
			DockerImages: []artifactRegistryDockerImage{
				{
					Name:           "projects/my-project/locations/us/repositories/my-repo/dockerImages/img@sha256:abc",
					URI:            "us-docker.pkg.dev/my-project/my-repo/img@sha256:abc",
					Tags:           []string{"latest", "v1.0"},
					ImageSizeBytes: "12345",
					UploadTime:     "2023-01-01T00:00:00Z",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := newTestProvider(server, GCPConfig{ProjectID: "my-project", Region: "us"})
	images, err := p.ListImages(context.Background(), "my-repo")
	if err != nil {
		t.Fatalf("ListImages: %v", err)
	}
	// One image with two tags produces two ImageTag entries.
	if len(images) != 2 {
		t.Errorf("expected 2 ImageTag entries, got %d", len(images))
	}
	if images[0].Tag != "latest" {
		t.Errorf("expected first tag 'latest', got %q", images[0].Tag)
	}
	if images[1].Tag != "v1.0" {
		t.Errorf("expected second tag 'v1.0', got %q", images[1].Tag)
	}
	if images[0].Size != 12345 {
		t.Errorf("expected size 12345, got %d", images[0].Size)
	}
}

// TestListImages_Pagination verifies that ListImages follows the nextPageToken.
func TestListImages_Pagination(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var resp artifactRegistryDockerImagesResponse
		if r.URL.Query().Get("pageToken") == "" {
			resp = artifactRegistryDockerImagesResponse{
				DockerImages:  []artifactRegistryDockerImage{{URI: "img1", Tags: []string{"t1"}}},
				NextPageToken: "token2",
			}
		} else {
			resp = artifactRegistryDockerImagesResponse{
				DockerImages: []artifactRegistryDockerImage{{URI: "img2", Tags: []string{"t2"}}},
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := newTestProvider(server, GCPConfig{ProjectID: "proj", Region: "us"})
	images, err := p.ListImages(context.Background(), "repo")
	if err != nil {
		t.Fatalf("ListImages: %v", err)
	}
	if len(images) != 2 {
		t.Errorf("expected 2 images across pages, got %d", len(images))
	}
	if callCount != 2 {
		t.Errorf("expected 2 API calls (pagination), got %d", callCount)
	}
}

// TestListImages_ErrorStatus verifies that a non-200 response is returned as an error.
func TestListImages_ErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":{"code":403,"message":"Permission denied"}}`))
	}))
	defer server.Close()

	p := newTestProvider(server, GCPConfig{ProjectID: "proj", Region: "us"})
	_, err := p.ListImages(context.Background(), "repo")
	if err == nil {
		t.Fatal("expected error for HTTP 403 response")
	}
	if !strings.Contains(err.Error(), "HTTP 403") {
		t.Errorf("error should mention HTTP 403, got: %v", err)
	}
}

// TestListImages_NoTags verifies that images without tags produce one entry with an empty tag.
func TestListImages_NoTags(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := artifactRegistryDockerImagesResponse{
			DockerImages: []artifactRegistryDockerImage{
				{URI: "gcr.io/proj/img@sha256:abc", Tags: nil, ImageSizeBytes: "0"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := newTestProvider(server, GCPConfig{ProjectID: "proj", Region: "us"})
	images, err := p.ListImages(context.Background(), "repo")
	if err != nil {
		t.Fatalf("ListImages: %v", err)
	}
	if len(images) != 1 {
		t.Errorf("expected 1 entry for image without tags, got %d", len(images))
	}
	if images[0].Tag != "" {
		t.Errorf("expected empty tag, got %q", images[0].Tag)
	}
}

// TestPushImage_Success verifies that PushImage succeeds when the manifest HEAD returns 200.
func TestPushImage_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if !strings.Contains(r.URL.Path, "manifests") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.docker.distribution.manifest.v2+json")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Use an image with the test server host embedded so redirectTransport works.
	serverHost := strings.TrimPrefix(server.URL, "http://")
	image := fmt.Sprintf("%s/my-project/my-repo/my-image:v1", serverHost)

	p := newTestProvider(server, GCPConfig{ProjectID: "my-project", Region: "us-central1"})
	err := p.PushImage(context.Background(), image, provider.RegistryAuth{Token: "test-token"})
	if err != nil {
		t.Fatalf("PushImage: %v", err)
	}
}

// TestPushImage_NotFound verifies that PushImage returns an error when the manifest is not found.
func TestPushImage_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	serverHost := strings.TrimPrefix(server.URL, "http://")
	image := fmt.Sprintf("%s/my-project/my-repo/my-image:v1", serverHost)

	p := newTestProvider(server, GCPConfig{ProjectID: "my-project", Region: "us-central1"})
	err := p.PushImage(context.Background(), image, provider.RegistryAuth{Token: "test-token"})
	if err == nil {
		t.Fatal("expected error when manifest not found")
	}
}

// TestPullImage_Success verifies that PullImage succeeds when the manifest HEAD returns 200.
func TestPullImage_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	serverHost := strings.TrimPrefix(server.URL, "http://")
	image := fmt.Sprintf("%s/proj/repo/img:latest", serverHost)

	p := newTestProvider(server, GCPConfig{ProjectID: "proj", Region: "us-central1"})
	if err := p.PullImage(context.Background(), image, provider.RegistryAuth{Token: "tok"}); err != nil {
		t.Fatalf("PullImage: %v", err)
	}
}

// TestPullImage_Unauthorized verifies that PullImage returns an error on HTTP 401.
func TestPullImage_Unauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	serverHost := strings.TrimPrefix(server.URL, "http://")
	image := fmt.Sprintf("%s/proj/repo/img:latest", serverHost)

	p := newTestProvider(server, GCPConfig{ProjectID: "proj", Region: "us-central1"})
	err := p.PullImage(context.Background(), image, provider.RegistryAuth{Token: "bad-token"})
	if err == nil {
		t.Fatal("expected error for HTTP 401")
	}
}

// TestParseImageRef tests the image reference parser.
func TestParseImageRef(t *testing.T) {
	tests := []struct {
		input     string
		wantHost  string
		wantPath  string
		wantRef   string
	}{
		{"gcr.io/project/image:v1", "gcr.io", "project/image", "v1"},
		{"us-central1-docker.pkg.dev/proj/repo/img:latest", "us-central1-docker.pkg.dev", "proj/repo/img", "latest"},
		{"us-central1-docker.pkg.dev/proj/repo/img@sha256:abc", "us-central1-docker.pkg.dev", "proj/repo/img", "sha256:abc"},
		{"image:tag", "", "image", "tag"},
		{"image", "", "image", "latest"},
		{"localhost:5000/img:tag", "localhost:5000", "img", "tag"},
	}
	for _, tt := range tests {
		host, path, ref := parseImageRef(tt.input)
		if host != tt.wantHost || path != tt.wantPath || ref != tt.wantRef {
			t.Errorf("parseImageRef(%q) = (%q, %q, %q), want (%q, %q, %q)",
				tt.input, host, path, ref, tt.wantHost, tt.wantPath, tt.wantRef)
		}
	}
}

// TestPushImage_FallbackTokenFromProvider verifies that PushImage uses the provider token
// when no auth token is supplied.
func TestPushImage_FallbackTokenFromProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	serverHost := strings.TrimPrefix(server.URL, "http://")
	image := fmt.Sprintf("%s/proj/repo/img:latest", serverHost)

	// No token in RegistryAuth — provider tokenFunc is used.
	p := newTestProvider(server, GCPConfig{ProjectID: "proj", Region: "us-central1"})
	if err := p.PushImage(context.Background(), image, provider.RegistryAuth{}); err != nil {
		t.Fatalf("PushImage with fallback token: %v", err)
	}
}

// TestListImages_InvalidSizeBytes verifies that ListImages returns an error when
// the API returns a non-numeric imageSizeBytes value.
func TestListImages_InvalidSizeBytes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := artifactRegistryDockerImagesResponse{
			DockerImages: []artifactRegistryDockerImage{
				{URI: "gcr.io/proj/img", Tags: []string{"v1"}, ImageSizeBytes: "not-a-number"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := newTestProvider(server, GCPConfig{ProjectID: "proj", Region: "us"})
	_, err := p.ListImages(context.Background(), "repo")
	if err == nil {
		t.Fatal("expected error for invalid imageSizeBytes")
	}
	if !strings.Contains(err.Error(), "parse image size") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestListImages_InvalidUploadTime verifies that ListImages returns an error when
// the API returns a non-RFC3339 uploadTime value.
func TestListImages_InvalidUploadTime(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := artifactRegistryDockerImagesResponse{
			DockerImages: []artifactRegistryDockerImage{
				{URI: "gcr.io/proj/img", Tags: []string{"v1"}, ImageSizeBytes: "100", UploadTime: "not-a-time"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := newTestProvider(server, GCPConfig{ProjectID: "proj", Region: "us"})
	_, err := p.ListImages(context.Background(), "repo")
	if err == nil {
		t.Fatal("expected error for invalid uploadTime")
	}
	if !strings.Contains(err.Error(), "parse upload time") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestNewGCPProviderWithClient_NilSafety verifies that nil client and tokenFunc
// arguments do not cause panics; the provider falls back to safe defaults.
func TestNewGCPProviderWithClient_NilSafety(t *testing.T) {
	// Should not panic.
	p := NewGCPProviderWithClient(GCPConfig{ProjectID: "proj"}, nil, nil)
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
	if p.httpClient == nil {
		t.Error("expected non-nil httpClient after nil default")
	}
	if p.tokenFunc == nil {
		t.Error("expected non-nil tokenFunc after nil default")
	}
}
