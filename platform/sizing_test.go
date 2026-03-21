package platform_test

import (
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/platform"
)

func TestSizing_DefaultM(t *testing.T) {
	result, err := platform.ResolveSizing("infra.database", interfaces.SizeM, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.CPU != "2" {
		t.Errorf("expected cpu=2, got %q", result.CPU)
	}
	if result.Memory != "4Gi" {
		t.Errorf("expected memory=4Gi, got %q", result.Memory)
	}
	if result.DBStorage != "100Gi" {
		t.Errorf("expected db_storage=100Gi, got %q", result.DBStorage)
	}
}

func TestSizing_WithHints(t *testing.T) {
	hints := &interfaces.ResourceHints{CPU: "3"}
	result, err := platform.ResolveSizing("infra.database", interfaces.SizeM, hints)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.CPU != "3" {
		t.Errorf("expected cpu hint to override to 3, got %q", result.CPU)
	}
	if result.Memory != "4Gi" {
		t.Errorf("expected memory unchanged at 4Gi, got %q", result.Memory)
	}
}

func TestSizing_AllSizes(t *testing.T) {
	cases := []struct {
		size    interfaces.Size
		cpu     string
		memory  string
		storage string
	}{
		{interfaces.SizeXS, "0.25", "512Mi", "10Gi"},
		{interfaces.SizeS, "1", "2Gi", "50Gi"},
		{interfaces.SizeM, "2", "4Gi", "100Gi"},
		{interfaces.SizeL, "4", "16Gi", "500Gi"},
		{interfaces.SizeXL, "8", "32Gi", "1Ti"},
	}
	for _, tc := range cases {
		result, err := platform.ResolveSizing("infra.database", tc.size, nil)
		if err != nil {
			t.Errorf("size %q: unexpected error: %v", tc.size, err)
			continue
		}
		if result.CPU != tc.cpu {
			t.Errorf("size %q: expected cpu=%q, got %q", tc.size, tc.cpu, result.CPU)
		}
		if result.Memory != tc.memory {
			t.Errorf("size %q: expected memory=%q, got %q", tc.size, tc.memory, result.Memory)
		}
		if result.DBStorage != tc.storage {
			t.Errorf("size %q: expected db_storage=%q, got %q", tc.size, tc.storage, result.DBStorage)
		}
	}
}

func TestSizing_UnknownSize(t *testing.T) {
	_, err := platform.ResolveSizing("infra.database", interfaces.Size("xxxl"), nil)
	if err == nil {
		t.Fatal("expected error for unknown size, got nil")
	}
}

func TestSizing_EmptyHints(t *testing.T) {
	result, err := platform.ResolveSizing("infra.container_service", interfaces.SizeS, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.CPU != "1" {
		t.Errorf("expected cpu=1, got %q", result.CPU)
	}
	if result.Memory != "2Gi" {
		t.Errorf("expected memory=2Gi, got %q", result.Memory)
	}
}

func TestSizing_HintsMemoryOverride(t *testing.T) {
	hints := &interfaces.ResourceHints{Memory: "8Gi"}
	result, err := platform.ResolveSizing("infra.database", interfaces.SizeS, hints)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.CPU != "1" {
		t.Errorf("expected cpu unchanged at 1, got %q", result.CPU)
	}
	if result.Memory != "8Gi" {
		t.Errorf("expected memory hint to override to 8Gi, got %q", result.Memory)
	}
}

func TestSizing_HintsStorageOverride(t *testing.T) {
	hints := &interfaces.ResourceHints{Storage: "200Gi"}
	result, err := platform.ResolveSizing("infra.database", interfaces.SizeM, hints)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.DBStorage != "200Gi" {
		t.Errorf("expected db_storage hint to override to 200Gi, got %q", result.DBStorage)
	}
}
