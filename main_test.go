package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/services"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestPostingPrinterText(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	print, flush, err := newPostingPrinter(&buf, postingOutput{})
	if err != nil {
		t.Fatalf("newPostingPrinter() error = %v", err)
	}

	posting := &internal.JobPosting{
		Company:  "acme",
		Title:    "Security Engineer",
		Location: "Remote",
		URL:      "https://example.test/1",
	}

	if err := print(posting); err != nil {
		t.Fatalf("print() error = %v", err)
	}

	if err := flush(); err != nil {
		t.Fatalf("flush() error = %v", err)
	}

	got := buf.String()

	for _, want := range []string{"acme", "Security Engineer", "Remote", "https://example.test/1"} {
		if !strings.Contains(got, want) {
			t.Errorf("output %q does not contain %q", got, want)
		}
	}
}

func TestPostingPrinterJSON(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	print, flush, err := newPostingPrinter(&buf, postingOutput{asJSON: true})
	if err != nil {
		t.Fatalf("newPostingPrinter() error = %v", err)
	}

	postings := []*internal.JobPosting{
		{Company: "acme", Title: "One", Location: "Remote", URL: "https://example.test/1"},
		{Company: "globex", Title: "Two", Location: "Austin", URL: "https://example.test/2"},
	}

	for _, posting := range postings {
		if err := print(posting); err != nil {
			t.Fatalf("print() error = %v", err)
		}
	}

	if err := flush(); err != nil {
		t.Fatalf("flush() error = %v", err)
	}

	// Output must be newline-delimited JSON: one complete object per line, so it
	// streams into jq and friends.
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")

	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2: %q", len(lines), buf.String())
	}

	for i, line := range lines {
		var decoded internal.JobPosting

		if err := json.Unmarshal([]byte(line), &decoded); err != nil {
			t.Fatalf("line %d is not valid JSON: %v", i, err)
		}

		if decoded.Title != postings[i].Title {
			t.Errorf("line %d title = %q, want %q", i, decoded.Title, postings[i].Title)
		}
	}
}

func TestPostingPrinterCSV(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	print, flush, err := newPostingPrinter(&buf, postingOutput{asCSV: true})
	if err != nil {
		t.Fatalf("newPostingPrinter() error = %v", err)
	}

	// A title containing a comma and a quote must survive CSV encoding.
	posting := &internal.JobPosting{
		Company:  "acme",
		Title:    `Engineer, "Staff" Level`,
		Location: "Remote",
		URL:      "https://example.test/1",
	}

	if err := print(posting); err != nil {
		t.Fatalf("print() error = %v", err)
	}

	if err := flush(); err != nil {
		t.Fatalf("flush() error = %v", err)
	}

	records, err := csv.NewReader(&buf).ReadAll()
	if err != nil {
		t.Fatalf("output is not valid CSV: %v", err)
	}

	if len(records) != 1 {
		t.Fatalf("got %d records, want 1", len(records))
	}

	want := []string{"acme", `Engineer, "Staff" Level`, "Remote", "https://example.test/1"}

	for i := range want {
		if records[0][i] != want[i] {
			t.Errorf("field %d = %q, want %q", i, records[0][i], want[i])
		}
	}
}

func TestCheckSourcesClassifiesResults(t *testing.T) {
	t.Parallel()

	sources := []services.Source{
		{
			Company: "has-postings",
			Jobs: func(ctx context.Context, _ *http.Client) internal.Jobs {
				return func(yield func(*internal.JobPosting, error) bool) {
					yield(&internal.JobPosting{Company: "has-postings", Title: "One"}, nil)
					yield(&internal.JobPosting{Company: "has-postings", Title: "Two"}, nil)
				}
			},
		},
		{
			Company: "no-postings",
			Jobs: func(ctx context.Context, _ *http.Client) internal.Jobs {
				return func(yield func(*internal.JobPosting, error) bool) {}
			},
		},
		{
			Company: "broken",
			Jobs: func(ctx context.Context, _ *http.Client) internal.Jobs {
				return func(yield func(*internal.JobPosting, error) bool) {
					yield(nil, errors.New("404 Not Found"))
				}
			},
		},
	}

	results := checkSources(t.Context(), nil, sources, 2)

	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}

	byCompany := map[string]sourceHealth{}
	for _, r := range results {
		byCompany[r.Company] = r
	}

	if got := byCompany["has-postings"]; got.Status != statusOK || got.Postings != 2 {
		t.Errorf("has-postings = %+v, want status %q with 2 postings", got, statusOK)
	}

	// A reachable board with nothing posted is a company that is not hiring,
	// which must not be reported as broken.
	if got := byCompany["no-postings"]; got.Status != statusEmpty {
		t.Errorf("no-postings status = %q, want %q", got.Status, statusEmpty)
	}

	if got := byCompany["broken"]; got.Status != statusFailed {
		t.Errorf("broken status = %q, want %q", got.Status, statusFailed)
	} else if !strings.Contains(got.Error, "404") {
		t.Errorf("broken error = %q, want it to carry the cause", got.Error)
	}
}

