package schema

import (
	"testing"
)

func TestModuleSchemaRegistry_RegisterAndGet(t *testing.T) {
	r := NewModuleSchemaRegistry()

	// All known module types should be registered
	for _, mt := range KnownModuleTypes() {
		s := r.Get(mt)
		if s == nil {
			t.Errorf("expected schema for %q but got nil", mt)
			continue
		}
		if s.Type != mt {
			t.Errorf("schema type = %q, want %q", s.Type, mt)
		}
		if s.Label == "" {
			t.Errorf("schema %q has empty label", mt)
		}
		if s.Category == "" {
			t.Errorf("schema %q has empty category", mt)
		}
	}
}

func TestModuleSchemaRegistry_All(t *testing.T) {
	r := NewModuleSchemaRegistry()
	all := r.All()
	known := KnownModuleTypes()
	if len(all) != len(known) {
		t.Errorf("All() returned %d schemas, want %d (known module types)", len(all), len(known))
	}
}

func TestModuleSchemaRegistry_AllMap(t *testing.T) {
	r := NewModuleSchemaRegistry()
	m := r.AllMap()
	if len(m) != len(KnownModuleTypes()) {
		t.Errorf("AllMap() returned %d entries, want %d", len(m), len(KnownModuleTypes()))
	}
	for _, mt := range KnownModuleTypes() {
		if _, ok := m[mt]; !ok {
			t.Errorf("AllMap() missing key %q", mt)
		}
	}
}

func TestModuleSchemaRegistry_ConfigFieldsMatchEngine(t *testing.T) {
	r := NewModuleSchemaRegistry()

	// Verify key modules have the correct config fields matching engine.go extraction
	tests := []struct {
		moduleType string
		wantFields []string
	}{
		{"http.server", []string{"address"}},
		{"http.handler", []string{"contentType"}},
		{"http.middleware.ratelimit", []string{"requestsPerMinute", "burstSize"}},
		{"http.middleware.cors", []string{"allowedOrigins", "allowedMethods"}},
		{"http.middleware.auth", []string{"authType"}},
		{"http.middleware.logging", []string{"logLevel"}},
		{"api.handler", []string{"resourceName", "workflowType", "workflowEngine", "initialTransition", "seedFile", "sourceResourceName", "stateFilter", "fieldMapping", "transitionMap", "summaryFields"}},
		{"database.workflow", []string{"driver", "dsn", "maxOpenConns", "maxIdleConns"}},
		{"messaging.kafka", []string{"brokers", "groupId"}},
		{"auth.jwt", []string{"secret", "tokenExpiry", "issuer", "seedFile", "responseFormat", "allowRegistration"}},
		{"static.fileserver", []string{"root", "prefix", "spaFallback", "cacheMaxAge", "router"}},
		{"processing.step", []string{"componentId", "successTransition", "compensateTransition", "maxRetries", "retryBackoffMs", "timeoutSeconds"}},
		{"http.middleware.securityheaders", []string{"contentSecurityPolicy", "frameOptions", "contentTypeOptions", "hstsMaxAge", "referrerPolicy", "permissionsPolicy"}},
		{"webhook.sender", []string{"maxRetries"}},
		{"persistence.store", []string{"database"}},
		{"dynamic.component", []string{"componentId", "source", "provides", "requires"}},
		{"http.simple_proxy", []string{"targets"}},
	}

	for _, tt := range tests {
		s := r.Get(tt.moduleType)
		if s == nil {
			t.Errorf("%s: schema not found", tt.moduleType)
			continue
		}
		fieldKeys := make(map[string]bool, len(s.ConfigFields))
		for _, f := range s.ConfigFields {
			fieldKeys[f.Key] = true
		}
		for _, wantKey := range tt.wantFields {
			if !fieldKeys[wantKey] {
				t.Errorf("%s: missing config field %q", tt.moduleType, wantKey)
			}
		}
		// Also check no extra fields beyond what we expect
		if len(s.ConfigFields) != len(tt.wantFields) {
			t.Errorf("%s: has %d config fields, want %d", tt.moduleType, len(s.ConfigFields), len(tt.wantFields))
		}
	}
}

func TestModuleSchemaRegistry_CustomRegister(t *testing.T) {
	r := NewModuleSchemaRegistry()
	r.Register(&ModuleSchema{
		Type:     "custom.module",
		Label:    "Custom Module",
		Category: "custom",
		ConfigFields: []ConfigFieldDef{
			{Key: "foo", Label: "Foo", Type: FieldTypeString},
		},
	})
	s := r.Get("custom.module")
	if s == nil {
		t.Fatal("expected custom schema to be registered")
	}
	if s.Label != "Custom Module" {
		t.Errorf("label = %q, want %q", s.Label, "Custom Module")
	}
	if len(s.ConfigFields) != 1 {
		t.Errorf("configFields = %d, want 1", len(s.ConfigFields))
	}
}
