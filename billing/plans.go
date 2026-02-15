package billing

// Plan represents a billing plan with usage limits.
type Plan struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	PriceMonthly        int      `json:"price_monthly"`          // cents
	ExecutionsPerMonth  int64    `json:"executions_per_month"`   // 0 = unlimited
	MaxPipelines        int      `json:"max_pipelines"`          // 0 = unlimited
	MaxStepsPerPipeline int      `json:"max_steps_per_pipeline"` // 0 = unlimited
	RetentionDays       int      `json:"retention_days"`
	MaxWorkers          int      `json:"max_workers"`
	Features            []string `json:"features,omitempty"`
}

// Predefined billing plans.
var (
	PlanFree = Plan{
		ID:                  "free",
		Name:                "Free",
		PriceMonthly:        0,
		ExecutionsPerMonth:  1000,
		MaxPipelines:        5,
		MaxStepsPerPipeline: 20,
		RetentionDays:       7,
		MaxWorkers:          2,
	}

	PlanStarter = Plan{
		ID:                  "starter",
		Name:                "Starter",
		PriceMonthly:        4900, // $49
		ExecutionsPerMonth:  50_000,
		MaxPipelines:        25,
		MaxStepsPerPipeline: 50,
		RetentionDays:       30,
		MaxWorkers:          8,
		Features:            []string{"email-support", "custom-domains"},
	}

	PlanProfessional = Plan{
		ID:                  "professional",
		Name:                "Professional",
		PriceMonthly:        19900, // $199
		ExecutionsPerMonth:  500_000,
		MaxPipelines:        0, // unlimited
		MaxStepsPerPipeline: 0, // unlimited
		RetentionDays:       90,
		MaxWorkers:          32,
		Features:            []string{"email-support", "custom-domains", "priority-builds", "advanced-analytics"},
	}

	PlanEnterprise = Plan{
		ID:                  "enterprise",
		Name:                "Enterprise",
		PriceMonthly:        0, // custom pricing
		ExecutionsPerMonth:  0, // unlimited
		MaxPipelines:        0, // unlimited
		MaxStepsPerPipeline: 0, // unlimited
		RetentionDays:       365,
		MaxWorkers:          0, // unlimited
		Features: []string{
			"sso",
			"multi-region",
			"dedicated-infrastructure",
			"sla-guarantee",
			"priority-support",
			"custom-domains",
			"advanced-analytics",
			"audit-log-export",
		},
	}

	// AllPlans is the ordered list of available plans.
	AllPlans = []Plan{PlanFree, PlanStarter, PlanProfessional, PlanEnterprise}
)

// PlanByID looks up a plan by its identifier. Returns nil if not found.
func PlanByID(id string) *Plan {
	for i := range AllPlans {
		if AllPlans[i].ID == id {
			p := AllPlans[i]
			return &p
		}
	}
	return nil
}

// IsUnlimited reports whether the plan has no execution limit.
func (p Plan) IsUnlimited() bool {
	return p.ExecutionsPerMonth == 0
}
