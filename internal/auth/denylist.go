package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const denylistPrefix = "denylist:jti:"

// Denylist stores revoked JWT IDs in Redis with TTL matching the token's remaining lifetime.
type Denylist struct {
	rdb *redis.Client
}

func NewDenylist(rdb *redis.Client) *Denylist {
	return &Denylist{rdb: rdb}
}

// Revoke adds a JTI to the denylist until the token would naturally expire.
func (d *Denylist) Revoke(ctx context.Context, jti string, expiresAt time.Time) error {
	ttl := time.Until(expiresAt)
	if ttl <= 0 {
		return nil // already expired, nothing to do
	}
	key := denylistPrefix + jti
	if err := d.rdb.Set(ctx, key, "1", ttl).Err(); err != nil {
		return fmt.Errorf("revoke jti %s: %w", jti, err)
	}
	return nil
}

// IsRevoked returns true if the given JTI has been revoked.
func (d *Denylist) IsRevoked(ctx context.Context, jti string) (bool, error) {
	key := denylistPrefix + jti
	err := d.rdb.Get(ctx, key).Err()
	if errors.Is(err, redis.Nil) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check denylist: %w", err)
	}
	return true, nil
}
