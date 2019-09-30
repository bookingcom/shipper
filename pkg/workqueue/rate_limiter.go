package workqueue

import (
	"math/rand"
	"time"

	"k8s.io/client-go/util/workqueue"
)

func NewDefaultControllerRateLimiter() workqueue.RateLimiter {
	fastDelay := 5 * time.Millisecond
	slowDelay := 5 * time.Second
	maxFastAttempts := 3

	return NewJitteredFastSlowRateLimiter(fastDelay, slowDelay, maxFastAttempts)
}

// JitteredRateLimiter does a quick retry for a certain number of attempts,
// then slow retries afterwards. It also adds some jitter to prevent Thundering
// Herds from happening.
type JitteredRateLimiter struct {
	workqueue.RateLimiter
}

func NewJitteredFastSlowRateLimiter(fastDelay, slowDelay time.Duration, maxFastAttempts int) workqueue.RateLimiter {
	ratelimiter := workqueue.NewItemFastSlowRateLimiter(fastDelay, slowDelay, maxFastAttempts)

	return &JitteredRateLimiter{
		RateLimiter: ratelimiter,
	}
}

func (r *JitteredRateLimiter) When(item interface{}) time.Duration {
	when := r.RateLimiter.When(item)

	// int64(when)/1e6 is equivalent to when.Milliseconds(), which is only
	// available in golang 1.13+
	jitter := time.Millisecond * time.Duration(rand.Int63n(int64(when)/1e6))

	return when + jitter/2
}
