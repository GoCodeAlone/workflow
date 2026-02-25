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

// ─── Route53 stub ─────────────────────────────────────────────────────────────

// route53Backend is a stub for Amazon Route 53.
// Real implementation would use aws-sdk-go-v2/service/route53 to:
//   - CreateHostedZone / GetHostedZone / DeleteHostedZone
//   - ChangeResourceRecordSets / ListResourceRecordSets
type route53Backend struct{}

func (b *route53Backend) planDNS(m *PlatformDNS) (*DNSPlan, error) {
	zone := m.zoneConfig()
	return &DNSPlan{
		Zone:    zone,
		Records: m.recordConfigs(),
		Changes: []string{fmt.Sprintf("create Route53 hosted zone %q (stub — use aws-sdk-go-v2/service/route53)", zone.Name)},
	}, nil
}

func (b *route53Backend) applyDNS(m *PlatformDNS) (*DNSState, error) {
	return nil, fmt.Errorf("route53 backend: not implemented — use aws-sdk-go-v2/service/route53")
}

func (b *route53Backend) statusDNS(m *PlatformDNS) (*DNSState, error) {
	m.state.Status = "unknown"
	return m.state, nil
}

func (b *route53Backend) destroyDNS(m *PlatformDNS) error {
	return fmt.Errorf("route53 backend: not implemented — use aws-sdk-go-v2/service/route53")
}
