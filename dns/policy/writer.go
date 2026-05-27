package policy

import "context"

// DNSRecordWriter performs arbitrary DNS record mutations (post-gate).
type DNSRecordWriter interface {
	UpsertRecord(ctx context.Context, zone, name, recordType, data string, ttl, priority int32) (recordID string, err error)
	DeleteRecord(ctx context.Context, zone, name, recordType string) error
}

// Adapter combines policy R/W and record R/W in one type.
// dnsprovider.NewAdapter returns this combined interface.
type Adapter interface {
	DNSPolicyReader
	DNSRecordWriter
}
