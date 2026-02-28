package external

import (
	"testing"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
)

// newTestAdapter builds an ExternalPluginAdapter with a populated manifest
// and optional config fragment without a real gRPC connection.
func newTestAdapter(manifest *pb.Manifest, configFragment []byte) *ExternalPluginAdapter {
	return &ExternalPluginAdapter{
		name:           manifest.Name,
		manifest:       manifest,
		configFragment: configFragment,
	}
}

func TestIsSamplePlugin_True(t *testing.T) {
	a := newTestAdapter(&pb.Manifest{
		Name:           "my-sample",
		SampleCategory: "ecommerce",
	}, nil)
	if !a.IsSamplePlugin() {
		t.Error("expected IsSamplePlugin()=true when SampleCategory is set")
	}
}

func TestIsSamplePlugin_False(t *testing.T) {
	a := newTestAdapter(&pb.Manifest{Name: "regular-plugin"}, nil)
	if a.IsSamplePlugin() {
		t.Error("expected IsSamplePlugin()=false when SampleCategory is empty")
	}
}

func TestIsConfigMutable_True(t *testing.T) {
	a := newTestAdapter(&pb.Manifest{
		Name:          "mutable-plugin",
		ConfigMutable: true,
	}, nil)
	if !a.IsConfigMutable() {
		t.Error("expected IsConfigMutable()=true")
	}
}

func TestIsConfigMutable_False(t *testing.T) {
	a := newTestAdapter(&pb.Manifest{Name: "immutable-plugin"}, nil)
	if a.IsConfigMutable() {
		t.Error("expected IsConfigMutable()=false when not set")
	}
}

func TestSampleCategory(t *testing.T) {
	a := newTestAdapter(&pb.Manifest{
		Name:           "cat-plugin",
		SampleCategory: "analytics",
	}, nil)
	if a.SampleCategory() != "analytics" {
		t.Errorf("expected SampleCategory=analytics, got %q", a.SampleCategory())
	}
}

func TestConfigFragmentBytes(t *testing.T) {
	frag := []byte("modules:\n  - name: foo\n")
	a := newTestAdapter(&pb.Manifest{Name: "frag-plugin"}, frag)
	if string(a.ConfigFragmentBytes()) != string(frag) {
		t.Errorf("expected config fragment %q, got %q", frag, a.ConfigFragmentBytes())
	}
}

func TestConfigFragmentBytes_Nil(t *testing.T) {
	a := newTestAdapter(&pb.Manifest{Name: "empty-plugin"}, nil)
	if a.ConfigFragmentBytes() != nil {
		t.Error("expected nil config fragment")
	}
}
