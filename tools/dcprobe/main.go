// Command dcprobe dumps one board as NDJSON, so two boards of the same employer
// can be compared.
//
// It exists because [services.TestNoUnreviewedDoubleCountedEmployer] tells a
// maintainer to resolve a cross-platform collision by measuring rather than by
// guessing from the name, and that instruction needs to be executable. Measured
// on 2026-07-28, of 28 companies registered on two platforms only 7 were the
// same openings counted twice; 13 were unrelated companies sharing a short name
// and 3 were one employer publishing different work on each board.
//
// Compare URLs before titles. [internal.Dedupe] keys on URL, so two boards
// serving identical URLs are already collapsed and cost only a request, while a
// count is inflated only when one opening arrives under two different URLs.
// Titles mislead in both directions: Home Depot's BrassRing and Workday boards
// share no title and duplicate nothing, and two unrelated companies both post
// "Software Engineer".
//
// Usage:
//
//	go run ./tools/dcprobe <platform> <key> > a.ndjson
//	go run ./tools/dcprobe <platform> <key> > b.ndjson
//	jq -r .url a.ndjson | sort > a.urls   # then comm -12 against b.urls
//
// The posting count and error count go to stderr, the rows to stdout, so the
// stream pipes cleanly.
//
// This deliberately talks to live boards through [httpx.NewClient], so it
// inherits the same pacing a crawl uses. It is not part of the binary's
// dependency closure.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/httpx"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/services"
)

type row struct {
	Platform string `json:"platform"`
	Key      string `json:"key"`
	Company  string `json:"company"`
	URL      string `json:"url"`
	Title    string `json:"title"`
	Location string `json:"location"`
	Comp     any    `json:"comp"`
	Dept     string `json:"dept"`
	Type     string `json:"etype"`
	Remote   any    `json:"remote"`
}

func main() {
	platform, key := os.Args[1], os.Args[2]

	var fn func(context.Context, *http.Client, string) internal.Jobs

	switch platform {
	case "ashby":
		fn = services.AshbyHQ
	case "greenhouse":
		fn = services.Greenhouse
	case "lever":
		fn = services.Lever
	case "phenom":
		fn = services.Phenom
	case "workday":
		fn = services.Workday
	case "smartrecruiters":
		fn = services.SmartRecruiters
	default:
		fmt.Fprintln(os.Stderr, "unknown platform", platform)
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	client := httpx.NewClient()
	enc := json.NewEncoder(os.Stdout)

	var n, errs int

	for posting, err := range fn(ctx, client, key) {
		if err != nil {
			errs++
			if errs < 5 {
				fmt.Fprintln(os.Stderr, "error:", err)
			}
			continue
		}
		n++
		_ = enc.Encode(row{platform, key, posting.Company, posting.URL, posting.Title, posting.Location, posting.Compensation, posting.Department, string(posting.EmploymentType), posting.Remote})
	}

	fmt.Fprintf(os.Stderr, "%s %s: %d postings, %d errors\n", platform, key, n, errs)
}
