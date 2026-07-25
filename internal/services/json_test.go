package services

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// helperDoc is a minimal decode target for the fetch helpers.
type helperDoc struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func TestFetchJSONDecodes(t *testing.T) {
	t.Parallel()

	client, transport := fixtureClient(map[string]string{
		"example.test": `{"name":"acme","count":7}`,
	})

	doc, err := fetchJSON[helperDoc](t.Context(), client, "TestPlatform", "acme", jsonRequest{
		URL: "https://example.test/jobs",
	})
	if err != nil {
		t.Fatalf("fetchJSON() error = %v, want nil", err)
	}

	if doc.Name != "acme" || doc.Count != 7 {
		t.Errorf("decoded = %+v, want acme/7", doc)
	}

	if len(transport.requests) != 1 {
		t.Errorf("made %d requests, want 1", len(transport.requests))
	}
}

func TestFetchJSONNamesPlatformAndCompanyInErrors(t *testing.T) {
	t.Parallel()

	// Attribution is the whole reason this is centralised: a failure among
	// ~1600 sources is useless if it does not say which source failed.
	tests := []struct {
		name      string
		transport http.RoundTripper
	}{
		{
			name: "bad status",
			transport: &fixtureTransport{
				routes: map[string]string{"example.test": `{}`},
				status: http.StatusNotFound,
			},
		},
		{
			name: "malformed body",
			transport: &fixtureTransport{
				routes: map[string]string{"example.test": `{"name": broken`},
			},
		},
		{
			name:      "no route matches",
			transport: &fixtureTransport{routes: map[string]string{"other.test": `{}`}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := fetchJSON[helperDoc](t.Context(), &http.Client{Transport: tt.transport},
				"TestPlatform", "sentinel-co", jsonRequest{URL: "https://example.test/jobs"})
			if err == nil {
				t.Fatal("fetchJSON() error = nil, want an error")
			}

			if !strings.Contains(err.Error(), "TestPlatform") {
				t.Errorf("error = %v, want it to name the platform", err)
			}

			if !strings.Contains(err.Error(), "sentinel-co") {
				t.Errorf("error = %v, want it to name the company", err)
			}
		})
	}
}

func TestFetchJSONSendsBodyAndMethod(t *testing.T) {
	t.Parallel()

	var (
		gotMethod      string
		gotBody        string
		gotContentType string
		gotAccept      string
		gotCustom      string
	)

	client := &http.Client{Transport: roundTripperFn(func(req *http.Request) (*http.Response, error) {
		gotMethod = req.Method
		gotContentType = req.Header.Get("Content-Type")
		gotAccept = req.Header.Get("Accept")
		gotCustom = req.Header.Get("X-Custom")

		if req.Body != nil {
			raw, _ := io.ReadAll(req.Body)
			gotBody = string(raw)
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{},
			Body:       http.NoBody,
			Request:    req,
		}, nil
	})}

	_, err := fetchJSON[helperDoc](t.Context(), client, "TestPlatform", "acme", jsonRequest{
		Method:  http.MethodPost,
		URL:     "https://example.test/graphql",
		Body:    `{"query":"x"}`,
		Headers: map[string]string{"X-Custom": "yes"},
	})
	// An empty 200 body is not valid JSON, so a decode error here is expected;
	// what is under test is the request that was sent.
	if err == nil {
		t.Log("fetchJSON returned no error; only the request shape is asserted")
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}

	if gotBody != `{"query":"x"}` {
		t.Errorf("body = %q, want the request body forwarded", gotBody)
	}

	// A body implies JSON content unless overridden.
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}

	if gotAccept != "application/json" {
		t.Errorf("Accept = %q, want application/json", gotAccept)
	}

	if gotCustom != "yes" {
		t.Errorf("X-Custom = %q, want the custom header set", gotCustom)
	}
}

func TestFetchHTMLParses(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{
		"example.test": `<html><body><a href="/job/1">Security Engineer</a></body></html>`,
	})

	doc, err := fetchHTML(t.Context(), client, "TestPlatform", "acme", "https://example.test/careers")
	if err != nil {
		t.Fatalf("fetchHTML() error = %v, want nil", err)
	}

	if doc == nil {
		t.Fatal("fetchHTML() returned a nil document")
	}
}

func TestFetchHTMLNamesCompanyInErrors(t *testing.T) {
	t.Parallel()

	transport := &fixtureTransport{
		routes: map[string]string{"example.test": `<html></html>`},
		status: http.StatusServiceUnavailable,
	}

	_, err := fetchHTML(t.Context(), &http.Client{Transport: transport},
		"TestPlatform", "sentinel-co", "https://example.test/careers")
	if err == nil {
		t.Fatal("fetchHTML() error = nil, want an error")
	}

	if !strings.Contains(err.Error(), "sentinel-co") {
		t.Errorf("error = %v, want it to name the company", err)
	}
}

// roundTripperFn adapts a function to http.RoundTripper.
type roundTripperFn func(*http.Request) (*http.Response, error)

func (f roundTripperFn) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
