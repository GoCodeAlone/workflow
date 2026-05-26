package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/iac/sensitive"
	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/secrets"
)

// envTestProvider is a tiny in-memory secrets.Provider for test assertions.
type envTestProvider struct {
	values  map[string]string
	failSet bool
}

func newEnvTestProvider() *envTestProvider { return &envTestProvider{values: map[string]string{}} }
func (p *envTestProvider) Name() string    { return "env-test" }
func (p *envTestProvider) Get(_ context.Context, k string) (string, error) {
	v, ok := p.values[k]
	if !ok {
		return "", secrets.ErrNotFound
	}
	return v, nil
}
func (p *envTestProvider) Set(_ context.Context, k, v string) error {
	if p.failSet {
		return fmt.Errorf("set rejected")
	}
	p.values[k] = v
	return nil
}
func (p *envTestProvider) Delete(_ context.Context, k string) error {
	delete(p.values, k)
	return nil
}
func (p *envTestProvider) List(_ context.Context) ([]string, error) {
	out := make([]string, 0, len(p.values))
	for k := range p.values {
		out = append(out, k)
	}
	return out, nil
}

// stubInfraStore captures SaveResource calls; implements infraStateStore.
type stubInfraStore struct {
	saved   []interfaces.ResourceState
	saveErr error
	deleted []string
}

func (s *stubInfraStore) ListResources(_ context.Context) ([]interfaces.ResourceState, error) {
	return s.saved, nil
}
func (s *stubInfraStore) SaveResource(_ context.Context, st interfaces.ResourceState) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	s.saved = append(s.saved, st)
	return nil
}
func (s *stubInfraStore) DeleteResource(_ context.Context, n string) error {
	s.deleted = append(s.deleted, n)
	return nil
}

// stubSensitiveDriver records Delete calls (for compensating-Delete tests).
// Implements interfaces.ResourceDriver.
type stubSensitiveDriver struct {
	deleteCalls []interfaces.ResourceRef
	deleteErr   error
}

func (d *stubSensitiveDriver) Create(_ context.Context, _ interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	return nil, nil
}
func (d *stubSensitiveDriver) Read(_ context.Context, _ interfaces.ResourceRef) (*interfaces.ResourceOutput, error) {
	return nil, nil
}
func (d *stubSensitiveDriver) Update(_ context.Context, _ interfaces.ResourceRef, _ interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	return nil, nil
}
func (d *stubSensitiveDriver) Delete(_ context.Context, ref interfaces.ResourceRef) error {
	d.deleteCalls = append(d.deleteCalls, ref)
	return d.deleteErr
}
func (d *stubSensitiveDriver) Diff(_ context.Context, _ interfaces.ResourceSpec, _ *interfaces.ResourceOutput) (*interfaces.DiffResult, error) {
	return nil, nil
}
func (d *stubSensitiveDriver) HealthCheck(_ context.Context, _ interfaces.ResourceRef) (*interfaces.HealthResult, error) {
	return nil, nil
}
func (d *stubSensitiveDriver) Scale(_ context.Context, _ interfaces.ResourceRef, _ int) (*interfaces.ResourceOutput, error) {
	return nil, nil
}
func (d *stubSensitiveDriver) SensitiveKeys() []string { return nil }

