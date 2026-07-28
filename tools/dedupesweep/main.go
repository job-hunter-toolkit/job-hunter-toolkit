// Command dedupesweep crawls the whole registry and reports employers whose two
// routes overlap, keyed on evidence rather than on the company name.
//
// # Why this exists
//
// [services.TestNoUnreviewedDoubleCountedEmployer] derives its subjects from
// company *names* in the registry, so an employer registered under a different
// name on each side is invisible to it. That is not hypothetical. Two real
// double counts hid behind that blind spot while the test stayed green:
//
//   - Southwest Airlines, phenom/careers.southwestair.com against
//     workday/swa.wd1 — 15 postings, found only because 15 of 18 Phenom URLs
//     were a Workday URL with "/apply" appended.
//   - Zimmer Biomet, phenom/careers.zimmerbiomet.com against
//     successfactors/zimmerin01 — 362 postings, and no URL rule reaches it at
//     all: the two adapters published different paths, extra parameters and a
//     different parameter order. Only the requisition ids matched, 362 of 365.
//
// Both were found by a person running a URL sweep by hand and noticing.
// docs/dedupe-audit.md says closing the gap properly means comparing boards
// rather than names, which is a crawl and not a unit test. This is that crawl,
// made repeatable so it runs on a schedule instead of when someone remembers.
//
// # What counts as evidence
//
// Three kinds, in the order docs/dedupe-audit.md ranks them:
//
//  1. A shared raw URL. [internal.Dedupe] keys on URL, so these are already
//     collapsed downstream and cost only a duplicate request — but a pair that
//     shares URLs is still two routes to one board and worth knowing about.
//  2. A shared URL after normalisation: lowercased host, trailing slash and
//     "/apply" segment removed, a leading locale segment ("/en-us") removed,
//     query parameters sorted. This is exactly what reached Southwest, Lowe's
//     and KBR, and what the audit measured as perfectly precise on the 2026-07-28
//     corpus (1,505 merges, all across sources, none within one). It is a probe,
//     not a change to Dedupe: the audit's argument against normalising in
//     Dedupe — that "/apply" is a legitimate path segment on some board added
//     next month — is an argument against silently collapsing postings, not
//     against comparing them in a report a human reads.
//  3. A shared requisition id. The only identity that survives a career-site
//     front end, and the only thing that reached Zimmer Biomet. It is also by
//     far the weakest, because req ids are not unique across employers and are
//     mostly per-tenant counters. [options] records what live runs measured
//     about that and why the threshold on it ended up where it is.
//
// Title and location are deliberately NOT evidence here. double_count_test.go
// records what they cost: Visa's two boards "shared 2 of 2 titles" and the two
// titles were the bare words "Sr. Manager" and "Director", and Chipotle shares
// 52 of 55 titles across two boards that share 12 of 178 title+location pairs
// because "General Manager" recurs at thousands of restaurants. A sweep that
// reddened a report on a shared title would be turned off within a month.
//
// # What it does with a finding
//
// Nothing but print it. A finding is a claim that two boards overlap, not a
// decision about which route to delete — docs/dedupe-audit.md and
// double_count_test.go are full of pairs where the right answer was to keep
// both. Findings are split into two lists:
//
//   - Pairs whose two sources carry DIFFERENT company names. These are the
//     blind spot: no existing test can see them, and a new one here is a real
//     finding.
//   - Pairs whose two sources carry the SAME company name. The unit test
//     already requires a recorded verdict for these, so they are reported for
//     context and for the numbers, not as news.
//
// That split is derived from the registry rather than from a hardcoded list of
// known pairs, so it cannot go stale when a verdict is added or a route deleted.
//
// # Usage
//
//	go run ./tools/dedupesweep -dump sweep.ndjson > report.md
//	go run ./tools/dedupesweep -platform phenom,workday -dump p.ndjson
//	go run ./tools/dedupesweep -in sweep.ndjson       # re-analyse without crawling
//
// The NDJSON read by -in and written by -dump is [tools/dedupeprobe]'s row
// format, so a dump can be analysed with the same tools a targeted two-board
// comparison uses, and a probe of two boards can be fed straight back in here.
//
// This talks to live boards through [httpx.NewClient], so it inherits the pacing
// a crawl uses. It is not part of the binary's dependency closure.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"hash/fnv"
	"io"
	"net/url"
	"os"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/httpx"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/services"
)

// Evidence kinds, weakest last. The order is the order the report prints them
// and the order docs/dedupe-audit.md argues for: URL first, requisition id only
// when no URL relationship exists.
const (
	kindURL uint8 = iota
	kindNormURL
	kindReq

	kindCount = 3
)

// kindNames label the evidence kinds in the report and in the JSON.
var kindNames = [kindCount]string{"url", "normalised_url", "requisition_id"}

