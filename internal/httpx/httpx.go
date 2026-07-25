// Package httpx provides the HTTP client used to fetch job postings.
//
// Job boards are third-party services of wildly varying quality: some
// rate-limit aggressively, some return transient 5xx responses under load, and
// some simply drop connections. The client returned by [NewClient] retries
// those failures with jittered exponential backoff so that a single flaky
// response does not silently remove a company from a crawl.
package httpx

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// DefaultUserAgent identifies this tool to job boards. Sending a real
// User-Agent is both polite and practical: some boards reject requests that
// do not set one.
const DefaultUserAgent = "job-hunter-toolkit/1.0 (+https://github.com/job-hunter-toolkit/job-hunter-toolkit)"

// Defaults for [NewClient].
const (
	defaultMaxAttempts = 4
	defaultBaseDelay   = 500 * time.Millisecond
	defaultMaxDelay    = 30 * time.Second
	defaultTimeout     = 2 * time.Minute

	// defaultPerHostLimit bounds concurrent requests to any single host.
	//
	// A crawl fans out over companies, but companies are not spread evenly over
	// hosts: every Workable board is served by apply.workable.com, so raising
	// overall concurrency just rate-limits that one host. Measured: 56 Workable
	// sources failed with HTTP 429 purely from self-inflicted load, which looks
	// identical to a dead board in a health report.
	//
	// Note this keys on the exact host, so it does NOT group platforms that give
	// each tenant its own subdomain, PeopleForce (<tenant>.peopleforce.io) still
	// rate-limits, because those are distinct hosts sharing one backend. Grouping
	// by registrable domain would fix that, but would also serialise Workday,
	// whose per-tenant subdomains are genuinely separate infrastructure.
	defaultPerHostLimit = 4
)

// Option configures the client returned by [NewClient].
type Option func(*retryTransport)

// WithMaxAttempts sets the total number of attempts per request, including the
// first. Values below 1 are ignored.
func WithMaxAttempts(attempts int) Option {
	return func(t *retryTransport) {
		if attempts >= 1 {
			t.maxAttempts = attempts
		}
	}
}

// WithBaseDelay sets the initial backoff delay, which doubles per attempt.
func WithBaseDelay(d time.Duration) Option {
	return func(t *retryTransport) {
		if d > 0 {
			t.baseDelay = d
		}
	}
}

// WithMaxDelay caps the backoff delay between attempts.
func WithMaxDelay(d time.Duration) Option {
	return func(t *retryTransport) {
		if d > 0 {
			t.maxDelay = d
		}
	}
}

// WithLogger sets the logger used to report retries. Retries are logged at
// debug level, and exhausted retries at warn level.
func WithLogger(logger *slog.Logger) Option {
	return func(t *retryTransport) {
		if logger != nil {
			t.logger = logger
		}
	}
}

// WithUserAgent overrides [DefaultUserAgent].
func WithUserAgent(ua string) Option {
	return func(t *retryTransport) {
		t.userAgent = ua
	}
}

// WithTransport sets the underlying transport. It defaults to a clone of
// [net/http.DefaultTransport].
func WithTransport(base http.RoundTripper) Option {
	return func(t *retryTransport) {
		if base != nil {
			t.base = base
		}
	}
}

// WithPerHostLimit bounds how many requests may be in flight to a single host.
// A value below 1 disables the limit.
func WithPerHostLimit(limit int) Option {
	return func(t *retryTransport) {
		t.perHostLimit = limit
	}
}

// NewClient returns an HTTP client that retries transient failures and bounds
// concurrency per host.
//
// The client has no overall timeout of its own beyond a generous safety net;
// crawls are expected to be bounded by the context passed to each request, so
// that cancelling a crawl cancels its in-flight work.
func NewClient(opts ...Option) *http.Client {
	base := http.DefaultTransport.(*http.Transport).Clone()

	// Job boards are spread across many hosts, and a crawl hits them
	// concurrently. The stdlib default of 2 idle connections per host is fine,
	// but the default idle-connection ceiling across all hosts is low enough to
	// cause needless reconnects at this fan-out.
	base.MaxIdleConns = 200
	base.MaxIdleConnsPerHost = 8

	t := &retryTransport{
		base:         base,
		maxAttempts:  defaultMaxAttempts,
		baseDelay:    defaultBaseDelay,
		maxDelay:     defaultMaxDelay,
		logger:       slog.New(slog.DiscardHandler),
		userAgent:    DefaultUserAgent,
		perHostLimit: defaultPerHostLimit,
	}

	for _, opt := range opts {
		opt(t)
	}

	// The host limiter wraps the base transport rather than the retry loop, so
	// each individual attempt is throttled, including retries, which are
	// exactly what a rate-limited host does not want more of.
	if t.perHostLimit > 0 {
		t.base = &hostLimiter{base: t.base, limit: t.perHostLimit}
	}

	return &http.Client{
		Transport: t,
		Timeout:   defaultTimeout,
	}
}

