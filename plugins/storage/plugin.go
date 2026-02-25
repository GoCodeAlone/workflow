package storage

import (
	"time"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/plugin"
	"github.com/GoCodeAlone/workflow/schema"
)

// Plugin provides storage and database capabilities: storage.s3, storage.local,
// storage.gcs, storage.sqlite, storage.artifact, database.workflow,
// persistence.store, cache.redis modules, and artifact pipeline step factories.
type Plugin struct {
	plugin.BaseEnginePlugin
}

// New creates a new Storage plugin.
func New() *Plugin {
	return &Plugin{
		BaseEnginePlugin: plugin.BaseEnginePlugin{
			BaseNativePlugin: plugin.BaseNativePlugin{
				PluginName:        "storage",
				PluginVersion:     "1.0.0",
				PluginDescription: "Storage, database, persistence, and cache modules with DB pipeline steps",
			},
			Manifest: plugin.PluginManifest{
				Name:        "storage",
				Version:     "1.0.0",
				Author:      "GoCodeAlone",
				Description: "Storage, database, persistence, and cache modules with DB pipeline steps",
				Tier:        plugin.TierCore,
				ModuleTypes: []string{
					"storage.s3",
					"storage.local",
					"storage.gcs",
					"storage.sqlite",
					"storage.artifact",
					"database.workflow",
					"persistence.store",
					"cache.redis",
				},
				StepTypes: []string{
					"step.artifact_upload",
					"step.artifact_download",
					"step.artifact_list",
					"step.artifact_delete",
				},
				Capabilities: []plugin.CapabilityDecl{
					{Name: "storage", Role: "provider", Priority: 10},
					{Name: "database", Role: "provider", Priority: 10},
					{Name: "persistence", Role: "provider", Priority: 10},
					{Name: "cache", Role: "provider", Priority: 10},
				},
			},
		},
	}
}

// Capabilities returns the capability contracts this plugin defines.
func (p *Plugin) Capabilities() []capability.Contract {
	return []capability.Contract{
		{
			Name:        "storage",
			Description: "Object and file storage (S3, GCS, local filesystem)",
		},
		{
			Name:        "database",
			Description: "SQL database connections (SQLite, PostgreSQL, MySQL)",
		},
		{
			Name:        "persistence",
			Description: "Persistence layer that uses a database service for storage",
		},
		{
			Name:        "cache",
			Description: "Redis-backed key/value cache for pipeline data",
		},
	}
}

