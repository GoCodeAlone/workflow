package module

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	r53types "github.com/aws/aws-sdk-go-v2/service/route53/types"
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

// ─── Route53 backend ──────────────────────────────────────────────────────────

// route53Backend manages Amazon Route 53 hosted zones and records
// using aws-sdk-go-v2/service/route53.
type route53Backend struct{}

func (b *route53Backend) planDNS(m *PlatformDNS) (*DNSPlan, error) {
	zone := m.zoneConfig()
	awsProv, ok := awsProviderFrom(m.provider)
	if !ok {
		return &DNSPlan{
			Zone:    zone,
			Records: m.recordConfigs(),
			Changes: []string{fmt.Sprintf("create Route53 hosted zone %q", zone.Name)},
		}, nil
	}

	cfg, err := awsProv.AWSConfig(context.Background())
	if err != nil {
		return nil, fmt.Errorf("route53 plan: AWS config: %w", err)
	}
	client := route53.NewFromConfig(cfg)

	// Check if zone already exists
	listOut, err := client.ListHostedZonesByName(context.Background(), &route53.ListHostedZonesByNameInput{
		DNSName: aws.String(zone.Name),
	})
	if err != nil {
		return nil, fmt.Errorf("route53 plan: ListHostedZonesByName: %w", err)
	}

	plan := &DNSPlan{Zone: zone, Records: m.recordConfigs()}
	for _, hz := range listOut.HostedZones {
		if hz.Name != nil && strings.TrimSuffix(*hz.Name, ".") == strings.TrimSuffix(zone.Name, ".") {
			plan.Changes = []string{fmt.Sprintf("noop: Route53 zone %q already exists", zone.Name)}
			return plan, nil
		}
	}

	plan.Changes = []string{fmt.Sprintf("create Route53 hosted zone %q", zone.Name)}
	for _, r := range m.recordConfigs() {
		plan.Changes = append(plan.Changes, fmt.Sprintf("create %s record %q -> %q", r.Type, r.Name, r.Value))
	}
	return plan, nil
}

func (b *route53Backend) applyDNS(m *PlatformDNS) (*DNSState, error) {
	awsProv, ok := awsProviderFrom(m.provider)
	if !ok {
		return nil, fmt.Errorf("route53 apply: no AWS cloud account configured")
	}

	cfg, err := awsProv.AWSConfig(context.Background())
	if err != nil {
		return nil, fmt.Errorf("route53 apply: AWS config: %w", err)
	}
	client := route53.NewFromConfig(cfg)
	zone := m.zoneConfig()

	// Find or create hosted zone
	zoneID := m.state.ZoneID
	if zoneID == "" {
		listOut, err := client.ListHostedZonesByName(context.Background(), &route53.ListHostedZonesByNameInput{
			DNSName: aws.String(zone.Name),
		})
		if err != nil {
			return nil, fmt.Errorf("route53 apply: ListHostedZonesByName: %w", err)
		}
		for _, hz := range listOut.HostedZones {
			if hz.Name != nil && strings.TrimSuffix(*hz.Name, ".") == strings.TrimSuffix(zone.Name, ".") {
				if hz.Id != nil {
					zoneID = strings.TrimPrefix(*hz.Id, "/hostedzone/")
				}
				break
			}
		}
	}

	if zoneID == "" {
		createOut, err := client.CreateHostedZone(context.Background(), &route53.CreateHostedZoneInput{
			Name:            aws.String(zone.Name),
			CallerReference: aws.String(fmt.Sprintf("workflow-%s", zone.Name)),
			HostedZoneConfig: &r53types.HostedZoneConfig{
				Comment:     aws.String(zone.Comment),
				PrivateZone: zone.Private,
			},
		})
		if err != nil {
			return nil, fmt.Errorf("route53 apply: CreateHostedZone: %w", err)
		}
		if createOut.HostedZone != nil && createOut.HostedZone.Id != nil {
			zoneID = strings.TrimPrefix(*createOut.HostedZone.Id, "/hostedzone/")
		}
	}

	// Upsert DNS records
	records := m.recordConfigs()
	if len(records) > 0 {
		var changes []r53types.Change
		for _, rec := range records {
			rrType, err := r53RecordType(rec.Type)
			if err != nil {
				continue
			}
			changes = append(changes, r53types.Change{
				Action: r53types.ChangeActionUpsert,
				ResourceRecordSet: &r53types.ResourceRecordSet{
					Name: aws.String(rec.Name),
					Type: rrType,
					TTL:  aws.Int64(int64(rec.TTL)),
					ResourceRecords: []r53types.ResourceRecord{
						{Value: aws.String(rec.Value)},
					},
				},
			})
		}
		if len(changes) > 0 {
			_, err = client.ChangeResourceRecordSets(context.Background(), &route53.ChangeResourceRecordSetsInput{
				HostedZoneId: aws.String(zoneID),
				ChangeBatch:  &r53types.ChangeBatch{Changes: changes},
			})
			if err != nil {
				return nil, fmt.Errorf("route53 apply: ChangeResourceRecordSets: %w", err)
			}
		}
	}

	m.state.ZoneID = zoneID
	m.state.ZoneName = zone.Name
	m.state.Records = records
	m.state.Status = "active"
	return m.state, nil
}

