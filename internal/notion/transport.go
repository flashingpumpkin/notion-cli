package notion

import (
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// CircuitState represents the state of the circuit breaker
type CircuitState int

const (
	CircuitClosed   CircuitState = iota // Normal operation
	CircuitOpen                         // Failing, rejecting requests
	CircuitHalfOpen                     // Testing if service recovered
)

func (s CircuitState) String() string {
	switch s {
	case CircuitClosed:
		return "closed"
	case CircuitOpen:
		return "open"
	case CircuitHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// CircuitBreaker implements the circuit breaker pattern
type CircuitBreaker struct {
	mu                sync.Mutex
	state             CircuitState
	failures          int
	successes         int
	lastFailure       time.Time
	failureThreshold  int           // Number of consecutive failures to open circuit
	successThreshold  int           // Number of successes in half-open to close circuit
	recoveryTimeout   time.Duration // Time to wait before testing again
	onStateChange     func(from, to CircuitState)
}

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(onStateChange func(from, to CircuitState)) *CircuitBreaker {
	return &CircuitBreaker{
		state:            CircuitClosed,
		failureThreshold: 5,
		successThreshold: 2,
		recoveryTimeout:  30 * time.Second,
		onStateChange:    onStateChange,
	}
}

// Allow checks if a request should be allowed
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CircuitClosed:
		return true
	case CircuitOpen:
		// Check if recovery timeout has passed
		if time.Since(cb.lastFailure) > cb.recoveryTimeout {
			cb.transitionTo(CircuitHalfOpen)
			return true
		}
		return false
	case CircuitHalfOpen:
		return true
	}
	return false
}

// RecordSuccess records a successful request
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures = 0

	if cb.state == CircuitHalfOpen {
		cb.successes++
		if cb.successes >= cb.successThreshold {
			cb.transitionTo(CircuitClosed)
		}
	}
}

// RecordFailure records a failed request
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures++
	cb.lastFailure = time.Now()
	cb.successes = 0

	if cb.state == CircuitClosed && cb.failures >= cb.failureThreshold {
		cb.transitionTo(CircuitOpen)
	} else if cb.state == CircuitHalfOpen {
		cb.transitionTo(CircuitOpen)
	}
}

// transitionTo changes the circuit state (must be called with lock held)
func (cb *CircuitBreaker) transitionTo(newState CircuitState) {
	if cb.state == newState {
		return
	}
	oldState := cb.state
	cb.state = newState
	cb.successes = 0

	if cb.onStateChange != nil {
		cb.onStateChange(oldState, newState)
	}
}

// State returns the current circuit state
func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

// ResilientTransport wraps http.RoundTripper with rate limiting, circuit breaking, and retries
type ResilientTransport struct {
	base           http.RoundTripper
	rateLimiter    *rate.Limiter
	circuitBreaker *CircuitBreaker
	maxRetries     int
	debug          bool
}

// NewResilientTransport creates a new resilient transport
func NewResilientTransport(debug bool) *ResilientTransport {
	rt := &ResilientTransport{
		base:        http.DefaultTransport,
		rateLimiter: rate.NewLimiter(rate.Limit(3), 5), // 3 req/sec, burst of 5
		maxRetries:  3,
		debug:       debug,
	}

	// Create circuit breaker with state change callback
	rt.circuitBreaker = NewCircuitBreaker(func(from, to CircuitState) {
		if to == CircuitOpen {
			fmt.Printf("Circuit breaker open - API unavailable, pausing...\n")
		}
		if rt.debug {
			fmt.Printf("[DEBUG] Circuit breaker: %s -> %s\n", from, to)
		}
	})

	return rt
}

