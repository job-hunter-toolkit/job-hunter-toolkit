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
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultUserAgent identifies this tool to job boards. Sending a real
// User-Agent is both polite and practical: some boards reject requests that
// do not set one.
//
// The contact URL is not decoration. Several canonical data providers require a
// reachable contact in their access policy (SEC EDGAR and the Wikimedia APIs
// both reject or throttle agents without one), and a crawler covering ~1,772
// companies owes any board it annoys a way to tell us to stop that is not
// "block the IP range and hope". It points at the issue tracker rather than a
// person's mailbox so the address stays valid as maintainers change.
const DefaultUserAgent = "job-hunter-toolkit/1.0 (+https://github.com/job-hunter-toolkit/job-hunter-toolkit; " +
	"contact: https://github.com/job-hunter-toolkit/job-hunter-toolkit/issues)"

// DefaultPerHostLimit bounds concurrent requests to any single service key.
//
// A crawl fans out over companies, but companies are not spread evenly over
// hosts: every Workable board is served by apply.workable.com, so raising
// overall concurrency just rate-limits that one host. Measured: 56 Workable
// sources failed with HTTP 429 purely from self-inflicted load, which looks
// identical to a dead board in a health report.
//
// Most requests key on the exact host. servicePolicyFor groups only known
// shared backends such as BambooHR and PeopleForce, while deliberately
// leaving Workday tenant hosts independent.
//
// This, and not the size of the crawl's worker pool, is the politeness ceiling:
// internal.DefaultConcurrency decides how many sources are in flight, but no
// number of workers can put more than this many requests on one backend.
const DefaultPerHostLimit = 4

// Defaults for [NewClient].
const (
	defaultMaxAttempts = 4
	defaultBaseDelay   = 500 * time.Millisecond
	defaultMaxDelay    = 30 * time.Second
	defaultTimeout     = 2 * time.Minute

	// defaultSlotWaitWarn is how long a request may wait for a per-service slot
	// before the limiter says so.
	//
	// A slot is held until the response body is closed, so an adapter that
	// defers Close outside its pagination loop parks every later request to that
	// service until the request context expires. That happened to every large
	// Workday tenant and cost minutes of crawl budget per tenant while producing
	// no log line at all. Waiting this long for a slot is not normal contention.
	defaultSlotWaitWarn = 30 * time.Second

	// shedAfterRejections is how many consecutive 429s from one service open its
	// circuit breaker.
	//
	// A single logical request can produce defaultMaxAttempts (4) 429s on its
	// own, so the threshold sits just above that: tripping requires at least two
	// separate requests to have found nothing but 429s, with no successful
	// response from that service in between.
	shedAfterRejections = 5

	// maxShedWindow bounds how long a service's breaker stays open.
	//
	// peopleforce.io has answered with Retry-After: 3510 (58 minutes), which is
	// longer than the nightly crawl's entire budget. Honouring that exactly
	// would be indistinguishable from marking the platform dead for the run, and
	// would leave a long-lived process unable to recover, so the window is
	// capped here.
	maxShedWindow = 15 * time.Minute
)

// ErrRateLimited reports that a service is being shed because it answered HTTP
// 429 repeatedly. Callers that need to tell "the board rate-limited us" apart
// from "the board is broken" can test for it with [errors.Is].
var ErrRateLimited = errors.New("service is rate limiting this crawl")

// RateLimitedError is the error returned for a request that was never sent
// because its service's circuit breaker was open. It unwraps to
// [ErrRateLimited].
type RateLimitedError struct {
	// Service is the limiter key that is shedding, which for a shared backend
	// covers every tenant on it: "peopleforce.io", not one tenant's subdomain.
	Service string

	// URL is the request that was not sent, with any credentials redacted.
	URL string

	// RetryAfter is how much of the shedding window remains.
	RetryAfter time.Duration
}

// Error implements the error interface.
func (e *RateLimitedError) Error() string {
	return fmt.Sprintf("httpx: not sending %s: service %q answered HTTP 429 repeatedly; shedding its requests for another %s",
		e.URL, e.Service, e.RetryAfter.Round(time.Second))
}