// row is one posting flattened to the fields an overlap comparison needs. It is
// [tools/dedupeprobe]'s row, field for field, so the two commands' NDJSON is
// interchangeable.
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

// sourceInfo is one crawled board and what it returned.
type sourceInfo struct {
	Platform string  `json:"platform"`
	Key      string  `json:"key"`
	Company  string  `json:"company"`
	Postings int     `json:"postings"`
	Errors   int     `json:"errors"`
	Seconds  float64 `json:"seconds"`
}

// id is how a source is named in the report: exactly as `companies --sources`
// prints it, so a finding can be pasted into dedupeprobe unchanged.
func (s sourceInfo) id() string { return s.Platform + "/" + s.Key }

// record is one evidence key held by one source, hashed. The whole sweep is
// held as a sorted slice of these rather than as a map of strings because a full
// registry crawl is ~1.24 million postings and this is 16 bytes per key: about
// 60 MB for the ~3.7 million records that produces, against several hundred for
// a map keyed on the strings themselves.
//
// Hashing loses the ability to print an example, which is why -dump exists: the
// examples in the report come from a second streaming pass over the dump, and
// the report says so when there is no dump to read.
type record struct {
	hash uint64
	src  int32
	kind uint8
}

// pair names two sources, lower index first so an unordered pair has one key.
type pair struct{ a, b int32 }

// finding is one pair of sources with at least one kind of shared evidence.
type finding struct {
	A         sourceInfo          `json:"a"`
	B         sourceInfo          `json:"b"`
	Shared    [kindCount]int      `json:"shared"`
	DistinctA [kindCount]int      `json:"distinct_a"`
	DistinctB [kindCount]int      `json:"distinct_b"`
	SameName  bool                `json:"same_company_name"`
	Strength  float64             `json:"strength"`
	Examples  map[string][]string `json:"examples,omitempty"`
}

// options are the knobs, and the requisition thresholds are the whole reason
// this can run unattended. Every number in them was moved by a live run.
//
// A shared URL is essentially never a coincidence: URLs are long, and the
// 2026-07-28 corpus of 1,278,491 distinct URLs contained exactly 82 that
// appeared under more than one source, all four pairs of them Recruitee aliases.
// So one shared URL is worth printing.
//
// A shared requisition id is a different animal, and three false positives from
// live sweeps say how different:
//
//   - `eightfold/fluor` against `jibe/carenewengland` — Fluor Corporation and
//     Care New England, unrelated employers — shared 136 ids, 24% of the smaller
//     board. The ids were four-digit counters: 6535, 7365, 7414.
//   - Seven small boards each matched `jibe/dunhamssports` on "100%" of their
//     ids. Dunham's publishes 1,610 postings numbered densely enough to cover
//     any small board's range, so a 9-posting board matches all of itself and
//     0.6% of Dunham's. Scoring against the smaller board is what let that in.
//   - `brassring/guess` against `brassring/publix` shared 116 ids at 31% of each
//     board. Those ids look distinctive — 38126BR, 38194BR — and are not: the
//     "BR" is constant decoration on a counter.
//
// The third one killed an earlier version of this file that counted ids
// carrying a letter separately from plain numbers and held plain numbers to a
// higher bar. Measured across the corpus, that distinction is worthless:
// Workday publishes R242668, BrassRing 38126BR, Gem R267, SuccessFactors 451001.
// All four are a per-tenant counter with a fixed prefix or suffix, and the
// letter carries no information at all. So there is one rule for all of them.
//
// What separates a real pair from a collision is not the shape but the share, on
// BOTH sides: Zimmer Biomet was 362 of 365 and 362 of 373; the real pair a
// partial sweep found, two BrassRing gateways for UnityPoint, was 147 of 147 and
// 147 of 149. The highest false positive measured was 33%. The default sits at
// 50%, which is a real margin over that and well under both true pairs.
//
// This is a stated limitation, not a claim of completeness: an employer whose
// two routes overlap only partly, with no URL relationship at all, is below this
// threshold and this sweep will not report it. Requisition ids in this corpus do
// not carry enough identity to find that case without also reporting Fluor
// against Care New England every week.
type options struct {
	concurrency  int
	timeout      time.Duration
	platforms    string
	limit        int
	in           string
	dump         string
	jsonOut      string
	out          string
	minSharedURL int
	minSharedReq int
	minReqShare  float64
	maxPerKey    int
	minReqLen    int
	examples     int
}