func TestPersistResourceWithSecretRouting_RoutesSensitiveAndSanitizesState(t *testing.T) {
	prov := newEnvTestProvider()
	store := &stubInfraStore{}
	drv := &stubSensitiveDriver{}
	out := interfaces.ResourceOutput{
		Name: "myres", Type: "infra.spaces_key", ProviderID: "AKIA",
		Outputs:   map[string]any{"access_key": "AK", "secret_key": "SK", "bucket": "b"},
		Sensitive: map[string]bool{"access_key": true, "secret_key": true},
	}
	rs := interfaces.ResourceState{
		ID: "myres", Name: "myres", Type: "infra.spaces_key",
		Provider: "digitalocean", ProviderID: "AKIA",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	hydrated, err := persistResourceWithSecretRouting(context.Background(), store, prov, drv, rs, out, persistModeApply)
	if err != nil {
		t.Fatalf("persist: %v", err)
	}
	if len(store.saved) != 1 {
		t.Fatalf("expected 1 saved, got %d", len(store.saved))
	}
	state := store.saved[0]
	secretKey := sensitive.SecretKey("myres", "secret_key")
	accessKey := sensitive.SecretKey("myres", "access_key")
	if state.Outputs["secret_key"] != sensitive.Placeholder("myres", "secret_key") {
		t.Errorf("state secret_key not sanitized: %v", state.Outputs["secret_key"])
	}
	if state.Outputs["access_key"] != sensitive.Placeholder("myres", "access_key") {
		t.Errorf("state access_key not sanitized: %v", state.Outputs["access_key"])
	}
	if state.Outputs["bucket"] != "b" {
		t.Errorf("state bucket lost: %v", state.Outputs["bucket"])
	}
	if prov.values[secretKey] != "SK" {
		t.Errorf("provider missing secret_key value")
	}
	if hydrated[secretKey] != "SK" {
		t.Errorf("hydrated missing secret_key: %v", hydrated)
	}
	if prov.values[accessKey] != "AK" {
		t.Errorf("provider missing access_key value")
	}
}

func TestPersistResourceWithSecretRouting_NoProviderHardFails(t *testing.T) {
	store := &stubInfraStore{}
	drv := &stubSensitiveDriver{}
	out := interfaces.ResourceOutput{
		Outputs:   map[string]any{"secret_key": "SK"},
		Sensitive: map[string]bool{"secret_key": true},
	}
	rs := interfaces.ResourceState{Name: "myres"}
	_, err := persistResourceWithSecretRouting(context.Background(), store, nil, drv, rs, out, persistModeApply)
	if err == nil {
		t.Fatal("expected error when provider nil and sensitive non-empty")
	}
	if !strings.Contains(err.Error(), "myres") {
		t.Errorf("error should name resource, got %q", err.Error())
	}
	if len(store.saved) != 0 {
		t.Error("state should NOT be saved when routing fails")
	}
	if len(drv.deleteCalls) != 1 {
		t.Fatalf("expected compensating Delete after routing failure, got %d", len(drv.deleteCalls))
	}
}

func TestPersistResourceWithSecretRouting_SaveFailureCompensatesWithDelete(t *testing.T) {
	prov := newEnvTestProvider()
	store := &stubInfraStore{saveErr: errors.New("disk full")}
	drv := &stubSensitiveDriver{}
	out := interfaces.ResourceOutput{
		Name: "myres", ProviderID: "AKIA",
		Outputs:   map[string]any{"secret_key": "SK"},
		Sensitive: map[string]bool{"secret_key": true},
	}
	rs := interfaces.ResourceState{Name: "myres", Type: "infra.spaces_key", ProviderID: "AKIA"}
	_, err := persistResourceWithSecretRouting(context.Background(), store, prov, drv, rs, out, persistModeApply)
	if err == nil {
		t.Fatal("expected error from SaveResource")
	}
	if !strings.Contains(err.Error(), "disk full") {
		t.Errorf("error should wrap original SaveResource err, got %q", err.Error())
	}
	if len(drv.deleteCalls) != 1 {
		t.Errorf("expected 1 compensating Delete call, got %d", len(drv.deleteCalls))
	}
	if drv.deleteCalls[0].ProviderID != "AKIA" {
		t.Errorf("compensating Delete used wrong ProviderID: %v", drv.deleteCalls[0])
	}
	if _, ok := prov.values[sensitive.SecretKey("myres", "secret_key")]; ok {
		t.Errorf("compensating Delete should have removed routed secret; got %v", prov.values)
	}
}

func TestPersistResourceWithSecretRouting_SaveFailureWithoutSecretsCompensatesWithDelete(t *testing.T) {
	store := &stubInfraStore{saveErr: errors.New("disk full")}
	drv := &stubSensitiveDriver{}
	out := interfaces.ResourceOutput{
		Name:       "myres",
		ProviderID: "plain-id",
		Outputs:    map[string]any{"bucket": "b"},
	}
	rs := interfaces.ResourceState{Name: "myres", Type: "infra.bucket", ProviderID: "plain-id"}
	_, err := persistResourceWithSecretRouting(context.Background(), store, nil, drv, rs, out, persistModeApply)
	if err == nil {
		t.Fatal("expected error from SaveResource")
	}
	if !strings.Contains(err.Error(), "compensating delete succeeded") {
		t.Fatalf("error = %v, want compensating delete success", err)
	}
	if len(drv.deleteCalls) != 1 {
		t.Fatalf("expected 1 compensating Delete call, got %d", len(drv.deleteCalls))
	}
	if drv.deleteCalls[0].ProviderID != "plain-id" {
		t.Errorf("compensating Delete used wrong ProviderID: %v", drv.deleteCalls[0])
	}
}

func TestPersistResourceWithSecretRouting_NoSensitivePassesThrough(t *testing.T) {
	store := &stubInfraStore{}
	drv := &stubSensitiveDriver{}
	out := interfaces.ResourceOutput{
		Outputs: map[string]any{"bucket": "b"},
	}
	rs := interfaces.ResourceState{Name: "myres"}
	hydrated, err := persistResourceWithSecretRouting(context.Background(), store, nil, drv, rs, out, persistModeApply)
	if err != nil {
		t.Fatalf("persist: %v", err)
	}
	if len(hydrated) != 0 {
		t.Errorf("hydrated should be empty: %v", hydrated)
	}
	if len(store.saved) != 1 {
		t.Fatalf("expected 1 saved, got %d", len(store.saved))
	}
	if store.saved[0].Outputs["bucket"] != "b" {
		t.Errorf("non-sensitive output corrupted: %v", store.saved[0].Outputs)
	}
}

func TestPersistResourceWithSecretRouting_Idempotent(t *testing.T) {
	prov := newEnvTestProvider()
	store := &stubInfraStore{}
	drv := &stubSensitiveDriver{}
	out := interfaces.ResourceOutput{
		Outputs:   map[string]any{"secret_key": "SK"},
		Sensitive: map[string]bool{"secret_key": true},
	}
	rs := interfaces.ResourceState{Name: "myres"}
	for i := 0; i < 2; i++ {
		_, err := persistResourceWithSecretRouting(context.Background(), store, prov, drv, rs, out, persistModeApply)
		if err != nil {
			t.Fatalf("persist iter %d: %v", i, err)
		}
	}
	if prov.values[sensitive.SecretKey("myres", "secret_key")] != "SK" {
		t.Errorf("provider value lost on re-Apply: %v", prov.values)
	}
	if len(store.saved) != 2 {
		t.Errorf("expected 2 saved, got %d", len(store.saved))
	}
}

func TestRequireSecretsProviderForSensitiveOutputs_PreflightDetect(t *testing.T) {
	result := &interfaces.ApplyResult{
		Resources: []interfaces.ResourceOutput{
			{Name: "ok", Outputs: map[string]any{"bucket": "b"}},
			{Name: "needs_routing", Outputs: map[string]any{"secret_key": "SK"}, Sensitive: map[string]bool{"secret_key": true}},
		},
	}
	err := requireSecretsProviderForSensitiveOutputs(nil, result)
	if err == nil {
		t.Fatal("expected pre-flight to reject")
	}
	if !strings.Contains(err.Error(), "needs_routing") {
		t.Errorf("error should name the offending resource, got %q", err.Error())
	}
}

func TestRequireSecretsProviderForSensitiveOutputs_ProviderConfigured_PassesThrough(t *testing.T) {
	prov := newEnvTestProvider()
	result := &interfaces.ApplyResult{
		Resources: []interfaces.ResourceOutput{
			{Name: "ok", Outputs: map[string]any{"secret_key": "SK"}, Sensitive: map[string]bool{"secret_key": true}},
		},
	}
	if err := requireSecretsProviderForSensitiveOutputs(prov, result); err != nil {
		t.Fatalf("provider configured should pass; got %v", err)
	}
}

func TestPersistResourceWithSecretRouting_ReadModeSanitizeOnly_PreservesPriorPlaceholder(t *testing.T) {
	prov := newEnvTestProvider() // should not be touched
	// Pre-existing state has a placeholder
	store := &stubInfraStore{
		saved: []interfaces.ResourceState{
			{Name: "myres", Outputs: map[string]any{"secret_key": sensitive.Placeholder("myres", "secret_key"), "bucket": "b"}},
		},
	}
	out := interfaces.ResourceOutput{
		Outputs:   map[string]any{"bucket": "b"}, // Read can't re-emit secret_key
		Sensitive: map[string]bool{"secret_key": true},
	}
	rs := interfaces.ResourceState{Name: "myres"}
	_, err := persistResourceWithSecretRouting(context.Background(), store, prov, &stubSensitiveDriver{}, rs, out, persistModeRead)
	if err != nil {
		t.Fatalf("persist read-mode: %v", err)
	}
	if len(prov.values) != 0 {
		t.Errorf("Read mode must NOT call provider.Set; got %v", prov.values)
	}
	if len(store.saved) != 2 {
		t.Fatalf("expected 2 saves (initial + this), got %d", len(store.saved))
	}
	latest := store.saved[1]
	if latest.Outputs["secret_key"] != sensitive.Placeholder("myres", "secret_key") {
		t.Errorf("Read mode lost prior placeholder: %v", latest.Outputs["secret_key"])
	}
	if latest.Outputs["bucket"] != "b" {
		t.Errorf("Read mode lost bucket: %v", latest.Outputs["bucket"])
	}
}

func TestPersistResourceWithSecretRouting_ReadModeNewSensitiveKey_Dropped(t *testing.T) {
	prov := newEnvTestProvider()
	store := &stubInfraStore{}
	out := interfaces.ResourceOutput{
		Outputs:   map[string]any{"secret_key": "FRESH-FROM-CLOUD-CACHE", "bucket": "b"},
		Sensitive: map[string]bool{"secret_key": true},
	}
	rs := interfaces.ResourceState{Name: "myres"}
	_, err := persistResourceWithSecretRouting(context.Background(), store, prov, &stubSensitiveDriver{}, rs, out, persistModeRead)
	if err != nil {
		t.Fatalf("persist read-mode: %v", err)
	}
	if len(prov.values) != 0 {
		t.Errorf("Read mode must NOT call provider.Set even for newly-declared sensitive; got %v", prov.values)
	}
	if len(store.saved) != 1 {
		t.Fatalf("expected 1 save, got %d", len(store.saved))
	}
	if _, ok := store.saved[0].Outputs["secret_key"]; ok {
		t.Errorf("newly-declared sensitive (no prior placeholder) should be dropped; got %v", store.saved[0].Outputs["secret_key"])
	}
	if store.saved[0].Outputs["bucket"] != "b" {
		t.Errorf("non-sensitive bucket lost: %v", store.saved[0].Outputs["bucket"])
	}
}