// ModuleFactories returns factories for all storage/database/persistence module types.
func (p *Plugin) ModuleFactories() map[string]plugin.ModuleFactory {
	return map[string]plugin.ModuleFactory{
		"storage.s3": func(name string, cfg map[string]any) modular.Module {
			s3Mod := module.NewS3Storage(name)
			if bucket, ok := cfg["bucket"].(string); ok {
				s3Mod.SetBucket(bucket)
			}
			if region, ok := cfg["region"].(string); ok {
				s3Mod.SetRegion(region)
			}
			if endpoint, ok := cfg["endpoint"].(string); ok {
				s3Mod.SetEndpoint(endpoint)
			}
			return s3Mod
		},
		"storage.local": func(name string, cfg map[string]any) modular.Module {
			rootDir := "./data/storage"
			if rd, ok := cfg["rootDir"].(string); ok {
				rootDir = rd
			}
			return module.NewLocalStorageModule(name, rootDir)
		},
		"storage.gcs": func(name string, cfg map[string]any) modular.Module {
			gcsMod := module.NewGCSStorage(name)
			if bucket, ok := cfg["bucket"].(string); ok {
				gcsMod.SetBucket(bucket)
			}
			if project, ok := cfg["project"].(string); ok {
				gcsMod.SetProject(project)
			}
			if creds, ok := cfg["credentialsFile"].(string); ok {
				gcsMod.SetCredentialsFile(creds)
			}
			return gcsMod
		},
		"storage.sqlite": func(name string, cfg map[string]any) modular.Module {
			dbPath := "data/workflow.db"
			if p, ok := cfg["dbPath"].(string); ok && p != "" {
				dbPath = p
			}
			dbPath = config.ResolvePathInConfig(cfg, dbPath)
			sqliteStorage := module.NewSQLiteStorage(name, dbPath)
			if mc, ok := cfg["maxConnections"].(float64); ok && mc > 0 {
				sqliteStorage.SetMaxConnections(int(mc))
			}
			if wal, ok := cfg["walMode"].(bool); ok {
				sqliteStorage.SetWALMode(wal)
			}
			return sqliteStorage
		},
		"database.workflow": func(name string, cfg map[string]any) modular.Module {
			dbConfig := module.DatabaseConfig{}
			if driver, ok := cfg["driver"].(string); ok {
				dbConfig.Driver = driver
			}
			if dsn, ok := cfg["dsn"].(string); ok {
				dbConfig.DSN = dsn
			}
			if maxOpen, ok := cfg["maxOpenConns"].(float64); ok {
				dbConfig.MaxOpenConns = int(maxOpen)
			}
			if maxIdle, ok := cfg["maxIdleConns"].(float64); ok {
				dbConfig.MaxIdleConns = int(maxIdle)
			}
			return module.NewWorkflowDatabase(name, dbConfig)
		},
		"persistence.store": func(name string, cfg map[string]any) modular.Module {
			dbServiceName := "database"
			if n, ok := cfg["database"].(string); ok && n != "" {
				dbServiceName = n
			}
			return module.NewPersistenceStore(name, dbServiceName)
		},
		"cache.redis": func(name string, cfg map[string]any) modular.Module {
			redisCfg := module.RedisCacheConfig{
				Address:    "localhost:6379",
				Prefix:     "wf:",
				DefaultTTL: time.Hour,
			}
			if addr, ok := cfg["address"].(string); ok && addr != "" {
				redisCfg.Address = module.ExpandEnvString(addr)
			}
			if pw, ok := cfg["password"].(string); ok {
				redisCfg.Password = module.ExpandEnvString(pw)
			}
			if db, ok := cfg["db"].(float64); ok {
				redisCfg.DB = int(db)
			}
			if prefix, ok := cfg["prefix"].(string); ok && prefix != "" {
				redisCfg.Prefix = prefix
			}
			if ttlStr, ok := cfg["defaultTTL"].(string); ok && ttlStr != "" {
				if d, err := time.ParseDuration(ttlStr); err == nil {
					redisCfg.DefaultTTL = d
				}
			}
			return module.NewRedisCache(name, redisCfg)
		},
		"storage.artifact": func(name string, cfg map[string]any) modular.Module {
			backend, _ := cfg["backend"].(string)
			switch backend {
			case "s3":
				s3Cfg := module.ArtifactS3Config{}
				if bucket, ok := cfg["bucket"].(string); ok {
					s3Cfg.Bucket = bucket
				}
				if prefix, ok := cfg["prefix"].(string); ok {
					s3Cfg.Prefix = prefix
				}
				if region, ok := cfg["region"].(string); ok {
					s3Cfg.Region = region
				}
				if endpoint, ok := cfg["endpoint"].(string); ok {
					s3Cfg.Endpoint = endpoint
				}
				if basePath, ok := cfg["basePath"].(string); ok {
					s3Cfg.BasePath = basePath
				}
				if creds, ok := cfg["credentials"].(map[string]any); ok {
					if k, ok := creds["accessKeyID"].(string); ok {
						s3Cfg.Credentials.AccessKeyID = k
					}
					if s, ok := creds["secretAccessKey"].(string); ok {
						s3Cfg.Credentials.SecretAccessKey = s
					}
				}
				return module.NewArtifactS3Module(name, s3Cfg)
			default: // filesystem
				fsCfg := module.ArtifactFSConfig{
					BasePath: "./data/artifacts",
				}
				if basePath, ok := cfg["basePath"].(string); ok && basePath != "" {
					fsCfg.BasePath = basePath
				}
				if maxSize, ok := cfg["maxSize"].(float64); ok && maxSize > 0 {
					fsCfg.MaxSize = int64(maxSize)
				}
				return module.NewArtifactFSModule(name, fsCfg)
			}
		},
	}
}

// StepFactories returns step factories for artifact pipeline steps.
func (p *Plugin) StepFactories() map[string]plugin.StepFactory {
	return map[string]plugin.StepFactory{
		"step.artifact_upload":   wrapStepFactory(module.NewArtifactUploadStepFactory()),
		"step.artifact_download": wrapStepFactory(module.NewArtifactDownloadStepFactory()),
		"step.artifact_list":     wrapStepFactory(module.NewArtifactListStepFactory()),
		"step.artifact_delete":   wrapStepFactory(module.NewArtifactDeleteStepFactory()),
	}
}

func wrapStepFactory(f module.StepFactory) plugin.StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (any, error) {
		return f(name, cfg, app)
	}
}