func (b *route53Backend) statusDNS(m *PlatformDNS) (*DNSState, error) {
	awsProv, ok := awsProviderFrom(m.provider)
	if !ok {
		return m.state, nil
	}

	cfg, err := awsProv.AWSConfig(context.Background())
	if err != nil {
		return m.state, fmt.Errorf("route53 status: AWS config: %w", err)
	}
	client := route53.NewFromConfig(cfg)

	if m.state.ZoneID == "" {
		m.state.Status = "not-found"
		return m.state, nil
	}

	_, getErr := client.GetHostedZone(context.Background(), &route53.GetHostedZoneInput{
		Id: aws.String(m.state.ZoneID),
	})
	if getErr == nil {
		// Zone found — list records
		listOut, listErr := client.ListResourceRecordSets(context.Background(), &route53.ListResourceRecordSetsInput{
			HostedZoneId: aws.String(m.state.ZoneID),
		})
		if listErr == nil {
			var records []DNSRecordConfig
			for i := range listOut.ResourceRecordSets {
				if listOut.ResourceRecordSets[i].Name == nil {
					continue
				}
				for _, rr := range listOut.ResourceRecordSets[i].ResourceRecords {
					if rr.Value == nil {
						continue
					}
					ttl := 300
					if listOut.ResourceRecordSets[i].TTL != nil {
						ttl = int(*listOut.ResourceRecordSets[i].TTL)
					}
					records = append(records, DNSRecordConfig{
						Name:  *listOut.ResourceRecordSets[i].Name,
						Type:  string(listOut.ResourceRecordSets[i].Type),
						Value: *rr.Value,
						TTL:   ttl,
					})
				}
			}
			m.state.Records = records
		}
		m.state.Status = "active"
	} else {
		m.state.Status = "not-found"
	}
	return m.state, nil
}

func (b *route53Backend) destroyDNS(m *PlatformDNS) error {
	awsProv, ok := awsProviderFrom(m.provider)
	if !ok {
		return fmt.Errorf("route53 destroy: no AWS cloud account configured")
	}

	cfg, err := awsProv.AWSConfig(context.Background())
	if err != nil {
		return fmt.Errorf("route53 destroy: AWS config: %w", err)
	}
	client := route53.NewFromConfig(cfg)

	if m.state.ZoneID == "" {
		return nil
	}

	// Delete all non-NS/SOA records before deleting the zone
	listOut, listErr := client.ListResourceRecordSets(context.Background(), &route53.ListResourceRecordSetsInput{
		HostedZoneId: aws.String(m.state.ZoneID),
	})
	if listErr != nil {
		return fmt.Errorf("route53 destroy: ListResourceRecordSets: %w", listErr)
	}
	var changes []r53types.Change
	for i := range listOut.ResourceRecordSets {
		if listOut.ResourceRecordSets[i].Type == r53types.RRTypeNs || listOut.ResourceRecordSets[i].Type == r53types.RRTypeSoa {
			continue
		}
		changes = append(changes, r53types.Change{
			Action:            r53types.ChangeActionDelete,
			ResourceRecordSet: &listOut.ResourceRecordSets[i],
		})
	}
	if len(changes) > 0 {
		if _, err := client.ChangeResourceRecordSets(context.Background(), &route53.ChangeResourceRecordSetsInput{
			HostedZoneId: aws.String(m.state.ZoneID),
			ChangeBatch:  &r53types.ChangeBatch{Changes: changes},
		}); err != nil {
			return fmt.Errorf("route53 destroy: ChangeResourceRecordSets: %w", err)
		}
	}

	_, err = client.DeleteHostedZone(context.Background(), &route53.DeleteHostedZoneInput{
		Id: aws.String(m.state.ZoneID),
	})
	if err != nil {
		return fmt.Errorf("route53 destroy: DeleteHostedZone: %w", err)
	}

	m.state.Status = "deleted"
	m.state.ZoneID = ""
	m.state.Records = nil
	return nil
}

// r53RecordType maps a DNS record type string to the Route53 RRType.
func r53RecordType(t string) (r53types.RRType, error) {
	switch strings.ToUpper(t) {
	case "A":
		return r53types.RRTypeA, nil
	case "AAAA":
		return r53types.RRTypeAaaa, nil
	case "CNAME":
		return r53types.RRTypeCname, nil
	case "TXT":
		return r53types.RRTypeTxt, nil
	case "MX":
		return r53types.RRTypeMx, nil
	case "NS":
		return r53types.RRTypeNs, nil
	case "SRV":
		return r53types.RRTypeSrv, nil
	case "PTR":
		return r53types.RRTypePtr, nil
	default:
		return "", fmt.Errorf("unsupported Route53 record type: %q", t)
	}
}
