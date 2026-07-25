package httpx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newTestClient builds a client with negligible delays so tests do not sleep.
func newTestClient(t *testing.T, opts ...Option) *http.Client {
	t.Helper()

	return NewClient(append([]Option{
		WithBaseDelay(time.Millisecond),
		WithMaxDelay(2 * time.Millisecond),
	}, opts...)...)
}

func TestRetriesTransientStatusThenSucceeds(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	resp, err := newTestClient(t).Get(srv.URL)
	if err != nil {
		t.Fatalf("Get() error = %v, want nil", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	if got := calls.Load(); got != 3 {
		t.Errorf("server calls = %d, want 3 (two failures then success)", got)
	}
}

func TestReturnsLastResponseWhenRetriesExhausted(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	resp, err := newTestClient(t, WithMaxAttempts(3)).Get(srv.URL)
	if err != nil {
		t.Fatalf("Get() error = %v, want nil (the final response should be returned)", err)
	}
	defer resp.Body.Close()

	// Callers rely on seeing the real status code so they can report *why* a
	// company failed, rather than a generic retry error.
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}

	if got := calls.Load(); got != 3 {
		t.Errorf("server calls = %d, want 3", got)
	}
}

func TestDoesNotRetryClientErrors(t *testing.T) {
	t.Parallel()

	// A 404 means the company's board is gone. Retrying it wastes the crawl's
	// time budget on an answer that will not change.
	for _, code := range []int{http.StatusNotFound, http.StatusGone, http.StatusForbidden, http.StatusUnauthorized} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			t.Parallel()

			var calls atomic.Int64

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(code)
			}))
			defer srv.Close()

			resp, err := newTestClient(t).Get(srv.URL)
			if err != nil {
				t.Fatalf("Get() error = %v, want nil", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != code {
				t.Errorf("status = %d, want %d", resp.StatusCode, code)
			}

			if got := calls.Load(); got != 1 {
				t.Errorf("server calls = %d, want 1 (must not retry)", got)
			}
		})
	}
}

func TestHonoursRetryAfterSeconds(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Base delay is 1ms, so a respected Retry-After of 1s is clearly
	// distinguishable from plain exponential backoff. Max delay must be raised
	// above the Retry-After value, or the cap would mask what is under test.
	start := time.Now()

	resp, err := newTestClient(t, WithMaxDelay(5*time.Second)).Get(srv.URL)
	if err != nil {
		t.Fatalf("Get() error = %v, want nil", err)
	}
	defer resp.Body.Close()

	if elapsed := time.Since(start); elapsed < time.Second {
		t.Errorf("elapsed = %v, want >= 1s (Retry-After was ignored)", elapsed)
	}
}

func TestRetryAfterIsCappedByMaxDelay(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			// A board asking us to wait an hour must not stall the whole crawl.
			w.Header().Set("Retry-After", "3600")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	start := time.Now()

	resp, err := newTestClient(t, WithMaxDelay(20*time.Millisecond)).Get(srv.URL)
	if err != nil {
		t.Fatalf("Get() error = %v, want nil", err)
	}
	defer resp.Body.Close()

	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("elapsed = %v, want capped near max delay", elapsed)
	}
}

func TestCancellationStopsRetrying(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	// A long backoff means the request is certainly sleeping between attempts
	// when the context expires.
	client := NewClient(WithBaseDelay(10*time.Second), WithMaxDelay(10*time.Second))

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}

	start := time.Now()

	if _, err := client.Do(req); err == nil {
		t.Fatal("Do() error = nil, want a context error")
	}

	// The point: cancellation must interrupt the backoff sleep, not be noticed
	// only after it finishes.
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("elapsed = %v, want cancellation to interrupt the backoff sleep", elapsed)
	}
}

func TestSetsUserAgent(t *testing.T) {
	t.Parallel()

	got := make(chan string, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- r.Header.Get("User-Agent")
	}))
	defer srv.Close()

	resp, err := newTestClient(t).Get(srv.URL)
	if err != nil {
		t.Fatalf("Get() error = %v, want nil", err)
	}
	defer resp.Body.Close()

	if ua := <-got; !strings.HasPrefix(ua, "job-hunter-toolkit/") {
		t.Errorf("User-Agent = %q, want it to identify this tool", ua)
	}
}

