package sensitive

import (
	"context"
	"regexp"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/secrets"
)

type memoryProvider struct {
	values map[string]string
}

func (p *memoryProvider) Name() string { return "memory" }
func (p *memoryProvider) Get(_ context.Context, key string) (string, error) {
	if v, ok := p.values[key]; ok {
		return v, nil
	}
	return "", secrets.ErrNotFound
}
func (p *memoryProvider) Set(_ context.Context, key, value string) error {
	p.values[key] = value
	return nil
}
func (p *memoryProvider) Delete(_ context.Context, key string) error {
	delete(p.values, key)
	return nil
}
func (p *memoryProvider) List(_ context.Context) ([]string, error) {
	out := make([]string, 0, len(p.values))
	for k := range p.values {
		out = append(out, k)
	}
	return out, nil
}

func TestSecretKey_GitHubSafeAndCollisionResistant(t *testing.T) {
	got := SecretKey("workflow-compute-staging-db", "uri")
	if ok, _ := regexp.MatchString(`^[A-Za-z_][A-Za-z0-9_]*$`, got); !ok {
		t.Fatalf("SecretKey = %q, want GitHub-compatible name", got)
	}
	if got == "workflow-compute-staging-db_uri" {
		t.Fatal("SecretKey must not preserve hyphens")
	}
	left := SecretKey("a-b", "uri")
	right := SecretKey("a_b", "uri")
	if left == right {
		t.Fatalf("SecretKey collision: %q == %q", left, right)
	}
	left = SecretKey("_a", "uri")
	right = SecretKey("a", "uri")
	if left == right {
		t.Fatalf("SecretKey underscore collision: %q == %q", left, right)
	}
	if got := SecretKey("github", "token"); regexp.MustCompile(`(?i)^github_`).MatchString(got) {
		t.Fatalf("SecretKey = %q, must not use GitHub-reserved prefix", got)
	}
}

func TestRoute_UsesProviderSafeSecretKey(t *testing.T) {
	provider := &memoryProvider{values: map[string]string{}}
	out := &interfaces.ResourceOutput{
		Outputs:   map[string]any{"uri": "postgres://secret"},
		Sensitive: map[string]bool{"uri": true},
	}
	sanitized, hydrated, err := Route(context.Background(), provider, "workflow-compute-staging-db", out)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	key := SecretKey("workflow-compute-staging-db", "uri")
	if _, ok := provider.values[key]; !ok {
		t.Fatalf("provider missing routed key %q; got %v", key, provider.values)
	}
	if hydrated[key] != "postgres://secret" {
		t.Fatalf("hydrated[%q] = %q", key, hydrated[key])
	}
	if sanitized["uri"] != Placeholder("workflow-compute-staging-db", "uri") {
		t.Fatalf("sanitized uri = %q", sanitized["uri"])
	}
}
