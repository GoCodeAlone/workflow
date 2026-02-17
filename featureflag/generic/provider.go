package generic

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"

	"github.com/GoCodeAlone/workflow/featureflag"
)

// Provider is the built-in generic feature-flag provider. Flag definitions,
// targeting rules, and overrides are stored in a SQLite database.
//
// Evaluation order:
//  1. Disabled check -> return default value
//  2. User overrides (scope="user", scope_key=UserKey)
//  3. Group overrides (scope="group", matched via "groups" attribute)
//  4. Targeting rules (evaluated top-to-bottom by priority)
//  5. Percentage rollout (deterministic hash of userKey+flagKey)
//  6. Default value
type Provider struct {
	store  *Store
	logger *slog.Logger

	mu          sync.RWMutex
	subscribers []func(featureflag.FlagChangeEvent)
}

// NewProvider creates a generic provider backed by the given store.
func NewProvider(store *Store, logger *slog.Logger) *Provider {
	if logger == nil {
		logger = slog.Default()
	}
	return &Provider{
		store:  store,
		logger: logger,
	}
}

// Name implements featureflag.Provider.
func (p *Provider) Name() string { return "generic" }

// Evaluate implements featureflag.Provider. It resolves the flag through the
// six-step evaluation order documented on the Provider type.
func (p *Provider) Evaluate(ctx context.Context, key string, evalCtx featureflag.EvaluationContext) (featureflag.FlagValue, error) {
	flag, err := p.store.GetFlag(ctx, key)
	if err != nil {
		return featureflag.FlagValue{}, fmt.Errorf("flag %q not found: %w", key, err)
	}

	flagType := featureflag.FlagType(flag.Type)

	mkVal := func(raw string, reason string) featureflag.FlagValue {
		return featureflag.FlagValue{
			Key:    key,
			Value:  parseTypedValue(raw, flagType),
			Type:   flagType,
			Source: "generic",
			Reason: reason,
		}
	}

	// Step 1: disabled -> default
	if !flag.Enabled {
		return mkVal(flag.DefaultVal, "disabled"), nil
	}

	// Step 2: user override
	overrides, err := p.store.GetOverrides(ctx, key)
	if err != nil {
		return featureflag.FlagValue{}, fmt.Errorf("get overrides: %w", err)
	}
	for _, o := range overrides {
		if o.Scope == "user" && o.ScopeKey == evalCtx.UserKey {
			return mkVal(o.Value, "user_override"), nil
		}
	}

	// Step 3: group override
	userGroups := splitGroups(evalCtx.Attributes["groups"])
	for _, o := range overrides {
		if o.Scope == "group" {
			for _, g := range userGroups {
				if g == o.ScopeKey {
					return mkVal(o.Value, "group_override"), nil
				}
			}
		}
	}

	// Step 4: targeting rules (by priority ascending)
	rules, err := p.store.GetRules(ctx, key)
	if err != nil {
		return featureflag.FlagValue{}, fmt.Errorf("get rules: %w", err)
	}
	for _, rule := range rules {
		attrVal := evalCtx.Attributes[rule.Attribute]
		if evaluateCondition(attrVal, rule.Operator, rule.Value) {
			return mkVal(rule.ServeValue, "targeting_rule"), nil
		}
	}

	// Step 5: percentage rollout
	if flag.Percentage > 0 {
		bucket := hashBucket(evalCtx.UserKey, key)
		if bucket < flag.Percentage {
			return mkVal(flag.DefaultVal, "percentage_rollout_in"), nil
		}
		// User outside percentage — fall through to default with distinct reason
		return mkVal(flag.DefaultVal, "percentage_rollout_out"), nil
	}

	// Step 6: default
	return mkVal(flag.DefaultVal, "default"), nil
}

// AllFlags implements featureflag.Provider.
func (p *Provider) AllFlags(ctx context.Context, evalCtx featureflag.EvaluationContext) ([]featureflag.FlagValue, error) {
	flags, err := p.store.ListFlags(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]featureflag.FlagValue, 0, len(flags))
	for i := range flags {
		val, evalErr := p.Evaluate(ctx, flags[i].Key, evalCtx)
		if evalErr != nil {
			p.logger.Warn("failed to evaluate flag in AllFlags", "key", flags[i].Key, "error", evalErr)
			continue
		}
		result = append(result, val)
	}
	return result, nil
}

// Subscribe implements featureflag.Provider.
func (p *Provider) Subscribe(fn func(featureflag.FlagChangeEvent)) (cancel func()) {
	p.mu.Lock()
	idx := len(p.subscribers)
	p.subscribers = append(p.subscribers, fn)
	p.mu.Unlock()

	return func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		if idx < len(p.subscribers) {
			// nil out to avoid holding reference; slice is append-only in practice
			p.subscribers[idx] = nil
		}
	}
}

// NotifyChange should be called after mutating a flag (e.g., from the admin API)
// to inform subscribers of the change.
func (p *Provider) NotifyChange(key string, value any, flagType featureflag.FlagType) {
	evt := featureflag.FlagChangeEvent{
		Key:    key,
		Value:  value,
		Type:   flagType,
		Source: "generic",
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, fn := range p.subscribers {
		if fn != nil {
			fn(evt)
		}
	}
}

// ---------- Evaluation helpers ----------

// evaluateCondition checks whether the attribute value satisfies the condition.
func evaluateCondition(attrVal, operator, condVal string) bool {
	switch operator {
	case "eq":
		return attrVal == condVal
	case "neq":
		return attrVal != condVal
	case "in":
		for _, v := range strings.Split(condVal, ",") {
			if strings.TrimSpace(v) == attrVal {
				return true
			}
		}
		return false
	case "contains":
		return strings.Contains(attrVal, condVal)
	case "startsWith":
		return strings.HasPrefix(attrVal, condVal)
	case "gt":
		av, err1 := strconv.ParseFloat(attrVal, 64)
		cv, err2 := strconv.ParseFloat(condVal, 64)
		return err1 == nil && err2 == nil && av > cv
	case "lt":
		av, err1 := strconv.ParseFloat(attrVal, 64)
		cv, err2 := strconv.ParseFloat(condVal, 64)
		return err1 == nil && err2 == nil && av < cv
	default:
		return false
	}
}

// hashBucket produces a deterministic number in [0, 100) from the user key and flag key.
// This ensures the same user always gets the same bucket for a given flag.
func hashBucket(userKey, flagKey string) float64 {
	h := sha256.Sum256([]byte(userKey + flagKey))
	n := binary.BigEndian.Uint32(h[:4])
	return float64(n % 100) // 0..99
}

// parseTypedValue converts a string representation into a Go value based on FlagType.
func parseTypedValue(raw string, ft featureflag.FlagType) any {
	switch ft {
	case featureflag.FlagTypeBoolean:
		return raw == "true" || raw == "1"
	case featureflag.FlagTypeInteger:
		v, _ := strconv.ParseInt(raw, 10, 64)
		return v
	case featureflag.FlagTypeFloat:
		v, _ := strconv.ParseFloat(raw, 64)
		return v
	case featureflag.FlagTypeJSON:
		var v any
		if err := json.Unmarshal([]byte(raw), &v); err != nil {
			return raw
		}
		return v
	default: // string
		return raw
	}
}

// splitGroups splits a comma-separated groups string into trimmed group names.
func splitGroups(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
