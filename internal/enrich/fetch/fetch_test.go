package fetch_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/enrich/enrichtest"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/enrich/fetch"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/httpx"
	"github.com/shoenig/test/must"
)

// TestUserAgentRefusesToBeAnonymous is the fail-closed half of the SEC and
// Wikimedia access policies.
//
// SEC publishes its requirement as a User-Agent naming a reachable contact and
// blocks requests without one; Wikimedia rejects generic agents outright.
// Discovering that from a ten-minute IP block on a shared GitHub Actions runner
// is a much worse way to learn it than a startup error.
func TestUserAgentRefusesToBeAnonymous(t *testing.T) {
	t.Parallel()

	for _, contact := range []string{"", "   "} {
		_, err := fetch.UserAgent(contact)
		must.ErrorContains(t, err, "refuses to make anonymous requests")
	}

	_, err := fetch.UserAgent("ops@example.com\r\nX-Evil: 1")
	must.ErrorContains(t, err, "cannot contain a newline")

	agent, err := fetch.UserAgent("ops@example.com")
	must.NoError(t, err)
	must.StrContains(t, agent, "ops@example.com")
	must.StrContains(t, agent, "job-hunter-toolkit")
}

// TestClientSendsTheContactBearingUserAgent checks the agent reaches the wire
// rather than merely being built.
func TestClientSendsTheContactBearingUserAgent(t *testing.T) {
	t.Parallel()

	transport := &enrichtest.Transport{Routes: map[string]string{"example.test": `{}`}}

	client, err := fetch.Client("ops@example.com", 0, httpx.WithTransport(transport))
	must.NoError(t, err)

	var payload map[string]any
	must.NoError(t, fetch.JSON(t.Context(), client, "https://example.test/x", "application/json", &payload))

	must.Len(t, 1, transport.UserAgents())
	must.StrContains(t, transport.UserAgents()[0], "ops@example.com")
}

// TestClientRefusesWithoutAContact: the whole point of the fail-closed
// UserAgent is that no code path can skip it.
func TestClientRefusesWithoutAContact(t *testing.T) {
	t.Parallel()

	_, err := fetch.Client("", time.Second)
	must.Error(t, err)
}

// TestPacedSpacesRequests is what keeps a generator run under EDGAR's published
// 10 requests per second. httpx.servicePolicyFor has no sec.gov branch, so an
// unpaced client would fall through to the generic policy — four concurrent
// requests, no spacing — and earn a block.
func TestPacedSpacesRequests(t *testing.T) {
	t.Parallel()

	transport := &enrichtest.Transport{Routes: map[string]string{"example.test": `{}`}}
	interval := 20 * time.Millisecond

	client := &http.Client{Transport: fetch.Paced(transport, interval)}

	started := time.Now()

	for range 3 {
		var payload map[string]any
		must.NoError(t, fetch.JSON(t.Context(), client, "https://example.test/x", "", &payload))
	}

	// Three requests are two intervals apart at minimum; the first does not
	// wait.
	must.GreaterEq(t, 2*interval, time.Since(started))
}

// TestPacedIsOneBudgetAcrossHosts: SEC documents its limit as a single budget
// shared by www.sec.gov, data.sec.gov and efts.sec.gov, and the generator talks
// to two of them. A per-host pacer would run at twice the rate it believes.
func TestPacedIsOneBudgetAcrossHosts(t *testing.T) {
	t.Parallel()

	transport := &enrichtest.Transport{Routes: map[string]string{".test": `{}`}}
	interval := 20 * time.Millisecond

	client := &http.Client{Transport: fetch.Paced(transport, interval)}

	started := time.Now()

	for _, host := range []string{"a.test", "b.test", "c.test"} {
		var payload map[string]any
		must.NoError(t, fetch.JSON(t.Context(), client, "https://"+host+"/x", "", &payload))
	}

	must.GreaterEq(t, 2*interval, time.Since(started))
}

// TestJSONReportsTheBodyOfAFailure: EDGAR and Wikidata both explain a refusal in
// the body, and that explanation is the difference between "we are being
// throttled" and "the endpoint moved".
func TestJSONReportsTheBodyOfAFailure(t *testing.T) {
	t.Parallel()

	transport := &enrichtest.Transport{
		Routes: map[string]string{"example.test": "Your request rate has exceeded the limit"},
		Status: http.StatusForbidden,
	}

	client := &http.Client{Transport: transport}

	var payload map[string]any

	err := fetch.JSON(t.Context(), client, "https://example.test/x", "", &payload)
	must.ErrorContains(t, err, "403")
	must.ErrorContains(t, err, "request rate has exceeded")
	must.ErrorContains(t, err, "example.test")
}
