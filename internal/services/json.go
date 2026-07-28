package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"golang.org/x/net/html"
)

// jsonRequest describes a single call to a job board's JSON API.
//
// The zero value issues a GET with an Accept header of application/json, which
// is what almost every board wants.
type jsonRequest struct {
	// URL is the fully-built request URL.
	URL string

	// Method defaults to GET.
	Method string

	// Body is sent as the request body when non-empty, and implies a
	// Content-Type of application/json unless Headers overrides it.
	Body string

	// Headers are set on the request, overriding the defaults.
	Headers map[string]string
}

// fetchJSON performs one request against a job board and decodes the response
// into a new T.
//
// Every adapter previously repeated this sequence, build request, set Accept,
// Do, check status, decode, close body, roughly a dozen times, each with
// slightly different error wording. Centralising it also centralises two things
// that were easy to get wrong per-adapter:
//
//   - the response body is closed as soon as this returns, so a paginated loop
//     cannot accumulate open bodies for the lifetime of a crawl;
//   - every error names both the platform and the company, without which a
//     failure among ~2,150 sources is unattributable.
func fetchJSON[T any](ctx context.Context, httpClient *http.Client, platform, company string, request jsonRequest) (*T, error) {
	method := request.Method
	if method == "" {
		method = http.MethodGet
	}

	var body io.Reader
	if request.Body != "" {
		body = strings.NewReader(request.Body)
	}

	req, err := http.NewRequestWithContext(ctx, method, request.URL, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request for %s company %q: %w", platform, company, err)
	}

	req.Header.Set("Accept", "application/json")

	if request.Body != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	for name, value := range request.Headers {
		req.Header.Set(name, value)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request to %s for company %q: %w", platform, company, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code from %s for company %q: %s", platform, company, resp.Status)
	}

	decoded := new(T)

	if err := json.NewDecoder(resp.Body).Decode(decoded); err != nil {
		return nil, fmt.Errorf("failed to decode response from %s for company %q: %w", platform, company, err)
	}

	return decoded, nil
}

// fetchHTML performs one request against a job board and parses the response as
// HTML.
//
// The companion to [fetchJSON], for the boards that publish no API and have to
// be scraped. It carries the same two guarantees: the body is closed before this
// returns, and errors name the platform and company.
func fetchHTML(ctx context.Context, httpClient *http.Client, platform, company, url string) (*html.Node, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request for %s company %q: %w", platform, company, err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request to %s for company %q: %w", platform, company, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code from %s for company %q: %s", platform, company, resp.Status)
	}

	doc, err := html.Parse(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML from %s for company %q: %w", platform, company, err)
	}

	return doc, nil
}
