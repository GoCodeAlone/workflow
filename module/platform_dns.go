package module

import (
	"fmt"
	"strings"

	"github.com/CrisisTextLine/modular"
)

// validDNSRecordTypes is the set of supported DNS record types.
var validDNSRecordTypes = map[string]bool{
	"A": true, "AAAA": true, "CNAME": true, "ALIAS": true,
	"TXT": true, "MX": true, "SRV": true, "NS": true, "PTR": true,
}

// DNSZoneConfig describes a DNS zone.
type DNSZoneConfig struct {
	Name    string `json:"name"`
	Comment string `json:"comment"`
	Private bool   `json:"private"`
	VPCID   string `json:"vpcId"` // for private hosted zones
}

// DNSRecordConfig describes a single DNS record.
type DNSRecordConfig struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Value string `json:"value"`
	TTL   int    `json:"ttl"`
}

// DNSPlan holds the planned DNS changes.
type DNSPlan struct {
	Zone    DNSZoneConfig     `json:"zone"`
	Records []DNSRecordConfig `json:"records"`
	Changes []string          `json:"changes"`
}

// DNSState describes the current state of a DNS zone.
type DNSState struct {
	ZoneID   string            `json:"zoneId"`
	ZoneName string            `json:"zoneName"`
	Records  []DNSRecordConfig `json:"records"`
	Status   string            `json:"status"` // pending, active, deleting, deleted
}

// dnsBackend is the internal interface DNS provider backends implement.
type dnsBackend interface {
	planDNS(m *PlatformDNS) (*DNSPlan, error)
	applyDNS(m *PlatformDNS) (*DNSState, error)
	statusDNS(m *PlatformDNS) (*DNSState, error)
	destroyDNS(m *PlatformDNS) error
}

// PlatformDNS manages DNS zones and records via pluggable backends.
// Config:
//
//	account:  name of a cloud.account module (optional)
//	provider: aws (Route53) | mock
//	zone:     zone config (name, comment, private, vpcId)
//	records:  list of DNS record definitions
type PlatformDNS struct {
	name     string
	config   map[string]any
	provider CloudCredentialProvider // resolved from service registry
	state    *DNSState
	backend  dnsBackend
}

// NewPlatformDNS creates a new PlatformDNS module.
func NewPlatformDNS(name string, cfg map[string]any) *PlatformDNS {
	return &PlatformDNS{name: name, config: cfg}
}

// Name returns the module name.
func (m *PlatformDNS) Name() string { return m.name }

// Init resolves the cloud.account service and initialises the backend.
func (m *PlatformDNS) Init(app modular.Application) error {
	accountName, _ := m.config["account"].(string)
	if accountName != "" {
		svc, ok := app.SvcRegistry()[accountName]
		if !ok {
			return fmt.Errorf("platform.dns %q: account service %q not found", m.name, accountName)
		}
		prov, ok := svc.(CloudCredentialProvider)
		if !ok {
			return fmt.Errorf("platform.dns %q: service %q does not implement CloudCredentialProvider", m.name, accountName)
		}
		m.provider = prov
	}

	providerType, _ := m.config["provider"].(string)
	if providerType == "" {
		providerType = "mock"
	}

	switch providerType {
	case "mock":
		m.backend = &mockDNSBackend{}
	case "aws":
		m.backend = &route53Backend{}
	default:
		return fmt.Errorf("platform.dns %q: unsupported provider %q", m.name, providerType)
	}

	zone := m.zoneConfig()
	if zone.Name == "" {
		return fmt.Errorf("platform.dns %q: zone.name is required", m.name)
	}

	if err := m.validateRecords(); err != nil {
		return fmt.Errorf("platform.dns %q: %w", m.name, err)
	}

	m.state = &DNSState{
		ZoneName: zone.Name,
		Status:   "pending",
	}

	return app.RegisterService(m.name, m)
}

// ProvidesServices declares the service this module provides.
func (m *PlatformDNS) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{Name: m.name, Description: "DNS zone manager: " + m.name, Instance: m},
	}
}

// RequiresServices returns nil — cloud.account is resolved by name.
func (m *PlatformDNS) RequiresServices() []modular.ServiceDependency {
	return nil
}

// Plan returns the DNS changes needed to reach desired state.
func (m *PlatformDNS) Plan() (*DNSPlan, error) {
	return m.backend.planDNS(m)
}

// Apply creates/updates the DNS zone and records.
func (m *PlatformDNS) Apply() (*DNSState, error) {
	return m.backend.applyDNS(m)
}

// Status returns the current DNS zone state.
func (m *PlatformDNS) Status() (*DNSState, error) {
	return m.backend.statusDNS(m)
}

// Destroy deletes the DNS zone and all records.
func (m *PlatformDNS) Destroy() error {
	return m.backend.destroyDNS(m)
}

// zoneConfig parses the zone config from module config.
func (m *PlatformDNS) zoneConfig() DNSZoneConfig {
	raw, ok := m.config["zone"].(map[string]any)
	if !ok {
		return DNSZoneConfig{}
	}
	name, _ := raw["name"].(string)
	comment, _ := raw["comment"].(string)
	private, _ := raw["private"].(bool)
	vpcID, _ := raw["vpcId"].(string)
	return DNSZoneConfig{Name: name, Comment: comment, Private: private, VPCID: vpcID}
}

// recordConfigs parses the records config.
func (m *PlatformDNS) recordConfigs() []DNSRecordConfig {
	raw, ok := m.config["records"].([]any)
	if !ok {
		return nil
	}
	var records []DNSRecordConfig
	for _, item := range raw {
		rec, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := rec["name"].(string)
		rtype, _ := rec["type"].(string)
		value, _ := rec["value"].(string)
		ttl, _ := intFromAny(rec["ttl"])
		if ttl == 0 {
			ttl = 300
		}
		records = append(records, DNSRecordConfig{
			Name:  name,
			Type:  strings.ToUpper(rtype),
			Value: value,
			TTL:   ttl,
		})
	}
	return records
}

// validateRecords checks that all configured records have valid types.
func (m *PlatformDNS) validateRecords() error {
	for _, rec := range m.recordConfigs() {
		if !validDNSRecordTypes[rec.Type] {
			return fmt.Errorf("invalid record type %q for %q (supported: A, AAAA, CNAME, ALIAS, TXT, MX, SRV, NS, PTR)", rec.Type, rec.Name)
		}
	}
	return nil
}