// ModuleSchemas returns UI schema definitions for storage/database module types.
func (p *Plugin) ModuleSchemas() []*schema.ModuleSchema {
	return []*schema.ModuleSchema{
		{
			Type:        "storage.s3",
			Label:       "S3 Storage",
			Category:    "integration",
			Description: "Amazon S3 compatible object storage integration",
			Inputs:      []schema.ServiceIODef{{Name: "object", Type: "[]byte", Description: "Object data to store or retrieve"}},
			Outputs:     []schema.ServiceIODef{{Name: "storage", Type: "ObjectStore", Description: "S3-compatible object storage service"}},
			ConfigFields: []schema.ConfigFieldDef{
				{Key: "bucket", Label: "Bucket", Type: schema.FieldTypeString, Required: true, Description: "S3 bucket name", Placeholder: "my-bucket"},
				{Key: "region", Label: "Region", Type: schema.FieldTypeString, DefaultValue: "us-east-1", Description: "AWS region", Placeholder: "us-east-1"},
				{Key: "endpoint", Label: "Endpoint", Type: schema.FieldTypeString, Description: "Custom S3 endpoint (for MinIO, etc.)", Placeholder: "http://localhost:9000"},
			},
			DefaultConfig: map[string]any{"region": "us-east-1"},
		},
		{
			Type:        "storage.local",
			Label:       "Local Storage",
			Category:    "integration",
			Description: "Local filesystem storage provider for workspace files",
			Inputs:      []schema.ServiceIODef{{Name: "file", Type: "[]byte", Description: "File data to store or retrieve"}},
			Outputs:     []schema.ServiceIODef{{Name: "storage", Type: "FileStore", Description: "Local filesystem storage service"}},
			ConfigFields: []schema.ConfigFieldDef{
				{Key: "rootDir", Label: "Root Directory", Type: schema.FieldTypeString, Required: true, Description: "Filesystem path for the storage root", Placeholder: "./data/storage"},
			},
			DefaultConfig: map[string]any{"rootDir": "./data/storage"},
		},
		{
			Type:        "storage.gcs",
			Label:       "GCS Storage",
			Category:    "integration",
			Description: "Google Cloud Storage integration",
			Inputs:      []schema.ServiceIODef{{Name: "object", Type: "[]byte", Description: "Object data to store or retrieve"}},
			Outputs:     []schema.ServiceIODef{{Name: "storage", Type: "ObjectStore", Description: "GCS-compatible object storage service"}},
			ConfigFields: []schema.ConfigFieldDef{
				{Key: "bucket", Label: "Bucket", Type: schema.FieldTypeString, Required: true, Description: "GCS bucket name", Placeholder: "my-bucket"},
				{Key: "project", Label: "GCP Project", Type: schema.FieldTypeString, Description: "Google Cloud project ID", Placeholder: "my-project"},
				{Key: "credentialsFile", Label: "Credentials File", Type: schema.FieldTypeFilePath, Description: "Path to service account JSON key file", Placeholder: "credentials/gcs-key.json", Sensitive: true},
			},
		},
		{
			Type:        "storage.sqlite",
			Label:       "SQLite Storage",
			Category:    "database",
			Description: "SQLite database connection provided as a service for other modules",
			Outputs:     []schema.ServiceIODef{{Name: "database", Type: "sql.DB", Description: "SQLite database connection"}},
			ConfigFields: []schema.ConfigFieldDef{
				{Key: "dbPath", Label: "Database Path", Type: schema.FieldTypeString, DefaultValue: "data/workflow.db", Description: "Path to the SQLite database file", Placeholder: "data/workflow.db"},
				{Key: "maxConnections", Label: "Max Connections", Type: schema.FieldTypeNumber, DefaultValue: 5, Description: "Maximum number of open database connections"},
				{Key: "walMode", Label: "WAL Mode", Type: schema.FieldTypeBool, DefaultValue: true, Description: "Enable Write-Ahead Logging for better concurrent read performance"},
			},
			DefaultConfig: map[string]any{"dbPath": "data/workflow.db", "maxConnections": 5, "walMode": true},
		},
		{
			Type:        "database.workflow",
			Label:       "Workflow Database",
			Category:    "database",
			Description: "SQL database for workflow state persistence (supports PostgreSQL, MySQL, SQLite)",
			Inputs:      []schema.ServiceIODef{{Name: "query", Type: "SQL", Description: "SQL query to execute"}},
			Outputs:     []schema.ServiceIODef{{Name: "database", Type: "sql.DB", Description: "SQL database connection pool"}},
			ConfigFields: []schema.ConfigFieldDef{
				{Key: "driver", Label: "Driver", Type: schema.FieldTypeSelect, Options: []string{"postgres", "mysql", "sqlite3"}, Required: true, Description: "Database driver to use"},
				{Key: "dsn", Label: "DSN", Type: schema.FieldTypeString, Required: true, Description: "Data source name / connection string", Placeholder: "postgres://user:pass@localhost/db?sslmode=disable", Sensitive: true}, //nolint:gosec // G101: placeholder DSN example in schema documentation
				{Key: "maxOpenConns", Label: "Max Open Connections", Type: schema.FieldTypeNumber, DefaultValue: 25, Description: "Maximum number of open database connections"},
				{Key: "maxIdleConns", Label: "Max Idle Connections", Type: schema.FieldTypeNumber, DefaultValue: 5, Description: "Maximum number of idle connections in the pool"},
			},
			DefaultConfig: map[string]any{"maxOpenConns": 25, "maxIdleConns": 5},
		},
		{
			Type:        "persistence.store",
			Label:       "Persistence Store",
			Category:    "database",
			Description: "Persistence layer that uses a database service for storage",
			Inputs:      []schema.ServiceIODef{{Name: "data", Type: "any", Description: "Data to persist or retrieve"}},
			Outputs:     []schema.ServiceIODef{{Name: "persistence", Type: "PersistenceStore", Description: "Persistence service for CRUD operations"}},
			ConfigFields: []schema.ConfigFieldDef{
				{Key: "database", Label: "Database Service", Type: schema.FieldTypeString, DefaultValue: "database", Description: "Name of the database module to use for storage", Placeholder: "database", InheritFrom: "dependency.name"},
			},
			DefaultConfig: map[string]any{"database": "database"},
		},
		{
			Type:        "storage.artifact",
			Label:       "Artifact Store",
			Category:    "storage",
			Description: "Named artifact storage with metadata support (filesystem or S3)",
			Outputs:     []schema.ServiceIODef{{Name: "store", Type: "ArtifactStore", Description: "Artifact storage service"}},
			ConfigFields: []schema.ConfigFieldDef{
				{Key: "backend", Label: "Backend", Type: schema.FieldTypeSelect, Options: []string{"filesystem", "s3"}, DefaultValue: "filesystem", Description: "Storage backend to use"},
				{Key: "basePath", Label: "Base Path", Type: schema.FieldTypeString, DefaultValue: "./data/artifacts", Description: "Root directory for filesystem backend", Placeholder: "./data/artifacts"},
				{Key: "maxSize", Label: "Max Size (bytes)", Type: schema.FieldTypeNumber, Description: "Maximum artifact size in bytes (0 = unlimited)"},
				{Key: "bucket", Label: "S3 Bucket", Type: schema.FieldTypeString, Description: "S3 bucket name (s3 backend only)", Placeholder: "my-artifacts"},
				{Key: "region", Label: "S3 Region", Type: schema.FieldTypeString, Description: "AWS region (s3 backend only)", Placeholder: "us-east-1"},
				{Key: "endpoint", Label: "S3 Endpoint", Type: schema.FieldTypeString, Description: "Custom S3 endpoint; use 'local' for filesystem fallback", Placeholder: "local"},
			},
			DefaultConfig: map[string]any{"backend": "filesystem", "basePath": "./data/artifacts"},
		},
		{
			Type:        "cache.redis",
			Label:       "Redis Cache",
			Category:    "cache",
			Description: "Redis-backed key/value cache for pipeline step data",
			Outputs:     []schema.ServiceIODef{{Name: "cache", Type: "CacheModule", Description: "Redis cache service"}},
			ConfigFields: []schema.ConfigFieldDef{
				{Key: "address", Label: "Address", Type: schema.FieldTypeString, DefaultValue: "localhost:6379", Description: "Redis server address (host:port)", Placeholder: "localhost:6379"},
				{Key: "password", Label: "Password", Type: schema.FieldTypeString, Description: "Redis password (optional)", Sensitive: true},
				{Key: "db", Label: "Database", Type: schema.FieldTypeNumber, DefaultValue: 0, Description: "Redis database number"},
				{Key: "prefix", Label: "Key Prefix", Type: schema.FieldTypeString, DefaultValue: "wf:", Description: "Prefix applied to all cache keys"},
				{Key: "defaultTTL", Label: "Default TTL", Type: schema.FieldTypeString, DefaultValue: "1h", Description: "Default time-to-live for cached values (e.g. 30m, 1h, 24h)"},
			},
			DefaultConfig: map[string]any{
				"address":    "localhost:6379",
				"db":         0,
				"prefix":     "wf:",
				"defaultTTL": "1h",
			},
		},
	}
}
