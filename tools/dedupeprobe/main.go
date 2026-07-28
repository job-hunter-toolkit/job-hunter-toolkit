// Command dedupeprobe dumps named sources as NDJSON, so their URLs can be
// compared.
//
// It is [tools/dcprobe] widened to the whole registry. dcprobe dispatches on a
// hardcoded switch over six platform names, which is fine for the
// Ashby-versus-Greenhouse comparisons it was written for and useless for the
// ones that actually inflate a count: the double counts found on 2026-07-28 were
// Phenom against Workday and Phenom against SuccessFactors, and none of those
// three platforms could be probed at all. This dispatches by looking the source
// up in [services.Builtin] instead, so it covers every platform automatically
// and cannot go stale when one is added.
//
// Sources are named "platform/key", exactly as [services.Source.Platform] and
// [services.Source.Key] spell them — which for Workday is the full tenant URL
// and for SuccessFactors is the "slug,companyId,host" triple. `job-hunter-toolkit
// companies --sources` prints them.
//
// Usage:
//
//	go run ./tools/dedupeprobe phenom/careers.southwestair.com > a.ndjson
//	go run ./tools/dedupeprobe "workday/https://swa.wd1.myworkdayjobs.com/external" > b.ndjson
//	jq -r .url a.ndjson | sort > a.urls   # then comm -12 against b.urls
//
// Rows go to stdout and the per-source count to stderr, so the stream pipes
// cleanly. Several sources may be named in one invocation; they are fetched in
// registry order, one at a time, so the output is deterministic.
//
// Compare URLs before titles, for the reason docs/dedupe-audit.md gives:
// [internal.Dedupe] keys on URL, so two boards serving identical URLs are
// already collapsed and cost only a request, while a count is inflated only when
// one opening arrives under two different URLs. The audit also records why that
// comparison must be done on the raw URL rather than a normalised one.
//
// This talks to live boards through [httpx.NewClient], so it inherits the pacing
// a crawl uses. It is not part of the binary's dependency closure.
//
// # Where this ends and dedupesweep begins
//
// This command answers "are these two named boards the same board?" once a
// suspect pair is already in hand, and it answers it in full: every row, no
// thresholds, nothing filtered. Finding the suspects is the other half, and it
// is [tools/dedupesweep] — the same fetch loop widened to the whole registry,
// with the pairwise comparison built in and a weekly workflow behind it
// (docs/dedupe-sweep.md). The two speak the same NDJSON, so a sweep's -dump can
// be sliced with the shell commands above and a probe's output can be handed
// back to `dedupesweep -in`.
//
// Reach for this one when a sweep finding needs settling, because a threshold
// that keeps a weekly report readable is exactly the wrong thing to have in the
// way when a specific pair is being measured for a deletion decision.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/httpx"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/services"
)

// row is one posting, flattened to the fields a URL comparison needs. The
// requisition and external ids are here because they are the only identity that
// survives a career-site front end, which is what settles a comparison the URLs
// cannot: Phenom's zimmerbiomet and SuccessFactors' zimmerin01 share 362 of 365
// requisition ids and zero URLs.
type row struct {
	Platform string `json:"platform"`
	Key      string `json:"key"`
	Company  string `json:"company"`
	URL      string `json:"url"`
	Title    string `json:"title"`
	Location string `json:"location"`
	ReqID    string `json:"req"`
	ExtID    string `json:"ext"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: dedupeprobe <platform>/<key> [<platform>/<key>...]")
		os.Exit(2)
	}

	wanted := make(map[string]bool, len(os.Args)-1)
	for _, arg := range os.Args[1:] {
		wanted[arg] = true
	}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
	defer cancel()

	client := httpx.NewClient()
	encoder := json.NewEncoder(os.Stdout)

	for _, source := range services.Builtin {
		id := source.Platform + "/" + source.Key
		if !wanted[id] {
			continue
		}

		delete(wanted, id)

		var postings, failures int

		start := time.Now()

		for posting, err := range source.Jobs(ctx, client) {
			if err != nil {
				failures++

				// Only the first few: a board that fails every page would
				// otherwise bury the counts this is read for.
				if failures < 5 {
					fmt.Fprintln(os.Stderr, "error:", id, err)
				}

				continue
			}

			postings++

			_ = encoder.Encode(row{
				Platform: source.Platform,
				Key:      source.Key,
				Company:  posting.Company,
				URL:      posting.URL,
				Title:    posting.Title,
				Location: posting.Location,
				ReqID:    posting.RequisitionID,
				ExtID:    posting.ExternalID,
			})
		}

		fmt.Fprintf(os.Stderr, "%s: %d postings, %d errors, %s\n",
			id, postings, failures, time.Since(start).Round(time.Second))
	}

	// Named but not registered. Reported rather than ignored, because a typo in
	// a tenant URL would otherwise look exactly like a board with no openings.
	for id := range wanted {
		fmt.Fprintln(os.Stderr, "not registered:", id)
	}

	if len(wanted) > 0 {
		os.Exit(1)
	}
}
