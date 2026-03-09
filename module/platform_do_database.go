package module

import (
	"context"
	"fmt"
	"time"

	"github.com/CrisisTextLine/modular"
	"github.com/digitalocean/godo"
)

// DODatabaseState holds the current state of a DO Managed Database.
type DODatabaseState struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Engine       string    `json:"engine"`       // pg, mysql, redis, mongodb, kafka
	Version      string    `json:"version"`
	Size         string    `json:"size"`          // e.g. db-s-1vcpu-1gb
	Region       string    `json:"region"`
	NumNodes     int       `json:"numNodes"`
	Status       string    `json:"status"`        // pending, online, resizing, migrating, error
	Host         string    `json:"host"`
	Port         int       `json:"port"`
	DatabaseName string    `json:"databaseName"`
	User         string    `json:"user"`
	Password     string    `json:"password"`
	URI          string    `json:"uri"`
	CreatedAt    time.Time `json:"createdAt"`
}

// doDatabaseBackend is the interface for DO managed database backends.
type doDatabaseBackend interface {
	create(m *PlatformDODatabase) (*DODatabaseState, error)
	status(m *PlatformDODatabase) (*DODatabaseState, error)
	destroy(m *PlatformDODatabase) error
}

// PlatformDODatabase manages DigitalOcean Managed Databases.
// Config:
//
//	account:   name of a cloud.account module (provider=digitalocean)
//	provider:  digitalocean | mock
//	engine:    pg | mysql | redis | mongodb | kafka
//	version:   engine version string (e.g. "16" for pg)
//	size:      droplet size slug (e.g. db-s-1vcpu-1gb)
//	region:    DO region slug (e.g. nyc1)
//	num_nodes: number of nodes (default: 1)
//	name:      database cluster name
type PlatformDODatabase struct {
	name    string
	config  map[string]any
	state   *DODatabaseState
	backend doDatabaseBackend
}

// NewPlatformDODatabase creates a new PlatformDODatabase module.
func NewPlatformDODatabase(name string, cfg map[string]any) *PlatformDODatabase {
	return &PlatformDODatabase{name: name, config: cfg}
}

func (m *PlatformDODatabase) Name() string { return m.name }

func (m *PlatformDODatabase) Init(app modular.Application) error {
	dbName, _ := m.config["name"].(string)
	if dbName == "" {
		dbName = m.name
	}
	engine, _ := m.config["engine"].(string)
	if engine == "" {
		engine = "pg"
	}
	version, _ := m.config["version"].(string)
	size, _ := m.config["size"].(string)
	if size == "" {
		size = "db-s-1vcpu-1gb"
	}
	region, _ := m.config["region"].(string)
	if region == "" {
		region = "nyc1"
	}
	numNodes, _ := intFromAny(m.config["num_nodes"])
	if numNodes == 0 {
		numNodes = 1
	}

	m.state = &DODatabaseState{
		Name:     dbName,
		Engine:   engine,
		Version:  version,
		Size:     size,
		Region:   region,
		NumNodes: numNodes,
		Status:   "pending",
	}

	providerType, _ := m.config["provider"].(string)
	if providerType == "" {
		providerType = "mock"
	}

	switch providerType {
	case "mock":
		m.backend = &doDatabaseMockBackend{}
	case "digitalocean":
		accountName, _ := m.config["account"].(string)
		acc, ok := app.SvcRegistry()[accountName].(*CloudAccount)
		if !ok {
			return fmt.Errorf("platform.do_database %q: account %q is not a *CloudAccount", m.name, accountName)
		}
		client, err := acc.doClient()
		if err != nil {
			return fmt.Errorf("platform.do_database %q: %w", m.name, err)
		}
		m.backend = &doDatabaseRealBackend{client: client}
	default:
		return fmt.Errorf("platform.do_database %q: unsupported provider %q", m.name, providerType)
	}

	return app.RegisterService(m.name, m)
}

func (m *PlatformDODatabase) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{Name: m.name, Description: "DO Database: " + m.name, Instance: m},
	}
}

func (m *PlatformDODatabase) RequiresServices() []modular.ServiceDependency { return nil }

// PlatformProvider implementation — directly, no adapter needed since this is new.