func TestCheckSourcesReportsPostingsDespiteAnError(t *testing.T) {
	t.Parallel()

	// A paginated source can fail partway through. It still found postings, so
	// it counts as ok rather than failed; but the error is retained.
	sources := []services.Source{
		{
			Company: "partial",
			Jobs: func(ctx context.Context, _ *http.Client) internal.Jobs {
				return func(yield func(*internal.JobPosting, error) bool) {
					yield(&internal.JobPosting{Company: "partial", Title: "One"}, nil)
					yield(nil, errors.New("page 2 failed"))
				}
			},
		},
	}

	results := checkSources(t.Context(), nil, sources, 1)

	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}

	if results[0].Status != statusOK {
		t.Errorf("status = %q, want %q", results[0].Status, statusOK)
	}

	if results[0].Error == "" {
		t.Error("Error is empty, want the partial failure retained")
	}
}

func TestCheckSourcesHandlesNoSources(t *testing.T) {
	t.Parallel()

	if got := checkSources(t.Context(), nil, nil, 4); len(got) != 0 {
		t.Errorf("got %d results, want 0", len(got))
	}
}

func TestCompaniesCommandOutput(t *testing.T) {
	t.Parallel()

	cmd := newRootCommand()

	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"companies"})

	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")

	if len(lines) < 100 {
		t.Errorf("got %d companies, want a substantial list", len(lines))
	}

	// The list is sorted and deduplicated, which is what makes it diffable
	// between releases.
	for i := 1; i < len(lines); i++ {
		if strings.EqualFold(lines[i-1], lines[i]) {
			t.Errorf("duplicate company at line %d: %q", i, lines[i])

			break
		}

		if strings.ToLower(lines[i-1]) > strings.ToLower(lines[i]) {
			t.Errorf("companies are not sorted: %q came before %q", lines[i-1], lines[i])

			break
		}
	}
}

func TestCompaniesCommandJSON(t *testing.T) {
	t.Parallel()

	cmd := newRootCommand()

	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"companies", "--json"})

	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}

	var companies []string
	if err := json.Unmarshal(stdout.Bytes(), &companies); err != nil {
		t.Fatalf("output is not a JSON array: %v", err)
	}

	if len(companies) < 100 {
		t.Errorf("got %d companies, want a substantial list", len(companies))
	}
}

func TestPostingsRejectsUnknownCompany(t *testing.T) {
	t.Parallel()

	// Narrowing to a company that does not exist should be an explicit error
	// rather than an empty result that looks like "nobody is hiring".
	cmd := newRootCommand()

	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"postings", "--company", "definitely-not-a-real-company-xyzzy"})

	err := cmd.ExecuteContext(t.Context())
	if err == nil {
		t.Fatal("ExecuteContext() error = nil, want an error")
	}

	if !strings.Contains(err.Error(), "no known companies") {
		t.Errorf("error = %v, want it to explain that no companies matched", err)
	}
}

func TestGlobalFlagsLoggerLevel(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	flags := globalFlags{logLevel: "debug"}
	logger := flags.logger(&buf)

	logger.Debug("a debug message")

	if !strings.Contains(buf.String(), "a debug message") {
		t.Errorf("debug level did not log: %q", buf.String())
	}

	// An unparseable level must not panic or silence logging entirely; it falls
	// back to warn.
	buf.Reset()

	flags = globalFlags{logLevel: "nonsense"}
	logger = flags.logger(&buf)

	logger.Warn("a warning")

	if !strings.Contains(buf.String(), "a warning") {
		t.Errorf("fallback level did not log warnings: %q", buf.String())
	}

	buf.Reset()
	flags = globalFlags{logLevel: "info", logFormat: "json"}
	logger = flags.logger(&buf)
	logger.Info("structured message", slog.String("platform", "workday"))

	var record map[string]any
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("JSON logger output = %q: %v", buf.String(), err)
	}
	if record["msg"] != "structured message" || record["platform"] != "workday" {
		t.Errorf("JSON logger record = %#v, want message and platform fields", record)
	}
}

