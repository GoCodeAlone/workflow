package module

import "context"

// NoSQLStore is the common interface implemented by all NoSQL backend modules.
// Backends: nosql.memory, nosql.dynamodb, nosql.mongodb, nosql.redis.
type NoSQLStore interface {
	// Get retrieves an item by key. Returns nil, nil when the key does not exist.
	Get(ctx context.Context, key string) (map[string]any, error)

	// Put inserts or replaces an item.
	Put(ctx context.Context, key string, item map[string]any) error

	// Delete removes an item by key. Does not error if the key does not exist.
	Delete(ctx context.Context, key string) error

	// Query returns all items that match the provided filter params.
	// Supported params: "prefix" (string) — key prefix filter.
	Query(ctx context.Context, params map[string]any) ([]map[string]any, error)
}
