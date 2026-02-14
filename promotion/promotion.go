package promotion

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// EnvironmentName identifies an environment.
type EnvironmentName string

const (
	EnvDev     EnvironmentName = "dev"
	EnvStaging EnvironmentName = "staging"
	EnvProd    EnvironmentName = "prod"
)

// ApprovalStatus represents the status of a promotion approval.
type ApprovalStatus string

const (
	ApprovalPending  ApprovalStatus = "pending"
	ApprovalApproved ApprovalStatus = "approved"
	ApprovalRejected ApprovalStatus = "rejected"
)

// PromotionStatus represents the overall status of a promotion.
type PromotionStatus string

const (
	PromotionPending  PromotionStatus = "pending_approval"
	PromotionApproved PromotionStatus = "approved"
	PromotionRejected PromotionStatus = "rejected"
	PromotionDeployed PromotionStatus = "deployed"
	PromotionFailed   PromotionStatus = "failed"
)

// Environment represents a deployment target.
type Environment struct {
	Name             EnvironmentName `json:"name"`
	Description      string          `json:"description"`
	RequiresApproval bool            `json:"requiresApproval"`
	ValidationRules  []string        `json:"validationRules,omitempty"`
	Order            int             `json:"order"` // lower = earlier in pipeline
}

// PromotionRecord tracks a single promotion operation.
type PromotionRecord struct {
	ID             string          `json:"id"`
	WorkflowName   string          `json:"workflowName"`
	ConfigYAML     string          `json:"configYaml"`
	FromEnv        EnvironmentName `json:"fromEnv"`
	ToEnv          EnvironmentName `json:"toEnv"`
	Status         PromotionStatus `json:"status"`
	RequestedBy    string          `json:"requestedBy"`
	ApprovalStatus ApprovalStatus  `json:"approvalStatus,omitempty"`
	ApprovedBy     string          `json:"approvedBy,omitempty"`
	ApprovedAt     *time.Time      `json:"approvedAt,omitempty"`
	Error          string          `json:"error,omitempty"`
	CreatedAt      time.Time       `json:"createdAt"`
	CompletedAt    *time.Time      `json:"completedAt,omitempty"`
}

// ValidateFunc validates a config for a target environment.
type ValidateFunc func(ctx context.Context, env EnvironmentName, configYAML string) error

// DeployFunc deploys a config to a target environment.
type DeployFunc func(ctx context.Context, env EnvironmentName, workflowName, configYAML string) error

// Pipeline manages promotions across environments.
type Pipeline struct {
	mu           sync.RWMutex
	environments map[EnvironmentName]*Environment
	promotions   map[string]*PromotionRecord           // id -> record
	configs      map[string]map[EnvironmentName]string // workflowName -> env -> configYAML
	validateFn   ValidateFunc
	deployFn     DeployFunc
	idCounter    int
}

// NewPipeline creates a new promotion pipeline with default environments.
func NewPipeline(validateFn ValidateFunc, deployFn DeployFunc) *Pipeline {
	p := &Pipeline{
		environments: make(map[EnvironmentName]*Environment),
		promotions:   make(map[string]*PromotionRecord),
		configs:      make(map[string]map[EnvironmentName]string),
		validateFn:   validateFn,
		deployFn:     deployFn,
	}

	// Register default environments
	p.environments[EnvDev] = &Environment{
		Name:             EnvDev,
		Description:      "Development environment",
		RequiresApproval: false,
		Order:            0,
	}
	p.environments[EnvStaging] = &Environment{
		Name:             EnvStaging,
		Description:      "Staging environment",
		RequiresApproval: false,
		Order:            1,
	}
	p.environments[EnvProd] = &Environment{
		Name:             EnvProd,
		Description:      "Production environment",
		RequiresApproval: true,
		Order:            2,
	}

	return p
}

// SetEnvironment adds or updates an environment definition.
func (p *Pipeline) SetEnvironment(env *Environment) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.environments[env.Name] = env
}

// GetEnvironment returns an environment by name.
func (p *Pipeline) GetEnvironment(name EnvironmentName) (*Environment, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	e, ok := p.environments[name]
	return e, ok
}

// ListEnvironments returns all environments sorted by order.
func (p *Pipeline) ListEnvironments() []*Environment {
	p.mu.RLock()
	defer p.mu.RUnlock()
	result := make([]*Environment, 0, len(p.environments))
	for _, e := range p.environments {
		result = append(result, e)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Order < result[j].Order
	})
	return result
}

// Deploy deploys a config directly to an environment (used for dev/staging without promotion).
func (p *Pipeline) Deploy(ctx context.Context, workflowName string, env EnvironmentName, configYAML string) error {
	p.mu.RLock()
	_, ok := p.environments[env]
	p.mu.RUnlock()
	if !ok {
		return fmt.Errorf("environment %q not found", env)
	}

	if p.validateFn != nil {
		if err := p.validateFn(ctx, env, configYAML); err != nil {
			return fmt.Errorf("validation failed for %s: %w", env, err)
		}
	}

	if p.deployFn != nil {
		if err := p.deployFn(ctx, env, workflowName, configYAML); err != nil {
			return fmt.Errorf("deploy to %s failed: %w", env, err)
		}
	}

	p.mu.Lock()
	if p.configs[workflowName] == nil {
		p.configs[workflowName] = make(map[EnvironmentName]string)
	}
	p.configs[workflowName][env] = configYAML
	p.mu.Unlock()

	return nil
}

