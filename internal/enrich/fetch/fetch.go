// Package fetch holds the HTTP plumbing the enrichment generators share.
//
// It is a separate package from [enrich] on purpose. Nothing here is imported by
// the CLI: the shipped binary embeds the generated tables but must not carry the
// clients that produced them, because a lookup that costs a request is the one
// thing the enrichment design forbids. Keeping the clients in a package only
// tools/enrichgen imports makes that structural rather than a matter of
// discipline.
//
// Two published access policies shape everything here.
//
// SEC EDGAR requires a User-Agent that names the requester and gives a way to
// contact them, and rate-limits every client to 10 requests per second across
// all of its hosts (www.sec.gov, data.sec.gov, efts.sec.gov), enforced with a
// 403 and roughly a ten-minute IP block. Wikimedia's user-agent policy rejects
// generic agents outright and escalates clients that ignore 429 to an outright
// ban. A generator that gets this wrong does not inconvenience a developer's
// laptop, it gets the shared GitHub Actions runner blocked.
package fetch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/httpx"
)

// MaxBody bounds how much of a response is read.
//
// company_tickers.json is around a megabyte and a SPARQL result set for a few
// hundred companies is smaller; anything materially larger than this is a
// changed endpoint or an error page, and streaming it into memory would be the
// wrong way to find out.
const MaxBody = 64 << 20

// Product is how the generator introduces itself, before the contact is
// appended.
//
// It is deliberately NOT built from [httpx.DefaultUserAgent]. That constant
// embeds this project's GitHub URL, and a live probe on 2026-07-28 measured SEC
// EDGAR answering 403 "Your Request Originates from an Undeclared Automated
// Tool" to every User-Agent containing the substring "github", whatever else the
// header said:
//
//	job-hunter-toolkit/0.0.0                                    -> 200
//	job-hunter-toolkit/0.0.0 (+https://example.com/x)            -> 200
//	somebot (+https://gitlab.com/x)                              -> 200
//	somebot (+https://github.com/x)                              -> 403
//	job-hunter-toolkit/0.0.0 (+https://github.com/...) (contact: ops@example.com) -> 403
//
// The last line is what the generator used to send, so before this change every
// EDGAR request a run made was refused and no table could ever be written. The
// crawler's own user agent is untouched: job boards are not SEC, and the whole
// reason the generator sends its own header is so one upstream's policy cannot
// change how this project introduces itself to another's.
const Product = "job-hunter-toolkit-enrichment/1.0"

// UserAgent builds the contact-bearing User-Agent both SEC and Wikimedia
// require, and refuses to build one without a usable contact.
//
// SEC publishes its requirement in the form "Sample Company Name
// AdminContact@<domain>.com", an address rather than a tracker, and Wikimedia's
// policy asks for an email or a full URL. An address satisfies both, so that is
// what this asks for.
//
// The errors are deliberate and fail-closed. An anonymous run is exactly the
// request SEC blocks, and a run whose contact reintroduces "github" into the
// header is the measured 403 above — discovering either from a wall of 403s in
// CI is a worse way to learn it than a startup error.
func UserAgent(contact string) (string, error) {
	contact = strings.TrimSpace(contact)

	if contact == "" {
		return "", fmt.Errorf("no contact address for the enrichment generator: SEC EDGAR and the Wikimedia APIs both require a User-Agent naming a reachable contact, so this refuses to make anonymous requests")
	}

	if strings.ContainsAny(contact, "\r\n") {
		return "", fmt.Errorf("invalid contact %q: a header value cannot contain a newline", contact)
	}

	// Measured, not assumed: see [Product]. This project's own repository URL is
	// the most natural thing for a maintainer to reach for as a contact, and it
	// is the one string that makes every EDGAR request fail.
	if strings.Contains(strings.ToLower(contact), "github") {
		return "", fmt.Errorf("invalid contact %q: SEC EDGAR answers 403 %q to any User-Agent containing \"github\", so a GitHub URL or handle cannot be the contact; use an email address, which is the form SEC's access policy documents", contact, "Undeclared Automated Tool")
	}

	return Product + " (contact: " + contact + ")", nil
}

// Client returns an HTTP client for one upstream service: the shared retry and
// per-service limiting from httpx, a contact-bearing user agent, and a hard
// floor on the spacing between requests.
//
// The spacing is applied here rather than left to httpx because
// httpx.servicePolicyFor has no sec.gov branch: an unknown host falls through to
// the generic policy, which is four concurrent requests with no pacing at all —
// comfortably over EDGAR's published ceiling. Pacing the generator's own client
// makes it safe today, and stays correct as belt and braces once httpx learns
// about sec.gov.
func Client(contact string, interval time.Duration, opts ...httpx.Option) (*http.Client, error) {
	userAgent, err := UserAgent(contact)
	if err != nil {
		return nil, err
	}

	options := []httpx.Option{
		httpx.WithUserAgent(userAgent),
		// One request at a time. A generator has no deadline worth defending
		// with concurrency: it runs monthly, in a workflow of its own, against
		// services whose goodwill this project cannot buy back.
		httpx.WithPerHostLimit(1),
		httpx.WithTransport(Paced(http.DefaultTransport, interval)),
	}

	return httpx.NewClient(append(options, opts...)...), nil
}

// Paced returns a transport that starts at most one request per interval,
// across every host it sees.
//
// Across every host, not per host, because EDGAR's limit is documented as a
// single budget shared by www.sec.gov, data.sec.gov and efts.sec.gov. A
// per-host pacer would let a generator that talks to two of them at once run at
// twice the rate it believes it is running at.
func Paced(base http.RoundTripper, interval time.Duration) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}

	return &pacedTransport{base: base, interval: interval}
}

type pacedTransport struct {
	base     http.RoundTripper
	interval time.Duration

	mu   sync.Mutex
	next time.Time
}

// RoundTrip implements [net/http.RoundTripper].
func (p *pacedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if p.interval <= 0 {
		return p.base.RoundTrip(req)
	}

	// The slot is reserved before sleeping, so concurrent callers queue in
	// order and each one waits for its own turn instead of all waking together.
	p.mu.Lock()

	start := time.Now()
	if p.next.After(start) {
		start = p.next
	}
	p.next = start.Add(p.interval)

	p.mu.Unlock()

	if wait := time.Until(start); wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()

		select {
		case <-req.Context().Done():
			return nil, fmt.Errorf("fetch: pacing %s: %w", req.URL.Redacted(), req.Context().Err())
		case <-timer.C:
		}
	}

	return p.base.RoundTrip(req)
}

// JSON performs a GET and decodes the response into dst.
//
// Errors name the URL, because a generator makes requests to several services
// and "unexpected EOF" without one is unactionable. The body of a failed
// response is included, truncated: EDGAR and Wikidata both explain a refusal in
// the body, and that explanation is the difference between "we are being
// throttled" and "the endpoint moved".
func JSON(ctx context.Context, client *http.Client, url string, accept string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("building request for %s: %w", url, err)
	}

	if accept != "" {
		req.Header.Set("Accept", accept)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("fetching %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))

		return fmt.Errorf("fetching %s: unexpected status %s: %s",
			url, resp.Status, strings.TrimSpace(string(snippet)))
	}

	if err := json.NewDecoder(io.LimitReader(resp.Body, MaxBody)).Decode(dst); err != nil {
		return fmt.Errorf("decoding %s: %w", url, err)
	}

	return nil
}