// Unwrap returns [ErrRateLimited].
func (e *RateLimitedError) Unwrap() error { return ErrRateLimited }

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

// WithProxyURL routes requests through proxyURL.
//
// Credentials in the URL are supported by net/http but are never written to
// logs. This option is fail-closed when combined with a non-*http.Transport:
// requests return an error rather than unexpectedly bypassing the proxy.
func WithProxyURL(proxyURL *url.URL) Option {
	return WithProxyURLs(proxyURL)
}

// WithProxyURLs distributes job boards deterministically across proxyURLs.
//
// Selection is sticky per board: pagination and retries use the same proxy.
// The pool does not fail over automatically, because replaying a request through
// another route can duplicate writes and conceal a broken proxy.
func WithProxyURLs(proxyURLs ...*url.URL) Option {
	return func(t *retryTransport) {
		t.proxyURLs = t.proxyURLs[:0]

		for _, proxyURL := range proxyURLs {
			if proxyURL != nil {
				cloned := *proxyURL
				t.proxyURLs = append(t.proxyURLs, &cloned)
			}
		}
	}
}

// WithPerHostLimit bounds how many requests may be in flight to a single
// service. Known shared backends are grouped even when tenants use different
// subdomains; unknown services use their exact host. A value below 1 disables
// the limit.
func WithPerHostLimit(limit int) Option {
	return func(t *retryTransport) {
		t.perHostLimit = limit
	}
}

// NewClient returns an HTTP client that retries transient failures and applies
// service-aware concurrency, pacing, and cooldown limits.
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
	//
	// Deliberately unchanged: this clone carries ForceAttemptHTTP2 from
	// http.DefaultTransport, so h2 is negotiated and a single connection
	// multiplexes; and DisableCompression stays false with no adapter setting
	// Accept-Encoding, so net/http's transparent gzip is already active on every
	// request. Neither needed touching.
	base.MaxIdleConns = 200
	base.MaxIdleConnsPerHost = 8

	t := &retryTransport{
		base:         base,
		maxAttempts:  defaultMaxAttempts,
		baseDelay:    defaultBaseDelay,
		maxDelay:     defaultMaxDelay,
		logger:       slog.New(slog.DiscardHandler),
		userAgent:    DefaultUserAgent,
		perHostLimit: DefaultPerHostLimit,
	}

	for _, opt := range opts {
		opt(t)
	}

	// The idle pool must not be scarcer than the concurrency allowed to one
	// service, or the requests past the eighth would dial a fresh connection
	// every time and the limit meant to reduce load on a board would instead
	// increase its connection churn. At the default limit of 4 this is a no-op;
	// it only moves if --per-host-limit is raised past 8.
	if t.perHostLimit > base.MaxIdleConnsPerHost {
		base.MaxIdleConnsPerHost = t.perHostLimit
	}

	if len(t.proxyURLs) > 0 {
		baseTransport, ok := t.base.(*http.Transport)
		if !ok {
			t.base = failingTransport{err: fmt.Errorf("httpx: explicit proxy cannot be combined with transport type %T", t.base)}
		} else {
			baseTransport = baseTransport.Clone()
			baseTransport.Proxy = proxyPool(t.proxyURLs)
			t.base = baseTransport

			t.logger.Info("using explicit HTTP proxy pool",
				slog.Int("proxies", len(t.proxyURLs)),
				slog.String("selection", "sticky-per-board"),
			)
		}
	}

	// The host limiter wraps the base transport rather than the retry loop, so
	// each individual attempt is throttled, including retries, which are
	// exactly what a rate-limited host does not want more of.
	//
	// retryTransport keeps its own reference so it can tell whether the shared
	// cooldown is in play; see the 429 handling in its RoundTrip.
	if t.perHostLimit > 0 {
		t.limiter = &hostLimiter{
			base:         t.base,
			limit:        t.perHostLimit,
			maxDelay:     t.maxDelay,
			logger:       t.logger,
			slotWaitWarn: defaultSlotWaitWarn,
		}

		t.base = t.limiter
	}

	return &http.Client{
		Transport: t,
		Timeout:   defaultTimeout,
	}
}