func main() {
	opts := options{}

	flag.IntVar(&opts.concurrency, "concurrency", 8, "how many sources to crawl at once")
	flag.DurationVar(&opts.timeout, "timeout", 90*time.Minute, "budget for the whole sweep")
	flag.StringVar(&opts.platforms, "platform", "", "comma-separated platforms to sweep; empty means all")
	flag.IntVar(&opts.limit, "limit", 0, "crawl at most this many sources (0 = no limit), for smoke tests")
	flag.StringVar(&opts.in, "in", "", "analyse this NDJSON dump instead of crawling")
	flag.StringVar(&opts.dump, "dump", "", "write crawled rows here as NDJSON; also what makes examples available")
	flag.StringVar(&opts.jsonOut, "json", "", "write the findings here as JSON")
	flag.StringVar(&opts.out, "out", "", "write the markdown report here instead of stdout")
	flag.IntVar(&opts.minSharedURL, "min-shared-urls", 1, "report a pair sharing at least this many URLs")
	flag.IntVar(&opts.minSharedReq, "min-shared-reqs", 5, "report a pair sharing at least this many requisition ids")
	flag.Float64Var(&opts.minReqShare, "min-req-share", 0.50, "...and at least this share of EACH board's requisition ids")
	flag.IntVar(&opts.maxPerKey, "max-sources-per-key", 4, "discard an evidence key held by more than this many sources")
	flag.IntVar(&opts.minReqLen, "min-req-len", 4, "ignore requisition ids shorter than this")
	flag.IntVar(&opts.examples, "examples", 3, "how many example keys to quote per finding")
	flag.Parse()

	if err := run(opts); err != nil {
		fmt.Fprintln(os.Stderr, "dedupesweep:", err)
		os.Exit(1)
	}
}

// run never exits non-zero for a finding. This is a report for a human, exactly
// like verify_candidates.yml: a false positive that reddened CI would get the
// scheduled job disabled, and then the blind spot would be open again with a
// green checkmark over it. Only a failure to produce a report at all is an
// error.
func run(opts options) error {
	started := time.Now()

	var (
		sources []sourceInfo
		records []record
		err     error
	)

	if opts.in != "" {
		sources, records, err = analyseDump(opts)
	} else {
		sources, records, err = crawl(opts)
	}

	if err != nil {
		return err
	}

	findings := analyse(sources, records, opts)

	// Examples come from a second pass over the raw rows, which only exists if
	// this run wrote a dump or was given one. Without it the counts still stand;
	// only the quoted URLs are missing, and the report says so.
	evidenceFile := opts.in
	if evidenceFile == "" {
		evidenceFile = opts.dump
	}

	if evidenceFile != "" && opts.examples > 0 {
		if err := attachExamples(evidenceFile, findings, opts); err != nil {
			fmt.Fprintln(os.Stderr, "dedupesweep: could not collect examples:", err)
		}
	}

	if opts.jsonOut != "" {
		if err := writeJSON(opts.jsonOut, findings); err != nil {
			return err
		}
	}

	report := renderReport(sources, records, findings, opts, time.Since(started), evidenceFile != "")

	if opts.out == "" {
		_, err = io.WriteString(os.Stdout, report)

		return err
	}

	return os.WriteFile(opts.out, []byte(report), 0o644)
}

// crawl fetches every selected source and turns each posting into its evidence
// keys as it arrives, so the postings themselves are never all held at once.
func crawl(opts options) ([]sourceInfo, []record, error) {
	selected := selectSources(opts)
	if len(selected) == 0 {
		return nil, nil, fmt.Errorf("no sources selected (platform=%q)", opts.platforms)
	}

	ctx, cancel := context.WithTimeout(context.Background(), opts.timeout)
	defer cancel()

	client := httpx.NewClient()

	infos := make([]sourceInfo, len(selected))
	for i, source := range selected {
		infos[i] = sourceInfo{Platform: source.Platform, Key: source.Key, Company: source.Company}
	}

	var dumpWriter *bufio.Writer

	if opts.dump != "" {
		file, err := os.Create(opts.dump)
		if err != nil {
			return nil, nil, fmt.Errorf("create dump: %w", err)
		}

		defer file.Close()

		dumpWriter = bufio.NewWriterSize(file, 1<<20)
		defer dumpWriter.Flush()
	}

	type emitted struct {
		src int32
		row row
	}

	// One consumer owns both the record slice and the dump file, so neither
	// needs a lock and the dump stays a valid stream even at concurrency 64.
	rows := make(chan emitted, 4096)
	records := make([]record, 0, 1<<20)

	var consumer sync.WaitGroup

	consumer.Add(1)

	// The progress printer below wants the key count while the consumer is still
	// appending, and reading len(records) from another goroutine is a data race
	// the race detector is right to flag. Publishing the count separately keeps
	// the consumer the sole owner of the slice.
	var keyCount atomic.Int64

	go func() {
		defer consumer.Done()

		encoder := json.NewEncoder(dumpWriter)

		for item := range rows {
			records = appendKeys(records, item.src, item.row.URL, item.row.ReqID, opts)
			keyCount.Store(int64(len(records)))

			if dumpWriter != nil {
				_ = encoder.Encode(item.row)
			}
		}
	}()

	concurrency := max(opts.concurrency, 1)

	work := make(chan int, len(selected))
	for i := range selected {
		work <- i
	}

	close(work)

	var workers sync.WaitGroup

	progress := make(chan int, len(selected))

	for range concurrency {
		workers.Add(1)

		go func() {
			defer workers.Done()

			for i := range work {
				source := selected[i]
				start := time.Now()

				var postings, failures int

				for posting, err := range source.Jobs(ctx, client) {
					if err != nil {
						failures++

						continue
					}

					postings++

					rows <- emitted{src: int32(i), row: row{
						Platform: source.Platform,
						Key:      source.Key,
						Company:  posting.Company,
						URL:      posting.URL,
						Title:    posting.Title,
						Location: posting.Location,
						ReqID:    posting.RequisitionID,
						ExtID:    posting.ExternalID,
					}}
				}

				infos[i].Postings = postings
				infos[i].Errors = failures
				infos[i].Seconds = time.Since(start).Seconds()

				progress <- i
			}
		}()
	}

	// Progress on stderr, because a 16-minute silent job on a runner is
	// indistinguishable from a hung one and gets cancelled by the next person to
	// look at it.
	done := make(chan struct{})

	go func() {
		defer close(done)

		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		var finished int

		for {
			select {
			case _, ok := <-progress:
				if !ok {
					return
				}

				finished++
			case <-ticker.C:
				var memory runtime.MemStats

				runtime.ReadMemStats(&memory)

				fmt.Fprintf(os.Stderr, "dedupesweep: %d/%d sources, %d evidence keys, %d MiB heap\n",
					finished, len(selected), keyCount.Load(), memory.HeapAlloc>>20)
			}
		}
	}()

	workers.Wait()
	close(progress)
	<-done
	close(rows)
	consumer.Wait()

	return infos, records, nil
}

