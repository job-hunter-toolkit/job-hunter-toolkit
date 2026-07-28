// Package enrichtest provides the stub HTTP transport the enrichment
// generators are tested against.
//
// It is a package rather than a copy in each test file because three packages
// need the same stub — edgar, wikidata and the generator that drives both — and
// the generator's test in particular has to serve two upstreams from one client
// set. It mirrors the fixtureTransport pattern in internal/services, including
// recording the requests it served, so a test can assert what was asked for as
// well as what came back.
//
// Every enrichment test is hermetic. Nothing in this project's test suite may
// reach SEC or Wikimedia: their access policies are written for identified,
// paced clients, and a test suite run in a loop is neither.
package enrichtest

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// Transport serves canned responses matched by URL substring.
type Transport struct {
	// Routes maps a substring of the request URL to the response body.
	Routes map[string]string

	// Status is served for matched routes, defaulting to 200.
	Status int

	mu sync.Mutex

	// requests records the URLs served, in order.
	requests []string

	// userAgents records the User-Agent of each request, so a test can assert
	// the contact-bearing agent SEC and Wikimedia require was actually sent.
	userAgents []string
}

// RoundTrip implements [net/http.RoundTripper].
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	// A real transport abandons a request whose context is done, and the
	// generator runs under a workflow timeout, so a stub that answered anyway
	// would let an uninterruptible fetch loop pass its tests.
	if err := req.Context().Err(); err != nil {
		return nil, err
	}

	t.mu.Lock()
	t.requests = append(t.requests, req.URL.String())
	t.userAgents = append(t.userAgents, req.Header.Get("User-Agent"))
	t.mu.Unlock()

	status := t.Status
	if status == 0 {
		status = http.StatusOK
	}

	for pattern, body := range t.Routes {
		if strings.Contains(req.URL.String(), pattern) {
			return response(req, status, body), nil
		}
	}

	// A 404 rather than an error, so a test that forgot a fixture sees the same
	// failure shape a real missing document would produce.
	return response(req, http.StatusNotFound, `{"error":"no fixture"}`), nil
}

func response(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		// "403 Forbidden", the way net/http spells it, so an error message
		// asserted here is the one a real refusal produces.
		Status:  strconv.Itoa(status) + " " + http.StatusText(status),
		Header:  http.Header{"Content-Type": []string{"application/json"}},
		Body:    io.NopCloser(strings.NewReader(body)),
		Request: req,
	}
}

// Requests returns the URLs served so far.
func (t *Transport) Requests() []string {
	t.mu.Lock()
	defer t.mu.Unlock()

	return append([]string(nil), t.requests...)
}

// UserAgents returns the User-Agent of each request served so far.
func (t *Transport) UserAgents() []string {
	t.mu.Lock()
	defer t.mu.Unlock()

	return append([]string(nil), t.userAgents...)
}

// Client returns a client serving the given routes, along with the transport so
// a test can inspect what was requested.
//
// It is a bare client rather than one built by fetch.Client, because pacing a
// unit test at EDGAR's 150ms would make the generator's test take longer than
// the crawl it is meant to protect. The pacing itself is tested directly in
// package fetch.
func Client(routes map[string]string) (*http.Client, *Transport) {
	transport := &Transport{Routes: routes}

	return &http.Client{Transport: transport}, transport
}
