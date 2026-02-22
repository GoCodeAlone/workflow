package gcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/GoCodeAlone/workflow/provider"
)

// artifactRegistryDockerImagesResponse is the response from the Artifact Registry
// projects.locations.repositories.dockerImages.list API.
type artifactRegistryDockerImagesResponse struct {
	DockerImages  []artifactRegistryDockerImage `json:"dockerImages"`
	NextPageToken string                        `json:"nextPageToken"`
}

// artifactRegistryDockerImage represents a single Docker image in Artifact Registry.
type artifactRegistryDockerImage struct {
	Name           string   `json:"name"`
	URI            string   `json:"uri"`
	Tags           []string `json:"tags"`
	ImageSizeBytes string   `json:"imageSizeBytes"`
	UploadTime     string   `json:"uploadTime"`
	UpdateTime     string   `json:"updateTime"`
}

// ListImages lists container images in a Google Artifact Registry repository.
// The repo parameter is the repository name within the configured project and region.
func (p *GCPProvider) ListImages(ctx context.Context, repo string) ([]provider.ImageTag, error) {
	region := p.config.Region
	if region == "" {
		region = "us"
	}
	endpoint := fmt.Sprintf(
		"https://artifactregistry.googleapis.com/v1/projects/%s/locations/%s/repositories/%s/dockerImages",
		p.config.ProjectID, region, repo,
	)

	var allImages []provider.ImageTag
	pageToken := ""
	for {
		reqURL := endpoint
		if pageToken != "" {
			reqURL += "?pageToken=" + url.QueryEscape(pageToken)
		}
		resp, err := p.doRequest(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return nil, fmt.Errorf("gcp: ListImages request failed: %w", err)
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("gcp: failed to read ListImages response: %w", readErr)
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("gcp: ListImages returned HTTP %d for repo %q: %s",
				resp.StatusCode, repo, string(body))
		}
		var result artifactRegistryDockerImagesResponse
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("gcp: failed to parse ListImages response: %w", err)
		}
		for _, img := range result.DockerImages {
			sizeBytes, err := strconv.ParseInt(img.ImageSizeBytes, 10, 64)
			if err != nil && img.ImageSizeBytes != "" {
				return nil, fmt.Errorf("gcp: failed to parse image size %q for image %q: %w",
					img.ImageSizeBytes, img.URI, err)
			}
			var pushedAt time.Time
			if img.UploadTime != "" {
				pushedAt, err = time.Parse(time.RFC3339, img.UploadTime)
				if err != nil {
					return nil, fmt.Errorf("gcp: failed to parse upload time %q for image %q: %w",
						img.UploadTime, img.URI, err)
				}
			}
			tags := img.Tags
			if len(tags) == 0 {
				tags = []string{""}
			}
			for _, tag := range tags {
				allImages = append(allImages, provider.ImageTag{
					Repository: img.URI,
					Tag:        tag,
					Size:       sizeBytes,
					PushedAt:   pushedAt,
				})
			}
		}
		if result.NextPageToken == "" {
			break
		}
		pageToken = result.NextPageToken
	}
	return allImages, nil
}

// PushImage validates that a container image is present in Google Artifact Registry after pushing.
// The actual image data must be pushed using `docker push` or a compatible tool before calling
// this method. PushImage verifies the image is accessible via the Docker Registry v2 manifest API.
func (p *GCPProvider) PushImage(ctx context.Context, image string, auth provider.RegistryAuth) error {
	return p.checkManifest(ctx, image, auth)
}

// PullImage validates that a container image is accessible in Google Artifact Registry.
// It verifies the image is reachable via the Docker Registry v2 manifest API.
func (p *GCPProvider) PullImage(ctx context.Context, image string, auth provider.RegistryAuth) error {
	return p.checkManifest(ctx, image, auth)
}

// checkManifest verifies that a container image is accessible using the Docker Registry v2
// manifest HEAD endpoint. It uses the auth token if provided, otherwise falls back to GCP
// Application Default Credentials.
func (p *GCPProvider) checkManifest(ctx context.Context, image string, auth provider.RegistryAuth) error {
	host, imagePath, ref := parseImageRef(image)
	if host == "" {
		region := p.config.Region
		if region == "" {
			region = "us-central1"
		}
		host = fmt.Sprintf("%s-docker.pkg.dev", region)
	}

	token := auth.Token
	if token == "" && auth.Password != "" {
		token = auth.Password
	}
	if token == "" {
		var err error
		token, err = p.tokenFunc(ctx)
		if err != nil {
			return fmt.Errorf("gcp: failed to get registry token for image %q: %w", image, err)
		}
	}

	manifestURL := fmt.Sprintf("https://%s/v2/%s/manifests/%s", host, imagePath, ref)
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, manifestURL, nil)
	if err != nil {
		return fmt.Errorf("gcp: failed to create manifest request for image %q: %w", image, err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.docker.distribution.manifest.v2+json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("gcp: manifest check failed for image %q: %w", image, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("gcp: image %q not accessible in registry (HTTP %d)", image, resp.StatusCode)
	}
	return nil
}

// parseImageRef parses an image reference such as "host/path:tag" or "host/path@digest"
// and returns (host, imagePath, reference). If no registry host is detected the returned
// host is an empty string.
func parseImageRef(image string) (host, imagePath, ref string) {
	ref = "latest"

	// Separate digest reference (path@digest).
	if atIdx := strings.Index(image, "@"); atIdx > 0 {
		ref = image[atIdx+1:]
		image = image[:atIdx]
	} else {
		// Separate tag (last colon that appears after the last slash).
		if colonIdx := strings.LastIndex(image, ":"); colonIdx > strings.LastIndex(image, "/") {
			ref = image[colonIdx+1:]
			image = image[:colonIdx]
		}
	}

	// The first path component is a registry host when it contains a dot or colon
	// (e.g., "us-central1-docker.pkg.dev" or "localhost:5000").
	parts := strings.SplitN(image, "/", 2)
	if len(parts) == 2 && (strings.Contains(parts[0], ".") || strings.Contains(parts[0], ":")) {
		return parts[0], parts[1], ref
	}
	return "", image, ref
}