// selectSources applies -platform and -limit to the registry, keeping the
// platform-interleaved order [services.SourcesMatching] returns.
//
// That order is not cosmetic, and this command was measured getting it wrong.
// Ranging over [services.Builtin] directly gives the registration order, which
// is one platform at a time, so a bounded worker pool spends its whole first
// wave inside a single ATS. With eight workers on Breezy's 1,492 tenants — all
// of them behind one limiter key at four concurrent — half the pool sat parked
// on a semaphore: 1,418 of 8,173 sources in 24 minutes, on track for well over
// two hours. Interleaved, the same pool has work on ~24 independent backends at
// all times. internal/services/builtin.go's interleaveSources comment predicts
// exactly this; the fix is to use it rather than to reimplement the walk.
func selectSources(opts options) []services.Source {
	var wanted map[string]bool

	if strings.TrimSpace(opts.platforms) != "" {
		wanted = make(map[string]bool)

		for _, name := range strings.Split(opts.platforms, ",") {
			if name = strings.TrimSpace(name); name != "" {
				wanted[name] = true
			}
		}
	}

	all := services.SourcesMatching(nil)
	selected := make([]services.Source, 0, len(all))

	for _, source := range all {
		if wanted != nil && !wanted[source.Platform] {
			continue
		}

		selected = append(selected, source)

		if opts.limit > 0 && len(selected) >= opts.limit {
			break
		}
	}

	return selected
}

// analyseDump reads a previous run's NDJSON instead of crawling. The source list
// is rebuilt from the rows, so a dump of two boards analyses exactly those two.
func analyseDump(opts options) ([]sourceInfo, []record, error) {
	file, err := os.Open(opts.in)
	if err != nil {
		return nil, nil, fmt.Errorf("open dump: %w", err)
	}

	defer file.Close()

	var (
		infos   []sourceInfo
		records []record
		index   = make(map[string]int32)
	)

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<20)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var item row

		if err := json.Unmarshal([]byte(line), &item); err != nil {
			return nil, nil, fmt.Errorf("parse dump: %w", err)
		}

		id := item.Platform + "/" + item.Key

		src, ok := index[id]
		if !ok {
			src = int32(len(infos))
			index[id] = src

			infos = append(infos, sourceInfo{Platform: item.Platform, Key: item.Key, Company: item.Company})
		}

		infos[src].Postings++
		records = appendKeys(records, src, item.URL, item.ReqID, opts)
	}

	if err := scanner.Err(); err != nil {
		return nil, nil, fmt.Errorf("read dump: %w", err)
	}

	return infos, records, nil
}

// appendKeys turns one posting into its evidence records.
func appendKeys(records []record, src int32, rawURL, reqID string, opts options) []record {
	if url := strings.TrimSpace(rawURL); url != "" {
		records = append(records, record{hash: hashKey(kindURL, url), src: src, kind: kindURL})

		if normalised := normaliseURL(url); normalised != "" {
			records = append(records, record{hash: hashKey(kindNormURL, normalised), src: src, kind: kindNormURL})
		}
	}

	if req := normaliseReq(reqID); len(req) >= opts.minReqLen {
		records = append(records, record{hash: hashKey(kindReq, req), src: src, kind: kindReq})
	}

	return records
}

