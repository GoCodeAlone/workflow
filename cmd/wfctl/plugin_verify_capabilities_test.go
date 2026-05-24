package main

import (
	"strings"
	"testing"
)

func TestVerifyCapabilitiesUsage(t *testing.T) {
	err := runPluginVerifyCapabilities([]string{})
	if err == nil {
		t.Fatal("want error for missing args")
	}
	if !strings.Contains(err.Error(), "--binary") {
		t.Errorf("error %q should mention --binary", err.Error())
	}
}

func TestVerifyCapabilitiesRequiresBinary(t *testing.T) {
	err := runPluginVerifyCapabilities([]string{"."})
	if err == nil {
		t.Fatal("want error when --binary missing")
	}
	if !strings.Contains(err.Error(), "--binary") {
		t.Errorf("error %q should mention --binary", err.Error())
	}
}