func TestGlobalFlagsRejectInvalidProxy(t *testing.T) {
	t.Parallel()

	flags := globalFlags{proxies: []string{"file:///tmp/proxy.sock"}}
	logger := slog.New(slog.DiscardHandler)

	if _, err := flags.client(logger); err == nil {
		t.Fatal("client() error = nil, want invalid proxy rejected before crawling")
	}
}

func TestTotalReportsTruncatedCrawl(t *testing.T) {
	t.Parallel()

	// A crawl cut short by its deadline must fail rather than print a count that
	// looks complete. The posting trend in jobs_record.txt is the project's only
	// historical record, and a silently partial row corrupts it permanently.
	cmd := newRootCommand()

	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	// A timeout this small cannot finish a crawl of over a thousand sources.
	cmd.SetArgs([]string{"total", "--timeout=1ns"})

	err := cmd.ExecuteContext(t.Context())
	if err == nil {
		t.Fatal("ExecuteContext() error = nil, want an error for a truncated crawl")
	}

	if !strings.Contains(err.Error(), "must not be recorded") {
		t.Errorf("error = %v, want it to warn against recording the count", err)
	}

	if !strings.HasSuffix(strings.TrimSpace(stdout.String()), "partial") {
		t.Errorf("stdout = %q, want an explicitly partial row", stdout.String())
	}
}

func TestTotalCanRecordExplicitPartialWithManifest(t *testing.T) {
	t.Parallel()

	cmd := newRootCommand()
	manifestPath := filepath.Join(t.TempDir(), "manifest.json")

	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{
		"total",
		"--timeout=1ns",
		"--allow-partial",
		"--manifest=" + manifestPath,
	})

	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("ExecuteContext() error = %v, want explicit partial recording to succeed", err)
	}

	fields := strings.Fields(stdout.String())
	if len(fields) != 4 || fields[3] != "partial" {
		t.Fatalf("stdout fields = %q, want DATE POSTINGS COMPANIES partial", fields)
	}

	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", manifestPath, err)
	}

	var manifest crawlManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("Unmarshal(manifest) error = %v", err)
	}
	if manifest.SchemaVersion != crawlManifestSchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", manifest.SchemaVersion, crawlManifestSchemaVersion)
	}
	if manifest.Status != "partial" {
		t.Errorf("Status = %q, want partial", manifest.Status)
	}
	if got := manifest.SourceCounts["planned"]; got == 0 {
		t.Errorf("planned source count = %d, want sources preserved as not started", got)
	}
}

func TestCheckSourcesCapsPostingCount(t *testing.T) {
	t.Parallel()

	// A health check must not paginate through an enormous employer: FedEx alone
	// publishes over 138,000 postings. Counting is capped, and breaking out of the
	// iteration is what tells the adapter to stop fetching pages.
	var yielded int

	sources := []services.Source{
		{
			Company: "enormous",
			Jobs: func(ctx context.Context, _ *http.Client) internal.Jobs {
				return func(yield func(*internal.JobPosting, error) bool) {
					for i := range 10_000 {
						yielded++
						if !yield(&internal.JobPosting{Company: "enormous", Title: fmt.Sprint(i)}, nil) {
							return
						}
					}
				}
			},
		},
	}

	results := checkSources(t.Context(), nil, sources, 1)

	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}

	if results[0].Postings != healthSampleLimit {
		t.Errorf("Postings = %d, want the cap of %d", results[0].Postings, healthSampleLimit)
	}

	if !results[0].Capped {
		t.Error("Capped = false, want true so the count is reported as a floor")
	}

	if results[0].Status != statusOK {
		t.Errorf("Status = %q, want %q", results[0].Status, statusOK)
	}

	// The source must have been stopped, not drained.
	if yielded > healthSampleLimit+1 {
		t.Errorf("source yielded %d postings, want it stopped near the cap of %d", yielded, healthSampleLimit)
	}
}