// hostLimiter applies service-aware concurrency, pacing, and shared cooldowns.
//
// The name is retained for compatibility with the public WithPerHostLimit
// option, but known multi-tenant backends may share one limiter key across
// subdomains. Unknown services remain isolated by exact host.
type hostLimiter struct {
	base     http.RoundTripper
	limit    int
	maxDelay time.Duration
	logger   *slog.Logger

	// slotWaitWarn is defaultSlotWaitWarn outside tests, which shorten it so a
	// leaked response body is observable without a 30-second test.
	slotWaitWarn time.Duration

	mu     sync.Mutex
	states map[string]*limitState
}

type limitState struct {
	key      string
	sem      chan struct{}
	interval time.Duration
	cooldown time.Duration

	mu   sync.Mutex
	next time.Time

	// rejections counts 429s since this service last answered with anything
	// else, and shedUntil is the circuit breaker those 429s open.
	rejections int
	shedUntil  time.Time
}

type servicePolicy struct {
	key           string
	maxConcurrent int
	interval      time.Duration
	cooldown      time.Duration
}

// ServicePolicy reports how the client will treat one service, for callers that
// need to check coverage rather than make a request.
//
// It exists because the interesting property is negative: a platform that
// matches no policy is not obviously broken, it simply runs unpaced, and six
// platforms were registered that way at once without anything failing. A test
// over the source registry can now assert every platform is accounted for.
type ServicePolicy struct {
	// Key is the limiter identity. Tenants of one shared backend must share it.
	Key string

	// MaxConcurrent bounds in-flight requests to Key.
	MaxConcurrent int

	// Interval is the minimum spacing between requests to Key. Zero means the
	// generic policy matched and no pacing is applied.
	Interval time.Duration

	// Cooldown is how long a 429 from Key delays its siblings.
	Cooldown time.Duration
}

// ServicePolicyForHost reports the policy that would apply to a request to host.
func ServicePolicyForHost(host string, defaultLimit int) ServicePolicy {
	req := &http.Request{URL: &url.URL{Scheme: "https", Host: host}}
	policy := servicePolicyFor(req, defaultLimit)

	return ServicePolicy{
		Key:           policy.key,
		MaxConcurrent: policy.maxConcurrent,
		Interval:      policy.interval,
		Cooldown:      policy.cooldown,
	}
}

// servicePolicyFor captures only platform behavior verified in this project.
// Unknown hosts get the generic exact-host policy instead of being guessed into
// a shared bucket.
// sharedBackendHosts maps a hostname to the limiter key it should share.
//
// Every other shared backend in this file is recognised by a suffix, because the
// vendor puts its tenants on its own domain. Some platforms do the opposite:
// the tenant keeps its own brand domain and points it at the vendor, so there is
// no suffix to match and the hostnames have nothing textually in common.
// Measured 2026-07-28, www.att.jobs and jobs.veolia.com both resolve to
// 23.215.11.242 and careers.munichre.com to 23.215.11.240 -- one backend behind
// twenty-nine unrelated domains.
//
// The list lives in the adapter that owns those hosts and is registered here at
// init, rather than being copied into this file. internal/shard already
// established why: a second hand-maintained list drifts from the first, and the
// drift shows up as a rate-limit problem, which is the hardest kind to diagnose.
var sharedBackendHosts = map[string]string{}

// RegisterSharedBackend declares that the given hosts are one backend behind
// different names, and must therefore share a limiter key.
//
// It is called from an adapter's init, before any request is made, and is not
// safe to call concurrently with a crawl. Registering a host that genuinely has
// its own infrastructure is the expensive mistake here: it would throttle
// unrelated employers to four requests between them, which is the error
// TestTenantIsolatedBackendsStayIndependent exists to prevent. Register only
// what has been shown to share a backend.
func RegisterSharedBackend(key string, hosts ...string) {
	for _, host := range hosts {
		sharedBackendHosts[strings.ToLower(host)] = key
	}
}

