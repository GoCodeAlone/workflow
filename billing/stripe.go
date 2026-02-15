package billing

import (
	"context"
	"fmt"
	"sync"
)

// BillingProvider abstracts payment and subscription management.
type BillingProvider interface {
	// CreateCustomer registers a new billing customer for the given tenant.
	CreateCustomer(ctx context.Context, tenantID, email string) (customerID string, err error)
	// CreateSubscription starts a subscription for the customer on the given plan.
	CreateSubscription(ctx context.Context, customerID, planID string) (subscriptionID string, err error)
	// CancelSubscription cancels an active subscription.
	CancelSubscription(ctx context.Context, subscriptionID string) error
	// ReportUsage reports metered usage for a subscription.
	ReportUsage(ctx context.Context, subscriptionID string, quantity int64) error
	// HandleWebhook processes an incoming webhook payload from the payment provider.
	HandleWebhook(ctx context.Context, payload []byte, signature string) error
}

// ---------- Mock implementation ----------

// MockBillingProvider is a test double that records calls and returns
// configurable results.
type MockBillingProvider struct {
	mu sync.Mutex

	// Customers maps tenantID -> customerID.
	Customers map[string]string
	// Subscriptions maps subscriptionID -> planID.
	Subscriptions map[string]string
	// UsageReports collects (subscriptionID, quantity) pairs.
	UsageReports []UsageEntry
	// WebhookPayloads collects raw webhook bodies.
	WebhookPayloads [][]byte

	// Error fields allow tests to inject failures.
	CreateCustomerErr     error
	CreateSubscriptionErr error
	CancelSubscriptionErr error
	ReportUsageErr        error
	HandleWebhookErr      error

	nextCustomerSeq int
	nextSubSeq      int
}

// UsageEntry records a single usage report.
type UsageEntry struct {
	SubscriptionID string
	Quantity       int64
}

// NewMockBillingProvider creates a MockBillingProvider ready for use.
func NewMockBillingProvider() *MockBillingProvider {
	return &MockBillingProvider{
		Customers:     make(map[string]string),
		Subscriptions: make(map[string]string),
	}
}

// CreateCustomer creates a mock customer.
func (m *MockBillingProvider) CreateCustomer(_ context.Context, tenantID, _ string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.CreateCustomerErr != nil {
		return "", m.CreateCustomerErr
	}

	m.nextCustomerSeq++
	id := fmt.Sprintf("cus_mock_%d", m.nextCustomerSeq)
	m.Customers[tenantID] = id
	return id, nil
}

// CreateSubscription creates a mock subscription.
func (m *MockBillingProvider) CreateSubscription(_ context.Context, customerID, planID string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.CreateSubscriptionErr != nil {
		return "", m.CreateSubscriptionErr
	}

	// Verify customer exists.
	found := false
	for _, cid := range m.Customers {
		if cid == customerID {
			found = true
			break
		}
	}
	if !found {
		return "", fmt.Errorf("billing: unknown customer %s", customerID)
	}

	m.nextSubSeq++
	id := fmt.Sprintf("sub_mock_%d", m.nextSubSeq)
	m.Subscriptions[id] = planID
	return id, nil
}

// CancelSubscription cancels a mock subscription.
func (m *MockBillingProvider) CancelSubscription(_ context.Context, subscriptionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.CancelSubscriptionErr != nil {
		return m.CancelSubscriptionErr
	}

	if _, ok := m.Subscriptions[subscriptionID]; !ok {
		return fmt.Errorf("billing: subscription %s not found", subscriptionID)
	}
	delete(m.Subscriptions, subscriptionID)
	return nil
}

// ReportUsage records a usage report.
func (m *MockBillingProvider) ReportUsage(_ context.Context, subscriptionID string, quantity int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.ReportUsageErr != nil {
		return m.ReportUsageErr
	}

	m.UsageReports = append(m.UsageReports, UsageEntry{
		SubscriptionID: subscriptionID,
		Quantity:       quantity,
	})
	return nil
}

// HandleWebhook records the webhook payload.
func (m *MockBillingProvider) HandleWebhook(_ context.Context, payload []byte, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.HandleWebhookErr != nil {
		return m.HandleWebhookErr
	}

	m.WebhookPayloads = append(m.WebhookPayloads, payload)
	return nil
}