// hashKey folds the kind into the hash so two kinds can share one sorted slice
// without a normalised URL ever matching a requisition id.
//
// A 64-bit hash over ~3.7 million keys has a chance-collision probability around
// 4e-7, which is below the rate at which a real overlap would be missed for any
// other reason. It is recorded here rather than assumed, because a collision
// would show up as a finding with one shared key and no explanation, and the
// minimum thresholds on requisition ids exist partly to keep a single spurious
// match from being reportable on its own.
func hashKey(kind uint8, key string) uint64 {
	digest := fnv.New64a()
	_, _ = digest.Write([]byte{kind})
	_, _ = io.WriteString(digest, key)

	return digest.Sum64()
}

// normaliseURL is the comparison key, NOT a proposal for [internal.Dedupe].
//
// docs/dedupe-audit.md measured every candidate rule against 1,278,491 distinct
// URLs and concluded Dedupe should normalise nothing: dropping the query string
// merges 56,847 URLs *within* a single board, and dropping "tracking"
// parameters deletes real Greenhouse postings whose whole identity is
// "?gh_jid=". Those findings are about collapsing postings silently. Here
// nothing is collapsed — two URLs that normalise alike are printed for a person
// to look at — so the rules that were too dangerous to apply are exactly the
// ones worth testing, and the two destructive ones are still left out.
//
// What is stripped, and why each one:
//
//   - a trailing "/apply" segment: the Phenom-onto-Workday shape. 4,729 of
//     4,731 Lowe's URLs, 1,556 of 1,558 KBR's, 15 of 18 Southwest's.
//   - a leading locale segment such as "/en-us": the Jibe-onto-Workday shape,
//     the only other prefix relationship in the corpus.
//   - lowercased scheme and host, sorted query, no trailing slash: measured to
//     merge nothing at all, kept because they cost nothing and a future adapter
//     may need them.
func normaliseURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return ""
	}

	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Fragment = ""
	parsed.RawFragment = ""

	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")

	if len(segments) > 0 && isLocaleSegment(segments[0]) {
		segments = segments[1:]
	}

	if len(segments) > 0 && strings.EqualFold(segments[len(segments)-1], "apply") {
		segments = segments[:len(segments)-1]
	}

	parsed.Path = "/" + strings.Join(segments, "/")
	parsed.RawPath = ""

	if query := parsed.Query(); len(query) > 0 {
		parsed.RawQuery = query.Encode() // Encode sorts by key.
	}

	return parsed.String()
}

// isLocaleSegment reports whether a path segment looks like "en", "en-us" or
// "en_US" — a language tag a career-site front end prepends to a path the
// underlying ATS serves without one.
//
// Deliberately narrow. Matching anything longer would eat a real path segment:
// "us" and "en" are locales, "job" and "careers" are not, and the difference is
// only two characters wide.
func isLocaleSegment(segment string) bool {
	language, region, split := strings.Cut(segment, "-")
	if !split {
		language, region, split = strings.Cut(segment, "_")
	}

	if !isAlpha(language) || len(language) != 2 {
		return false
	}

	if !split {
		return true
	}

	return len(region) == 2 && isAlpha(region)
}

func isAlpha(value string) bool {
	if value == "" {
		return false
	}

	for _, char := range value {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') {
			return false
		}
	}

	return true
}

// normaliseReq folds a requisition id to its comparable form. Case and
// surrounding space differ between adapters reading the same field; the digits
// and letters do not.
func normaliseReq(raw string) string {
	return strings.ToUpper(strings.TrimSpace(raw))
}

