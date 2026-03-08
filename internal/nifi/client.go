package nifi

import "context"

// Client is the future seam for NiFi API orchestration.
// The scaffold keeps the interface small until lifecycle logic is implemented.
type Client interface {
	Ping(ctx context.Context, baseURL string) error
}
