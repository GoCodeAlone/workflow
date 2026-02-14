package tenant

import (
	"testing"
)

func TestDefaultQuota(t *testing.T) {
	q := DefaultQuota("t1")
	if q.TenantID != "t1" {
		t.Errorf("expected tenant ID t1, got %s", q.TenantID)
	}
	if q.MaxWorkflowsPerMinute <= 0 {
		t.Error("MaxWorkflowsPerMinute should be positive")
	}
	if q.MaxConcurrentWorkflows <= 0 {
		t.Error("MaxConcurrentWorkflows should be positive")
	}
	if q.MaxStorageBytes <= 0 {
		t.Error("MaxStorageBytes should be positive")
	}
	if q.MaxAPIRequestsPerMinute <= 0 {
		t.Error("MaxAPIRequestsPerMinute should be positive")
	}
}

func TestQuotaRegistrySetGet(t *testing.T) {
	reg := NewQuotaRegistry()

	q := TenantQuota{
		TenantID:                "t1",
		MaxWorkflowsPerMinute:   50,
		MaxConcurrentWorkflows:  5,
		MaxStorageBytes:         1024,
		MaxAPIRequestsPerMinute: 500,
	}

	reg.SetQuota(q)

	got, ok := reg.GetQuota("t1")
	if !ok {
		t.Fatal("expected to find quota")
	}
	if got.MaxWorkflowsPerMinute != 50 {
		t.Errorf("expected 50, got %d", got.MaxWorkflowsPerMinute)
	}

	_, ok = reg.GetQuota("nonexistent")
	if ok {
		t.Error("should not find nonexistent tenant")
	}
}

func TestQuotaRegistryRemove(t *testing.T) {
	reg := NewQuotaRegistry()
	reg.SetQuota(DefaultQuota("t1"))

	reg.RemoveQuota("t1")

	_, ok := reg.GetQuota("t1")
	if ok {
		t.Error("should not find removed tenant")
	}
}

func TestQuotaRegistryWorkflowRate(t *testing.T) {
	reg := NewQuotaRegistry()
	reg.SetQuota(TenantQuota{
		TenantID:                "t1",
		MaxWorkflowsPerMinute:   5,
		MaxConcurrentWorkflows:  10,
		MaxStorageBytes:         1 << 30,
		MaxAPIRequestsPerMinute: 100,
	})

	// Should succeed 5 times
	for i := 0; i < 5; i++ {
		if err := reg.CheckWorkflowRate("t1"); err != nil {
			t.Fatalf("CheckWorkflowRate failed on call %d: %v", i, err)
		}
	}

	// Should fail on 6th (tokens exhausted)
	if err := reg.CheckWorkflowRate("t1"); err == nil {
		t.Error("expected rate limit error")
	}
}

func TestQuotaRegistryAPIRate(t *testing.T) {
	reg := NewQuotaRegistry()
	reg.SetQuota(TenantQuota{
		TenantID:                "t1",
		MaxWorkflowsPerMinute:   100,
		MaxConcurrentWorkflows:  10,
		MaxStorageBytes:         1 << 30,
		MaxAPIRequestsPerMinute: 3,
	})

	for i := 0; i < 3; i++ {
		if err := reg.CheckAPIRate("t1"); err != nil {
			t.Fatalf("CheckAPIRate failed on call %d: %v", i, err)
		}
	}

	if err := reg.CheckAPIRate("t1"); err == nil {
		t.Error("expected API rate limit error")
	}
}

func TestQuotaRegistryConcurrency(t *testing.T) {
	reg := NewQuotaRegistry()
	reg.SetQuota(TenantQuota{
		TenantID:                "t1",
		MaxWorkflowsPerMinute:   100,
		MaxConcurrentWorkflows:  2,
		MaxStorageBytes:         1 << 30,
		MaxAPIRequestsPerMinute: 100,
	})

	// Acquire two slots
	if err := reg.AcquireWorkflowSlot("t1"); err != nil {
		t.Fatalf("AcquireWorkflowSlot 1 failed: %v", err)
	}
	if err := reg.AcquireWorkflowSlot("t1"); err != nil {
		t.Fatalf("AcquireWorkflowSlot 2 failed: %v", err)
	}

	// Third should fail
	if err := reg.AcquireWorkflowSlot("t1"); err == nil {
		t.Error("expected concurrency limit error")
	}

	// Release one and try again
	reg.ReleaseWorkflowSlot("t1")

	if err := reg.CheckConcurrency("t1"); err != nil {
		t.Errorf("CheckConcurrency should pass after release: %v", err)
	}
}

func TestQuotaRegistryStorage(t *testing.T) {
	reg := NewQuotaRegistry()
	reg.SetQuota(TenantQuota{
		TenantID:                "t1",
		MaxWorkflowsPerMinute:   100,
		MaxConcurrentWorkflows:  10,
		MaxStorageBytes:         1000,
		MaxAPIRequestsPerMinute: 100,
	})

	if err := reg.CheckStorage("t1", 500); err != nil {
		t.Fatalf("CheckStorage should pass: %v", err)
	}

	reg.UpdateStorage("t1", 800)

	if err := reg.CheckStorage("t1", 300); err == nil {
		t.Error("expected storage limit error")
	}

	if err := reg.CheckStorage("t1", 100); err != nil {
		t.Errorf("CheckStorage should pass for smaller amount: %v", err)
	}
}

func TestQuotaRegistryUsageSnapshot(t *testing.T) {
	reg := NewQuotaRegistry()
	reg.SetQuota(TenantQuota{
		TenantID:                "t1",
		MaxWorkflowsPerMinute:   100,
		MaxConcurrentWorkflows:  10,
		MaxStorageBytes:         1 << 30,
		MaxAPIRequestsPerMinute: 50,
	})

	_ = reg.AcquireWorkflowSlot("t1")

	snap, ok := reg.GetUsageSnapshot("t1")
	if !ok {
		t.Fatal("expected usage snapshot")
	}
	if snap.ConcurrentWorkflows != 1 {
		t.Errorf("expected 1 concurrent workflow, got %d", snap.ConcurrentWorkflows)
	}

	_, ok = reg.GetUsageSnapshot("nonexistent")
	if ok {
		t.Error("should not find nonexistent tenant usage")
	}
}

func TestQuotaRegistryNoQuota(t *testing.T) {
	reg := NewQuotaRegistry()

	if err := reg.CheckWorkflowRate("unknown"); err == nil {
		t.Error("expected error for unconfigured tenant")
	}
	if err := reg.CheckAPIRate("unknown"); err == nil {
		t.Error("expected error for unconfigured tenant")
	}
	if err := reg.CheckConcurrency("unknown"); err == nil {
		t.Error("expected error for unconfigured tenant")
	}
	if err := reg.AcquireWorkflowSlot("unknown"); err == nil {
		t.Error("expected error for unconfigured tenant")
	}
	if err := reg.CheckStorage("unknown", 100); err == nil {
		t.Error("expected error for unconfigured tenant")
	}
}

func TestQuotaRegistryReleaseNonexistent(t *testing.T) {
	reg := NewQuotaRegistry()
	// Should not panic
	reg.ReleaseWorkflowSlot("nonexistent")
}

func TestQuotaRegistryUpdateStorageNonexistent(t *testing.T) {
	reg := NewQuotaRegistry()
	// Should not panic
	reg.UpdateStorage("nonexistent", 100)
}
