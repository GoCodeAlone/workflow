package sensitive

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/secrets"
)

// fakeProvider records Set/Delete/Get/List calls for assertions.
type fakeProvider struct {
	values map[string]string
	setErr map[string]error // per-key Set override
	delErr map[string]error
	setLog []string
	delLog []string
}

func newFakeProvider() *fakeProvider {
	return &fakeProvider{values: map[string]string{}, setErr: map[string]error{}, delErr: map[string]error{}}
}

func (p *fakeProvider) Name() string { return "fake" }
func (p *fakeProvider) Get(_ context.Context, k string) (string, error) {
	v, ok := p.values[k]
	if !ok {
		return "", secrets.ErrNotFound
	}
	return v, nil
}
func (p *fakeProvider) Set(_ context.Context, k, v string) error {
	if err, ok := p.setErr[k]; ok {
		return err
	}
	p.setLog = append(p.setLog, k)
	p.values[k] = v
	return nil
}
func (p *fakeProvider) Delete(_ context.Context, k string) error {
	if err, ok := p.delErr[k]; ok {
		return err
	}
	p.delLog = append(p.delLog, k)
	delete(p.values, k)
	return nil
}
func (p *fakeProvider) List(_ context.Context) ([]string, error) {
	out := make([]string, 0, len(p.values))
	for k := range p.values {
		out = append(out, k)
	}
	return out, nil
}

func TestRoute_NoSensitive_PassesThrough(t *testing.T) {
	p := newFakeProvider()
	out := &interfaces.ResourceOutput{
		Name: "x", Type: "infra.spaces_key",
		Outputs: map[string]any{"bucket": "b", "endpoint": "e"},
	}
	sanitized, hydrated, err := Route(context.Background(), p, "myres", out)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if len(p.setLog) != 0 {
		t.Errorf("expected no Set calls, got %v", p.setLog)
	}
	if sanitized["bucket"] != "b" || sanitized["endpoint"] != "e" {
		t.Errorf("non-sensitive outputs corrupted: %v", sanitized)
	}
	if len(hydrated) != 0 {
		t.Errorf("expected empty hydrated, got %v", hydrated)
	}
}

func TestRoute_SensitiveValuePresent_RoutesAndSanitizes(t *testing.T) {
	p := newFakeProvider()
	out := &interfaces.ResourceOutput{
		Outputs:   map[string]any{"access_key": "AK", "secret_key": "SECRET", "bucket": "b"},
		Sensitive: map[string]bool{"secret_key": true, "access_key": true},
	}
	sanitized, hydrated, err := Route(context.Background(), p, "myres", out)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	secretKey := SecretKey("myres", "secret_key")
	accessKey := SecretKey("myres", "access_key")
	if p.values[secretKey] != "SECRET" {
		t.Errorf("provider did not receive secret_key; got %v", p.values)
	}
	if p.values[accessKey] != "AK" {
		t.Errorf("provider did not receive access_key; got %v", p.values)
	}
	if sanitized["secret_key"] != Placeholder("myres", "secret_key") {
		t.Errorf("sanitized[secret_key] = %v, want placeholder", sanitized["secret_key"])
	}
	if sanitized["access_key"] != Placeholder("myres", "access_key") {
		t.Errorf("sanitized[access_key] = %v, want placeholder", sanitized["access_key"])
	}
	if sanitized["bucket"] != "b" {
		t.Errorf("non-sensitive bucket lost: %v", sanitized["bucket"])
	}
	if hydrated[secretKey] != "SECRET" {
		t.Errorf("hydrated missing secret_key: %v", hydrated)
	}
}

func TestRoute_SensitiveKeyAbsentFromOutputs_Skipped(t *testing.T) {
	p := newFakeProvider()
	out := &interfaces.ResourceOutput{
		Outputs:   map[string]any{"access_key": "AK"},
		Sensitive: map[string]bool{"secret_key": true, "access_key": true},
	}
	sanitized, hydrated, err := Route(context.Background(), p, "myres", out)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if _, ok := sanitized["secret_key"]; ok {
		t.Errorf("absent sensitive key should NOT yield placeholder, got %v", sanitized["secret_key"])
	}
	if _, ok := p.values[SecretKey("myres", "secret_key")]; ok {
		t.Errorf("provider should not have received secret_key (absent value)")
	}
	if hydrated[SecretKey("myres", "secret_key")] != "" {
		t.Errorf("hydrated should not contain absent key")
	}
	// access_key was present, should be routed
	if sanitized["access_key"] != Placeholder("myres", "access_key") {
		t.Errorf("access_key routing failed: %v", sanitized["access_key"])
	}
}