func TestCheckSourcesSurvivesPanickingSource(t *testing.T) {
	t.Parallel()

	// A health check exists to find broken sources, so it must survive one that
	// is broken badly enough to panic. This is not hypothetical: the Jobvite
	// adapter ignored yield's return value and panicked as soon as this command
	// began stopping early at the posting cap.
	sources := []services.Source{
		{
			Company: "panics",
			Jobs: func(ctx context.Context, _ *http.Client) internal.Jobs {
				return func(yield func(*internal.JobPosting, error) bool) {
					panic("adapter walked off the end of the page")
				}
			},
		},
		{
			Company: "healthy",
			Jobs: func(ctx context.Context, _ *http.Client) internal.Jobs {
				return func(yield func(*internal.JobPosting, error) bool) {
					yield(&internal.JobPosting{Company: "healthy", Title: "One"}, nil)
				}
			},
		},
	}

	results := checkSources(t.Context(), nil, sources, 2)

	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}

	byCompany := map[string]sourceHealth{}
	for _, r := range results {
		byCompany[r.Company] = r
	}

	panicked := byCompany["panics"]
	if panicked.Status != statusFailed {
		t.Errorf("panicking source status = %q, want %q", panicked.Status, statusFailed)
	}

	if !strings.Contains(panicked.Error, "panicked") {
		t.Errorf("panicking source error = %q, want it to mention the panic", panicked.Error)
	}

	// The healthy source must still have been checked.
	if got := byCompany["healthy"]; got.Status != statusOK || got.Postings != 1 {
		t.Errorf("healthy source = %+v, want ok with 1 posting", got)
	}
}

func TestPostingPrinterCSVIncludesPayColumns(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	print, flush, err := newPostingPrinter(&buf, postingOutput{asCSV: true})
	if err != nil {
		t.Fatalf("newPostingPrinter() error = %v", err)
	}

	postings := []*internal.JobPosting{
		{
			Company:  "harvey",
			Title:    "Staff Engineer",
			Location: "San Francisco",
			URL:      "https://example.test/1",
			Compensation: &internal.Compensation{
				Min: 236000, Max: 290000, Currency: "USD", Period: internal.PeriodYear,
			},
		},
		{
			Company:  "semgrep",
			Title:    "Product Manager",
			Location: "Boston",
			URL:      "https://example.test/2",
		},
	}

	for _, p := range postings {
		if err := print(p); err != nil {
			t.Fatalf("print() error = %v", err)
		}
	}

	if err := flush(); err != nil {
		t.Fatalf("flush() error = %v", err)
	}

	records, err := csv.NewReader(&buf).ReadAll()
	if err != nil {
		t.Fatalf("output is not valid CSV: %v", err)
	}

	if len(records) != 2 {
		t.Fatalf("got %d records, want 2", len(records))
	}

	// The original four fields must stay in place so existing consumers keep
	// working; pay is appended after them.
	want := []string{"harvey", "Staff Engineer", "San Francisco", "https://example.test/1", "236000", "290000", "USD", "year"}
	if len(records[0]) != len(want) {
		t.Fatalf("got %d columns, want %d: %v", len(records[0]), len(want), records[0])
	}

	for i := range want {
		if records[0][i] != want[i] {
			t.Errorf("column %d = %q, want %q", i, records[0][i], want[i])
		}
	}

	// An undisclosed range leaves the pay columns empty rather than writing zeros,
	// which would read as "this job pays nothing".
	for i, got := range records[1][4:] {
		if got != "" {
			t.Errorf("undisclosed pay column %d = %q, want empty", i+4, got)
		}
	}
}

