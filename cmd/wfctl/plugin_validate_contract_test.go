package main

import (
	"strings"
	"testing"
)

func TestRunPluginValidateContract_GoodPasses(t *testing.T) {
	err := runPluginValidateContract([]string{"testdata/plugin_validate_contract/good"})
	if err != nil {
		t.Fatalf("expected PASS for good fixture, got %v", err)
	}
}

func TestRunPluginValidateContract_BadMissingCapsFails(t *testing.T) {
	err := runPluginValidateContract([]string{"testdata/plugin_validate_contract/bad-missing-caps"})
	if err == nil {
		t.Fatal("expected FAIL for bad-missing-caps fixture, got nil")
	}
	if !strings.Contains(err.Error(), "contract check") {
		t.Errorf("error should mention contract check, got %v", err)
	}
}

func TestRunPluginValidateContract_BadMissingLdflagFails(t *testing.T) {
	err := runPluginValidateContract([]string{"testdata/plugin_validate_contract/bad-missing-ldflag"})
	if err == nil {
		t.Fatal("expected FAIL for bad-missing-ldflag fixture, got nil")
	}
}

func TestRunPluginValidateContract_ForPublishGoodTag(t *testing.T) {
	err := runPluginValidateContract([]string{
		"--for-publish", "--tag", "v1.2.3",
		"testdata/plugin_validate_contract/good",
	})
	if err != nil {
		t.Fatalf("expected PASS for good fixture + good tag, got %v", err)
	}
}

func TestRunPluginValidateContract_ForPublishBadTag(t *testing.T) {
	err := runPluginValidateContract([]string{
		"--for-publish", "--tag", "v1.2.3-rc.1",
		"testdata/plugin_validate_contract/good",
	})
	if err == nil {
		t.Fatal("expected FAIL for prerelease tag, got nil")
	}
	if !strings.Contains(err.Error(), "contract check") {
		t.Errorf("error should mention contract check, got %v", err)
	}
}

func TestRunPluginValidateContract_ForPublishBadTagShape(t *testing.T) {
	err := runPluginValidateContract([]string{
		"--for-publish", "--tag", "release-2026",
		"testdata/plugin_validate_contract/good",
	})
	if err == nil {
		t.Fatal("expected FAIL for non-semver tag, got nil")
	}
}

func TestRunPluginValidateContract_ReleaseDirGoodMatches(t *testing.T) {
	err := runPluginValidateContract([]string{
		"--for-publish", "--tag", "v1.2.3",
		"--release-dir", "testdata/plugin_validate_contract/release-dir-good/.release",
		"testdata/plugin_validate_contract/release-dir-good",
	})
	if err != nil {
		t.Fatalf("expected PASS for release-dir-good, got %v", err)
	}
}

func TestRunPluginValidateContract_ReleaseDirStaleFails(t *testing.T) {
	err := runPluginValidateContract([]string{
		"--for-publish", "--tag", "v1.2.3",
		"--release-dir", "testdata/plugin_validate_contract/release-dir-stale/.release",
		"testdata/plugin_validate_contract/release-dir-stale",
	})
	if err == nil {
		t.Fatal("expected FAIL for release-dir-stale (.release plugin.json has 1.0.0 not 1.2.3)")
	}
}

func TestRunPluginValidateContract_GithubRefNameFallback(t *testing.T) {
	t.Setenv("GITHUB_REF_NAME", "v1.2.3")
	err := runPluginValidateContract([]string{
		"--for-publish",
		"testdata/plugin_validate_contract/good",
	})
	if err != nil {
		t.Fatalf("expected PASS via GITHUB_REF_NAME fallback, got %v", err)
	}
}

func TestRunPluginValidateContract_MissingArg(t *testing.T) {
	err := runPluginValidateContract([]string{})
	if err == nil {
		t.Fatal("expected error for missing plugin-dir arg")
	}
}