// RoundTrip implements http.RoundTripper with resilience features
func (rt *ResilientTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var lastErr error
	var resp *http.Response

	for attempt := 0; attempt <= rt.maxRetries; attempt++ {
		// Check circuit breaker
		if !rt.circuitBreaker.Allow() {
			// Wait for recovery timeout before retrying
			waitWithProgress(rt.circuitBreaker.recoveryTimeout, "Circuit breaker open, waiting for recovery,")
			if !rt.circuitBreaker.Allow() {
				return nil, fmt.Errorf("circuit breaker open - API unavailable")
			}
		}

		// Rate limiting
		waitStart := time.Now()
		err := rt.rateLimiter.Wait(req.Context())
		if err != nil {
			return nil, fmt.Errorf("rate limiter error: %w", err)
		}
		waitDuration := time.Since(waitStart)

		if rt.debug && waitDuration > 100*time.Millisecond {
			fmt.Printf("[DEBUG] Rate limiter wait: %v\n", waitDuration)
		}

		if rt.debug {
			fmt.Printf("[DEBUG] %s %s (attempt %d/%d)\n", req.Method, req.URL.Path, attempt+1, rt.maxRetries+1)
		}

		// Execute request
		resp, err = rt.base.RoundTrip(req)
		if err != nil {
			lastErr = err
			rt.circuitBreaker.RecordFailure()

			if attempt < rt.maxRetries {
				backoff := rt.calculateBackoff(attempt)
				fmt.Printf("Retrying request (attempt %d/%d)...\n", attempt+2, rt.maxRetries+1)
				time.Sleep(backoff)
				continue
			}
			return nil, lastErr
		}

		if rt.debug {
			fmt.Printf("[DEBUG] Response: %d %s\n", resp.StatusCode, resp.Status)
		}

		// Check if we should retry based on status code
		if rt.shouldRetry(resp.StatusCode) {
			lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
			rt.circuitBreaker.RecordFailure()

			// Handle 429 rate limiting
			if resp.StatusCode == 429 {
				retryAfter := rt.parseRetryAfter(resp)
				waitWithProgress(retryAfter, "Rate limited by Notion API,")
				continue
			}

			if attempt < rt.maxRetries {
				backoff := rt.calculateBackoff(attempt)
				fmt.Printf("Retrying request (attempt %d/%d)...\n", attempt+2, rt.maxRetries+1)
				time.Sleep(backoff)
				continue
			}

			return resp, nil
		}

		// Success
		rt.circuitBreaker.RecordSuccess()
		return resp, nil
	}

	return resp, lastErr
}

// shouldRetry returns true if the status code indicates a retryable error
func (rt *ResilientTransport) shouldRetry(statusCode int) bool {
	switch statusCode {
	case 429, 500, 502, 503, 504, 409:
		return true
	default:
		return false
	}
}

// calculateBackoff returns the backoff duration for the given attempt
func (rt *ResilientTransport) calculateBackoff(attempt int) time.Duration {
	// Exponential backoff: 1s, 2s, 4s
	backoff := time.Duration(1<<uint(attempt)) * time.Second
	if backoff > 4*time.Second {
		backoff = 4 * time.Second
	}
	return backoff
}

// parseRetryAfter parses the Retry-After header and returns the duration to wait
func (rt *ResilientTransport) parseRetryAfter(resp *http.Response) time.Duration {
	retryAfter := resp.Header.Get("Retry-After")
	if retryAfter == "" {
		return 1 * time.Second
	}

	// Try parsing as seconds
	if seconds, err := strconv.Atoi(retryAfter); err == nil {
		return time.Duration(seconds) * time.Second
	}

	// Try parsing as HTTP date
	if t, err := http.ParseTime(retryAfter); err == nil {
		return time.Until(t)
	}

	return 1 * time.Second
}

// waitWithProgress waits for the specified duration while showing a countdown
func waitWithProgress(duration time.Duration, message string) {
	if duration <= 0 {
		return
	}

	endTime := time.Now().Add(duration)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	// Print initial message with time remaining
	remaining := time.Until(endTime).Round(time.Second)
	fmt.Printf("\r%s %v remaining...  ", message, remaining)

	for {
		select {
		case <-ticker.C:
			remaining = time.Until(endTime).Round(time.Second)
			if remaining <= 0 {
				fmt.Printf("\r%s done.                    \n", message)
				return
			}
			fmt.Printf("\r%s %v remaining...  ", message, remaining)
		}
	}
}
