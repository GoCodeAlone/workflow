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
