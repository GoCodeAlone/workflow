package module

import (
	"context"
	"fmt"

	"github.com/CrisisTextLine/modular"
)

// MongoDBNoSQLConfig holds configuration for the nosql.mongodb module.
//
// Full MongoDB implementation would use:
//   - go.mongodb.org/mongo-driver/mongo
//
// When uri == "memory://" the module falls back to the in-memory backend.
type MongoDBNoSQLConfig struct {
	URI        string `json:"uri"        yaml:"uri"`        // "memory://" => in-memory fallback
	Database   string `json:"database"   yaml:"database"`
	Collection string `json:"collection" yaml:"collection"`
}

// MongoDBNoSQL is the nosql.mongodb module.
// In memory mode (uri: "memory://") it delegates to MemoryNoSQL.
// For real MongoDB, replace backend with a mongo.Collection and implement
// Get/Put/Delete/Query using FindOne, ReplaceOne, DeleteOne, Find.
type MongoDBNoSQL struct {
	name    string
	cfg     MongoDBNoSQLConfig
	backend NoSQLStore
}

// NewMongoDBNoSQL creates a new MongoDBNoSQL module.
func NewMongoDBNoSQL(name string, cfg MongoDBNoSQLConfig) *MongoDBNoSQL {
	return &MongoDBNoSQL{name: name, cfg: cfg}
}

func (m *MongoDBNoSQL) Name() string { return m.name }

func (m *MongoDBNoSQL) Init(_ modular.Application) error {
	if m.cfg.URI == "memory://" || m.cfg.URI == "" {
		m.backend = NewMemoryNoSQL(m.name+"-mem", MemoryNoSQLConfig{Collection: m.cfg.Collection})
		return nil
	}
	// Full MongoDB implementation:
	// client, err := mongo.Connect(ctx, options.Client().ApplyURI(m.cfg.URI))
	// if err != nil { return err }
	// m.collection = client.Database(m.cfg.Database).Collection(m.cfg.Collection)
	return fmt.Errorf("nosql.mongodb %q: real MongoDB URI not yet implemented; use uri: memory:// for testing", m.name)
}

func (m *MongoDBNoSQL) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{Name: m.name, Description: "MongoDB NoSQL store: " + m.name, Instance: m},
	}
}

func (m *MongoDBNoSQL) RequiresServices() []modular.ServiceDependency { return nil }

func (m *MongoDBNoSQL) Get(ctx context.Context, key string) (map[string]any, error) {
	if m.backend == nil {
		return nil, fmt.Errorf("nosql.mongodb %q: not initialized", m.name)
	}
	// Real implementation: collection.FindOne(ctx, bson.M{"_id": key})
	return m.backend.Get(ctx, key)
}

func (m *MongoDBNoSQL) Put(ctx context.Context, key string, item map[string]any) error {
	if m.backend == nil {
		return fmt.Errorf("nosql.mongodb %q: not initialized", m.name)
	}
	// Real implementation: collection.ReplaceOne(ctx, bson.M{"_id": key}, item, opts.Upsert(true))
	return m.backend.Put(ctx, key, item)
}

func (m *MongoDBNoSQL) Delete(ctx context.Context, key string) error {
	if m.backend == nil {
		return fmt.Errorf("nosql.mongodb %q: not initialized", m.name)
	}
	// Real implementation: collection.DeleteOne(ctx, bson.M{"_id": key})
	return m.backend.Delete(ctx, key)
}

func (m *MongoDBNoSQL) Query(ctx context.Context, params map[string]any) ([]map[string]any, error) {
	if m.backend == nil {
		return nil, fmt.Errorf("nosql.mongodb %q: not initialized", m.name)
	}
	// Real implementation: collection.Find(ctx, bson.M{...filter from params...})
	return m.backend.Query(ctx, params)
}
