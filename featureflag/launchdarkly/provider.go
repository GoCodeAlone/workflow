//go:build launchdarkly

// Package launchdarkly provides a feature flag Provider backed by LaunchDarkly.
// This file is only compiled when the "launchdarkly" build tag is set, so the
// LaunchDarkly SDK dependency is opt-in.
package launchdarkly

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/GoCodeAlone/workflow/featureflag"
	ld "github.com/launchdarkly/go-server-sdk/v7"
	"github.com/launchdarkly/go-server-sdk/v7/interfaces"
	"github.com/launchdarkly/go-server-sdk/v7/ldcomponents"
	ldcontext "github.com/launchdarkly/go-sdk-common/v3/ldcontext"
	"github.com/launchdarkly/go-sdk-common/v3/ldvalue"
)

// Config holds configuration for the LaunchDarkly provider.
type Config struct {
	SDKKey       string        `yaml:"sdk_key"`
	Stream       bool          `yaml:"stream"`
	PollInterval time.Duration `yaml:"poll_interval"`
	RelayProxy   string        `yaml:"relay_proxy"`
}

// Provider implements featureflag.Provider using the LaunchDarkly Go Server SDK.
type Provider struct {
	client *ld.LDClient
	config Config

	mu          sync.RWMutex
	subscribers []func(featureflag.FlagChangeEvent)
}

// NewProvider creates a new LaunchDarkly provider. It blocks until the SDK
// initialises or 10 seconds elapse.
func NewProvider(cfg Config) (*Provider, error) {
	if cfg.SDKKey == "" {
		return nil, fmt.Errorf("launchdarkly: sdk_key is required")
	}

	ldCfg := ld.Config{}

	if !cfg.Stream {
		pollInterval := cfg.PollInterval
		if pollInterval == 0 {
			pollInterval = 30 * time.Second
		}
		ldCfg.DataSource = ldcomponents.PollingDataSource().PollInterval(pollInterval)
	}

	if cfg.RelayProxy != "" {
		ldCfg.ServiceEndpoints.Streaming = cfg.RelayProxy
		ldCfg.ServiceEndpoints.Polling = cfg.RelayProxy
		ldCfg.ServiceEndpoints.Events = cfg.RelayProxy
	}

	client, err := ld.MakeCustomClient(cfg.SDKKey, ldCfg, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("launchdarkly: init failed: %w", err)
	}

	p := &Provider{
		client: client,
		config: cfg,
	}

	return p, nil
}

// Name implements featureflag.Provider.
func (p *Provider) Name() string { return "launchdarkly" }

// Evaluate implements featureflag.Provider.
func (p *Provider) Evaluate(ctx context.Context, key string, evalCtx featureflag.EvaluationContext) (featureflag.FlagValue, error) {
	ldCtx := buildLDContext(evalCtx)

	// Try JSON variation first (most general)
	detail, err := p.client.JSONVariationDetail(key, ldCtx, ldvalue.Null())
	if err != nil {
		return featureflag.FlagValue{}, fmt.Errorf("launchdarkly: evaluate %q: %w", key, err)
	}

	val := ldValueToGo(detail.Value)
	flagType := inferFlagType(detail.Value)

	return featureflag.FlagValue{
		Key:    key,
		Value:  val,
		Type:   flagType,
		Source: "launchdarkly",
		Reason: detail.Reason.String(),
	}, nil
}

// AllFlags implements featureflag.Provider.
func (p *Provider) AllFlags(ctx context.Context, evalCtx featureflag.EvaluationContext) ([]featureflag.FlagValue, error) {
	ldCtx := buildLDContext(evalCtx)
	state := p.client.AllFlagsState(ldCtx)
	if !state.IsValid() {
		return nil, fmt.Errorf("launchdarkly: AllFlagsState returned invalid state")
	}

	valuesMap := state.ToValuesMap()
	result := make([]featureflag.FlagValue, 0, len(valuesMap))
	for key, val := range valuesMap {
		result = append(result, featureflag.FlagValue{
			Key:    key,
			Value:  ldValueToGo(val),
			Type:   inferFlagType(val),
			Source: "launchdarkly",
		})
	}
	return result, nil
}

// Subscribe implements featureflag.Provider.
func (p *Provider) Subscribe(fn func(featureflag.FlagChangeEvent)) (cancel func()) {
	p.mu.Lock()
	idx := len(p.subscribers)
	p.subscribers = append(p.subscribers, fn)
	p.mu.Unlock()

	// Use LD's flag change listener
	tracker := p.client.GetFlagTracker()
	ch := tracker.AddFlagChangeListener()

	go func() {
		for event := range ch {
			p.mu.RLock()
			if idx < len(p.subscribers) && p.subscribers[idx] != nil {
				p.subscribers[idx](featureflag.FlagChangeEvent{
					Key:    event.Key,
					Source: "launchdarkly",
				})
			}
			p.mu.RUnlock()
		}
	}()

	return func() {
		tracker.RemoveFlagChangeListener(ch)
		p.mu.Lock()
		defer p.mu.Unlock()
		if idx < len(p.subscribers) {
			p.subscribers[idx] = nil
		}
	}
}

// Close shuts down the LD client gracefully.
func (p *Provider) Close() error {
	return p.client.Close()
}

// buildLDContext converts our EvaluationContext to the LD context type.
func buildLDContext(ec featureflag.EvaluationContext) ldcontext.Context {
	builder := ldcontext.NewBuilder(ec.UserKey)
	for k, v := range ec.Attributes {
		builder.SetString(k, v)
	}
	return builder.Build()
}

// ldValueToGo converts an ldvalue.Value to a native Go type.
func ldValueToGo(v ldvalue.Value) any {
	switch v.Type() {
	case ldvalue.BoolType:
		return v.BoolValue()
	case ldvalue.NumberType:
		if v.IsInt() {
			return v.IntValue()
		}
		return v.Float64Value()
	case ldvalue.StringType:
		return v.StringValue()
	default:
		return v.JSONString()
	}
}

// inferFlagType maps an ldvalue type to our FlagType.
func inferFlagType(v ldvalue.Value) featureflag.FlagType {
	switch v.Type() {
	case ldvalue.BoolType:
		return featureflag.FlagTypeBoolean
	case ldvalue.NumberType:
		if v.IsInt() {
			return featureflag.FlagTypeInteger
		}
		return featureflag.FlagTypeFloat
	case ldvalue.StringType:
		return featureflag.FlagTypeString
	default:
		return featureflag.FlagTypeJSON
	}
}

// Ensure Provider implements the interface at compile time.
var _ featureflag.Provider = (*Provider)(nil)