func servicePolicyFor(req *http.Request, defaultLimit int) servicePolicy {
	host := strings.ToLower(req.URL.Hostname())
	policy := servicePolicy{
		key:           strings.ToLower(req.URL.Host),
		maxConcurrent: defaultLimit,
		cooldown:      5 * time.Second,
	}

	// Checked before the suffix arms below, since a registered host is a
	// statement of measured fact about infrastructure and nothing in the switch
	// can know better.
	if key, ok := sharedBackendHosts[host]; ok {
		policy.key = key
		policy.maxConcurrent = min(defaultLimit, 4)
		policy.interval = 25 * time.Millisecond
		policy.cooldown = 10 * time.Second

		return policy
	}

	// The min(defaultLimit, N) caps below are ceilings, not targets. N is what
	// this project has measured as safe for that backend, so --per-host-limit
	// can lower every service but cannot raise a named one past its measured
	// value; only hosts with no measured ceiling (Workday and Phenom tenants,
	// which are genuinely tenant-isolated infrastructure) follow the flag
	// upward. The asymmetry is deliberate: the dial exists to be turned down
	// when a board complains, not up.
	switch {
	case host == "apply.workable.com":
		policy.maxConcurrent = min(defaultLimit, 2)
		policy.interval = 100 * time.Millisecond
		policy.cooldown = 30 * time.Second
	case host == "ats.rippling.com",
		host == "jobs.gem.com",
		host == "api.smartrecruiters.com",
		host == "api.lever.co",
		host == "boards-api.greenhouse.io":
		policy.maxConcurrent = min(defaultLimit, 4)
		policy.interval = 25 * time.Millisecond
		policy.cooldown = 10 * time.Second
	case host == "api.ashbyhq.com",
		host == "jobs.jobvite.com":
		// Both are single-host multi-tenant backends: all 418 Ashby boards (the
		// second-largest platform here) and all 33 Jobvite boards are served by
		// one hostname each. They were already sharing a limiter key, but only
		// by accident of the generic exact-host policy, which leaves interval at
		// zero, so the only thing standing between api.ashbyhq.com and a burst
		// was how many workers happened to be free. Raising the worker pool made
		// that gap load-bearing, so name them explicitly and give them the same
		// 25ms spacing and 10s cooldown as the other shared JSON backends.
		policy.maxConcurrent = min(defaultLimit, 4)
		policy.interval = 25 * time.Millisecond
		policy.cooldown = 10 * time.Second
	case strings.HasSuffix(host, ".peopleforce.io"):
		policy.key = "peopleforce.io"
		policy.maxConcurrent = min(defaultLimit, 2)
		policy.interval = 100 * time.Millisecond
		policy.cooldown = 15 * time.Second
	case strings.HasSuffix(host, ".bamboohr.com"):
		policy.key = "bamboohr.com"
		policy.maxConcurrent = min(defaultLimit, 4)
		policy.interval = 25 * time.Millisecond
		policy.cooldown = 10 * time.Second
	case strings.HasSuffix(host, ".jibeapply.com"):
		policy.key = "jibeapply.com"
		policy.maxConcurrent = min(defaultLimit, 4)
		policy.interval = 25 * time.Millisecond
		policy.cooldown = 10 * time.Second
	case strings.HasSuffix(host, ".teamtailor.com"),
		strings.HasSuffix(host, ".recruitee.com"),
		strings.HasSuffix(host, ".pinpointhq.com"),
		strings.HasSuffix(host, ".jobs.personio.de"),
		strings.HasSuffix(host, ".breezy.hr"):
		// Four SMB platforms that give every tenant its own subdomain on one
		// shared backend, exactly like bamboohr.com and peopleforce.io above.
		// The generic policy keys on the exact host, so without this each
		// tenant would get a private limiter and the platform would see
		// tenants*4 concurrent requests from one crawl. That is how 56 Workable
		// boards were rate-limited into looking dead, which is the incident the
		// shared keys exist to prevent. One key per platform.
		//
		// These four were ~35 tenants each when this policy was written. After
		// probing the staged candidate lists they are 970 Personio, 492
		// Recruitee, 117 Pinpoint and 34 Teamtailor, so the pressure this
		// prevents is now up to 3,880 concurrent requests rather than 140, and
		// the case for one key per platform is correspondingly stronger.
		//
		// It has a cost worth naming, because it is the same shape as the tail
		// docs/research/crawl-performance.md measured on Greenhouse and Ashby:
		// 970 Personio sources can only make progress four at a time no matter
		// how many workers are idle, so this platform alone is ~243 sequential
		// rounds. That is affordable in a 330-minute budget and it is NOT a
		// reason to raise maxConcurrent -- the ceiling is politeness, not
		// scheduling. It is a reason to shard: internal/shard keys on this same
		// limiter table, so a shared backend stays whole on one runner and
		// parallelism comes from the tenant-isolated platforms instead.
		policy.key = registrableSuffix(host)
		policy.maxConcurrent = min(defaultLimit, 4)
		policy.interval = 25 * time.Millisecond
		policy.cooldown = 10 * time.Second
	case host == "sjobs.brassring.com":
		// One hostname serves every BrassRing customer, so the generic
		// exact-host key is already the right grouping and the only thing
		// missing is pacing. It is named explicitly rather than left to fall
		// through because the generic policy applies no interval at all, and an
		// unpaced burst against a single host shared by every tenant on the
		// platform is the shape that got 56 Workable boards rate-limited into
		// looking dead.
		//
		// Paced like the other single-host platforms rather than like a
		// tenant-isolated one: this is not a per-employer budget, it is the
		// whole platform's.
		policy.maxConcurrent = min(defaultLimit, 4)
		policy.interval = 25 * time.Millisecond
		policy.cooldown = 10 * time.Second
	case strings.HasSuffix(host, ".successfactors.com"),
		strings.HasSuffix(host, ".successfactors.eu"):
		// SAP serves every RMK tenant from a handful of numbered pods
		// (career2.successfactors.eu, career5..., and so on), so the generic
		// exact-host key already groups tenants the way the infrastructure
		// does: one measured pod carries 17 of the 30 tenants registered here.
		// The key is therefore left alone deliberately. What the generic policy
		// does not supply is pacing, and an unpaced burst is precisely what a
		// pod with 17 tenants behind it would feel, so add spacing and a long
		// cooldown without collapsing genuinely separate pods together.
		policy.maxConcurrent = min(defaultLimit, 2)
		policy.interval = 100 * time.Millisecond
		policy.cooldown = 30 * time.Second
	case strings.HasSuffix(host, ".oraclecloud.com"):
		// Oracle Cloud HCM is the opposite case, and the reason this is not
		// simply grouped by registrable domain: every tenant has its own host
		// and, as far as can be told from outside, its own pod. Collapsing them
		// onto one "oraclecloud.com" key would throttle 30 unrelated employers
		// to four requests between them, which is the mistake
		// TestTenantIsolatedBackendsStayIndependent exists to prevent for
		// Workday and Phenom. Keep the per-host key; add pacing only.
		policy.maxConcurrent = min(defaultLimit, 4)
		policy.interval = 25 * time.Millisecond
		policy.cooldown = 10 * time.Second
	}

	return policy
}

