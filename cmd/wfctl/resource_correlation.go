package main

import (
	"fmt"
	"strings"

	"github.com/GoCodeAlone/workflow/config"
)

// ResourceKind categorizes a module's infrastructure resource type.
type ResourceKind string

const (
	ResourceKindDatabase  ResourceKind = "database"
	ResourceKindBroker    ResourceKind = "broker"
	ResourceKindCache     ResourceKind = "cache"
	ResourceKindVolume    ResourceKind = "volume"
	ResourceKindStateless ResourceKind = "stateless"
)

// ClassifyModule maps a workflow module type string to a ResourceKind.
func ClassifyModule(moduleType string) ResourceKind {
	switch moduleType {
	case "storage.sqlite", "database.workflow", "persistence.store":
		return ResourceKindDatabase
	case "messaging.broker", "messaging.nats", "messaging.kafka", "messaging.broker.eventbus":
		return ResourceKindBroker
	case "cache.redis":
		return ResourceKindCache
	case "static.fileserver":
		return ResourceKindVolume
	default:
		return ResourceKindStateless
	}
}

// IsStateful returns true if the module type manages persistent state that
// must survive redeployments.
func IsStateful(moduleType string) bool {
	switch ClassifyModule(moduleType) {
	case ResourceKindDatabase, ResourceKindBroker, ResourceKindVolume:
		return true
	case ResourceKindCache:
		// Redis is semi-stateful: ephemeral by default but can be persistent.
		return false
	default:
		return false
	}
}

// GenerateResourceID produces a deterministic resource identifier that links
// a workflow module to the infrastructure resource it owns.
//
// Format: <kind>/<namespace>-<moduleName>
// e.g.  database/prod-orders-db
func GenerateResourceID(moduleName, moduleType, namespace string) string {
	kind := ClassifyModule(moduleType)
	if namespace == "" {
		return fmt.Sprintf("%s/%s", kind, moduleName)
	}
	return fmt.Sprintf("%s/%s-%s", kind, namespace, moduleName)
}

// BreakingChange describes a single breaking change detected when comparing
// two module configurations.
type BreakingChange struct {
	// Field is the config key that changed (empty if the whole module changed).
	Field string
	// OldValue is the previous value as a string representation.
	OldValue string
	// NewValue is the new value as a string representation.
	NewValue string
	// Message is a human-readable description.
	Message string
}

// DetectBreakingChanges compares old and new ModuleConfig instances and
// returns a list of changes that could cause data loss or service disruption.
// Only meaningful for stateful module types; callers should check IsStateful
// before acting on an empty result.
func DetectBreakingChanges(oldMod, newMod *config.ModuleConfig) []BreakingChange {
	if oldMod == nil || newMod == nil {
		return nil
	}

	var changes []BreakingChange

	// Type change is always breaking.
	if oldMod.Type != newMod.Type {
		changes = append(changes, BreakingChange{
			Field:    "type",
			OldValue: oldMod.Type,
			NewValue: newMod.Type,
			Message:  fmt.Sprintf("module type changed from %q to %q", oldMod.Type, newMod.Type),
		})
		// Don't bother comparing config if the type changed entirely.
		return changes
	}

	// For stateful modules, flag any config key changes that could affect data
	// location or connectivity.
	if !IsStateful(oldMod.Type) {
		return nil
	}

	breakingKeys := statefulBreakingKeys(oldMod.Type)
	for _, key := range breakingKeys {
		oldVal := configValueStr(oldMod.Config, key)
		newVal := configValueStr(newMod.Config, key)
		if oldVal != newVal {
			changes = append(changes, BreakingChange{
				Field:    key,
				OldValue: oldVal,
				NewValue: newVal,
				Message: fmt.Sprintf("config key %q changed: %s → %s",
					key, describeValue(oldVal), describeValue(newVal)),
			})
		}
	}

	return changes
}

// statefulBreakingKeys returns the config keys that are breaking for a given
// stateful module type when changed.
func statefulBreakingKeys(moduleType string) []string {
	switch moduleType {
	case "storage.sqlite":
		return []string{"dbPath", "path"}
	case "database.workflow":
		return []string{"dsn", "driver", "host", "port", "database", "dbname"}
	case "persistence.store":
		return []string{"database"}
	case "messaging.broker", "messaging.broker.eventbus":
		return []string{"persistence", "dataDir"}
	case "messaging.nats":
		return []string{"url", "clusterID"}
	case "messaging.kafka":
		return []string{"brokers", "topic"}
	case "static.fileserver":
		return []string{"rootDir", "dir"}
	default:
		return nil
	}
}

// configValueStr returns the string representation of a config map value for
// the given key, or an empty string if missing.
func configValueStr(cfg map[string]any, key string) string {
	if cfg == nil {
		return ""
	}
	v, ok := cfg[key]
	if !ok {
		return ""
	}
	return fmt.Sprintf("%v", v)
}

// describeValue renders a config value for display, showing "(unset)" for
// empty strings.
func describeValue(v string) string {
	if v == "" {
		return "(unset)"
	}
	return v
}

// moduleTypeLabel returns a short human-readable label for a module type,
// stripping well-known prefixes to keep output concise.
func moduleTypeLabel(moduleType string) string {
	return strings.TrimSpace(moduleType)
}