func TestRoute_SensitiveFalseValue_NotRouted(t *testing.T) {
	p := newFakeProvider()
	out := &interfaces.ResourceOutput{
		Outputs:   map[string]any{"bucket_uri": "https://b.example/"},
		Sensitive: map[string]bool{"bucket_uri": false},
	}
	sanitized, _, err := Route(context.Background(), p, "myres", out)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if sanitized["bucket_uri"] != "https://b.example/" {
		t.Errorf("Sensitive=false should not route, got %v", sanitized["bucket_uri"])
	}
	if len(p.setLog) != 0 {
		t.Errorf("Sensitive=false triggered Set: %v", p.setLog)
	}
}

func TestRoute_ProviderSetError_ReturnsError(t *testing.T) {
	p := newFakeProvider()
	p.setErr[SecretKey("myres", "secret_key")] = errors.New("boom")
	out := &interfaces.ResourceOutput{
		Outputs:   map[string]any{"secret_key": "S"},
		Sensitive: map[string]bool{"secret_key": true},
	}
	_, _, err := Route(context.Background(), p, "myres", out)
	if err == nil {
		t.Fatal("expected error from Set")
	}
}

func TestRoute_ProviderSetErrorReturnsPartialHydrated(t *testing.T) {
	p := newFakeProvider()
	failKey := SecretKey("myres", "b_key")
	p.setErr[failKey] = errors.New("boom")
	out := &interfaces.ResourceOutput{
		Outputs:   map[string]any{"a_key": "A", "b_key": "B"},
		Sensitive: map[string]bool{"a_key": true, "b_key": true},
	}
	_, hydrated, err := Route(context.Background(), p, "myres", out)
	if err == nil {
		t.Fatal("expected error from second Set")
	}
	routedKey := SecretKey("myres", "a_key")
	if hydrated[routedKey] != "A" {
		t.Fatalf("hydrated = %v, want partial routed key %q", hydrated, routedKey)
	}
	if _, ok := hydrated[failKey]; ok {
		t.Fatalf("hydrated contains failed key %q: %v", failKey, hydrated)
	}
}

func TestRoute_NilProviderWithSensitive_Errors(t *testing.T) {
	out := &interfaces.ResourceOutput{
		Outputs:   map[string]any{"secret_key": "S"},
		Sensitive: map[string]bool{"secret_key": true},
	}
	_, _, err := Route(context.Background(), nil, "myres", out)
	if err == nil {
		t.Fatal("expected error when provider nil and Sensitive non-empty")
	}
	if !strings.Contains(err.Error(), "myres") {
		t.Errorf("error should name resource, got %q", err.Error())
	}
}

func TestRoute_NilProviderWithoutSensitive_OK(t *testing.T) {
	out := &interfaces.ResourceOutput{
		Outputs: map[string]any{"bucket": "b"},
	}
	sanitized, hydrated, err := Route(context.Background(), nil, "myres", out)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if sanitized["bucket"] != "b" {
		t.Errorf("nil-provider non-sensitive corrupted: %v", sanitized)
	}
	if len(hydrated) != 0 {
		t.Errorf("expected empty hydrated, got %v", hydrated)
	}
}

func TestRoute_EmptyResourceName_Errors(t *testing.T) {
	p := newFakeProvider()
	out := &interfaces.ResourceOutput{
		Outputs:   map[string]any{"secret_key": "S"},
		Sensitive: map[string]bool{"secret_key": true},
	}
	_, _, err := Route(context.Background(), p, "", out)
	if err == nil {
		t.Fatal("expected error on empty resourceName")
	}
}

func TestRoute_DeterministicSetOrder(t *testing.T) {
	// Multiple sensitive keys: ensure Set order is sorted by key (deterministic).
	p := newFakeProvider()
	out := &interfaces.ResourceOutput{
		Outputs:   map[string]any{"a_key": "A", "b_key": "B", "c_key": "C"},
		Sensitive: map[string]bool{"a_key": true, "b_key": true, "c_key": true},
	}
	_, _, err := Route(context.Background(), p, "r", out)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	want := []string{SecretKey("r", "a_key"), SecretKey("r", "b_key"), SecretKey("r", "c_key")}
	if len(p.setLog) != len(want) {
		t.Fatalf("setLog len: got %v want %v", p.setLog, want)
	}
	for i, w := range want {
		if p.setLog[i] != w {
			t.Errorf("setLog[%d] = %v want %v", i, p.setLog[i], w)
		}
	}
}

