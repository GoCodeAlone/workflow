package bdd_test

import (
	"testing"

	"github.com/GoCodeAlone/workflow/wftest/bdd"
)

func TestRunFeatures_Minimal(t *testing.T) {
	bdd.RunFeatures(t, "testdata/minimal.feature")
}

func TestRunFeatures_Mock(t *testing.T) {
	bdd.RunFeatures(t, "testdata/mock.feature")
}

func TestRunFeatures_HTTP(t *testing.T) {
	bdd.RunFeatures(t, "testdata/http.feature")
}

func TestRunFeatures_Triggers(t *testing.T) {
	bdd.RunFeatures(t, "testdata/triggers.feature")
}

func TestRunFeatures_Assertions(t *testing.T) {
	bdd.RunFeatures(t, "testdata/assertions.feature")
}

func TestRunFeatures_State(t *testing.T) {
	bdd.RunFeatures(t, "testdata/state.feature")
}

// TestRunFeatures_UndefinedLenient verifies that undefined steps do NOT fail the
// suite in the default (lenient) mode — they are warned via t.Log and skipped.
func TestRunFeatures_UndefinedLenient(t *testing.T) {
	bdd.RunFeatures(t, "testdata/undefined.feature")
}

// Strict-mode variants: verify that no undefined/pending steps exist in the feature files.
func TestRunFeatures_MinimalStrict(t *testing.T) {
	bdd.RunFeatures(t, "testdata/minimal.feature", bdd.Strict())
}

func TestRunFeatures_MockStrict(t *testing.T) {
	bdd.RunFeatures(t, "testdata/mock.feature", bdd.Strict())
}

func TestRunFeatures_HTTPStrict(t *testing.T) {
	bdd.RunFeatures(t, "testdata/http.feature", bdd.Strict())
}

func TestRunFeatures_TriggersStrict(t *testing.T) {
	bdd.RunFeatures(t, "testdata/triggers.feature", bdd.Strict())
}

func TestRunFeatures_AssertionsStrict(t *testing.T) {
	bdd.RunFeatures(t, "testdata/assertions.feature", bdd.Strict())
}

func TestRunFeatures_StateStrict(t *testing.T) {
	bdd.RunFeatures(t, "testdata/state.feature", bdd.Strict())
}
