package interfaces_test

import (
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

func TestJobSpec_ZeroValue(t *testing.T) {
	var j interfaces.JobSpec
	if j.Name != "" || j.Kind != "" || j.Image != "" {
		t.Error("zero JobSpec should have empty fields")
	}
}

func TestJobSpec_Fields(t *testing.T) {
	j := interfaces.JobSpec{
		Name:       "migrate",
		Kind:       "PRE_DEPLOY",
		Image:      "gcr.io/distroless/base-debian12:nonroot",
		RunCommand: "workflow-migrate up",
		EnvVars:    map[string]string{"ENV": "prod"},
		EnvVarsSecret: map[string]string{"DB_URL": "DB_URL"},
	}
	if j.Name != "migrate" {
		t.Errorf("Name: got %q, want migrate", j.Name)
	}
	if j.Kind != "PRE_DEPLOY" {
		t.Errorf("Kind: got %q, want PRE_DEPLOY", j.Kind)
	}
}

func TestWorkerSpec_Fields(t *testing.T) {
	w := interfaces.WorkerSpec{
		Name:       "queue-consumer",
		Image:      "gcr.io/distroless/base-debian12:nonroot",
		RunCommand: "./worker",
	}
	if w.Name != "queue-consumer" {
		t.Errorf("Name: got %q", w.Name)
	}
}

func TestStaticSiteSpec_Fields(t *testing.T) {
	s := interfaces.StaticSiteSpec{
		Name:       "frontend",
		OutputDir:  "dist",
		BuildCommand: "npm run build",
	}
	if s.Name != "frontend" {
		t.Errorf("Name: got %q", s.Name)
	}
}

func TestSidecarSpec_Fields(t *testing.T) {
	sc := interfaces.SidecarSpec{
		Name:             "tailscale",
		Image:            "tailscale/tailscale:latest",
		RunCommand:       "tailscaled",
		SharesNetworkWith: "main-service",
	}
	if sc.Name != "tailscale" {
		t.Errorf("Name: got %q", sc.Name)
	}
	if sc.SharesNetworkWith != "main-service" {
		t.Errorf("SharesNetworkWith: got %q", sc.SharesNetworkWith)
	}
}

func TestPortSpec_Fields(t *testing.T) {
	p := interfaces.PortSpec{
		Name:     "http",
		Port:     8080,
		Protocol: "http",
		Public:   true,
	}
	if p.Port != 8080 {
		t.Errorf("Port: got %d, want 8080", p.Port)
	}
}

func TestAutoscalingSpec_Fields(t *testing.T) {
	a := interfaces.AutoscalingSpec{
		Min:           1,
		Max:           5,
		CPUPercent:    70,
		MemoryPercent: 80,
	}
	if a.Min != 1 || a.Max != 5 {
		t.Errorf("Min/Max: got %d/%d", a.Min, a.Max)
	}
}

func TestHealthCheckSpec_Fields(t *testing.T) {
	h := interfaces.HealthCheckSpec{
		HTTPPath:            "/healthz",
		Port:                8080,
		InitialDelaySeconds: 5,
		PeriodSeconds:       10,
		TimeoutSeconds:      3,
		SuccessThreshold:    1,
		FailureThreshold:    3,
	}
	if h.HTTPPath != "/healthz" {
		t.Errorf("HTTPPath: got %q", h.HTTPPath)
	}
}

func TestRouteSpec_Fields(t *testing.T) {
	r := interfaces.RouteSpec{
		Path:               "/api",
		PreservePathPrefix: true,
	}
	if r.Path != "/api" {
		t.Errorf("Path: got %q", r.Path)
	}
}

func TestCORSSpec_Fields(t *testing.T) {
	c := interfaces.CORSSpec{
		AllowOrigins:     []string{"https://example.com"},
		AllowMethods:     []string{"GET", "POST"},
		AllowHeaders:     []string{"Authorization"},
		ExposeHeaders:    []string{"X-Request-ID"},
		AllowCredentials: true,
		MaxAge:           "3600",
	}
	if len(c.AllowOrigins) != 1 {
		t.Errorf("AllowOrigins: got %d, want 1", len(c.AllowOrigins))
	}
}

func TestDomainSpec_Fields(t *testing.T) {
	d := interfaces.DomainSpec{
		Name:     "example.com",
		Zone:     "example.com",
		Type:     "PRIMARY",
		Wildcard: false,
	}
	if d.Name != "example.com" {
		t.Errorf("Name: got %q", d.Name)
	}
}

func TestAlertSpec_Fields(t *testing.T) {
	a := interfaces.AlertSpec{
		Rule:     "CPU_UTILIZATION",
		Operator: "GREATER_THAN",
		Value:    90,
		Window:   "10m",
	}
	if a.Rule != "CPU_UTILIZATION" {
		t.Errorf("Rule: got %q", a.Rule)
	}
}

func TestLogDestinationSpec_Fields(t *testing.T) {
	l := interfaces.LogDestinationSpec{
		Name:     "datadog",
		Endpoint: "https://http-intake.logs.datadoghq.com",
		Headers:  map[string]string{"DD-API-KEY": "secret"},
	}
	if l.Name != "datadog" {
		t.Errorf("Name: got %q", l.Name)
	}
}

func TestTerminationSpec_Fields(t *testing.T) {
	tr := interfaces.TerminationSpec{
		DrainSeconds:       30,
		GracePeriodSeconds: 5,
	}
	if tr.DrainSeconds != 30 {
		t.Errorf("DrainSeconds: got %d", tr.DrainSeconds)
	}
}

func TestIngressSpec_Fields(t *testing.T) {
	i := interfaces.IngressSpec{
		LoadBalancer: "round_robin",
	}
	if i.LoadBalancer != "round_robin" {
		t.Errorf("LoadBalancer: got %q", i.LoadBalancer)
	}
}

func TestEgressSpec_Fields(t *testing.T) {
	e := interfaces.EgressSpec{
		Bandwidth: "1Gbps",
	}
	if e.Bandwidth != "1Gbps" {
		t.Errorf("Bandwidth: got %q", e.Bandwidth)
	}
}

func TestMaintenanceSpec_Fields(t *testing.T) {
	m := interfaces.MaintenanceSpec{
		Window: "Sun 03:00-05:00 UTC",
	}
	if m.Window != "Sun 03:00-05:00 UTC" {
		t.Errorf("Window: got %q", m.Window)
	}
}

func TestWorkloadResourceSpec_Fields(t *testing.T) {
	r := interfaces.WorkloadResourceSpec{
		CPUMillis: 500,
		MemoryMiB: 512,
	}
	if r.CPUMillis != 500 {
		t.Errorf("CPUMillis: got %d", r.CPUMillis)
	}
}
