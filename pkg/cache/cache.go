// Package cache provides a small key/value store with TTL, used for upload
// sessions, OIDC states, and other ephemeral data. A process-memory driver is
// provided; a Redis driver can be added later behind the same interface.
package cache

import "time"

// Store is a TTL key/value cache.
type Store interface {
	Set(key string, value []byte, ttl time.Duration) error
	Get(key string) ([]byte, bool)
	Delete(key string) error
}