func TestDescribeCompensation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		comp *internal.Compensation
		want string
	}{
		{
			name: "prefers the board's own summary",
			comp: &internal.Compensation{Min: 1, Max: 2, Summary: "$160K – $185K • Offers Equity"},
			want: "$160K – $185K • Offers Equity",
		},
		{
			name: "range with currency and period",
			comp: &internal.Compensation{Min: 17.17, Max: 29.95, Period: internal.PeriodHour},
			want: "17.17-29.95/hour",
		},
		{
			name: "max only",
			comp: &internal.Compensation{Max: 150000, Currency: "USD", Period: internal.PeriodYear},
			want: "USD 150000/year",
		},
		{
			name: "min only",
			comp: &internal.Compensation{Min: 90000, Period: internal.PeriodYear},
			want: "90000/year",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := describeCompensation(tt.comp); got != tt.want {
				t.Errorf("describeCompensation() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPostingFilterDropsCompanyConstraint(t *testing.T) {
	t.Parallel()

	// Regression test. Company selection is honoured by narrowing which sources
	// are crawled. Applying it again to the postings compared a source key
	// (a Workday tenant URL, a Phenom hostname) against the short company name
	// derived from it, so every posting was discarded and the run reported
	// "0 postings" as though nobody were hiring.
	//
	// Verified live before the fix: `--company careers.molsoncoors.com` returned
	// 0 postings from 1 source; after, 197.
	original := internal.Filter{
		Companies:       []string{"careers.molsoncoors.com", "pfizer.wd1.myworkdayjobs.com"},
		Titles:          []string{"engineer"},
		ExcludeTitles:   []string{"intern"},
		Locations:       []string{"remote"},
		Remote:          true,
		HasCompensation: true,
		MinAnnual:       150000,
	}

	got := postingFilterFor(original)

	if len(got.Companies) != 0 {
		t.Errorf("Companies = %v, want it dropped", got.Companies)
	}

	// Every other constraint must survive untouched.
	if !slices.Equal(got.Titles, original.Titles) {
		t.Errorf("Titles = %v, want %v", got.Titles, original.Titles)
	}

	if !slices.Equal(got.ExcludeTitles, original.ExcludeTitles) {
		t.Errorf("ExcludeTitles = %v, want %v", got.ExcludeTitles, original.ExcludeTitles)
	}

	if !slices.Equal(got.Locations, original.Locations) {
		t.Errorf("Locations = %v, want %v", got.Locations, original.Locations)
	}

	if !got.Remote || !got.HasCompensation || got.MinAnnual != 150000 {
		t.Errorf("got %+v, want the remote and pay constraints preserved", got)
	}

	// The caller's filter must not be mutated.
	if len(original.Companies) != 2 {
		t.Errorf("original filter was mutated: Companies = %v", original.Companies)
	}

	// A posting whose company name differs from the source key must now survive.
	posting := &internal.JobPosting{
		Company:      "molsoncoors",
		Title:        "Security Engineer",
		Location:     "Remote",
		Compensation: &internal.Compensation{Min: 160000, Max: 200000, Period: internal.PeriodYear},
	}

	if !got.Match(posting) {
		t.Error("posting was rejected; the source-key/company-name mismatch is back")
	}

	if original.Match(posting) {
		t.Error("test premise is wrong: the original filter should reject this posting")
	}
}

// enrichedPosting is a posting carrying every schema field, used to prove the
// default output formats ignore all of them.
func enrichedPosting() *internal.JobPosting {
	posted := time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC)

	return &internal.JobPosting{
		Company:        "harvey",
		Title:          "Staff Engineer",
		Location:       "San Francisco",
		URL:            "https://example.test/1",
		Compensation:   &internal.Compensation{Min: 236000, Max: 290000, Currency: "USD", Period: internal.PeriodYear},
		Department:     "Engineering",
		Team:           "Platform",
		EmploymentType: internal.EmploymentTypeFullTime,
		WorkplaceType:  internal.WorkplaceTypeHybrid,
		Seniority:      "Staff",
		PostedAt:       posted,
		UpdatedAt:      posted.Add(48 * time.Hour),
		RequisitionID:  "JR0012345",
		ExternalID:     "4019283",
		Source:         internal.PostingSource{Platform: "greenhouse", Key: "harveyai"},
	}
}

func TestPostingPrinterCSVDefaultIgnoresEnrichment(t *testing.T) {
	t.Parallel()

	// The default CSV is a committed contract: eight columns, no header. Someone
	// piping it into cut(1) or a spreadsheet import must see the same bytes after
	// the schema grew as before, so this is asserted as bytes rather than as a
	// column count.
	var buf bytes.Buffer

	print, flush, err := newPostingPrinter(&buf, postingOutput{asCSV: true})
	must.NoError(t, err)
	must.NoError(t, print(enrichedPosting()))
	must.NoError(t, flush())

	const want = "harvey,Staff Engineer,San Francisco,https://example.test/1,236000,290000,USD,year\n"

	must.Eq(t, want, buf.String(), must.Sprint("the headerless 8-column CSV contract changed"))
}

func TestPostingPrinterCSVExtendedColumns(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	columns, err := resolveCSVColumns(csvColumnsExtended)
	must.NoError(t, err)

	output := postingOutput{asCSV: true, csvColumns: columns, csvHeader: true}

	print, flush, err := newPostingPrinter(&buf, output)
	must.NoError(t, err)
	must.NoError(t, print(enrichedPosting()))
	must.NoError(t, flush())

	records, err := csv.NewReader(&buf).ReadAll()
	must.NoError(t, err)
	must.Len(t, 2, records, must.Sprint("want a header row and one posting"))

	header, row := records[0], records[1]

	// The core eight keep their names, their order, and their position, so an
	// extended file can be read by anything that reads the default one, once the
	// header is skipped.
	must.Eq(t, csvCoreColumnNames, header[:len(csvCoreColumnNames)])
	must.Eq(t, csvExtendedColumnNames, header)

	byName := map[string]string{}
	for i, name := range header {
		byName[name] = row[i]
	}

	for name, want := range map[string]string{
		"company":         "harvey",
		"department":      "Engineering",
		"team":            "Platform",
		"employment_type": "full_time",
		"workplace_type":  "hybrid",
		"seniority":       "Staff",
		"posted_at":       "2026-06-01T09:30:00Z",
		"updated_at":      "2026-06-03T09:30:00Z",
		"requisition_id":  "JR0012345",
		"external_id":     "4019283",
		"source_platform": "greenhouse",
		"source_key":      "harveyai",
	} {
		test.Eq(t, want, byName[name], test.Sprintf("column %q", name))
	}
}

func TestPostingPrinterCSVEmptyEnrichmentCellsStayEmpty(t *testing.T) {
	t.Parallel()

	// A board that publishes nothing must produce empty cells, never a
	// placeholder: an empty cell reads as absent everywhere, while "unknown" or
	// a zero timestamp is data that was never collected.
	columns, err := resolveCSVColumns(csvColumnsExtended)
	must.NoError(t, err)

	var buf bytes.Buffer

	print, flush, err := newPostingPrinter(&buf, postingOutput{asCSV: true, csvColumns: columns})
	must.NoError(t, err)
	must.NoError(t, print(&internal.JobPosting{Company: "acme", Title: "Cook", URL: "https://example.test/2"}))
	must.NoError(t, flush())

	records, err := csv.NewReader(&buf).ReadAll()
	must.NoError(t, err)
	must.Len(t, 1, records)

	for i, cell := range records[0][len(csvCoreColumnNames):] {
		test.Eq(t, "", cell, test.Sprintf("enrichment column %q", csvExtendedColumnNames[len(csvCoreColumnNames)+i]))
	}
}

func TestResolveCSVColumns(t *testing.T) {
	t.Parallel()

	core, err := resolveCSVColumns(csvColumnsCore)
	must.NoError(t, err)
	must.Len(t, 8, core, must.Sprint("the core column set must stay at exactly 8 columns"))

	empty, err := resolveCSVColumns("")
	must.NoError(t, err)
	must.Len(t, 8, empty, must.Sprint("an unset --csv-columns means the core set"))

	explicit, err := resolveCSVColumns("url, title ,posted_at")
	must.NoError(t, err)
	must.Len(t, 3, explicit)

	// An explicit list is emitted in the order given, not in schema order.
	names := make([]string, len(explicit))
	for i, column := range explicit {
		names[i] = column.name
	}

	must.Eq(t, []string{"url", "title", "posted_at"}, names)

	// A misspelled column must fail loudly, listing what is available. Silently
	// dropping it would produce a file whose shape depends on a typo.
	_, err = resolveCSVColumns("company,depatment")
	must.Error(t, err)
	must.StrContains(t, err.Error(), "depatment")
	must.StrContains(t, err.Error(), "department")

	_, err = resolveCSVColumns(",")
	must.Error(t, err)
}

func TestResolvePostingOutputHeaderDefaults(t *testing.T) {
	t.Parallel()

	// The default set stays headerless forever; asking for any other set turns
	// the header on, because an unfamiliar column set is unusable unnamed.
	cmd := newPostingsCommand()
	must.NoError(t, cmd.ParseFlags([]string{"--csv"}))

	output, err := resolvePostingOutput(cmd, false, true, csvColumnsCore, false)
	must.NoError(t, err)
	must.False(t, output.csvHeader)
	must.Len(t, 8, output.csvColumns)

	cmd = newPostingsCommand()
	must.NoError(t, cmd.ParseFlags([]string{"--csv", "--csv-columns", "extended"}))

	output, err = resolvePostingOutput(cmd, false, true, csvColumnsExtended, false)
	must.NoError(t, err)
	must.True(t, output.csvHeader, must.Sprint("an opt-in column set should name its columns"))

	// An explicit --csv-header=false still wins: appending to an existing file
	// has to remain possible.
	cmd = newPostingsCommand()
	must.NoError(t, cmd.ParseFlags([]string{"--csv", "--csv-columns", "extended", "--csv-header=false"}))

	output, err = resolvePostingOutput(cmd, false, true, csvColumnsExtended, false)
	must.NoError(t, err)
	must.False(t, output.csvHeader)
}

func TestResolvePostingOutputRejectsCSVFlagsWithoutCSV(t *testing.T) {
	t.Parallel()

	// Accepting a flag and ignoring it is how someone ends up wondering where
	// their columns went.
	cmd := newPostingsCommand()
	must.NoError(t, cmd.ParseFlags([]string{"--json", "--csv-columns", "extended"}))

	_, err := resolvePostingOutput(cmd, true, false, csvColumnsExtended, false)
	must.Error(t, err)
	must.StrContains(t, err.Error(), "--csv")
}

func TestPostingPrinterTextAddsOnlyUsefulDetail(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		posting *internal.JobPosting
		want    string
	}{
		{
			// The pre-enrichment format, byte for byte. Until adapters populate
			// the new fields, nothing about this output changes.
			name:    "no enrichment",
			posting: &internal.JobPosting{Company: "acme", Title: "Cook", Location: "Paris", URL: "https://example.test/1"},
			want:    "company: acme title: Cook location: Paris url: https://example.test/1\n",
		},
		{
			// Full-time is the assumption a reader already has, and the location
			// already says hybrid, so both segments stay out of the way.
			name: "full time restates nothing",
			posting: &internal.JobPosting{
				Company: "acme", Title: "Cook", Location: "Hybrid - Paris", URL: "https://example.test/1",
				EmploymentType: internal.EmploymentTypeFullTime,
				WorkplaceType:  internal.WorkplaceTypeHybrid,
			},
			want: "company: acme title: Cook location: Hybrid - Paris url: https://example.test/1\n",
		},
		{
			name: "workplace type the location does not mention",
			posting: &internal.JobPosting{
				Company: "acme", Title: "Cook", Location: "Paris", URL: "https://example.test/1",
				WorkplaceType: internal.WorkplaceTypeRemote,
			},
			want: "company: acme title: Cook location: Paris workplace: remote url: https://example.test/1\n",
		},
		{
			name: "onsite is the assumption, so it is not printed",
			posting: &internal.JobPosting{
				Company: "acme", Title: "Cook", Location: "Paris", URL: "https://example.test/1",
				WorkplaceType: internal.WorkplaceTypeOnsite,
			},
			want: "company: acme title: Cook location: Paris url: https://example.test/1\n",
		},
		{
			name: "an internship is worth knowing before clicking",
			posting: &internal.JobPosting{
				Company: "acme", Title: "Cook", Location: "Paris", URL: "https://example.test/1",
				EmploymentType: internal.EmploymentTypeInternship,
				PostedAt:       time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC),
			},
			want: "company: acme title: Cook location: Paris type: internship posted: 2026-06-01 url: https://example.test/1\n",
		},
		{
			name:    "pay keeps its place among the new segments",
			posting: enrichedPosting(),
			want: "company: harvey title: Staff Engineer location: San Francisco workplace: hybrid " +
				"pay: USD 236000-290000/year posted: 2026-06-01 url: https://example.test/1\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			print, flush, err := newPostingPrinter(&buf, postingOutput{})
			must.NoError(t, err)
			must.NoError(t, print(tt.posting))
			must.NoError(t, flush())

			must.Eq(t, tt.want, buf.String())

			// One posting stays one line: this output is what people grep.
			must.Eq(t, 1, strings.Count(buf.String(), "\n"))
		})
	}
}