func TestDoesNotOverrideCallerUserAgent(t *testing.T) {
	t.Parallel()

	got := make(chan string, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- r.Header.Get("User-Agent")
	}))
	defer srv.Close()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}
	req.Header.Set("User-Agent", "custom-agent/2.0")

	resp, err := newTestClient(t).Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v, want nil", err)
	}
	defer resp.Body.Close()

	if ua := <-got; ua != "custom-agent/2.0" {
		t.Errorf("User-Agent = %q, want the caller's value preserved", ua)
	}
}

func TestRetryAfterParsing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		header string
		want   bool
	}{
		{name: "seconds", header: "5", want: true},
		{name: "zero seconds", header: "0", want: true},
		{name: "negative seconds", header: "-5", want: false},
		{name: "garbage", header: "soon", want: false},
		{name: "absent", header: "", want: false},
		{name: "past http date", header: "Mon, 02 Jan 2006 15:04:05 GMT", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			resp := &http.Response{Header: http.Header{}}
			if tt.header != "" {
				resp.Header.Set("Retry-After", tt.header)
			}

			if _, ok := retryAfter(resp); ok != tt.want {
				t.Errorf("retryAfter(%q) ok = %v, want %v", tt.header, ok, tt.want)
			}
		})
	}
}

func TestReplayable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		req  *http.Request
		want bool
	}{
		{
			name: "GET",
			req:  &http.Request{Method: http.MethodGet},
			want: true,
		},
		{
			name: "HEAD",
			req:  &http.Request{Method: http.MethodHead},
			want: true,
		},
		{
			name: "POST with no body",
			req:  &http.Request{Method: http.MethodPost},
			want: true,
		},
		{
			name: "POST with rewindable body",
			req: &http.Request{
				Method:  http.MethodPost,
				Body:    http.NoBody,
				GetBody: func() (io.ReadCloser, error) { return http.NoBody, nil },
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := replayable(tt.req); got != tt.want {
				t.Errorf("replayable() = %v, want %v", got, tt.want)
			}
		})
	}
}

// spyTransport records what each attempt actually handed to the base transport.
type spyTransport struct {
	mu       sync.Mutex
	bodies   []string
	statuses []int
}

func (s *spyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		body, _ = io.ReadAll(req.Body)
		_ = req.Body.Close()
	}

	s.mu.Lock()
	s.bodies = append(s.bodies, string(body))
	attempt := len(s.bodies)
	s.mu.Unlock()

	// Fail the first two attempts so the retry path is exercised.
	status := http.StatusServiceUnavailable
	if attempt >= 3 {
		status = http.StatusOK
	}

	s.mu.Lock()
	s.statuses = append(s.statuses, status)
	s.mu.Unlock()

	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     http.Header{},
		Body:       http.NoBody,
		Request:    req,
	}, nil
}

func (s *spyTransport) seen() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return slices.Clone(s.bodies)
}

func TestRetryRewindsRequestBody(t *testing.T) {
	t.Parallel()

	// Without an explicit rewind, attempts 2 and 3 send an empty body while
	// still declaring the original Content-Length. net/http's own retry logic
	// hides this when it is the base transport, so the bug only shows through a
	// wrapping transport, which is exactly what WithTransport allows, and what
	// an instrumented (otelhttp) client would be.
	spy := &spyTransport{}

	client := NewClient(
		WithTransport(spy),
		WithBaseDelay(time.Millisecond),
		WithMaxDelay(2*time.Millisecond),
		WithMaxAttempts(3),
	)

	const payload = `{"limit":20,"offset":40}`

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		"https://example.test/wday/cxs/acme/site/jobs", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v, want nil", err)
	}
	defer resp.Body.Close()

	bodies := spy.seen()

	if len(bodies) != 3 {
		t.Fatalf("base transport saw %d attempts, want 3: %q", len(bodies), bodies)
	}

	// Every attempt must carry the full payload. A pagination body that arrives
	// empty asks the server for the wrong page.
	for i, got := range bodies {
		if got != payload {
			t.Errorf("attempt %d body = %q, want %q", i+1, got, payload)
		}
	}
}

