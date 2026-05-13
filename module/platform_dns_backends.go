package module

import (
	"fmt"
)

// ─── Mock backend ─────────────────────────────────────────────────────────────

// mockDNSBackend is an in-memory DNS backend for testing and local use.
// No real DNS API calls are made; state is tracked in memory.
type mockDNSBackend struct{}

func (b *mockDNSBackend) planDNS(m *PlatformDNS) (*DNSPlan, error) {
	zone := m.zoneConfig()
	records := m.recordConfigs()
	plan := &DNSPlan{
		Zone:    zone,
		Records: records,
	}

	switch m.state.Status {
	case "pending":
		plan.Changes = append(plan.Changes, fmt.Sprintf("create zone %q", zone.Name))
		for _, r := range records {
			plan.Changes = append(plan.Changes, fmt.Sprintf("create %s record %q -> %q", r.Type, r.Name, r.Value))
		}
	case "active":
		// diff existing records vs desired
		existing := map[string]DNSRecordConfig{}
		for _, r := range m.state.Records {
			existing[r.Name+"/"+r.Type] = r
		}
		for _, r := range records {
			key := r.Name + "/" + r.Type
			if e, ok := existing[key]; !ok {
				plan.Changes = append(plan.Changes, fmt.Sprintf("create %s record %q -> %q", r.Type, r.Name, r.Value))
			} else if e.Value != r.Value || e.TTL != r.TTL {
				plan.Changes = append(plan.Changes, fmt.Sprintf("update %s record %q: %q -> %q", r.Type, r.Name, e.Value, r.Value))
			}
		}
		if len(plan.Changes) == 0 {
			plan.Changes = []string{"no changes"}
		}
	case "deleted":
		plan.Changes = append(plan.Changes, fmt.Sprintf("create zone %q (previously deleted)", zone.Name))
		for _, r := range records {
			plan.Changes = append(plan.Changes, fmt.Sprintf("create %s record %q -> %q", r.Type, r.Name, r.Value))
		}
	default:
		plan.Changes = []string{fmt.Sprintf("zone status=%s, no action", m.state.Status)}
	}

	return plan, nil
}

func (b *mockDNSBackend) applyDNS(m *PlatformDNS) (*DNSState, error) {
	if m.state.Status == "active" {
		// update records in place
		m.state.Records = m.recordConfigs()
		return m.state, nil
	}

	zone := m.zoneConfig()
	m.state.ZoneID = fmt.Sprintf("mock-zone-%s", zone.Name)
	m.state.ZoneName = zone.Name
	m.state.Records = m.recordConfigs()
	m.state.Status = "active"
	return m.state, nil
}

func (b *mockDNSBackend) statusDNS(m *PlatformDNS) (*DNSState, error) {
	return m.state, nil
}

func (b *mockDNSBackend) destroyDNS(m *PlatformDNS) error {
	if m.state.Status == "deleted" {
		return nil
	}
	m.state.Status = "deleting"
	m.state.Records = nil
	m.state.Status = "deleted"
	return nil
}

// ─── AWS Route53 migration error backend ──────────────────────────────────────

// awsRoute53ErrorBackend is registered under provider "aws" after the Route53
// backend was removed from workflow core in v0.53.0 (issue #653).
// All methods return the actionable migration error directing the operator to
// infra.dns + workflow-plugin-aws.
type awsRoute53ErrorBackend struct{}

func (b *awsRoute53ErrorBackend) planDNS(m *PlatformDNS) (*DNSPlan, error) {
	return nil, b.err(m)
}

func (b *awsRoute53ErrorBackend) applyDNS(m *PlatformDNS) (*DNSState, error) {
	return nil, b.err(m)
}

func (b *awsRoute53ErrorBackend) statusDNS(m *PlatformDNS) (*DNSState, error) {
	return nil, b.err(m)
}

func (b *awsRoute53ErrorBackend) destroyDNS(m *PlatformDNS) error {
	return b.err(m)
}

func (b *awsRoute53ErrorBackend) err(m *PlatformDNS) error {
	return fmt.Errorf(
		"platform.dns %q: AWS Route53 backend removed from workflow core in v0.53.0 (issue #653).\n"+
			"Migrate to: infra.dns (provider: aws) with workflow-plugin-aws v0.2.0+.\n"+
			"Install: https://github.com/GoCodeAlone/workflow-plugin-aws\n"+
			"See docs/migrations/v0.53.0-aws-iac-removal.md",
		m.name,
	)
}