func TestParsePostedSince(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name  string
		value string
		want  time.Time
	}{
		{name: "unset", value: "", want: time.Time{}},
		{name: "date", value: "2026-01-31", want: time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)},
		{
			name:  "rfc 3339",
			value: "2026-01-31T08:15:00Z",
			want:  time.Date(2026, 1, 31, 8, 15, 0, 0, time.UTC),
		},
		{
			// An offset timestamp is converted, not rejected: the comparison is
			// between instants.
			name:  "rfc 3339 with an offset",
			value: "2026-01-31T08:15:00+02:00",
			want:  time.Date(2026, 1, 31, 6, 15, 0, 0, time.UTC),
		},
		{name: "days", value: "7d", want: now.Add(-7 * 24 * time.Hour)},
		{name: "weeks", value: "2w", want: now.Add(-14 * 24 * time.Hour)},
		{name: "go duration", value: "72h", want: now.Add(-72 * time.Hour)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := parsePostedSince(tt.value, now)
			must.NoError(t, err)
			must.True(t, got.Equal(tt.want), must.Sprintf("parsePostedSince(%q) = %s, want %s", tt.value, got, tt.want))
		})
	}

	for _, invalid := range []string{"last tuesday", "7 days", "0d", "-3d", "2026-13-45"} {
		_, err := parsePostedSince(invalid, now)
		test.Error(t, err, test.Sprintf("parsePostedSince(%q) should be rejected", invalid))
	}
}

