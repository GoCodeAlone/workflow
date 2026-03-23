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
