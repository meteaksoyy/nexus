package ratelimit

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// SlidingWindow implements a sliding window rate limiter using Redis sorted sets.
//
// Each client has a sorted set keyed by `ratelimit:<clientID>` where members are
// unique request IDs and scores are Unix nanosecond timestamps. On each request:
//   1. Entries older than the window are removed (ZREMRANGEBYSCORE)
//   2. The current count is checked (ZCARD)
//   3. If under the limit, the current request is recorded (ZADD)
//
// This is a "sliding window log" — exact but uses O(n) Redis memory per client.
type SlidingWindow struct {
	rdb    *redis.Client
	limit  int
	window time.Duration
}

func NewSlidingWindow(rdb *redis.Client, limit int, window time.Duration) *SlidingWindow {
	return &SlidingWindow{rdb: rdb, limit: limit, window: window}
}

// Allow returns (allowed, remaining, retryAfter, error).
// retryAfter is only meaningful when allowed == false.
func (s *SlidingWindow) Allow(ctx context.Context, clientID string) (bool, int, time.Duration, error) {
	key := "ratelimit:" + clientID
	now := time.Now()
	windowStart := now.Add(-s.window)

	pipe := s.rdb.Pipeline()

	// Remove entries outside the window
	pipe.ZRemRangeByScore(ctx, key, "0", fmt.Sprintf("%d", windowStart.UnixNano()))

	// Count current entries
	countCmd := pipe.ZCard(ctx, key)

	if _, err := pipe.Exec(ctx); err != nil {
		return false, 0, 0, fmt.Errorf("ratelimit pipeline: %w", err)
	}

	count := int(countCmd.Val())
	remaining := s.limit - count

	if count >= s.limit {
		// Estimate retry-after: time until the oldest entry falls outside the window
		oldest, err := s.rdb.ZRange(ctx, key, 0, 0).Result()
		retryAfter := s.window
		if err == nil && len(oldest) > 0 {
			var oldestNs int64
			fmt.Sscanf(oldest[0], "%d", &oldestNs)
			oldestTime := time.Unix(0, oldestNs)
			retryAfter = s.window - now.Sub(oldestTime)
			if retryAfter < 0 {
				retryAfter = 0
			}
		}
		return false, 0, retryAfter, nil
	}

	// Record this request — use nanosecond timestamp as score; add random suffix
	// so that concurrent requests at the same nanosecond get distinct members.
	var rndBuf [4]byte
	rand.Read(rndBuf[:]) //nolint:errcheck
	member := fmt.Sprintf("%d-%s", now.UnixNano(), hex.EncodeToString(rndBuf[:]))
	pipe2 := s.rdb.Pipeline()
	pipe2.ZAdd(ctx, key, redis.Z{Score: float64(now.UnixNano()), Member: member})
	pipe2.Expire(ctx, key, s.window+time.Second) // auto-expire the key
	if _, err := pipe2.Exec(ctx); err != nil {
		return false, 0, 0, fmt.Errorf("ratelimit record: %w", err)
	}

	return true, remaining - 1, 0, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