// hostLimiter bounds the number of requests in flight to any single host.
type hostLimiter struct {
	base  http.RoundTripper
	limit int

	mu   sync.Mutex
	sems map[string]chan struct{}
}

// sem returns the semaphore for a host, creating it on first use.
func (h *hostLimiter) sem(host string) chan struct{} {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.sems == nil {
		h.sems = make(map[string]chan struct{})
	}

	if _, ok := h.sems[host]; !ok {
		h.sems[host] = make(chan struct{}, h.limit)
	}

	return h.sems[host]
}

// RoundTrip implements [net/http.RoundTripper].
func (h *hostLimiter) RoundTrip(req *http.Request) (*http.Response, error) {
	sem := h.sem(req.URL.Host)

	select {
	case sem <- struct{}{}:
	case <-req.Context().Done():
		return nil, req.Context().Err()
	}

	resp, err := h.base.RoundTrip(req)
	if err != nil {
		<-sem

		return nil, err
	}

	// The slot is held until the body is closed, not merely until the headers
	// arrive: a response whose body is still streaming is still occupying the
	// server's attention.
	resp.Body = &releaseOnClose{ReadCloser: resp.Body, release: func() { <-sem }}

	return resp, nil
}

// releaseOnClose releases a semaphore slot exactly once, when the body is closed.
type releaseOnClose struct {
	io.ReadCloser

	once    sync.Once
	release func()
}

// Close closes the underlying body and releases the slot.
func (r *releaseOnClose) Close() error {
	err := r.ReadCloser.Close()

	r.once.Do(r.release)

	return err
}

// retryTransport retries idempotent requests that fail with a transient error
// or a retryable status code.
type retryTransport struct {
	base         http.RoundTripper
	maxAttempts  int
	baseDelay    time.Duration
	maxDelay     time.Duration
	logger       *slog.Logger
	userAgent    string
	perHostLimit int
}

// RoundTrip implements [net/http.RoundTripper].
func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.userAgent != "" && req.Header.Get("User-Agent") == "" {
		// RoundTrippers must not modify the request they are given, so clone it
		// before setting the header.
		req = req.Clone(req.Context())
		req.Header.Set("User-Agent", t.userAgent)
	}

	var (
		lastErr  error
		attempts int
	)

	for attempt := 1; attempt <= t.maxAttempts; attempt++ {
		attempts = attempt

		resp, err := t.base.RoundTrip(req)

		switch {
		case err != nil:
			// A transport returning both a response and an error breaks the
			// RoundTripper contract, but WithTransport accepts arbitrary
			// transports, so close the body rather than leak it.
			drain(resp)

			resp = nil

			// A cancelled or expired context is deliberate, not transient.
			if ctxErr := req.Context().Err(); ctxErr != nil {
				return nil, err
			}

			lastErr = err
		case !retryableStatus(resp.StatusCode):
			return resp, nil
		default:
			lastErr = errStatus(resp.StatusCode)
		}

		lastAttempt := attempt == t.maxAttempts

		// Only a replayable request may be retried. A request with a body that
		// cannot be rewound would be sent truncated on the second attempt.
		if lastAttempt || !replayable(req) {
			if resp != nil {
				// Retries are exhausted on a retryable status. Report it, then
				// hand back the real response so the caller can see the status.
				t.logExhausted(req, attempts, lastErr)

				return resp, nil
			}

			break
		}

		delay := t.backoff(attempt, resp)

		// The response body must be drained and closed before the connection
		// can be reused, and leaking it would hold a connection per retry.
		drain(resp)

		// Rewind the request body before replaying it. Without this the next
		// attempt sends an empty body with the original Content-Length: today
		// net/http's own retry logic papers over it, but any wrapping transport
		// would silently send a truncated request; and for Workday the body is
		// the pagination payload, so an empty one asks for the wrong page.
		rewound, err := rewind(req)
		if err != nil {
			return nil, err
		}

		req = rewound

		t.logger.DebugContext(req.Context(), "retrying HTTP request",
			slog.String("url", req.URL.Redacted()),
			slog.Int("attempt", attempt),
			slog.Int("max_attempts", t.maxAttempts),
			slog.Duration("delay", delay),
			slog.String("cause", lastErr.Error()),
		)

		if err := sleep(req.Context(), delay); err != nil {
			return nil, err
		}
	}

	t.logExhausted(req, attempts, lastErr)

	return nil, lastErr
}

