package registrydo_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/plugin/registry"
	registrydo "github.com/GoCodeAlone/workflow/plugins/registry-do"
)

func TestDOProvider_Prune_FailsClosedBeforeCredentials(t *testing.T) {
	// An empty fixture token makes even the historical implementation unable to
	// reach a real command or registry API. The safety check must run first.
	t.Setenv("WFCTL_PRUNE_TEST_UNUSED_TOKEN", "")
	cases := []struct {
		name, path, ttl, want string
		keep                  int
	}{
		{"registry root", "registry.digitalocean.com/myorg", "", "requires an exact", 5},
		{"wrong host", "example.com/myorg/image", "", "requires an exact", 5},
		{"URL scheme", "https://registry.digitalocean.com/myorg/image", "", "requires an exact", 5},
		{"empty owner", "registry.digitalocean.com//image", "", "requires an exact", 5},
		{"empty image", "registry.digitalocean.com/myorg/", "", "requires an exact", 5},
		{"tag suffix", "registry.digitalocean.com/myorg/image:latest", "", "requires an exact", 5},
		{"digest suffix", "registry.digitalocean.com/myorg/image@sha256:abc", "", "requires an exact", 5},
		{"traversal", "registry.digitalocean.com/myorg/../image", "", "requires an exact", 5},
		{"query", "registry.digitalocean.com/myorg/image?scope=all", "", "requires an exact", 5},
		{"encoded separator", "registry.digitalocean.com/myorg/image%2fall", "", "requires an exact", 5},
		{"whitespace", "registry.digitalocean.com/myorg/image all", "", "requires an exact", 5},
		{"exact scope without protection or approval", "registry.digitalocean.com/myorg/image", "", "deployed/rollback digests", 5},
		{"nested exact scope without protection or approval", "registry.digitalocean.com/myorg/group/image", "", "deployed/rollback digests", 5},
		{"unsupported TTL", "registry.digitalocean.com/myorg/image", "168h", "untagged_ttl is unsupported", 5},
		{"invalid keep", "registry.digitalocean.com/myorg/image", "", "keep_latest must be positive", 0},
	}
	for _, tc := range cases {
		for _, dry := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/dry=%t", tc.name, dry), func(t *testing.T) {
				reg := config.CIRegistry{
					Name: "fixture", Type: "do", Path: tc.path,
					Auth:      &config.CIRegistryAuth{Env: "WFCTL_PRUNE_TEST_UNUSED_TOKEN"},
					Retention: &config.CIRegistryRetention{KeepLatest: tc.keep, UntaggedTTL: tc.ttl},
				}
				var out bytes.Buffer
				err := registrydo.New().Prune(registry.NewContext(t.Context(), &out, dry), registry.ProviderConfig{Registry: reg})
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("Prune() = %v, want safety refusal containing %q", err, tc.want)
				}
				if tc.want == "deployed/rollback digests" && !strings.Contains(err.Error(), "explicit approval of each") {
					t.Fatalf("safety refusal omits action approval: %v", err)
				}
				if out.Len() != 0 {
					t.Fatalf("refused prune must not advertise or execute commands: %q", out.String())
				}
			})
		}
	}
}

func TestDOProvider_Prune_NoRetention_NoOp(t *testing.T) {
	for _, dry := range []bool{false, true} {
		var out bytes.Buffer
		reg := config.CIRegistry{Name: "fixture", Type: "do", Path: "registry.digitalocean.com/myorg"}
		if err := registrydo.New().Prune(registry.NewContext(t.Context(), &out, dry), registry.ProviderConfig{Registry: reg}); err != nil {
			t.Fatalf("no retention: %v", err)
		}
		if out.Len() != 0 {
			t.Fatalf("no retention must produce no commands: %q", out.String())
		}
	}
}
