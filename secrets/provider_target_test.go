package secrets

import (
	"strings"
	"testing"
)

func TestGitHubProviderSecretTargetDescribesScope(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "x")

	repoProvider, err := NewGitHubSecretsProvider("owner/repo", "GITHUB_TOKEN")
	if err != nil {
		t.Fatalf("NewGitHubSecretsProvider: %v", err)
	}
	repoTarget := repoProvider.SecretTarget()
	if repoTarget.Provider != "github" || repoTarget.Scope != "repo" || repoTarget.Subject != "owner/repo" {
		t.Fatalf("repo target = %+v", repoTarget)
	}
	if !strings.Contains(repoTarget.Label, "github repo owner/repo") {
		t.Fatalf("repo label = %q", repoTarget.Label)
	}

	repoProvider.SetEnvironment("staging")
	envTarget := repoProvider.SecretTarget()
	if envTarget.Scope != "env" || envTarget.Subject != "staging on owner/repo" {
		t.Fatalf("env target = %+v", envTarget)
	}
	if !strings.Contains(envTarget.Label, "github env staging on owner/repo") {
		t.Fatalf("env label = %q", envTarget.Label)
	}

	orgProvider, err := NewGitHubOrgSecretsProvider("my-org", "GITHUB_TOKEN", OrgVisibilityPrivate, nil)
	if err != nil {
		t.Fatalf("NewGitHubOrgSecretsProvider: %v", err)
	}
	orgTarget := orgProvider.SecretTarget()
	if orgTarget.Scope != "org" || orgTarget.Subject != "my-org" {
		t.Fatalf("org target = %+v", orgTarget)
	}
	if !strings.Contains(orgTarget.Label, "github org my-org") {
		t.Fatalf("org label = %q", orgTarget.Label)
	}
}

func TestProviderSecretTargetDescribesNonGitHubProviders(t *testing.T) {
	cases := []struct {
		name        string
		provider    Provider
		wantScope   string
		wantSubject string
		wantLabel   string
	}{
		{
			name:        "env",
			provider:    NewEnvProvider("APP_"),
			wantScope:   "process",
			wantSubject: "APP_",
			wantLabel:   "env prefix APP_",
		},
		{
			name:        "file",
			provider:    NewFileProvider("/tmp/workflow-secrets"),
			wantScope:   "directory",
			wantSubject: "/tmp/workflow-secrets",
			wantLabel:   "file /tmp/workflow-secrets",
		},
		{
			name:        "aws",
			provider:    NewAWSSecretsManagerProviderWithClient(AWSConfig{Region: "us-west-2"}, nil),
			wantScope:   "region",
			wantSubject: "us-west-2",
			wantLabel:   "aws secrets-manager us-west-2",
		},
		{
			name:        "vault",
			provider:    NewVaultProviderFromClient(nil, VaultConfig{Address: "https://vault.example", MountPath: "kv"}),
			wantScope:   "mount",
			wantSubject: "https://vault.example/kv",
			wantLabel:   "vault https://vault.example/kv",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			target := DescribeTarget(tc.provider)
			if target.Scope != tc.wantScope || target.Subject != tc.wantSubject {
				t.Fatalf("target = %+v, want scope=%q subject=%q", target, tc.wantScope, tc.wantSubject)
			}
			if target.Label != tc.wantLabel {
				t.Fatalf("label = %q, want %q", target.Label, tc.wantLabel)
			}
		})
	}
}