func (m *PlatformDODatabase) Plan() (*PlatformPlan, error) {
	actionType := "create"
	detail := fmt.Sprintf("Create %s %s database %q in %s (%s, %d nodes)",
		m.state.Engine, m.state.Version, m.state.Name, m.state.Region, m.state.Size, m.state.NumNodes)
	if m.state.ID != "" {
		actionType = "update"
		detail = fmt.Sprintf("Update database %q (%s → %s, %d nodes)",
			m.state.Name, m.state.Size, m.state.Size, m.state.NumNodes)
	}
	return &PlatformPlan{
		Provider: "digitalocean",
		Resource: "managed_database",
		Actions:  []PlatformAction{{Type: actionType, Resource: m.state.Name, Detail: detail}},
	}, nil
}

func (m *PlatformDODatabase) Apply() (*PlatformResult, error) {
	st, err := m.backend.create(m)
	if err != nil {
		return &PlatformResult{Success: false, Message: err.Error()}, err
	}
	m.state = st
	return &PlatformResult{
		Success: true,
		Message: fmt.Sprintf("Database %s online (host: %s:%d)", st.Name, st.Host, st.Port),
		State:   st,
	}, nil
}

func (m *PlatformDODatabase) Status() (any, error) {
	return m.backend.status(m)
}

func (m *PlatformDODatabase) Destroy() error {
	return m.backend.destroy(m)
}

// ─── mock backend ──────────────────────────────────────────────────────────────

type doDatabaseMockBackend struct{}

func (b *doDatabaseMockBackend) create(m *PlatformDODatabase) (*DODatabaseState, error) {
	m.state.ID = "mock-db-" + m.state.Name
	m.state.Status = "online"
	m.state.Host = m.state.Name + ".db.ondigitalocean.com"
	m.state.Port = 25060
	m.state.DatabaseName = "defaultdb"
	m.state.User = "doadmin"
	m.state.Password = "mock-password"
	m.state.URI = fmt.Sprintf("postgresql://%s:%s@%s:%d/%s?sslmode=require",
		m.state.User, m.state.Password, m.state.Host, m.state.Port, m.state.DatabaseName)
	m.state.CreatedAt = time.Now().UTC()
	return m.state, nil
}

func (b *doDatabaseMockBackend) status(m *PlatformDODatabase) (*DODatabaseState, error) {
	return m.state, nil
}

func (b *doDatabaseMockBackend) destroy(m *PlatformDODatabase) error {
	m.state.Status = "deleted"
	m.state.ID = ""
	return nil
}

// ─── real backend ──────────────────────────────────────────────────────────────

type doDatabaseRealBackend struct {
	client *godo.Client
}

func (b *doDatabaseRealBackend) create(m *PlatformDODatabase) (*DODatabaseState, error) {
	req := &godo.DatabaseCreateRequest{
		Name:       m.state.Name,
		EngineSlug: m.state.Engine,
		Version:    m.state.Version,
		SizeSlug:   m.state.Size,
		Region:     m.state.Region,
		NumNodes:   m.state.NumNodes,
	}
	db, _, err := b.client.Databases.Create(context.Background(), req)
	if err != nil {
		return nil, fmt.Errorf("create database: %w", err)
	}
	return doDatabaseFromGodo(db), nil
}

func (b *doDatabaseRealBackend) status(m *PlatformDODatabase) (*DODatabaseState, error) {
	if m.state.ID == "" {
		return m.state, nil
	}
	db, _, err := b.client.Databases.Get(context.Background(), m.state.ID)
	if err != nil {
		return nil, fmt.Errorf("get database: %w", err)
	}
	return doDatabaseFromGodo(db), nil
}

func (b *doDatabaseRealBackend) destroy(m *PlatformDODatabase) error {
	if m.state.ID == "" {
		return nil
	}
	_, err := b.client.Databases.Delete(context.Background(), m.state.ID)
	if err != nil {
		return fmt.Errorf("delete database: %w", err)
	}
	m.state.Status = "deleted"
	return nil
}

func doDatabaseFromGodo(db *godo.Database) *DODatabaseState {
	st := &DODatabaseState{
		ID:        db.ID,
		Name:      db.Name,
		Engine:    db.EngineSlug,
		Version:   db.VersionSlug,
		Size:      db.SizeSlug,
		Region:    db.RegionSlug,
		NumNodes:  db.NumNodes,
		Status:    db.Status,
		CreatedAt: db.CreatedAt,
	}
	if db.Connection != nil {
		st.Host = db.Connection.Host
		st.Port = db.Connection.Port
		st.DatabaseName = db.Connection.Database
		st.User = db.Connection.User
		st.Password = db.Connection.Password
		st.URI = db.Connection.URI
	}
	return st
}
