package module

import (
	"context"
	"fmt"
	"strings"

	"github.com/CrisisTextLine/modular"
	"github.com/digitalocean/godo"
)

// DODNSState holds the current state of DigitalOcean DNS.
type DODNSState struct {
	DomainName string             `json:"domainName"`
	Records    []DODNSRecordState `json:"records"`
	Status     string             `json:"status"` // pending, active, deleting, deleted
}

// DODNSRecordState describes a single DigitalOcean DNS record.
type DODNSRecordState struct {
	ID   int    `json:"id"`
	Type string `json:"type"`
	Name string `json:"name"`
	Data string `json:"data"`
	TTL  int    `json:"ttl"`
}

// doDNSBackend is the interface DO DNS backends implement.
type doDNSBackend interface {
	plan(m *PlatformDODNS) (*DODNSPlan, error)
	apply(m *PlatformDODNS) (*DODNSState, error)
	status(m *PlatformDODNS) (*DODNSState, error)
	destroy(m *PlatformDODNS) error
}

// DODNSPlan describes planned DNS changes.
type DODNSPlan struct {
	Domain  string             `json:"domain"`
	Records []DODNSRecordState `json:"records"`
	Changes []string           `json:"changes"`
}

// PlatformDODNS manages DigitalOcean domains and DNS records.
// Config:
//
//	account:  name of a cloud.account module (provider=digitalocean)
//	provider: digitalocean | mock
//	domain:   domain name (e.g. example.com)
//	records:  list of DNS record definitions (name, type, data, ttl)
type PlatformDODNS struct {
	name     string
	config   map[string]any
	provider CloudCredentialProvider
	state    *DODNSState
	backend  doDNSBackend
}

// NewPlatformDODNS creates a new PlatformDODNS module.
func NewPlatformDODNS(name string, cfg map[string]any) *PlatformDODNS {
	return &PlatformDODNS{name: name, config: cfg}
}

// Name returns the module name.
func (m *PlatformDODNS) Name() string { return m.name }

// Init resolves the cloud.account service and initializes the backend.
func (m *PlatformDODNS) Init(app modular.Application) error {
	domain, _ := m.config["domain"].(string)
	if domain == "" {
		return fmt.Errorf("platform.do_dns %q: 'domain' is required", m.name)
	}

	accountName, _ := m.config["account"].(string)
	providerType, _ := m.config["provider"].(string)
	if providerType == "" {
		providerType = "mock"
	}

	if accountName != "" {
		svc, ok := app.SvcRegistry()[accountName]
		if !ok {
			return fmt.Errorf("platform.do_dns %q: account service %q not found", m.name, accountName)
		}
		prov, ok := svc.(CloudCredentialProvider)
		if !ok {
			return fmt.Errorf("platform.do_dns %q: service %q does not implement CloudCredentialProvider", m.name, accountName)
		}
		m.provider = prov
		if providerType == "mock" {
			providerType = prov.Provider()
		}
	}

	m.state = &DODNSState{
		DomainName: domain,
		Status:     "pending",
	}

	switch providerType {
	case "mock":
		m.backend = &doDNSMockBackend{}
	case "digitalocean":
		acc, ok := app.SvcRegistry()[accountName].(*CloudAccount)
		if !ok {
			return fmt.Errorf("platform.do_dns %q: account %q is not a *CloudAccount", m.name, accountName)
		}
		client, err := acc.doClient()
		if err != nil {
			return fmt.Errorf("platform.do_dns %q: %w", m.name, err)
		}
		m.backend = &doDNSRealBackend{client: client}
	default:
		return fmt.Errorf("platform.do_dns %q: unsupported provider %q", m.name, providerType)
	}

	return app.RegisterService(m.name, m)
}

// ProvidesServices declares the service this module provides.
func (m *PlatformDODNS) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{Name: m.name, Description: "DO DNS: " + m.name, Instance: m},
	}
}

// RequiresServices returns nil.
func (m *PlatformDODNS) RequiresServices() []modular.ServiceDependency { return nil }

// Plan returns the planned DNS changes.
func (m *PlatformDODNS) Plan() (*DODNSPlan, error) { return m.backend.plan(m) }

// Apply creates or updates the domain and records.
func (m *PlatformDODNS) Apply() (*DODNSState, error) { return m.backend.apply(m) }

// Status returns the current DNS state.
func (m *PlatformDODNS) Status() (*DODNSState, error) { return m.backend.status(m) }

// Destroy deletes the domain and all records.
func (m *PlatformDODNS) Destroy() error { return m.backend.destroy(m) }