// registrableSuffix returns the last two labels of a hostname, which is the
// limiter key shared by every tenant of a one-backend-many-subdomains platform.
//
// It is deliberately naive: it is only ever called from a switch arm that has
// already matched a specific known platform suffix, so it never has to reason
// about multi-label public suffixes such as .co.uk. Personio is why it takes the
// last two labels rather than the matched suffix itself; its tenants live at
// {slug}.jobs.personio.de, so the matched suffix and the shared backend are not
// the same string.
func registrableSuffix(host string) string {
	labels := strings.Split(host, ".")
	if len(labels) < 2 {
		return host
	}

	return strings.Join(labels[len(labels)-2:], ".")
}

// state returns the limiter state for a service, creating it on first use.
func (h *hostLimiter) state(req *http.Request) *limitState {
	policy := servicePolicyFor(req, h.limit)

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.states == nil {
		h.states = make(map[string]*limitState)
	}

	if _, ok := h.states[policy.key]; !ok {
		h.states[policy.key] = &limitState{
			key:      policy.key,
			sem:      make(chan struct{}, policy.maxConcurrent),
			interval: policy.interval,
			cooldown: policy.cooldown,
		}
	}

	return h.states[policy.key]
}

// wait reserves the next request start time for a service.
func (s *limitState) wait(ctx context.Context) error {
	s.mu.Lock()

	now := time.Now()
	start := now
	if s.next.After(start) {
		start = s.next
	}
	if s.interval > 0 {
		s.next = start.Add(s.interval)
	}

	s.mu.Unlock()

	return sleep(ctx, time.Until(start))
}