// analyse turns the records into findings.
//
// It sorts once and scans runs rather than building a map from key to sources:
// on a full sweep that is ~3.7 million records, where the sort is a few seconds
// and the map would be most of a gigabyte.
func analyse(sources []sourceInfo, records []record, opts options) []*finding {
	slices.SortFunc(records, func(a, b record) int {
		if a.kind != b.kind {
			return int(a.kind) - int(b.kind)
		}

		if a.hash != b.hash {
			if a.hash < b.hash {
				return -1
			}

			return 1
		}

		return int(a.src - b.src)
	})

	distinct := make([][kindCount]int, len(sources))
	pairs := make(map[pair]*[kindCount]int)

	for start := 0; start < len(records); {
		end := start + 1
		for end < len(records) && records[end].kind == records[start].kind && records[end].hash == records[start].hash {
			end++
		}

		// One source holding the same key twice is intra-board duplication,
		// which internal.Dedupe already handles and which says nothing about a
		// second route. Count each source once per key.
		run := records[start:end]
		holders := make([]int32, 0, 4)

		for i, item := range run {
			if i == 0 || item.src != run[i-1].src {
				holders = append(holders, item.src)
			}
		}

		kind := records[start].kind

		for _, src := range holders {
			distinct[src][kind]++
		}

		// A key held by more sources than this is a generic string, not an
		// identity: requisition ids like "1000" recur across unrelated
		// employers. Discarding it costs nothing real, because a genuine
		// two-route duplicate is held by exactly two.
		if len(holders) < 2 || len(holders) > opts.maxPerKey {
			start = end

			continue
		}

		for i := range holders {
			for j := i + 1; j < len(holders); j++ {
				key := pair{a: holders[i], b: holders[j]}

				counts, ok := pairs[key]
				if !ok {
					counts = &[kindCount]int{}
					pairs[key] = counts
				}

				counts[kind]++
			}
		}

		start = end
	}

	findings := make([]*finding, 0, 16)

	for key, counts := range pairs {
		item := &finding{
			A:         sources[key.a],
			B:         sources[key.b],
			Shared:    *counts,
			DistinctA: distinct[key.a],
			DistinctB: distinct[key.b],
			SameName:  strings.EqualFold(strings.TrimSpace(sources[key.a].Company), strings.TrimSpace(sources[key.b].Company)),
		}

		if !reportable(item, opts) {
			continue
		}

		item.Strength = strength(item)
		findings = append(findings, item)
	}

	// Blind-spot pairs first, then by strength: the report's first table is the
	// one no other check in this repo can produce.
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].SameName != findings[j].SameName {
			return !findings[i].SameName
		}

		if findings[i].Strength != findings[j].Strength {
			return findings[i].Strength > findings[j].Strength
		}

		return findings[i].A.id()+findings[i].B.id() < findings[j].A.id()+findings[j].B.id()
	})

	return findings
}

// reportable applies the thresholds. URL evidence is reportable on its own;
// requisition evidence needs a count and a share, for the reason [options]
// gives.
func reportable(item *finding, opts options) bool {
	if item.Shared[kindURL] >= opts.minSharedURL || item.Shared[kindNormURL] >= opts.minSharedURL {
		return true
	}

	return reqReportable(item, opts)
}

// reqReportable applies the requisition floors: an absolute count, so a handful
// of matches is never a finding, and a share that BOTH boards must meet.
//
// Both sides is the part that was measured rather than guessed. Scoring against
// the smaller board alone — the obvious statistic, and the first one this
// command used — reported 22 pairs on a partial sweep, seven of them small
// boards sitting inside `jibe/dunhamssports`'s numbering at a nominal 100%.
// Requiring the share on both sides is what a duplicate actually looks like:
// Zimmer Biomet was 362 of 365 AND 362 of 373, and Domino's against the
// University of Kansas — 55 shared ids, 59% of the smaller board — is 0.2% of
// the larger, and goes away.
func reqReportable(item *finding, opts options) bool {
	if item.Shared[kindReq] < opts.minSharedReq {
		return false
	}

	if item.DistinctA[kindReq] == 0 || item.DistinctB[kindReq] == 0 {
		return false
	}

	return float64(item.Shared[kindReq])/float64(item.DistinctA[kindReq]) >= opts.minReqShare &&
		float64(item.Shared[kindReq])/float64(item.DistinctB[kindReq]) >= opts.minReqShare
}

// strength is the share of the LARGER board that is shared, over the strongest
// kind of evidence the pair has.
//
// The larger board, deliberately: scoring against the smaller one turns "9 of my
// 9 postings appear somewhere in your 1,610" into a perfect 100%, which is how a
// partial sweep produced seven unrelated pairs at the top of its table. This
// number is the conservative reading, so a pair near 1.0 is two views of one
// board and a low one is a subset relationship worth a look but not a mirror.
// It orders the report and nothing else; the counts are printed beside it.
func strength(item *finding) float64 {
	var best float64

	for kind := range kindCount {
		larger := max(item.DistinctA[kind], item.DistinctB[kind])
		if larger == 0 {
			continue
		}

		if share := float64(item.Shared[kind]) / float64(larger); share > best {
			best = share
		}
	}

	return best
}

