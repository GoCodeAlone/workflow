package sdk

import (
	"context"
	"errors"
	"testing"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
)

// --- minimal test providers ---

type minimalProvider struct{}

func (p *minimalProvider) Manifest() PluginManifest {
	return PluginManifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Author:  "tester",
	}
}

// assetProvider embeds AssetProvider in a PluginProvider.
type assetProvider struct {
	minimalProvider
	assets map[string][]byte
}

func (p *assetProvider) GetAsset(path string) ([]byte, string, error) {
	data, ok := p.assets[path]
	if !ok {
		return nil, "", errors.New("asset not found: " + path)
	}
	ct := detectContentType(path)
	return data, ct, nil
}

// sampleProvider returns manifest with ConfigMutable and SampleCategory set.
type sampleProvider struct {
	minimalProvider
}

func (p *sampleProvider) Manifest() PluginManifest {
	return PluginManifest{
		Name:           "sample-plugin",
		Version:        "1.0.0",
		Author:         "tester",
		ConfigMutable:  true,
		SampleCategory: "ecommerce",
	}
}

// --- tests ---

func TestGetAsset_WithAssetProvider(t *testing.T) {
	provider := &assetProvider{
		assets: map[string][]byte{
			"index.html": []byte("<html>hello</html>"),
			"app.js":     []byte("console.log('hi')"),
		},
	}
	srv := newGRPCServer(provider)

	resp, err := srv.GetAsset(context.Background(), &pb.GetAssetRequest{Path: "index.html"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Error != "" {
		t.Fatalf("unexpected response error: %s", resp.Error)
	}
	if string(resp.Content) != "<html>hello</html>" {
		t.Errorf("expected html content, got %q", resp.Content)
	}
	if resp.ContentType != "text/html" {
		t.Errorf("expected text/html content type, got %q", resp.ContentType)
	}
}

func TestGetAsset_JSMimeType(t *testing.T) {
	provider := &assetProvider{
		assets: map[string][]byte{
			"app.js": []byte("var x = 1;"),
		},
	}
	srv := newGRPCServer(provider)

	resp, err := srv.GetAsset(context.Background(), &pb.GetAssetRequest{Path: "app.js"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ContentType != "application/javascript" {
		t.Errorf("expected application/javascript, got %q", resp.ContentType)
	}
}

func TestGetAsset_AssetNotFound(t *testing.T) {
	provider := &assetProvider{assets: map[string][]byte{}}
	srv := newGRPCServer(provider)

	resp, err := srv.GetAsset(context.Background(), &pb.GetAssetRequest{Path: "missing.txt"})
	if err != nil {
		t.Fatalf("unexpected rpc error: %v", err)
	}
	if resp.Error == "" {
		t.Error("expected error for missing asset, got empty error")
	}
}

func TestGetAsset_WithoutAssetProvider(t *testing.T) {
	srv := newGRPCServer(&minimalProvider{})

	resp, err := srv.GetAsset(context.Background(), &pb.GetAssetRequest{Path: "index.html"})
	if err != nil {
		t.Fatalf("unexpected rpc error: %v", err)
	}
	if resp.Error == "" {
		t.Error("expected error when AssetProvider not implemented")
	}
}

func TestGetManifest_NewFields(t *testing.T) {
	srv := newGRPCServer(&sampleProvider{})

	m, err := srv.GetManifest(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !m.ConfigMutable {
		t.Error("expected ConfigMutable=true")
	}
	if m.SampleCategory != "ecommerce" {
		t.Errorf("expected SampleCategory=ecommerce, got %q", m.SampleCategory)
	}
}

// detectContentType maps common extensions to MIME types.
func detectContentType(path string) string {
	switch {
	case len(path) > 5 && path[len(path)-5:] == ".html":
		return "text/html"
	case len(path) > 4 && path[len(path)-4:] == ".css":
		return "text/css"
	case len(path) > 3 && path[len(path)-3:] == ".js":
		return "application/javascript"
	case len(path) > 4 && path[len(path)-4:] == ".png":
		return "image/png"
	default:
		return "application/octet-stream"
	}
}