// penalize delays new requests after a service returns 429 and, once 429 is all
// the service has to say, opens its circuit breaker. It reports the shedding
// window when this response is what opened (or re-opened) it.
//
// Retry-After is bounded by maxDelay for the cooldown, so one hostile or
// day-long value cannot stall the crawl. The breaker deliberately uses the raw
// value instead, capped only by maxShedWindow: shedding occupies no worker and
// issues no request, so there is no cost to waiting exactly as long as the
// service asked, and every reason to.
func (s *limitState) penalize(resp *http.Response, maxDelay time.Duration) time.Duration {
	delay := min(s.cooldown, maxDelay)

	requested, hasRetryAfter := retryAfter(resp)
	if hasRetryAfter {
		delay = max(delay, min(requested, maxDelay))
	}

	now := time.Now()
	until := now.Add(delay)

	s.mu.Lock()
	defer s.mu.Unlock()

	if until.After(s.next) {
		s.next = until
	}

	s.rejections++
	if s.rejections < shedAfterRejections {
		return 0
	}

	window := s.cooldown
	if hasRetryAfter && requested > window {
		window = requested
	}

	window = min(window, maxShedWindow)

	if shedUntil := now.Add(window); shedUntil.After(s.shedUntil) {
		s.shedUntil = shedUntil

		return window
	}

	return 0
}

// succeed records that the service answered with something other than 429,
// which resets the breaker.
//
// A request already in flight when the breaker tripped can land here and clear
// it early. That is the intended reading: a backend that is returning real
// responses is not one worth shedding.
func (s *limitState) succeed() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.rejections = 0
	s.shedUntil = time.Time{}
}

// shedding reports whether the service's breaker is open, and for how long yet.
func (s *limitState) shedding() (time.Duration, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.shedUntil.IsZero() {
		return 0, false
	}

	remaining := time.Until(s.shedUntil)
	if remaining <= 0 {
		// Half-open. rejections is deliberately left where it is, so the first
		// 429 from the probe re-opens the breaker immediately rather than
		// paying the whole threshold again; only a real response clears it.
		s.shedUntil = time.Time{}

		return 0, false
	}

	return remaining, true
}

// acquire reserves one of the service's request slots.
//
// A wait long enough to look like a leak rather than contention is logged with
// the service name, and the context error that ends it names the service too.
// Both exist because the failure this catches used to be completely silent: a
// slot is only released when the response body is closed, so an adapter that
// defers Close outside its pagination loop stalls every later request to that
// service until the request context expires, producing no log line and an
// unattributable "context deadline exceeded".
func (h *hostLimiter) acquire(req *http.Request, state *limitState) error {
	select {
	case state.sem <- struct{}{}:
		return nil
	default:
	}

	started := time.Now()

	warn := time.NewTimer(h.slotWaitWarn)
	defer warn.Stop()

	for {
		select {
		case state.sem <- struct{}{}:
			return nil
		case <-req.Context().Done():
			return fmt.Errorf("httpx: %s: waited %s for a request slot on service %q (limit %d): %w",
				req.URL.Redacted(), time.Since(started).Round(time.Millisecond),
				state.key, cap(state.sem), req.Context().Err())
		case <-warn.C:
			h.logger.WarnContext(req.Context(), "waiting for a service request slot",
				slog.String("url", req.URL.Redacted()),
				slog.String("service", state.key),
				slog.Duration("waited", time.Since(started)),
				slog.Int("limit", cap(state.sem)),
				slog.String("hint", "a slot is held until the response body is closed; an adapter deferring Close outside its pagination loop will stall here"),
			)
		}
	}
}