func TestParseVocabularyFlags(t *testing.T) {
	t.Parallel()

	// The flags accept board spellings as well as canonical ones, because a user
	// who saw "FullTime" in JSON output should be able to type it back.
	types, err := parseEmploymentTypes([]string{"full-time", "Intern", "contract"})
	must.NoError(t, err)
	must.Eq(t, []internal.EmploymentType{
		internal.EmploymentTypeFullTime,
		internal.EmploymentTypeInternship,
		internal.EmploymentTypeContract,
	}, types)

	places, err := parseWorkplaceTypes([]string{"on-site", "REMOTE"})
	must.NoError(t, err)
	must.Eq(t, []internal.WorkplaceType{internal.WorkplaceTypeOnsite, internal.WorkplaceTypeRemote}, places)

	// An unknown value is rejected at parse time. Passing it through would filter
	// a 1,900-source crawl down to nothing, which reads as "nobody is hiring"
	// rather than "that is not a category".
	_, err = parseEmploymentTypes([]string{"salaried"})
	must.Error(t, err)
	must.StrContains(t, err.Error(), "full_time")

	_, err = parseWorkplaceTypes([]string{"flexible"})
	must.Error(t, err)
	must.StrContains(t, err.Error(), "onsite")

	// A blank entry is no constraint, matching how blank --title terms behave.
	types, err = parseEmploymentTypes([]string{"", "  "})
	must.NoError(t, err)
	must.Nil(t, types)
}

func TestPostingsCommandRejectsBadFilterValues(t *testing.T) {
	t.Parallel()

	// End-to-end wiring: these must fail before any crawl starts, which is also
	// what keeps this test hermetic.
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "employment type",
			args: []string{"postings", "--employment-type", "salaried"},
			want: "invalid --employment-type",
		},
		{
			name: "workplace type",
			args: []string{"postings", "--workplace-type", "flexible"},
			want: "invalid --workplace-type",
		},
		{
			name: "posted since",
			args: []string{"postings", "--posted-since", "last tuesday"},
			want: "invalid --posted-since",
		},
		{
			name: "csv column",
			args: []string{"postings", "--csv", "--csv-columns", "salary"},
			want: "unknown --csv-columns entry",
		},
		{
			name: "csv columns without csv",
			args: []string{"postings", "--csv-columns", "extended"},
			want: "--csv output only",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cmd := newRootCommand()
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetArgs(tt.args)

			err := cmd.ExecuteContext(t.Context())
			must.Error(t, err)
			must.StrContains(t, err.Error(), tt.want)
		})
	}
}