func TestRoute_NonStringSensitiveValue_Errors(t *testing.T) {
	p := newFakeProvider()
	out := &interfaces.ResourceOutput{
		Outputs:   map[string]any{"port": 8080},
		Sensitive: map[string]bool{"port": true},
	}
	_, _, err := Route(context.Background(), p, "r", out)
	if err == nil {
		t.Fatal("expected error for non-string sensitive value")
	}
	if !strings.Contains(err.Error(), "not supported") {
		t.Errorf("error should explain non-string: %q", err.Error())
	}
}

func TestRoute_NilOut_Errors(t *testing.T) {
	_, _, err := Route(context.Background(), newFakeProvider(), "r", nil)
	if err == nil {
		t.Fatal("expected error for nil out")
	}
}

func TestRevoke_DeletesAllKeys(t *testing.T) {
	p := newFakeProvider()
	p.values[SecretKey("r", "secret_key")] = "S"
	p.values[SecretKey("r", "access_key")] = "A"
	if err := Revoke(context.Background(), p, "r", []string{"secret_key", "access_key"}); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if _, ok := p.values[SecretKey("r", "secret_key")]; ok {
		t.Errorf("secret_key not deleted")
	}
	if _, ok := p.values[SecretKey("r", "access_key")]; ok {
		t.Errorf("access_key not deleted")
	}
}