// RoundTrip implements [net/http.RoundTripper].
func (h *hostLimiter) RoundTrip(req *http.Request) (*http.Response, error) {
	state := h.state(req)

	// Checked before the semaphore, deliberately: a shed request must fail
	// immediately rather than queue behind the very service it is being shed
	// from. Failing fast and loudly costs the crawl nothing; queueing for a
	// backend that is answering nothing but 429 costs it minutes per source.
	if remaining, open := state.shedding(); open {
		return nil, &RateLimitedError{
			Service:    state.key,
			URL:        req.URL.Redacted(),
			RetryAfter: remaining,
		}
	}

	if err := h.acquire(req, state); err != nil {
		return nil, err
	}

	if err := state.wait(req.Context()); err != nil {
		<-state.sem

		return nil, fmt.Errorf("httpx: %s: waiting out the cooldown on service %q: %w",
			req.URL.Redacted(), state.key, err)
	}

	resp, err := h.base.RoundTrip(req)
	if err != nil {
		<-state.sem

		return nil, err
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		if window := state.penalize(resp, h.maxDelay); window > 0 {
			h.logger.WarnContext(req.Context(), "shedding requests to a rate-limited service",
				slog.String("url", req.URL.Redacted()),
				slog.String("service", state.key),
				slog.Int("consecutive_429", shedAfterRejections),
				// The raw header, not the clamped delay: the value the board
				// actually sent is the diagnostic.
				slog.String("retry_after", retryAfterValue(resp)),
				slog.Duration("shedding_for", window),
			)
		}
	} else {
		state.succeed()
	}

	// The slot is held until the body is closed, not merely until the headers
	// arrive: a response whose body is still streaming is still occupying the
	// server's attention.
	resp.Body = &releaseOnClose{ReadCloser: resp.Body, release: func() { <-state.sem }}

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
	proxyURLs    []*url.URL

	// limiter is the hostLimiter inside base, when one is installed. It is held
	// so the retry loop can tell whether a service-wide cooldown has already
	// been applied to a 429 and avoid waiting for it a second time.
	limiter *hostLimiter
}

type failingTransport struct {
	err error
}

func (t failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, t.err
}

// ParseProxyURL validates a user-supplied proxy URL without making a request.
func ParseProxyURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("proxy URL is empty")
	}
	if len(raw) > 2048 {
		return nil, fmt.Errorf("proxy URL is too long")
	}

	proxyURL, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse proxy URL: %w", err)
	}

	switch strings.ToLower(proxyURL.Scheme) {
	case "http", "https", "socks5":
	default:
		return nil, fmt.Errorf("unsupported proxy scheme %q; use http, https, or socks5", proxyURL.Scheme)
	}

	if proxyURL.Hostname() == "" {
		return nil, fmt.Errorf("proxy URL has no host")
	}
	if proxyURL.RawQuery != "" || proxyURL.Fragment != "" {
		return nil, fmt.Errorf("proxy URL must not contain a query or fragment")
	}
	if proxyURL.Path != "" && proxyURL.Path != "/" {
		return nil, fmt.Errorf("proxy URL must not contain a path")
	}

	proxyURL.Path = ""

	return proxyURL, nil
}

// proxyEndpoint deliberately omits userinfo so credentials never reach logs.
func proxyEndpoint(proxyURL *url.URL) string {
	if proxyURL == nil {
		return ""
	}

	return proxyURL.Scheme + "://" + proxyURL.Host
}

// proxyPool returns a net/http proxy selector that shards boards, rather than
// individual requests, across the configured pool.
func proxyPool(proxyURLs []*url.URL) func(*http.Request) (*url.URL, error) {
	return func(req *http.Request) (*url.URL, error) {
		if len(proxyURLs) == 0 {
			return nil, nil
		}

		hash := fnv.New64a()
		_, _ = io.WriteString(hash, proxyShardKey(req))

		return proxyURLs[hash.Sum64()%uint64(len(proxyURLs))], nil
	}
}

