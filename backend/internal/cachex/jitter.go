package cachex

import (
	"math"
	"math/rand"
	"time"
)

// TTLWithJitterRatio applies symmetric jitter around base TTL.
func TTLWithJitterRatio(base time.Duration, ratio float64) time.Duration {
	if base <= 0 || ratio <= 0 {
		return base
	}

	maxJitter := time.Duration(math.Round(float64(base) * ratio))
	if maxJitter <= 0 {
		return base
	}

	return TTLWithJitter(base, maxJitter)
}

// TTLWithJitter returns a TTL in [base-maxJitter, base+maxJitter].
func TTLWithJitter(base time.Duration, maxJitter time.Duration) time.Duration {
	if base <= 0 || maxJitter <= 0 {
		return base
	}

	delta := time.Duration(rand.Int63n(int64(maxJitter)*2+1)) - maxJitter
	ttl := base + delta
	if ttl <= 0 {
		return time.Millisecond
	}
	return ttl
}