// recordConfigs parses DNS record configs from module config.
func (m *PlatformDODNS) recordConfigs() []DODNSRecordState {
	raw, ok := m.config["records"].([]any)
	if !ok {
		return nil
	}
	var records []DODNSRecordState
	for _, item := range raw {
		rec, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := rec["name"].(string)
		rtype, _ := rec["type"].(string)
		data, _ := rec["data"].(string)
		ttl, _ := intFromAny(rec["ttl"])
		if ttl == 0 {
			ttl = 300
		}
		records = append(records, DODNSRecordState{
			Type: strings.ToUpper(rtype),
			Name: name,
			Data: data,
			TTL:  ttl,
		})
	}
	return records
}

// ─── mock backend ──────────────────────────────────────────────────────────────

type doDNSMockBackend struct{}

func (b *doDNSMockBackend) plan(m *PlatformDODNS) (*DODNSPlan, error) {
	if m.state.Status == "active" {
		return &DODNSPlan{
			Domain:  m.state.DomainName,
			Changes: []string{"no changes"},
		}, nil
	}
	records := m.recordConfigs()
	changes := []string{fmt.Sprintf("create domain %q", m.state.DomainName)}
	for _, r := range records {
		changes = append(changes, fmt.Sprintf("create %s record %q → %s", r.Type, r.Name, r.Data))
	}
	return &DODNSPlan{
		Domain:  m.state.DomainName,
		Records: records,
		Changes: changes,
	}, nil
}

func (b *doDNSMockBackend) apply(m *PlatformDODNS) (*DODNSState, error) {
	if m.state.Status == "active" {
		return m.state, nil
	}
	records := m.recordConfigs()
	for i := range records {
		records[i].ID = i + 1
	}
	m.state.Records = records
	m.state.Status = "active"
	return m.state, nil
}

func (b *doDNSMockBackend) status(m *PlatformDODNS) (*DODNSState, error) {
	return m.state, nil
}

func (b *doDNSMockBackend) destroy(m *PlatformDODNS) error {
	if m.state.Status == "deleted" {
		return nil
	}
	m.state.Records = nil
	m.state.Status = "deleted"
	return nil
}

// ─── real backend ──────────────────────────────────────────────────────────────

type doDNSRealBackend struct {
	client *godo.Client
}

func (b *doDNSRealBackend) plan(m *PlatformDODNS) (*DODNSPlan, error) {
	records := m.recordConfigs()
	changes := []string{fmt.Sprintf("create/update domain %q", m.state.DomainName)}
	for _, r := range records {
		changes = append(changes, fmt.Sprintf("create %s record %q → %s", r.Type, r.Name, r.Data))
	}
	return &DODNSPlan{
		Domain:  m.state.DomainName,
		Records: records,
		Changes: changes,
	}, nil
}

func (b *doDNSRealBackend) apply(m *PlatformDODNS) (*DODNSState, error) {
	// Create domain if it doesn't exist.
	_, _, err := b.client.Domains.Create(context.Background(), &godo.DomainCreateRequest{
		Name: m.state.DomainName,
	})
	if err != nil {
		// Domain may already exist — continue.
		_ = err
	}

	records := m.recordConfigs()
	for i, r := range records {
		req := &godo.DomainRecordEditRequest{
			Type: r.Type,
			Name: r.Name,
			Data: r.Data,
			TTL:  r.TTL,
		}
		created, _, err := b.client.Domains.CreateRecord(context.Background(), m.state.DomainName, req)
		if err != nil {
			return nil, fmt.Errorf("do_dns create record %q: %w", r.Name, err)
		}
		records[i].ID = created.ID
	}
	m.state.Records = records
	m.state.Status = "active"
	return m.state, nil
}

func (b *doDNSRealBackend) status(m *PlatformDODNS) (*DODNSState, error) {
	recs, _, err := b.client.Domains.Records(context.Background(), m.state.DomainName, nil)
	if err != nil {
		return nil, fmt.Errorf("do_dns list records: %w", err)
	}
	var records []DODNSRecordState
	for _, r := range recs {
		records = append(records, DODNSRecordState{
			ID:   r.ID,
			Type: r.Type,
			Name: r.Name,
			Data: r.Data,
			TTL:  r.TTL,
		})
	}
	m.state.Records = records
	return m.state, nil
}

func (b *doDNSRealBackend) destroy(m *PlatformDODNS) error {
	for _, r := range m.state.Records {
		if _, err := b.client.Domains.DeleteRecord(context.Background(), m.state.DomainName, r.ID); err != nil {
			return fmt.Errorf("do_dns delete record %d: %w", r.ID, err)
		}
	}
	if _, err := b.client.Domains.Delete(context.Background(), m.state.DomainName); err != nil {
		return fmt.Errorf("do_dns delete domain: %w", err)
	}
	m.state.Records = nil
	m.state.Status = "deleted"
	return nil
}