// Promote initiates a promotion from one environment to another.
// If the target environment requires approval, the promotion status is set to pending.
func (p *Pipeline) Promote(ctx context.Context, workflowName string, fromEnv, toEnv EnvironmentName, requestedBy string) (*PromotionRecord, error) {
	p.mu.Lock()

	fromE, ok := p.environments[fromEnv]
	if !ok {
		p.mu.Unlock()
		return nil, fmt.Errorf("source environment %q not found", fromEnv)
	}
	toE, ok := p.environments[toEnv]
	if !ok {
		p.mu.Unlock()
		return nil, fmt.Errorf("target environment %q not found", toEnv)
	}

	if toE.Order <= fromE.Order {
		p.mu.Unlock()
		return nil, fmt.Errorf("cannot promote from %s to %s (must promote to a higher environment)", fromEnv, toEnv)
	}

	// Get the config from source environment
	envConfigs := p.configs[workflowName]
	if envConfigs == nil {
		p.mu.Unlock()
		return nil, fmt.Errorf("no config found for workflow %q in %s", workflowName, fromEnv)
	}
	configYAML, ok := envConfigs[fromEnv]
	if !ok {
		p.mu.Unlock()
		return nil, fmt.Errorf("no config found for workflow %q in %s", workflowName, fromEnv)
	}

	p.idCounter++
	id := fmt.Sprintf("promo-%d", p.idCounter)

	record := &PromotionRecord{
		ID:           id,
		WorkflowName: workflowName,
		ConfigYAML:   configYAML,
		FromEnv:      fromEnv,
		ToEnv:        toEnv,
		RequestedBy:  requestedBy,
		CreatedAt:    time.Now(),
	}

	if toE.RequiresApproval {
		record.Status = PromotionPending
		record.ApprovalStatus = ApprovalPending
		p.promotions[id] = record
		p.mu.Unlock()
		return record, nil
	}

	p.promotions[id] = record
	p.mu.Unlock()

	// No approval required, execute immediately
	return p.executePromotion(ctx, record)
}

// Approve approves a pending promotion and executes it.
func (p *Pipeline) Approve(ctx context.Context, promotionID, approvedBy string) (*PromotionRecord, error) {
	p.mu.Lock()
	record, ok := p.promotions[promotionID]
	if !ok {
		p.mu.Unlock()
		return nil, fmt.Errorf("promotion %q not found", promotionID)
	}
	if record.Status != PromotionPending {
		p.mu.Unlock()
		return nil, fmt.Errorf("promotion %q is not pending approval (status: %s)", promotionID, record.Status)
	}

	now := time.Now()
	record.ApprovalStatus = ApprovalApproved
	record.ApprovedBy = approvedBy
	record.ApprovedAt = &now
	record.Status = PromotionApproved
	p.mu.Unlock()

	return p.executePromotion(ctx, record)
}

// Reject rejects a pending promotion.
func (p *Pipeline) Reject(promotionID, rejectedBy, reason string) (*PromotionRecord, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	record, ok := p.promotions[promotionID]
	if !ok {
		return nil, fmt.Errorf("promotion %q not found", promotionID)
	}
	if record.Status != PromotionPending {
		return nil, fmt.Errorf("promotion %q is not pending approval", promotionID)
	}

	now := time.Now()
	record.ApprovalStatus = ApprovalRejected
	record.Status = PromotionRejected
	record.ApprovedBy = rejectedBy
	record.ApprovedAt = &now
	record.Error = reason
	record.CompletedAt = &now

	return record, nil
}

func (p *Pipeline) executePromotion(ctx context.Context, record *PromotionRecord) (*PromotionRecord, error) {
	// Validate
	if p.validateFn != nil {
		if err := p.validateFn(ctx, record.ToEnv, record.ConfigYAML); err != nil {
			now := time.Now()
			record.Status = PromotionFailed
			record.Error = fmt.Sprintf("validation failed: %v", err)
			record.CompletedAt = &now
			return record, fmt.Errorf("validation failed: %w", err)
		}
	}

	// Deploy
	if p.deployFn != nil {
		if err := p.deployFn(ctx, record.ToEnv, record.WorkflowName, record.ConfigYAML); err != nil {
			now := time.Now()
			record.Status = PromotionFailed
			record.Error = fmt.Sprintf("deploy failed: %v", err)
			record.CompletedAt = &now
			return record, fmt.Errorf("deploy failed: %w", err)
		}
	}

	now := time.Now()
	record.Status = PromotionDeployed
	record.CompletedAt = &now

	// Track config in target environment
	p.mu.Lock()
	if p.configs[record.WorkflowName] == nil {
		p.configs[record.WorkflowName] = make(map[EnvironmentName]string)
	}
	p.configs[record.WorkflowName][record.ToEnv] = record.ConfigYAML
	p.mu.Unlock()

	return record, nil
}

// GetPromotion returns a promotion record by ID.
func (p *Pipeline) GetPromotion(id string) (*PromotionRecord, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	r, ok := p.promotions[id]
	return r, ok
}

// ListPromotions returns all promotions sorted newest first.
func (p *Pipeline) ListPromotions() []*PromotionRecord {
	p.mu.RLock()
	defer p.mu.RUnlock()
	result := make([]*PromotionRecord, 0, len(p.promotions))
	for _, r := range p.promotions {
		result = append(result, r)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	return result
}

// GetConfig returns the currently deployed config for a workflow in an environment.
func (p *Pipeline) GetConfig(workflowName string, env EnvironmentName) (string, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	envConfigs := p.configs[workflowName]
	if envConfigs == nil {
		return "", false
	}
	cfg, ok := envConfigs[env]
	return cfg, ok
}

// GetAllConfigs returns all deployed configs for a workflow across environments.
func (p *Pipeline) GetAllConfigs(workflowName string) map[EnvironmentName]string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	envConfigs := p.configs[workflowName]
	if envConfigs == nil {
		return nil
	}
	result := make(map[EnvironmentName]string, len(envConfigs))
	for k, v := range envConfigs {
		result[k] = v
	}
	return result
}
