package openaillm

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// retryPolicy bounds how a failed request is retried.
type retryPolicy struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
}

// defaultRetryPolicy retries transient failures with exponential backoff.
var defaultRetryPolicy = retryPolicy{
	MaxAttempts: 4, // first try + 3 retries
	BaseDelay:   500 * time.Millisecond,
	MaxDelay:    8 * time.Second,
}

// postWithRetry sends a JSON POST, retrying transient failures. It returns the
// response on success (2xx) or an error. A non-retryable status returns
// immediately with the response body included in the error.
func postWithRetry(ctx context.Context, client *http.Client, url string, headers map[string]string, body []byte, policy retryPolicy) (*http.Response, error) {
	if policy.MaxAttempts <= 0 {
		policy = defaultRetryPolicy
	}

	var lastErr error
	for attempt := 1; attempt <= policy.MaxAttempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		for k, v := range headers {
			req.Header.Set(k, v)
		}

		resp, err := client.Do(req)
		if err != nil {
			// Transport error (connection refused, TLS, timeout): retryable.
			lastErr = err
			if !sleepBackoff(ctx, policy, attempt) {
				return nil, ctx.Err()
			}
			continue
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return resp, nil
		}

		status := resp.StatusCode
		// 429 and 5xx are transient; retry. Others are terminal.
		if status == http.StatusTooManyRequests || status >= 500 {
			detail, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
			resp.Body.Close()
			lastErr = fmt.Errorf("HTTP %d: %s", status, strings.TrimSpace(string(detail)))
			delay := retryAfter(resp, policy, attempt)
			if !sleepFor(ctx, delay) {
				return nil, ctx.Err()
			}
			continue
		}

		return resp, nil // terminal non-2xx: caller reads the body for the error
	}
	return nil, fmt.Errorf("request failed after %d attempts: %w", policy.MaxAttempts, lastErr)
}

// retryAfter honors a Retry-After header when present, else uses backoff.
func retryAfter(resp *http.Response, policy retryPolicy, attempt int) time.Duration {
	if v := resp.Header.Get("Retry-After"); v != "" {
		if secs, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && secs >= 0 {
			return time.Duration(secs) * time.Second
		}
	}
	return backoff(policy, attempt)
}

// backoff computes the exponential delay for an attempt, capped at MaxDelay.
func backoff(policy retryPolicy, attempt int) time.Duration {
	delay := float64(policy.BaseDelay) * math.Pow(2, float64(attempt-1))
	if delay > float64(policy.MaxDelay) {
		delay = float64(policy.MaxDelay)
	}
	return time.Duration(delay)
}

// sleepBackoff sleeps the backoff delay; returns false if ctx was cancelled.
func sleepBackoff(ctx context.Context, policy retryPolicy, attempt int) bool {
	return sleepFor(ctx, backoff(policy, attempt))
}

// sleepFor waits for d or until ctx is done. Returns false on cancellation.
func sleepFor(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
