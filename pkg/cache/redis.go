package cache

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis is a Store backed by a Redis server, letting several OrionDrive nodes
// share ephemeral state (upload sessions, OIDC states, ...). It implements the
// same interface as the in-memory driver.
type Redis struct {
	client *redis.Client
}

// NewRedis connects to the Redis server at addr and returns a Store. It pings
// the server so a misconfiguration surfaces at startup rather than on first use.
func NewRedis(addr, password string, db int) (*Redis, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, err
	}
	return &Redis{client: client}, nil
}

// Set stores value under key for ttl (0 = no expiry).
func (r *Redis) Set(key string, value []byte, ttl time.Duration) error {
	return r.client.Set(context.Background(), key, value, ttl).Err()
}

// Get returns the value and whether it was present and unexpired.
func (r *Redis) Get(key string) ([]byte, bool) {
	b, err := r.client.Get(context.Background(), key).Bytes()
	if err != nil {
		// redis.Nil is a plain miss; any other (connection) error is also
		// reported as absent since this interface can't surface it, so callers
		// fall back to regenerating the value.
		return nil, false
	}
	return b, true
}

// Delete removes a key.
func (r *Redis) Delete(key string) error {
	return r.client.Del(context.Background(), key).Err()
}