// attachExamples re-reads the rows to quote a few of the keys behind each
// finding. Counts alone are not actionable: "shares 362 requisition ids" sends a
// maintainer looking, but "shares R0012345" lets them check it in one request.
func attachExamples(path string, findings []*finding, opts options) error {
	if len(findings) == 0 {
		return nil
	}

	// Only the sources involved in a finding matter, and only the kinds that
	// finding actually has evidence for.
	interesting := make(map[string]bool)

	for _, item := range findings {
		interesting[item.A.id()] = true
		interesting[item.B.id()] = true
	}

	file, err := os.Open(path)
	if err != nil {
		return err
	}

	defer file.Close()

	// key -> source ids holding it, for the involved sources only.
	held := make(map[string]map[string]bool)

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<20)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var item row

		if err := json.Unmarshal([]byte(line), &item); err != nil {
			return err
		}

		id := item.Platform + "/" + item.Key
		if !interesting[id] {
			continue
		}

		for kind, key := range keysOf(item, opts) {
			if key == "" {
				continue
			}

			full := kindNames[kind] + "\x00" + key

			if held[full] == nil {
				held[full] = make(map[string]bool, 2)
			}

			held[full][id] = true
		}
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	for _, item := range findings {
		item.Examples = make(map[string][]string)

		for full, holders := range held {
			if !holders[item.A.id()] || !holders[item.B.id()] {
				continue
			}

			name, key, _ := strings.Cut(full, "\x00")

			if len(item.Examples[name]) < opts.examples {
				item.Examples[name] = append(item.Examples[name], key)
			}
		}

		for name := range item.Examples {
			sort.Strings(item.Examples[name])
		}
	}

	return nil
}

// keysOf returns a row's evidence keys by kind, in the same shape appendKeys
// hashes them. The two must agree or the examples would quote keys the counts
// were never derived from.
func keysOf(item row, opts options) [kindCount]string {
	var keys [kindCount]string

	if raw := strings.TrimSpace(item.URL); raw != "" {
		keys[kindURL] = raw
		keys[kindNormURL] = normaliseURL(raw)
	}

	if req := normaliseReq(item.ReqID); len(req) >= opts.minReqLen {
		keys[kindReq] = req
	}

	return keys
}

func writeJSON(path string, findings []*finding) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}

	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")

	if findings == nil {
		findings = []*finding{}
	}

	return encoder.Encode(findings)
}

// renderReport writes the markdown a human reads. It is written to be pasted
// into a GitHub step summary unchanged.
func renderReport(sources []sourceInfo, records []record, findings []*finding, opts options, elapsed time.Duration, haveExamples bool) string {
	var out strings.Builder

	var postings, errored, empty int

	for _, source := range sources {
		postings += source.Postings

		if source.Errors > 0 {
			errored++
		}

		if source.Postings == 0 {
			empty++
		}
	}

	var blind, named []*finding

	for _, item := range findings {
		if item.SameName {
			named = append(named, item)
		} else {
			blind = append(blind, item)
		}
	}

	fmt.Fprintf(&out, "## Registry dedupe sweep\n\n")
	fmt.Fprintf(&out, "%d sources, %s postings, %s evidence keys, %s wall clock. "+
		"%d sources returned nothing and %d reported at least one error.\n\n",
		len(sources), commas(postings), commas(len(records)), elapsed.Round(time.Second), empty, errored)

	fmt.Fprintf(&out, "**%d pair(s) with evidence of overlap: %d under different company names "+
		"(the blind spot), %d under the same name.**\n\n", len(findings), len(blind), len(named))

	// Said plainly, because the two modes count sources differently and a reader
	// comparing two reports would otherwise see a source census move on its own.
	if opts.in != "" {
		fmt.Fprintf(&out, "This is a re-analysis of `%s`, not a crawl. The source list is rebuilt from "+
			"the dump, so a board that returned nothing when the dump was made is absent here rather "+
			"than counted as empty.\n\n", opts.in)
	}

	// Coverage, stated rather than implied. A sweep that crawled 60% of the
	// registry and found nothing has not shown that there is nothing.
	if empty > 0 || errored > 0 {
		fmt.Fprintf(&out, "A board that returned nothing this run cannot be compared with anything, "+
			"so the %d empty and %d erroring sources are not evidence of no overlap. "+
			"They are unmeasured.\n\n", empty, errored)
	}

	out.WriteString("### Under different company names — invisible to TestNoUnreviewedDoubleCountedEmployer\n\n")

	if len(blind) == 0 {
		out.WriteString("None. This is the list the sweep exists for: `southwestair`/`swa` and " +
			"`zimmerbiomet`/`zimmerin01` would appear here.\n\n")
	} else {
		writeFindingsTable(&out, blind, haveExamples, opts)
		out.WriteString("\nEach of these is a **new finding**: no test in this repo can see it, because " +
			"the two sides carry different company names. Measure the pair with " +
			"`go run ./tools/dedupeprobe` before deciding anything — an overlap is not " +
			"automatically a route to delete, and `deletedDoubleCountRoutes` records several " +
			"pairs where keeping both was right.\n\n" +
			"Read the columns in order of seriousness. A pair whose only evidence is **shared raw " +
			"URLs** is not a double count: `internal.Dedupe` keys on URL and already collapses " +
			"those, so the cost is duplicate requests. The four Recruitee alias pairs measured on " +
			"2026-07-28 are exactly that shape. A pair that shares URLs **only after normalising**, " +
			"or shares **requisition ids with no URL relationship at all**, is the shape that " +
			"inflates the count, because each opening then reaches the corpus under two different " +
			"URLs and nothing downstream can see it.\n\n")
	}

	out.WriteString("### Under the same company name — already reviewed by the unit test\n\n")

	if len(named) == 0 {
		out.WriteString("None.\n\n")
	} else {
		out.WriteString("`TestNoUnreviewedDoubleCountedEmployer` already requires a recorded verdict for " +
			"each of these, so they are context, not news. What is new is the numbers: a pair drifting " +
			"toward total overlap is a route that has become a mirror and should be re-decided.\n\n")
		writeFindingsTable(&out, named, haveExamples, opts)
		out.WriteString("\n")
	}

	fmt.Fprintf(&out, "### What was compared\n\n")
	fmt.Fprintf(&out, "| Evidence | Threshold |\n| --- | --- |\n")
	fmt.Fprintf(&out, "| Shared raw URL | report at %d |\n", opts.minSharedURL)
	fmt.Fprintf(&out, "| Shared URL after stripping `/apply`, a leading locale segment and sorting query parameters | report at %d |\n", opts.minSharedURL)
	fmt.Fprintf(&out, "| Shared requisition id | report at %d **and** %.0f%% of *each* board |\n",
		opts.minSharedReq, opts.minReqShare*100)
	fmt.Fprintf(&out, "| Any key held by more than %d sources | discarded as a generic string |\n\n", opts.maxPerKey)

	fmt.Fprintf(&out, "**A shared count here is a floor, not the overlap.** Discarding a key held by more "+
		"than %d sources also discards it for a pair that genuinely shares it, so a real pair reads low: "+
		"`eightfold/houstonisd` against `successfactors/hisd` reported 239 shared requisition ids on "+
		"2026-07-28 where a direct two-board comparison of the same crawl found 296 of 298. Raising the "+
		"limit recovers those and lets coincidences back in — at 32 the same sweep reported 293 for that "+
		"pair and one extra pair sharing 7 ids. Settle a pair with `tools/dedupeprobe`, which applies no "+
		"thresholds at all.\n\n", opts.maxPerKey)

	out.WriteString("Titles and locations are deliberately not evidence: `double_count_test.go` records " +
		"Visa flagged on the two bare titles \"Sr. Manager\" and \"Director\", and Chipotle sharing " +
		"52 of 55 titles across boards that share 12 of 178 title+location pairs.\n\n")

	if !haveExamples {
		out.WriteString("_Examples are unavailable: this run wrote no `-dump`, so the keys behind each " +
			"count could not be quoted._\n\n")
	}

	return out.String()
}