func TestDoesNotRetryUnrewindableBody(t *testing.T) {
	t.Parallel()

	// A body that cannot be rewound must be sent exactly once: replaying it
	// would transmit a truncated request. http.NewRequest only populates GetBody
	// for known body types, so an opaque io.Reader leaves GetBody nil.
	spy := &spyTransport{}

	client := NewClient(
		WithTransport(spy),
		WithBaseDelay(time.Millisecond),
		WithMaxAttempts(4),
	)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		"https://example.test/", io.NopCloser(strings.NewReader("streamed-payload")))
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}

	if req.GetBody != nil {
		t.Fatal("GetBody is set; this test needs an unrewindable body")
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v, want nil", err)
	}
	defer resp.Body.Close()

	// The retryable 503 must be handed back rather than retried.
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}

	if got := len(spy.seen()); got != 1 {
		t.Errorf("base transport saw %d attempts, want 1", got)
	}
}

func TestReplayableRejectsUnrewindableBody(t *testing.T) {
	t.Parallel()

	// The false branch of replayable is the whole reason it exists.
	req := &http.Request{
		Method:  http.MethodPost,
		Body:    io.NopCloser(strings.NewReader("x")),
		GetBody: nil,
	}

	if replayable(req) {
		t.Error("replayable() = true, want false for a body that cannot be rewound")
	}
}

func TestBackoffGuardsAgainstOverflow(t *testing.T) {
	t.Parallel()

	// The earlier version of this test used a 1s base delay, where 1s<<32 still
	// fits in an int64; so the overflow guard it was named for never ran. These
	// base delays genuinely wrap negative without the guard.
	for _, base := range []time.Duration{30 * time.Second, time.Minute, time.Hour} {
		t.Run(base.String(), func(t *testing.T) {
			t.Parallel()

			tr := &retryTransport{
				baseDelay:   base,
				maxDelay:    30 * time.Second,
				maxAttempts: 200,
			}

			for attempt := 1; attempt <= 200; attempt++ {
				got := tr.backoff(attempt, nil)

				if got <= 0 {
					t.Fatalf("backoff(%d) with base %v = %v, want positive", attempt, base, got)
				}

				if got > tr.maxDelay {
					t.Fatalf("backoff(%d) with base %v = %v, want at most %v", attempt, base, got, tr.maxDelay)
				}
			}
		})
	}
}

func TestRetriesConnectionFailureRepeatedly(t *testing.T) {
	t.Parallel()

	// The previous version of this test only asserted err != nil, so it would
	// have passed with the retry loop deleted entirely.
	var attempts atomic.Int64

	failing := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		attempts.Add(1)

		return nil, errors.New("dial tcp: connection refused")
	})

	client := NewClient(
		WithTransport(failing),
		WithBaseDelay(time.Millisecond),
		WithMaxDelay(2*time.Millisecond),
		WithMaxAttempts(3),
	)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://example.test/", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}

	if _, err := client.Do(req); err == nil {
		t.Fatal("Do() error = nil, want a connection error")
	}

	if got := attempts.Load(); got != 3 {
		t.Errorf("attempts = %d, want 3", got)
	}
}

// roundTripperFunc adapts a function to http.RoundTripper.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestPerHostLimitBoundsConcurrency(t *testing.T) {
	t.Parallel()

	// The measured motivation: every PeopleForce tenant is a subdomain of one
	// platform and every Workable board shares apply.workable.com, so raising
	// overall crawl concurrency rate-limited those hosts into looking dead.
	const limit = 3

	var (
		active atomic.Int64
		peak   atomic.Int64
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := active.Add(1)
		for {
			old := peak.Load()
			if n <= old || peak.CompareAndSwap(old, n) {
				break
			}
		}

		time.Sleep(20 * time.Millisecond)
		active.Add(-1)

		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := NewClient(WithPerHostLimit(limit))

	var wg sync.WaitGroup

	for range 24 {
		wg.Add(1)

		go func() {
			defer wg.Done()

			resp, err := client.Get(srv.URL)
			if err != nil {
				t.Errorf("Get() error = %v", err)

				return
			}

			// Closing the body is what releases the slot.
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}()
	}

	wg.Wait()

	if got := peak.Load(); got > limit {
		t.Errorf("peak concurrent requests to one host = %d, want at most %d", got, limit)
	}

	if peak.Load() == 0 {
		t.Error("no requests reached the server")
	}
}