func TestRevoke_AggregatesErrors(t *testing.T) {
	p := newFakeProvider()
	p.values[SecretKey("r", "secret_key")] = "S"
	p.values[SecretKey("r", "access_key")] = "A"
	p.delErr[SecretKey("r", "secret_key")] = errors.New("boom1")
	p.delErr[SecretKey("r", "access_key")] = errors.New("boom2")
	err := Revoke(context.Background(), p, "r", []string{"secret_key", "access_key"})
	if err == nil {
		t.Fatal("expected aggregated error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "boom1") || !strings.Contains(msg, "boom2") {
		t.Errorf("aggregated error missing one or both: %q", msg)
	}
}

func TestRevoke_ContinuesAfterError(t *testing.T) {
	// First key errors; second key should still be Deleted.
	p := newFakeProvider()
	p.values[SecretKey("r", "secret_key")] = "S"
	p.values[SecretKey("r", "access_key")] = "A"
	p.delErr[SecretKey("r", "secret_key")] = errors.New("boom")
	_ = Revoke(context.Background(), p, "r", []string{"secret_key", "access_key"})
	if _, ok := p.values[SecretKey("r", "access_key")]; ok {
		t.Error("access_key not deleted (Revoke should continue past first error)")
	}
}

func TestRevoke_NotFoundIsSuccess(t *testing.T) {
	p := newFakeProvider()
	// Pre-populate one key; Delete on a missing one returns ErrNotFound.
	p.values[SecretKey("r", "secret_key")] = "S"
	p.delErr[SecretKey("r", "access_key")] = secrets.ErrNotFound
	if err := Revoke(context.Background(), p, "r", []string{"secret_key", "access_key"}); err != nil {
		t.Fatalf("Revoke should swallow ErrNotFound, got %v", err)
	}
}

func TestRevoke_NilProvider_NoOp(t *testing.T) {
	if err := Revoke(context.Background(), nil, "r", []string{"k"}); err != nil {
		t.Fatalf("nil provider should be no-op, got %v", err)
	}
}

func TestRevoke_EmptyResourceName_Errors(t *testing.T) {
	if err := Revoke(context.Background(), newFakeProvider(), "", []string{"k"}); err == nil {
		t.Fatal("expected error on empty resourceName")
	}
}

func TestIsPlaceholder(t *testing.T) {
	cases := map[string]bool{
		"secret_ref://x":     true,
		"secret_ref://r_key": true,
		"secret_ref://":      true, // edge: empty key after prefix; still has prefix
		"secret://x":         false,
		"plain":              false,
		"":                   false,
	}
	for in, want := range cases {
		if got := IsPlaceholder(in); got != want {
			t.Errorf("IsPlaceholder(%q) = %v, want %v", in, got, want)
		}
	}
	// non-string input
	if IsPlaceholder(42) {
		t.Error("IsPlaceholder(int) should be false")
	}
	if IsPlaceholder(nil) {
		t.Error("IsPlaceholder(nil) should be false")
	}
}

func TestPlaceholder(t *testing.T) {
	got := Placeholder("myres", "secret_key")
	want := PlaceholderPrefix + SecretKey("myres", "secret_key")
	if got != want {
		t.Errorf("Placeholder = %q, want %q", got, want)
	}
}

func TestSecretKey(t *testing.T) {
	got := SecretKey("myres", "secret_key")
	providerSafeName := regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	if !providerSafeName.MatchString(got) {
		t.Fatalf("SecretKey = %q, want provider-safe name", got)
	}
	if len(got) > secretKeyMaxLength {
		t.Fatalf("SecretKey len = %d, want <= %d", len(got), secretKeyMaxLength)
	}
	if !strings.HasPrefix(got, "myres__secret_key_") {
		t.Errorf("SecretKey = %q, want sanitized parts plus hash suffix", got)
	}
	for _, pair := range [][2]string{
		{"a", "b_c"},
		{"a_b", "c"},
		{"a-b", "c"},
		{"9resource", "secret-key"},
		{"github", "token"},
	} {
		if key := SecretKey(pair[0], pair[1]); !providerSafeName.MatchString(key) {
			t.Fatalf("SecretKey(%q,%q) = %q, want provider-safe name", pair[0], pair[1], key)
		}
	}
	if SecretKey("a", "b_c") == SecretKey("a_b", "c") {
		t.Fatal("SecretKey must not collide for underscore-ambiguous pairs")
	}
	if SecretKey("a-b", "c") == SecretKey("a_b", "c") {
		t.Fatal("SecretKey must not collide for sanitized-equivalent resource names")
	}
	if key := SecretKey("github", "token"); regexp.MustCompile(`(?i)^github_`).MatchString(key) {
		t.Fatalf("SecretKey = %q, must not use GitHub-reserved prefix", key)
	}
	longA := SecretKey(strings.Repeat("resource", 20), strings.Repeat("output", 20))
	longB := SecretKey(strings.Repeat("resource", 20)+"x", strings.Repeat("output", 20))
	for _, key := range []string{longA, longB} {
		if len(key) > secretKeyMaxLength {
			t.Fatalf("long SecretKey len = %d, want <= %d: %q", len(key), secretKeyMaxLength, key)
		}
		if !providerSafeName.MatchString(key) {
			t.Fatalf("long SecretKey = %q, want provider-safe name", key)
		}
	}
	if longA == longB {
		t.Fatal("truncated SecretKey values must retain hash collision resistance")
	}
}

func TestMaskSensitiveForDiff_MasksPlaceholdersAndDriverKeys(t *testing.T) {
	desired := map[string]any{"region": "nyc3", "secret_key": "should-mask", "bucket": "b"}
	current := map[string]any{"region": "nyc3", "secret_key": "secret_ref://r_secret_key", "bucket": "b"}
	d2, c2 := MaskSensitiveForDiff([]string{"secret_key"}, desired, current)
	if _, ok := d2["secret_key"]; ok {
		t.Errorf("desired should have secret_key elided, got %v", d2["secret_key"])
	}
	if _, ok := c2["secret_key"]; ok {
		t.Errorf("current should have secret_key elided, got %v", c2["secret_key"])
	}
	if d2["region"] != "nyc3" || c2["region"] != "nyc3" {
		t.Errorf("non-sensitive keys must survive: d=%v c=%v", d2["region"], c2["region"])
	}
}

func TestMaskSensitiveForDiff_PlaceholderInDesired(t *testing.T) {
	// Edge: a desired map carrying a placeholder shouldn't leak it into Diff.
	desired := map[string]any{"k": "secret_ref://r_k"}
	current := map[string]any{"k": "secret_ref://r_k"}
	d2, c2 := MaskSensitiveForDiff(nil, desired, current)
	if _, ok := d2["k"]; ok {
		t.Errorf("placeholder in desired should be elided")
	}
	if _, ok := c2["k"]; ok {
		t.Errorf("placeholder in current should be elided")
	}
}

func TestMaskSensitiveForDiff_NilInputs(t *testing.T) {
	d, c := MaskSensitiveForDiff(nil, nil, nil)
	if d != nil || c != nil {
		t.Errorf("nil inputs should yield nil outputs, got d=%v c=%v", d, c)
	}
}
