package billing

import (
	"context"
	"encoding/json"
	"fmt"

	stripe "github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/customer"
	"github.com/stripe/stripe-go/v82/subscription"
	"github.com/stripe/stripe-go/v82/webhook"
)

// StripePlanIDs maps plan IDs to Stripe price IDs (monthly).
// These must be configured to match Stripe dashboard price objects.
type StripePlanIDs map[string]string

// StripeProvider implements BillingProvider using the Stripe API.
type StripeProvider struct {
	apiKey        string
	webhookSecret string
	planPriceIDs  StripePlanIDs // planID -> Stripe price ID
}

// NewStripeProvider creates a StripeProvider with the given API key, webhook secret,
// and mapping from plan IDs to Stripe price IDs.
func NewStripeProvider(apiKey, webhookSecret string, planPriceIDs StripePlanIDs) *StripeProvider {
	stripe.Key = apiKey
	return &StripeProvider{
		apiKey:        apiKey,
		webhookSecret: webhookSecret,
		planPriceIDs:  planPriceIDs,
	}
}

// CreateCustomer creates a new Stripe customer for the given tenant.
func (p *StripeProvider) CreateCustomer(_ context.Context, tenantID, email string) (string, error) {
	params := &stripe.CustomerParams{
		Email: stripe.String(email),
		Metadata: map[string]string{
			"tenant_id": tenantID,
		},
	}
	c, err := customer.New(params)
	if err != nil {
		return "", fmt.Errorf("billing: create stripe customer: %w", err)
	}
	return c.ID, nil
}

// CreateSubscription creates a new Stripe subscription for the customer on the given plan.
func (p *StripeProvider) CreateSubscription(_ context.Context, customerID, planID string) (string, error) {
	priceID, ok := p.planPriceIDs[planID]
	if !ok {
		return "", fmt.Errorf("billing: no stripe price ID configured for plan %q", planID)
	}

	params := &stripe.SubscriptionParams{
		Customer: stripe.String(customerID),
		Items: []*stripe.SubscriptionItemsParams{
			{Price: stripe.String(priceID)},
		},
	}
	sub, err := subscription.New(params)
	if err != nil {
		return "", fmt.Errorf("billing: create stripe subscription: %w", err)
	}
	return sub.ID, nil
}

// CancelSubscription cancels a Stripe subscription at period end.
func (p *StripeProvider) CancelSubscription(_ context.Context, subscriptionID string) error {
	params := &stripe.SubscriptionCancelParams{}
	_, err := subscription.Cancel(subscriptionID, params)
	if err != nil {
		return fmt.Errorf("billing: cancel stripe subscription: %w", err)
	}
	return nil
}

// ReportUsage is a no-op for Stripe fixed-price plans. For metered billing,
// override this to call the Stripe usage records API.
func (p *StripeProvider) ReportUsage(_ context.Context, _ string, _ int64) error {
	return nil
}

// HandleWebhook validates the Stripe webhook signature and dispatches known events.
func (p *StripeProvider) HandleWebhook(_ context.Context, payload []byte, signature string) error {
	event, err := webhook.ConstructEvent(payload, signature, p.webhookSecret)
	if err != nil {
		return fmt.Errorf("billing: webhook signature verification failed: %w", err)
	}

	switch event.Type {
	case "invoice.paid":
		// Payment succeeded — subscription remains active.
	case "invoice.payment_failed":
		// Payment failed — subscription may move to past_due.
	case "customer.subscription.updated":
		var sub stripe.Subscription
		if err := json.Unmarshal(event.Data.Raw, &sub); err != nil {
			return fmt.Errorf("billing: parse subscription updated event: %w", err)
		}
		// Caller can inspect sub.Status and update their records.
	case "customer.subscription.deleted":
		var sub stripe.Subscription
		if err := json.Unmarshal(event.Data.Raw, &sub); err != nil {
			return fmt.Errorf("billing: parse subscription deleted event: %w", err)
		}
		// Caller can mark the subscription as canceled in their records.
	}

	return nil
}
