package main

import "testing"

// TestResolveIaCProviderRef covers the disambiguation between an
// implementation-level "provider" field (e.g. infra.eventbus's provider="nats")
// and the iac.provider module reference. The canonical key is iac_provider;
// "provider" is the backward-compat fallback.
func TestResolveIaCProviderRef(t *testing.T) {
	cases := []struct {
		name string
		cfg  map[string]any
		want string
	}{
		{
			name: "iac_provider_only",
			cfg:  map[string]any{"iac_provider": "do-provider"},
			want: "do-provider",
		},
		{
			name: "provider_only_back_compat",
			cfg:  map[string]any{"provider": "do-provider"},
			want: "do-provider",
		},
		{
			name: "iac_provider_wins_over_provider",
			cfg: map[string]any{
				"iac_provider": "do-provider",
				"provider":     "nats", // implementation, ignored for IaC routing
			},
			want: "do-provider",
		},
		{
			name: "empty_iac_provider_falls_back",
			cfg: map[string]any{
				"iac_provider": "",
				"provider":     "do-provider",
			},
			want: "do-provider",
		},
		{
			name: "neither_set",
			cfg:  map[string]any{},
			want: "",
		},
		{
			name: "nil_config",
			cfg:  nil,
			want: "",
		},
		{
			name: "non_string_iac_provider_falls_back",
			cfg: map[string]any{
				"iac_provider": 42, // type mismatch, treated as missing
				"provider":     "do-provider",
			},
			want: "do-provider",
		},
		{
			name: "non_string_provider",
			cfg:  map[string]any{"provider": 42},
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveIaCProviderRef(tc.cfg)
			if got != tc.want {
				t.Errorf("resolveIaCProviderRef(%v) = %q; want %q", tc.cfg, got, tc.want)
			}
		})
	}
}
