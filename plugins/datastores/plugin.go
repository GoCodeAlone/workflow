// Package datastores provides an EnginePlugin that registers NoSQL data store
// module types (nosql.memory, nosql.dynamodb, nosql.mongodb, nosql.redis) and
// their corresponding pipeline step types (step.nosql_get, step.nosql_put,
// step.nosql_delete, step.nosql_query).
package datastores

import (
	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/plugin"
)

// Plugin is the datastores EnginePlugin.
type Plugin struct {
	plugin.BaseEnginePlugin
}

// New creates a new datastores plugin with a valid manifest.
func New() *Plugin {
	return &Plugin{
		BaseEnginePlugin: plugin.BaseEnginePlugin{
			BaseNativePlugin: plugin.BaseNativePlugin{
				PluginName:        "datastores",
				PluginVersion:     "1.0.0",
				PluginDescription: "NoSQL data store modules: memory, DynamoDB, MongoDB, Redis",
			},
			Manifest: plugin.PluginManifest{
				Name:        "datastores",
				Version:     "1.0.0",
				Author:      "GoCodeAlone",
				Description: "NoSQL data store modules and pipeline steps",
				Tier:        plugin.TierCore,
				ModuleTypes: []string{
					"nosql.memory",
					"nosql.dynamodb",
					"nosql.mongodb",
					"nosql.redis",
				},
				StepTypes: []string{
					"step.nosql_get",
					"step.nosql_put",
					"step.nosql_delete",
					"step.nosql_query",
				},
				Capabilities: []plugin.CapabilityDecl{
					{Name: "nosql-store", Role: "provider", Priority: 50},
				},
			},
		},
	}
}

// Capabilities returns the capability contracts defined by this plugin.
func (p *Plugin) Capabilities() []capability.Contract {
	return []capability.Contract{
		{
			Name:        "nosql-store",
			Description: "NoSQL key-value data store operations: get, put, delete, query",
		},
	}
}

// ModuleFactories returns factories for all NoSQL module types.
func (p *Plugin) ModuleFactories() map[string]plugin.ModuleFactory {
	return map[string]plugin.ModuleFactory{
		"nosql.memory": func(name string, cfg map[string]any) modular.Module {
			collection, _ := cfg["collection"].(string)
			return module.NewMemoryNoSQL(name, module.MemoryNoSQLConfig{Collection: collection})
		},
		"nosql.dynamodb": func(name string, cfg map[string]any) modular.Module {
			c := module.DynamoDBNoSQLConfig{}
			c.TableName, _ = cfg["tableName"].(string)
			c.Region, _ = cfg["region"].(string)
			c.Endpoint, _ = cfg["endpoint"].(string)
			c.Credentials, _ = cfg["credentials"].(string)
			return module.NewDynamoDBNoSQL(name, c)
		},
		"nosql.mongodb": func(name string, cfg map[string]any) modular.Module {
			c := module.MongoDBNoSQLConfig{}
			c.URI, _ = cfg["uri"].(string)
			c.Database, _ = cfg["database"].(string)
			c.Collection, _ = cfg["collection"].(string)
			return module.NewMongoDBNoSQL(name, c)
		},
		"nosql.redis": func(name string, cfg map[string]any) modular.Module {
			c := module.RedisNoSQLConfig{}
			c.Addr, _ = cfg["addr"].(string)
			c.Password, _ = cfg["password"].(string)
			if db, ok := cfg["db"].(float64); ok {
				c.DB = int(db)
			}
			return module.NewRedisNoSQL(name, c)
		},
	}
}

// StepFactories returns factories for all NoSQL pipeline step types.
func (p *Plugin) StepFactories() map[string]plugin.StepFactory {
	return map[string]plugin.StepFactory{
		"step.nosql_get":    wrapStepFactory(module.NewNoSQLGetStepFactory()),
		"step.nosql_put":    wrapStepFactory(module.NewNoSQLPutStepFactory()),
		"step.nosql_delete": wrapStepFactory(module.NewNoSQLDeleteStepFactory()),
		"step.nosql_query":  wrapStepFactory(module.NewNoSQLQueryStepFactory()),
	}
}

// wrapStepFactory converts a module.StepFactory to a plugin.StepFactory.
func wrapStepFactory(f module.StepFactory) plugin.StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (any, error) {
		return f(name, cfg, app)
	}
}
