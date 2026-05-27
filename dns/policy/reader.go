package policy

import "context"

// DNSPolicyReader is the narrow interface the gate needs.
// Tests mock this directly; only 2 methods to fake.
type DNSPolicyReader interface {
	GetTXT(ctx context.Context, name string) ([]string, error)
	UpsertTXT(ctx context.Context, name string, values []string, ttl int) error
}
