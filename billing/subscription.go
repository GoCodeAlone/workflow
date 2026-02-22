package billing

import "time"

// Subscription represents an active or historical billing subscription for a tenant.
type Subscription struct {
	ID                   string    `json:"id"`
	CustomerID           string    `json:"customer_id"`
	PlanID               string    `json:"plan_id"`
	Status               string    `json:"status"` // active, past_due, canceled, trialing
	CurrentPeriodEnd     time.Time `json:"current_period_end"`
	StripeSubscriptionID string    `json:"stripe_subscription_id,omitempty"`
}

// SubscriptionStatus constants for well-known Stripe subscription states.
const (
	StatusActive   = "active"
	StatusPastDue  = "past_due"
	StatusCanceled = "canceled"
	StatusTrialing = "trialing"
)
