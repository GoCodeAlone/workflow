package manifest

import (
	"net"
	"strconv"
	"strings"

	"github.com/GoCodeAlone/workflow/config"
)

// analyzeModules walks all modules and extracts requirements by type.
func analyzeModules(cfg *config.WorkflowConfig, m *WorkflowManifest) {
	for _, mod := range cfg.Modules {
		switch {
		case isDatabase(mod.Type):
			analyzeDatabase(mod, m)
		case isHTTPServer(mod.Type):
			analyzeHTTPServer(mod, m)
		case isMessaging(mod.Type):
			analyzeMessaging(mod, m)
		case isStorage(mod.Type):
			analyzeStorage(mod, m)
		}

		// Every module is a service
		m.Services = append(m.Services, ServiceRequirement{
			Name: mod.Name,
			Type: mod.Type,
		})
	}
}

// analyzePipelines extracts requirements from pipeline step definitions.
func analyzePipelines(cfg *config.WorkflowConfig, m *WorkflowManifest) {
	// Pipelines are stored as map[string]any; each value has "steps" as []any.
	for _, v := range cfg.Pipelines {
		pipelineMap, ok := v.(map[string]any)
		if !ok {
			continue
		}
		stepsRaw, ok := pipelineMap["steps"]
		if !ok {
			continue
		}
		steps, ok := stepsRaw.([]any)
		if !ok {
			continue
		}
		for _, stepRaw := range steps {
			step, ok := stepRaw.(map[string]any)
			if !ok {
				continue
			}
			analyzeStep(step, m)
		}
	}
}

// analyzeStep extracts requirements from a single pipeline step.
func analyzeStep(step map[string]any, m *WorkflowManifest) {
	stepType, _ := step["type"].(string)
	stepName, _ := step["name"].(string)
	stepConfig, _ := step["config"].(map[string]any)

	if stepType == "step.http_call" && stepConfig != nil {
		url, _ := stepConfig["url"].(string)
		method, _ := stepConfig["method"].(string)
		if method == "" {
			method = "GET"
		}
		if url != "" {
			m.ExternalAPIs = append(m.ExternalAPIs, ExternalAPIRequirement{
				StepName: stepName,
				URL:      url,
				Method:   method,
			})
		}
	}

	if (stepType == "step.db_query" || stepType == "step.db_exec") && stepConfig != nil {
		dbName, _ := stepConfig["database"].(string)
		if dbName != "" {
			// Check if we already know about this database
			found := false
			for _, db := range m.Databases {
				if db.ModuleName == dbName {
					found = true
					break
				}
			}
			if !found {
				m.Databases = append(m.Databases, DatabaseRequirement{
					ModuleName: dbName,
					Driver:     "unknown (referenced by pipeline step)",
				})
			}
		}
	}
}

// analyzeWorkflows extracts topics from messaging workflow subscriptions.
func analyzeWorkflows(cfg *config.WorkflowConfig, m *WorkflowManifest) {
	for _, v := range cfg.Workflows {
		wfMap, ok := v.(map[string]any)
		if !ok {
			continue
		}
		analyzeWorkflowMessaging(wfMap, m)
	}
}

// analyzeWorkflowMessaging extracts topic names from messaging subscriptions/producers.
func analyzeWorkflowMessaging(wfMap map[string]any, m *WorkflowManifest) {
	// Look for subscriptions
	subsRaw, ok := wfMap["subscriptions"]
	if ok {
		subs, ok := subsRaw.([]any)
		if ok {
			for _, subRaw := range subs {
				sub, ok := subRaw.(map[string]any)
				if !ok {
					continue
				}
				topic, _ := sub["topic"].(string)
				if topic != "" {
					addTopic(m, topic)
				}
			}
		}
	}

	// Look for producers
	prodsRaw, ok := wfMap["producers"]
	if ok {
		prods, ok := prodsRaw.([]any)
		if ok {
			for _, prodRaw := range prods {
				prod, ok := prodRaw.(map[string]any)
				if !ok {
					continue
				}
				fwdRaw, ok := prod["forwardTo"]
				if !ok {
					continue
				}
				fwds, ok := fwdRaw.([]any)
				if !ok {
					continue
				}
				for _, fwd := range fwds {
					topic, ok := fwd.(string)
					if ok && topic != "" {
						addTopic(m, topic)
					}
				}
			}
		}
	}
}

// addTopic ensures a topic is tracked in the EventBus requirement.
func addTopic(m *WorkflowManifest, topic string) {
	if m.EventBus == nil {
		m.EventBus = &EventBusRequirement{Technology: "in-memory"}
	}
	for _, t := range m.EventBus.Topics {
		if t == topic {
			return
		}
	}
	m.EventBus.Topics = append(m.EventBus.Topics, topic)
}

// --- Type matchers ---

func isDatabase(moduleType string) bool {
	return moduleType == "database.workflow" ||
		moduleType == "storage.sqlite" ||
		moduleType == "persistence.store"
}

func isHTTPServer(moduleType string) bool {
	return moduleType == "http.server"
}