// proxyShardKey is stable across a board's pagination requests.
//
// URL queries are intentionally excluded because they usually contain an
// offset. Gem is the exception: all tenants use one GraphQL URL, so its small,
// replayable request body is included to distinguish board IDs.
func proxyShardKey(req *http.Request) string {
	key := strings.ToLower(req.URL.Host) + req.URL.EscapedPath()

	if strings.EqualFold(req.URL.Hostname(), "jobs.gem.com") && req.GetBody != nil {
		body, err := req.GetBody()
		if err == nil {
			defer body.Close()

			content, _ := io.ReadAll(io.LimitReader(body, 8<<10))
			key += "\n" + string(content)
		}
	}

	return key
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

			// The request was never sent: the service's breaker is open.
			// Retrying would only re-check the same breaker three more times
			// and report a vaguer error at the end of it.
			var rateLimited *RateLimitedError
			if errors.As(err, &rateLimited) {
				return nil, err
			}

			// A cancelled or expired context is deliberate, not transient.
			if ctxErr := req.Context().Err(); ctxErr != nil {
				// Logged because this was the one exit with no diagnostics at
				// all. A crawl whose adapters leak response bodies parks every
				// later request on a service semaphore until the deadline, and
				// a 75-minute failing run reported exactly nothing about it.
				t.logger.WarnContext(req.Context(), "HTTP request abandoned",
					slog.String("url", req.URL.Redacted()),
					slog.String("service", servicePolicyFor(req, t.perHostLimit).key),
					slog.Int("attempts", attempts),
					slog.String("cause", err.Error()),
				)

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
				t.logExhausted(req, attempts, lastErr, resp)

				return resp, nil
			}

			break
		}

		delay := t.retryDelay(attempt, resp)

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
			slog.String("service", servicePolicyFor(req, t.perHostLimit).key),
			slog.Int("attempt", attempt),
			slog.Int("max_attempts", t.maxAttempts),
			slog.Duration("delay", delay),
			slog.String("retry_after", retryAfterValue(resp)),
			slog.String("cause", lastErr.Error()),
		)

		if err := sleep(req.Context(), delay); err != nil {
			return nil, err
		}
	}

	t.logExhausted(req, attempts, lastErr, nil)

	return nil, lastErr
}

// logExhausted reports a request that ran out of attempts. The count is the
// number of attempts actually made, which is not necessarily maxAttempts: a
// non-replayable request stops after the first.
func (t *retryTransport) logExhausted(req *http.Request, attempts int, cause error, resp *http.Response) {
	t.logger.WarnContext(req.Context(), "HTTP request failed after retries",
		slog.String("url", req.URL.Redacted()),
		slog.String("service", servicePolicyFor(req, t.perHostLimit).key),
		slog.Int("attempts", attempts),
		slog.String("retry_after", retryAfterValue(resp)),
		slog.String("cause", cause.Error()),
	)
}

func retryAfterValue(resp *http.Response) string {
	if resp == nil {
		return ""
	}

	return resp.Header.Get("Retry-After")
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

// retryDelay returns how long this loop should sleep before the next attempt,
// after accounting for what the service limiter is already going to make it
// wait.
//
// A 429 is paced twice otherwise, and the two waits stack. hostLimiter's
// penalize has already pushed the service's shared next-start time out by the
// (clamped) Retry-After, and *every* attempt re-enters the limiter, so that
// cooldown is enforced whether or not this loop also sleeps. Sleeping the
// Retry-After here as well makes a retried request pay it twice: once outside
// the limiter, and then again when it re-queues behind a cooldown that other
// tenants extended while it slept. Measured on peopleforce.io, where all ~37
// tenants share one limiter key, that serialised the entire platform at roughly
// one request per maxDelay and produced zero postings for an extrapolated ~37
// minutes of crawl budget.
//
// The shared cooldown is the one worth keeping, because it is the one that
// protects the other 36 tenants rather than just this request. What is left
// here is a small jitter: the limiter can release everyone at the same instant,
// and requests that resume in lockstep get rate-limited in lockstep. With no
// limiter installed (WithPerHostLimit(0)) nothing else is pacing the retry, so
// the full backoff still applies.
func (t *retryTransport) retryDelay(attempt int, resp *http.Response) time.Duration {
	delay := t.backoff(attempt, resp)

	if t.limiter != nil && resp != nil && resp.StatusCode == http.StatusTooManyRequests {
		return min(delay, rand.N(t.baseDelay+1))
	}

	return delay
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