// logExhausted reports a request that ran out of attempts. The count is the
// number of attempts actually made, which is not necessarily maxAttempts: a
// non-replayable request stops after the first.
func (t *retryTransport) logExhausted(req *http.Request, attempts int, cause error) {
	t.logger.WarnContext(req.Context(), "HTTP request failed after retries",
		slog.String("url", req.URL.Redacted()),
		slog.Int("attempts", attempts),
		slog.String("cause", cause.Error()),
	)
}

// rewind returns a request whose body can be read again, for replaying a
// retried request. Requests without a body are returned unchanged.
func rewind(req *http.Request) (*http.Request, error) {
	if req.GetBody == nil {
		return req, nil
	}

	body, err := req.GetBody()
	if err != nil {
		return nil, fmt.Errorf("rewinding request body for retry: %w", err)
	}

	clone := req.Clone(req.Context())
	clone.Body = body

	return clone, nil
}

// backoff returns how long to wait before the next attempt, honouring a
// Retry-After header when the server sends one.
func (t *retryTransport) backoff(attempt int, resp *http.Response) time.Duration {
	if d, ok := retryAfter(resp); ok {
		return min(d, t.maxDelay)
	}

	// Exponential backoff with full jitter. Jitter matters here because a crawl
	// hits many companies on the same host at once: without it, rate-limited
	// requests retry in lockstep and get rate-limited again together.
	//
	// The shift is capped because a large WithMaxAttempts would otherwise
	// overflow the duration and produce a negative delay.
	shift := min(attempt-1, 32)

	backoff := min(t.baseDelay<<shift, t.maxDelay)
	if backoff <= 0 {
		backoff = t.maxDelay
	}

	return backoff/2 + rand.N(backoff/2+1)
}

// errStatus reports a retryable HTTP status as an error.
//
// This is diagnostic only: it never reaches a caller. When retries are exhausted
// on a retryable status the real response is returned instead, deliberately, so
// callers can report the actual status rather than a synthesised error. This type
// exists to carry that status into the logs.
type errStatus int

func (e errStatus) Error() string {
	return "unexpected status " + strconv.Itoa(int(e)) + " " + http.StatusText(int(e))
}

// retryableStatus reports whether a status code is worth retrying: the server
// is overloaded, rate-limiting, or briefly unavailable.
func retryableStatus(code int) bool {
	switch code {
	case http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	}

	return false
}

// replayable reports whether a request can safely be sent again.
func replayable(req *http.Request) bool {
	// GET and HEAD carry no body. Other methods are retried only when the body
	// can be rewound, which net/http arranges for in-memory bodies.
	switch req.Method {
	case http.MethodGet, http.MethodHead:
		return true
	}

	return req.Body == nil || req.Body == http.NoBody || req.GetBody != nil
}

// retryAfter returns the delay requested by a Retry-After header, if present.
// It accepts both the delay-seconds and HTTP-date forms.
func retryAfter(resp *http.Response) (time.Duration, bool) {
	if resp == nil {
		return 0, false
	}

	value := resp.Header.Get("Retry-After")
	if value == "" {
		return 0, false
	}

	if secs, err := strconv.Atoi(value); err == nil {
		if secs < 0 {
			return 0, false
		}

		return time.Duration(secs) * time.Second, true
	}

	if when, err := http.ParseTime(value); err == nil {
		if d := time.Until(when); d > 0 {
			return d, true
		}
	}

	return 0, false
}

// drain reads and closes a response body so its connection can be reused.
func drain(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}

	// Bounded so a huge error page cannot stall a retry.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	_ = resp.Body.Close()
}

// sleep waits for d, or returns early if ctx is cancelled.
func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