func isMessaging(moduleType string) bool {
	return moduleType == "messaging.broker" ||
		moduleType == "messaging.nats" ||
		moduleType == "messaging.kafka" ||
		moduleType == "messaging.broker.eventbus"
}

func isStorage(moduleType string) bool {
	return moduleType == "storage.s3" ||
		moduleType == "storage.local" ||
		moduleType == "storage.gcs"
}

// --- Per-type analyzers ---

func analyzeDatabase(mod config.ModuleConfig, m *WorkflowManifest) {
	req := DatabaseRequirement{
		ModuleName: mod.Name,
	}
	if mod.Config != nil {
		req.Driver, _ = mod.Config["driver"].(string)
		req.DSN, _ = mod.Config["dsn"].(string)
		if v, ok := toInt(mod.Config["maxOpenConns"]); ok {
			req.MaxOpenConns = v
		}
		if v, ok := toInt(mod.Config["maxIdleConns"]); ok {
			req.MaxIdleConns = v
		}
		if v, ok := toInt(mod.Config["maxConnections"]); ok {
			req.MaxOpenConns = v
		}
		// For sqlite, extract the path
		if mod.Type == "storage.sqlite" {
			req.Driver = "sqlite3"
			if dbPath, ok := mod.Config["dbPath"].(string); ok {
				req.DSN = dbPath
			}
		}
	}

	// Default driver labels for known types
	if req.Driver == "" && mod.Type == "persistence.store" {
		req.Driver = "delegates to database module"
	}

	// Rough capacity estimate
	switch {
	case req.MaxOpenConns > 50:
		req.EstCapacityMB = 1024
	case req.MaxOpenConns > 10:
		req.EstCapacityMB = 512
	default:
		req.EstCapacityMB = 256
	}

	m.Databases = append(m.Databases, req)
}

func analyzeHTTPServer(mod config.ModuleConfig, m *WorkflowManifest) {
	address := ":8080" // default
	if mod.Config != nil {
		if addr, ok := mod.Config["address"].(string); ok && addr != "" {
			address = addr
		}
	}

	port := parsePort(address)
	if port > 0 {
		m.Ports = append(m.Ports, PortRequirement{
			ModuleName: mod.Name,
			Port:       port,
			Protocol:   "http",
		})
	}
}

func analyzeMessaging(mod config.ModuleConfig, m *WorkflowManifest) {
	var tech string
	switch mod.Type {
	case "messaging.nats":
		tech = "nats"
	case "messaging.kafka":
		tech = "kafka"
	case "messaging.broker.eventbus":
		tech = "eventbus-bridge"
	default:
		tech = "in-memory"
	}

	if m.EventBus == nil {
		m.EventBus = &EventBusRequirement{Technology: tech}
	} else if tech != "in-memory" {
		// Upgrade technology if we find a real broker
		m.EventBus.Technology = tech
	}

	if mod.Config != nil {
		// Extract Kafka brokers as queue info
		if mod.Type == "messaging.kafka" {
			if brokersRaw, ok := mod.Config["brokers"]; ok {
				if brokers, ok := brokersRaw.([]any); ok {
					for _, b := range brokers {
						if bs, ok := b.(string); ok {
							m.EventBus.Queues = appendUnique(m.EventBus.Queues, bs)
						}
					}
				}
			}
		}
		// Extract NATS URL
		if mod.Type == "messaging.nats" {
			if url, ok := mod.Config["url"].(string); ok {
				m.EventBus.Queues = appendUnique(m.EventBus.Queues, url)
			}
		}
	}
}

func analyzeStorage(mod config.ModuleConfig, m *WorkflowManifest) {
	req := StorageRequirement{
		ModuleName: mod.Name,
	}

	switch mod.Type {
	case "storage.s3":
		req.Type = "s3"
		if mod.Config != nil {
			req.Bucket, _ = mod.Config["bucket"].(string)
			req.Region, _ = mod.Config["region"].(string)
		}
	case "storage.local":
		req.Type = "local"
		if mod.Config != nil {
			req.RootDir, _ = mod.Config["rootDir"].(string)
		}
	case "storage.gcs":
		req.Type = "gcs"
		if mod.Config != nil {
			req.Bucket, _ = mod.Config["bucket"].(string)
		}
	}

	m.Storage = append(m.Storage, req)
}

// --- Helpers ---

func parsePort(address string) int {
	// Handle ":8080" or "0.0.0.0:8080" or "localhost:8080"
	_, portStr, err := net.SplitHostPort(address)
	if err != nil {
		// Try treating the whole thing as a port
		portStr = strings.TrimPrefix(address, ":")
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return 0
	}
	return port
}

func toInt(v any) (int, bool) {
	switch val := v.(type) {
	case int:
		return val, true
	case int64:
		return int(val), true
	case float64:
		return int(val), true
	case string:
		n, err := strconv.Atoi(val)
		return n, err == nil
	default:
		return 0, false
	}
}

func appendUnique(slice []string, val string) []string {
	for _, s := range slice {
		if s == val {
			return slice
		}
	}
	return append(slice, val)
}