func writeFindingsTable(out *strings.Builder, findings []*finding, haveExamples bool, opts options) {
	out.WriteString("| A | B | postings | shared URLs | shared after normalising | shared req ids | overlap, of the larger board |\n")
	out.WriteString("| --- | --- | --- | ---: | ---: | ---: | ---: |\n")

	for _, item := range findings {
		fmt.Fprintf(out, "| `%s` | `%s` | %s / %s | %s | %s | %s | %.0f%% |\n",
			cell(item.A.id()), cell(item.B.id()),
			commas(item.A.Postings), commas(item.B.Postings),
			commas(item.Shared[kindURL]), commas(item.Shared[kindNormURL]), commas(item.Shared[kindReq]),
			item.Strength*100)
	}

	if !haveExamples {
		return
	}

	for _, item := range findings {
		if len(item.Examples) == 0 {
			continue
		}

		fmt.Fprintf(out, "\n<details><summary><code>%s</code> vs <code>%s</code></summary>\n\n",
			item.A.id(), item.B.id())

		for kind := range kindCount {
			examples := item.Examples[kindNames[kind]]
			if len(examples) == 0 {
				continue
			}

			fmt.Fprintf(out, "%s, %s shared, showing %d:\n\n```\n", kindNames[kind],
				commas(item.Shared[kind]), min(len(examples), opts.examples))

			for _, example := range examples {
				fmt.Fprintf(out, "%s\n", example)
			}

			out.WriteString("```\n\n")
		}

		out.WriteString("</details>\n")
	}
}

// cell escapes the one character that can break a markdown table. Source keys
// are tenant URLs and comma-joined triples, so this has never fired, but a table
// silently losing a column is a bad way to find out that a new platform's key
// carries a pipe.
func cell(value string) string {
	return strings.ReplaceAll(value, "|", "\\|")
}

// commas formats an integer with thousands separators, because the numbers this
// prints are routinely six digits and a maintainer has to compare them by eye.
func commas(value int) string {
	digits := fmt.Sprintf("%d", value)

	sign := ""
	if strings.HasPrefix(digits, "-") {
		sign, digits = "-", digits[1:]
	}

	var out strings.Builder

	for i, char := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			out.WriteByte(',')
		}

		out.WriteRune(char)
	}

	return sign + out.String()
}