func TestPerHostLimitIsPerHostNotGlobal(t *testing.T) {
	t.Parallel()

	// Two different hosts must not contend with each other, or the limit would
	// silently become a global cap and serialise the whole crawl.
	var (
		hits  atomic.Int64
		start = make(chan struct{})
	)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until both hosts have a request in flight. If the limiter were
		// global with limit 1, this would deadlock and the test would time out.
		if hits.Add(1) == 2 {
			close(start)
		}

		<-start

		w.WriteHeader(http.StatusOK)
	})

	srvA := httptest.NewServer(handler)
	defer srvA.Close()

	srvB := httptest.NewServer(handler)
	defer srvB.Close()

	client := NewClient(WithPerHostLimit(1))

	var wg sync.WaitGroup

	for _, url := range []string{srvA.URL, srvB.URL} {
		wg.Add(1)

		go func(url string) {
			defer wg.Done()

			resp, err := client.Get(url)
			if err != nil {
				t.Errorf("Get(%s) error = %v", url, err)

				return
			}

			_ = resp.Body.Close()
		}(url)
	}

	wg.Wait()

	if got := hits.Load(); got != 2 {
		t.Errorf("server hits = %d, want 2", got)
	}
}

func TestPerHostLimitReleasesSlotOnError(t *testing.T) {
	t.Parallel()

	// A failed request must give its slot back, or a host that errors would
	// permanently starve every later request to it.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	client := NewClient(
		WithPerHostLimit(1),
		WithBaseDelay(time.Millisecond),
		WithMaxDelay(2*time.Millisecond),
		WithMaxAttempts(2),
	)

	// Several sequential failures: if the slot leaked, the second would block
	// until the context deadline instead of failing fast.
	for range 3 {
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			cancel()
			t.Fatalf("NewRequestWithContext() error = %v", err)
		}

		if _, err := client.Do(req); err == nil {
			cancel()
			t.Fatal("Do() error = nil, want a connection error")
		}

		cancel()
	}
}

func TestServicePoliciesMatchBackendTopology(t *testing.T) {
	t.Parallel()

	request := func(rawURL string) *http.Request {
		t.Helper()

		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, rawURL, nil)
		if err != nil {
			t.Fatalf("NewRequestWithContext(%q) error = %v", rawURL, err)
		}

		return req
	}

	peopleforceA := servicePolicyFor(request("https://alpha.peopleforce.io/careers"), 8)
	peopleforceB := servicePolicyFor(request("https://beta.peopleforce.io/careers"), 8)
	if peopleforceA.key != peopleforceB.key {
		t.Errorf("PeopleForce keys = %q and %q, want one shared backend", peopleforceA.key, peopleforceB.key)
	}

	workdayA := servicePolicyFor(request("https://alpha.wd1.myworkdayjobs.com/jobs"), 8)
	workdayB := servicePolicyFor(request("https://beta.wd5.myworkdayjobs.com/jobs"), 8)
	if workdayA.key == workdayB.key {
		t.Errorf("Workday keys are both %q, want tenant-isolated infrastructure", workdayA.key)
	}

	workable := servicePolicyFor(request("https://apply.workable.com/api/v1/widget/accounts/acme"), 8)
	if workable.maxConcurrent != 2 {
		t.Errorf("Workable concurrency = %d, want 2", workable.maxConcurrent)
	}
	if workable.interval <= 0 {
		t.Errorf("Workable interval = %v, want paced requests", workable.interval)
	}
}

