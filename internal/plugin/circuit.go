package plugin

import (
	"sync"
	"time"
)

// DefaultCircuitThreshold is the number of consecutive runtime failures before
// a plugin is automatically tripped into failed status and unloaded.
const DefaultCircuitThreshold = 5

// CircuitBreaker tracks consecutive execution failures for one plugin.
type CircuitBreaker struct {
	threshold int

	mu          sync.Mutex
	consecutive int
	tripped     bool
	lastError   string
	lastFailAt  time.Time
}

// NewCircuitBreaker constructs a CircuitBreaker; threshold <= 0 defaults to 5.
func NewCircuitBreaker(threshold int) *CircuitBreaker {
	if threshold <= 0 {
		threshold = DefaultCircuitThreshold
	}
	return &CircuitBreaker{threshold: threshold}
}

// RecordSuccess clears any streak of consecutive non-user errors.
func (c *CircuitBreaker) RecordSuccess() {
	c.mu.Lock()
	c.consecutive = 0
	c.mu.Unlock()
}

// RecordFailure records an internal/runtime failure (timeouts, OOM, uncaught
// exceptions) and returns true on the exact failure that trips the breaker.
func (c *CircuitBreaker) RecordFailure(err error) (justTripped bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil {
		c.lastError = err.Error()
	}
	c.lastFailAt = time.Now()
	if c.tripped {
		return false
	}
	c.consecutive++
	if c.consecutive >= c.threshold {
		c.tripped = true
		return true
	}
	return false
}

// Reset restores the breaker to closed state after an administrator re-enables the plugin.
func (c *CircuitBreaker) Reset() {
	c.mu.Lock()
	c.consecutive = 0
	c.tripped = false
	c.lastError = ""
	c.lastFailAt = time.Time{}
	c.mu.Unlock()
}

// State snapshots the circuit breaker status.
func (c *CircuitBreaker) State() (consecutive int, tripped bool, lastError string, lastFailAt time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.consecutive, c.tripped, c.lastError, c.lastFailAt
}