func TestServiceLimiterPacesAndCapsLongCooldown(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	var logs bytes.Buffer
	base := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		status := http.StatusOK
		header := http.Header{}

		if calls.Add(1) == 1 {
			status = http.StatusTooManyRequests
			// Real Workable responses have carried day-long values. The limiter
			// must expose that in logs but cap how long it stalls the crawl.
			header.Set("Retry-After", "76447")
		}

		return &http.Response{
			StatusCode: status,
			Status:     http.StatusText(status),
			Header:     header,
			Body:       http.NoBody,
			Request:    req,
		}, nil
	})

	client := NewClient(
		WithTransport(base),
		WithMaxAttempts(1),
		WithMaxDelay(20*time.Millisecond),
		WithLogger(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		}))),
	)

	const endpoint = "https://apply.workable.com/api/v1/widget/accounts/acme"

	first, err := client.Get(endpoint)
	if err != nil {
		t.Fatalf("first Get() error = %v", err)
	}
	_ = first.Body.Close()

	start := time.Now()
	second, err := client.Get(endpoint)
	if err != nil {
		t.Fatalf("second Get() error = %v", err)
	}
	_ = second.Body.Close()

	elapsed := time.Since(start)
	if elapsed < 20*time.Millisecond {
		t.Errorf("second request waited %v, want at least the capped cooldown", elapsed)
	}
	if elapsed > time.Second {
		t.Errorf("second request waited %v, want the day-long Retry-After capped", elapsed)
	}

	logOutput := logs.String()
	if !strings.Contains(logOutput, "retry_after=76447") {
		t.Errorf("logs = %q, want the original Retry-After value", logOutput)
	}
	if !strings.Contains(logOutput, "service=apply.workable.com") {
		t.Errorf("logs = %q, want the affected service key", logOutput)
	}
}

func TestParseProxyURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{name: "HTTP", raw: "http://proxy.example:8080"},
		{name: "HTTPS with credentials", raw: "https://user:secret@proxy.example:8443"},
		{name: "SOCKS5", raw: "socks5://127.0.0.1:1080"},
		{name: "missing scheme", raw: "proxy.example:8080", wantErr: true},
		{name: "unsupported scheme", raw: "file:///tmp/socket", wantErr: true},
		{name: "path", raw: "https://proxy.example/not-a-proxy-path", wantErr: true},
		{name: "query", raw: "https://proxy.example?token=secret", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			proxyURL, err := ParseProxyURL(tt.raw)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseProxyURL(%q) error = %v, wantErr %v", tt.raw, err, tt.wantErr)
			}

			if err == nil && strings.Contains(proxyEndpoint(proxyURL), "secret") {
				t.Errorf("proxyEndpoint(%q) exposed credentials", tt.raw)
			}
		})
	}
}

func TestProxyPoolIsStickyAndDistributesBoards(t *testing.T) {
	t.Parallel()

	proxies := make([]*url.URL, 3)
	for i, raw := range []string{
		"https://one.example:8443",
		"https://two.example:8443",
		"socks5://three.example:1080",
	} {
		proxyURL, err := ParseProxyURL(raw)
		if err != nil {
			t.Fatalf("ParseProxyURL(%q) error = %v", raw, err)
		}
		proxies[i] = proxyURL
	}

	selectProxy := proxyPool(proxies)
	used := make(map[string]struct{})

	for i := range 100 {
		rawURL := fmt.Sprintf("https://apply.workable.com/api/v1/widget/accounts/company-%d", i)
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, rawURL, nil)
		if err != nil {
			t.Fatalf("NewRequestWithContext() error = %v", err)
		}

		first, err := selectProxy(req)
		if err != nil {
			t.Fatalf("proxy selection error = %v", err)
		}
		second, err := selectProxy(req)
		if err != nil {
			t.Fatalf("repeat proxy selection error = %v", err)
		}

		if first.String() != second.String() {
			t.Errorf("board %q moved from %q to %q", rawURL, first, second)
		}

		used[first.String()] = struct{}{}
	}

	if len(used) != len(proxies) {
		t.Errorf("100 boards used %d proxies, want all %d", len(used), len(proxies))
	}
}

func TestExplicitProxyWithCustomTransportFailsClosed(t *testing.T) {
	t.Parallel()

	proxyURL, err := ParseProxyURL("https://proxy.example:8443")
	if err != nil {
		t.Fatalf("ParseProxyURL() error = %v", err)
	}

	var directCalls atomic.Int64
	direct := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		directCalls.Add(1)
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       http.NoBody,
			Header:     http.Header{},
			Request:    req,
		}, nil
	})

	client := NewClient(
		WithTransport(direct),
		WithProxyURL(proxyURL),
		WithMaxAttempts(1),
	)

	if _, err := client.Get("https://example.test/"); err == nil {
		t.Fatal("Get() error = nil, want proxy/custom-transport configuration error")
	}

	if got := directCalls.Load(); got != 0 {
		t.Errorf("custom transport made %d direct calls, want 0 when proxy setup cannot be honored", got)
	}
}
