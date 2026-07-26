// Command job-hunter-toolkit searches job postings across many companies.
package main

import (
	"cmp"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/httpx"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/services"
	"github.com/spf13/cobra"
)

func main() {
	// signal.NotifyContext makes Ctrl-C cancel the crawl, which unwinds the
	// in-flight HTTP requests rather than killing the process mid-write.
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	if err := newRootCommand().ExecuteContext(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// globalFlags are shared by the commands that perform a crawl.
type globalFlags struct {
	timeout      time.Duration
	concurrency  int
	perHostLimit int
	logLevel     string
	logFormat    string
	proxies      []string
}

// register attaches the crawl flags to a command.
func (g *globalFlags) register(cmd *cobra.Command) {
	cmd.Flags().DurationVar(&g.timeout, "timeout", time.Hour, "overall time budget for the crawl")
	cmd.Flags().IntVar(&g.concurrency, "concurrency", internal.DefaultConcurrency,
		"number of job sources to fetch at once")
	// --concurrency is throughput; this is politeness. They are separate because
	// they trade against different things: workers cost file descriptors and
	// memory here, while this costs the job board's patience. Tuning the crawl
	// used to mean recompiling, which made "is Ashby's limit right?" an
	// unanswerable question in CI.
	cmd.Flags().IntVar(&g.perHostLimit, "per-host-limit", httpx.DefaultPerHostLimit,
		"maximum requests in flight to any single job-board service; known shared backends (Workable, PeopleForce) have lower measured ceilings this cannot raise")
	cmd.Flags().StringVar(&g.logLevel, "log-level", "warn", "log verbosity: debug, info, warn, or error")
	cmd.Flags().StringVar(&g.logFormat, "log-format", "text", "log encoding: text or json")
	cmd.Flags().StringArrayVar(&g.proxies, "proxy", nil,
		"HTTP, HTTPS, or SOCKS5 proxy URL; repeat for sticky load balancing (standard proxy environment variables also work)")
}

// logger builds the structured logger for a run.
//
// Logs go to stderr so that stdout stays a clean data stream: `postings --json`
// can be piped into jq while diagnostics remain readable in a terminal.
func (g *globalFlags) logger(w io.Writer) *slog.Logger {
	var level slog.Level

	if err := level.UnmarshalText([]byte(g.logLevel)); err != nil {
		level = slog.LevelWarn
	}

	options := &slog.HandlerOptions{Level: level}
	if strings.EqualFold(g.logFormat, "json") {
		return slog.New(slog.NewJSONHandler(w, options))
	}

	return slog.New(slog.NewTextHandler(w, options))
}

// client builds the shared crawler client, validating an explicit proxy before
// any source work starts. The default transport already honors the standard
// HTTP_PROXY, HTTPS_PROXY, and NO_PROXY environment variables.
func (g *globalFlags) client(logger *slog.Logger) (*http.Client, error) {
	// httpx treats a limit below 1 as "no limit at all". That is a reasonable
	// library default and a terrible thing to let a command line ask for: an
	// unlimited crawl of ~1,772 companies is indistinguishable from an attack on
	// whichever backend hosts the most of them. Zero means "flag not set", which
	// is how a zero-valued globalFlags reaches here in tests.
	perHostLimit := g.perHostLimit
	switch {
	case perHostLimit == 0:
		perHostLimit = httpx.DefaultPerHostLimit
	case perHostLimit < 0:
		return nil, fmt.Errorf("invalid --per-host-limit %d: must be at least 1, because the per-service limit is what keeps a crawl of this size polite", g.perHostLimit)
	}

	opts := []httpx.Option{
		httpx.WithLogger(logger),
		httpx.WithPerHostLimit(perHostLimit),
	}

	proxyURLs := make([]*url.URL, 0, len(g.proxies))
	for _, rawProxy := range g.proxies {
		proxyURL, err := httpx.ParseProxyURL(rawProxy)
		if err != nil {
			return nil, fmt.Errorf("invalid --proxy: %w", err)
		}

		proxyURLs = append(proxyURLs, proxyURL)
	}

	if len(proxyURLs) > 0 {
		opts = append(opts, httpx.WithProxyURLs(proxyURLs...))
	}

	return httpx.NewClient(opts...), nil
}

// crawlContext derives the crawl's context from the command's, applying the
// time budget.
func (g *globalFlags) crawlContext(cmd *cobra.Command) (context.Context, context.CancelFunc) {
	if g.timeout <= 0 {
		return context.WithCancel(cmd.Context())
	}

	return context.WithTimeout(cmd.Context(), g.timeout)
}

func newRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "job-hunter-toolkit",
		Short: "Search job postings across many companies",
		Long: "The job hunter's toolkit. Searches the job boards of more than a\n" +
			"thousand companies across the major applicant tracking systems.",
		SilenceUsage: true,
	}

	root.AddCommand(
		newPostingsCommand(),
		newCompaniesCommand(),
		newTotalCommand(),
		newHealthCommand(),
	)

	return root
}

// newPostingsCommand builds the `postings` command.
func newPostingsCommand() *cobra.Command {
	var (
		flags          globalFlags
		asJSON         bool
		asCSV          bool
		csvColumnSpec  string
		csvHeader      bool
		noDedupe       bool
		filter         internal.Filter
		showStats      bool
		employmentType []string
		workplaceType  []string
		postedSince    string
	)

	cmd := &cobra.Command{
		Use:   "postings",
		Short: "Find job postings from various companies",
		Long: "Find job postings from various companies.\n\n" +
			"Filters combine as you would expect: values within a flag are OR-ed,\n" +
			"and different flags are AND-ed. Matching is case-insensitive substring\n" +
			"matching against the text the job board publishes.\n\n" +
			"--employment-type and --workplace-type are the exception: they compare\n" +
			"against a normalized vocabulary by equality, because \"contract\" is a\n" +
			"prefix of \"contractor\". Like --min-pay, they exclude postings whose\n" +
			"board published nothing, since a category filter cannot be applied to an\n" +
			"unknown.\n\n" +
			"--csv emits the same 8 columns it always has, with no header, so\n" +
			"existing pipelines keep working. The richer fields are opt-in through\n" +
			"--csv-columns, which turns the header on because an unfamiliar column\n" +
			"set is unusable unnamed.",
		Example: "  # Remote application security roles\n" +
			"  job-hunter-toolkit postings --remote --title security --title appsec\n\n" +
			"  # Everything at a few companies, as JSON\n" +
			"  job-hunter-toolkit postings --company stripe --company cloudflare --json\n\n" +
			"  # Internships posted in the last fortnight, as a named-column CSV\n" +
			"  job-hunter-toolkit postings --employment-type internship --posted-since 2w \\\n" +
			"    --csv --csv-columns extended",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := flags.logger(cmd.ErrOrStderr())

			output, err := resolvePostingOutput(cmd, asJSON, asCSV, csvColumnSpec, csvHeader)
			if err != nil {
				return err
			}

			if filter.EmploymentTypes, err = parseEmploymentTypes(employmentType); err != nil {
				return err
			}

			if filter.WorkplaceTypes, err = parseWorkplaceTypes(workplaceType); err != nil {
				return err
			}

			if filter.PostedSince, err = parsePostedSince(postedSince, time.Now()); err != nil {
				return err
			}

			emit, flush, err := newPostingPrinter(cmd.OutOrStdout(), output)
			if err != nil {
				return err
			}
			defer flush()

			ctx, cancel := flags.crawlContext(cmd)
			defer cancel()

			client, err := flags.client(logger)
			if err != nil {
				return err
			}

			// Narrow the crawl to the requested companies before fetching, so a
			// targeted query does not pay for a full crawl.
			sources := services.SourcesMatching(filter.Companies)
			if len(sources) == 0 {
				return fmt.Errorf("no known companies match %s", strings.Join(filter.Companies, ", "))
			}

			logger.InfoContext(ctx, "starting crawl", slog.Int("sources", len(sources)))

			jobs := internal.AllWithConcurrency(ctx, client, flags.concurrency, services.JobsFuncs(sources)...)

			if !noDedupe {
				jobs = internal.Dedupe(jobs)
			}

			jobs = postingFilterFor(filter).Apply(jobs)

			var found, failed int

			for jobPosting, err := range jobs {
				if err != nil {
					// A failing company is expected at this scale: boards get
					// retired constantly. Report it without ending the crawl,
					// and keep it off stdout so the data stream stays clean.
					failed++
					logger.DebugContext(ctx, "job source failed", slog.String("cause", err.Error()))

					continue
				}

				found++

				if err := emit(jobPosting); err != nil {
					return err
				}
			}

			if err := flush(); err != nil {
				return err
			}

			logger.InfoContext(ctx, "crawl finished",
				slog.Int("postings", found),
				slog.Int("failed_sources", failed),
			)

			if showStats {
				fmt.Fprintf(cmd.ErrOrStderr(), "%d postings from %d sources (%d sources failed)\n",
					found, len(sources), failed)
			}

			return nil
		},
	}

	flags.register(cmd)

	cmd.Flags().BoolVar(&asJSON, "json", false, "output newline-delimited JSON")
	cmd.Flags().BoolVar(&asCSV, "csv", false, "output CSV (company,title,location,url,pay_min,pay_max,currency,period) with no header")
	cmd.Flags().StringVar(&csvColumnSpec, "csv-columns", csvColumnsCore,
		"CSV columns: core (the frozen 8 above), extended (core plus department, employment type, dates and source identity), or an explicit comma-separated list; anything other than core also turns --csv-header on")
	cmd.Flags().BoolVar(&csvHeader, "csv-header", false,
		"write a header row; off by default because a pipeline reading today's headerless CSV would take it for a posting")
	cmd.Flags().BoolVar(&noDedupe, "no-dedupe", false,
		"keep duplicate postings; by default postings sharing a URL are emitted once")
	cmd.Flags().BoolVar(&showStats, "stats", false, "print a summary to stderr when the crawl finishes")

	cmd.Flags().StringSliceVar(&filter.Titles, "title", nil,
		"only postings whose title contains any of these terms")
	cmd.Flags().StringSliceVar(&filter.ExcludeTitles, "exclude-title", nil,
		"skip postings whose title contains any of these terms")
	cmd.Flags().StringSliceVar(&filter.Locations, "location", nil,
		"only postings whose location contains any of these terms")
	cmd.Flags().StringSliceVar(&filter.Companies, "company", nil,
		"only postings from companies matching any of these terms")
	cmd.Flags().BoolVar(&filter.Remote, "remote", false, "only postings that look remote")
	cmd.Flags().BoolVar(&filter.HasCompensation, "has-pay", false,
		"only postings that publish a pay range")
	cmd.Flags().Float64Var(&filter.MinAnnual, "min-pay", 0,
		"only postings publishing pay of at least this much per year (hourly rates are annualized); implies --has-pay")
	cmd.Flags().StringSliceVar(&filter.Departments, "department", nil,
		"only postings whose department or team contains any of these terms")
	cmd.Flags().StringSliceVar(&employmentType, "employment-type", nil,
		"only postings of these employment types ("+joinValues(internal.EmploymentTypeValues())+"); board spellings such as full-time or FullTime are accepted, and postings with no published type are excluded")
	cmd.Flags().StringSliceVar(&workplaceType, "workplace-type", nil,
		"only postings of these workplace types ("+joinValues(internal.WorkplaceTypeValues())+"); remote and hybrid fall back to reading the location text when the board published no structured field, onsite does not")
	cmd.Flags().StringVar(&postedSince, "posted-since", "",
		"only postings published at or after this point: a date (2026-01-31), an RFC 3339 timestamp, or an age such as 7d, 2w or 72h; postings with no publication date are excluded")

	cmd.MarkFlagsMutuallyExclusive("json", "csv")

	return cmd
}

// postingFilterFor returns the filter to apply to postings, given that company
// selection has already been applied by narrowing which sources are crawled.
//
// The company constraint is deliberately dropped. A source is keyed by whatever
// its platform uses to identify a tenant, a Workday tenant URL, a Phenom
// hostname, while the posting it produces carries a short company name derived
// from that key. Applying the constraint a second time compared those two
// different forms and silently discarded every posting, reporting "0 postings"
// as though the company simply were not hiring.
func postingFilterFor(f internal.Filter) internal.Filter {
	f.Companies = nil

	return f
}

// postingOutput is the resolved output format for a postings run.
type postingOutput struct {
	asJSON bool
	asCSV  bool

	// csvColumns is the ordered column set, already resolved from --csv-columns.
	csvColumns []postingColumn

	// csvHeader reports whether to write a header row first.
	csvHeader bool
}

// postingColumn is one CSV column: the name it is known by and how to read it
// off a posting.
type postingColumn struct {
	name  string
	value func(*internal.JobPosting) string
}

// The named CSV column sets.
const (
	csvColumnsCore     = "core"
	csvColumnsExtended = "extended"
)

// csvCoreColumnNames is the default CSV column set, and it is frozen.
//
// main.go has documented this output as headerless since it existed, and a test
// asserts the exact width, so these eight columns in this order are a committed
// contract with every pipeline already reading them. Columns may be appended to
// the end of a *named* set; nothing may be inserted, renamed, or removed here.
var csvCoreColumnNames = []string{
	"company", "title", "location", "url", "pay_min", "pay_max", "currency", "period",
}

// csvExtendedColumnNames is the opt-in wide set: the core eight, unchanged and
// in place, followed by the enrichment fields the adapters can fill in for free.
var csvExtendedColumnNames = slices.Concat(csvCoreColumnNames, []string{
	"department", "team", "employment_type", "workplace_type", "seniority",
	"posted_at", "updated_at", "requisition_id", "external_id",
	"source_platform", "source_key",
})

// postingColumns is every column an explicit --csv-columns list may name.
//
// Timestamps are written as RFC 3339 in UTC, matching the JSON output, so the
// two formats never disagree about what a date means. Every column is empty
// rather than a placeholder when the board published nothing: an empty cell is
// read as absent by every spreadsheet and every parser, whereas "unknown" or "0"
// is data that was never collected.
var postingColumns = map[string]func(*internal.JobPosting) string{
	"company":  func(j *internal.JobPosting) string { return j.Company },
	"title":    func(j *internal.JobPosting) string { return j.Title },
	"location": func(j *internal.JobPosting) string { return j.Location },
	"url":      func(j *internal.JobPosting) string { return j.URL },
	"pay_min": func(j *internal.JobPosting) string {
		if j.Compensation.IsZero() || j.Compensation.Min <= 0 {
			return ""
		}

		return strconv.FormatFloat(j.Compensation.Min, 'f', -1, 64)
	},
	"pay_max": func(j *internal.JobPosting) string {
		if j.Compensation.IsZero() || j.Compensation.Max <= 0 {
			return ""
		}

		return strconv.FormatFloat(j.Compensation.Max, 'f', -1, 64)
	},
	"currency": func(j *internal.JobPosting) string {
		if j.Compensation.IsZero() {
			return ""
		}

		return j.Compensation.Currency
	},
	"period": func(j *internal.JobPosting) string {
		if j.Compensation.IsZero() {
			return ""
		}

		return string(j.Compensation.Period)
	},
	"department":      func(j *internal.JobPosting) string { return j.Department },
	"team":            func(j *internal.JobPosting) string { return j.Team },
	"employment_type": func(j *internal.JobPosting) string { return string(j.EmploymentType) },
	"workplace_type":  func(j *internal.JobPosting) string { return string(j.WorkplaceType) },
	"seniority":       func(j *internal.JobPosting) string { return j.Seniority },
	"posted_at":       func(j *internal.JobPosting) string { return formatTimestamp(j.PostedAt) },
	"updated_at":      func(j *internal.JobPosting) string { return formatTimestamp(j.UpdatedAt) },
	"requisition_id":  func(j *internal.JobPosting) string { return j.RequisitionID },
	"external_id":     func(j *internal.JobPosting) string { return j.ExternalID },
	"source_platform": func(j *internal.JobPosting) string { return j.Source.Platform },
	"source_key":      func(j *internal.JobPosting) string { return j.Source.Key },
}

// formatTimestamp renders a posting timestamp for tabular output, leaving the
// cell empty when the board published no date.
func formatTimestamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}

	return t.UTC().Format(time.RFC3339)
}

// resolvePostingOutput turns the output flags into a resolved format, rejecting
// combinations that would otherwise be silently ignored.
func resolvePostingOutput(cmd *cobra.Command, asJSON, asCSV bool, columnSpec string, header bool) (postingOutput, error) {
	output := postingOutput{asJSON: asJSON, asCSV: asCSV}

	// Failing loudly beats accepting a flag and doing nothing with it: someone
	// who asked for extra columns and got the default eight would not notice
	// until the columns were missing from something downstream.
	if !asCSV && (cmd.Flags().Changed("csv-columns") || cmd.Flags().Changed("csv-header")) {
		return output, fmt.Errorf("--csv-columns and --csv-header apply to --csv output only")
	}

	columns, err := resolveCSVColumns(columnSpec)
	if err != nil {
		return output, err
	}

	output.csvColumns = columns
	output.csvHeader = header

	// An unfamiliar column set is unusable unnamed, so asking for one turns the
	// header on. The default set stays headerless forever, and an explicit
	// --csv-header=false still wins: someone appending to an existing file needs
	// to be able to say so.
	if !cmd.Flags().Changed("csv-header") && columnSpec != csvColumnsCore && columnSpec != "" {
		output.csvHeader = true
	}

	return output, nil
}

// resolveCSVColumns turns a --csv-columns value into an ordered column set.
func resolveCSVColumns(spec string) ([]postingColumn, error) {
	var names []string

	switch strings.TrimSpace(spec) {
	case "", csvColumnsCore:
		names = csvCoreColumnNames
	case csvColumnsExtended:
		names = csvExtendedColumnNames
	default:
		names = strings.Split(spec, ",")
	}

	columns := make([]postingColumn, 0, len(names))

	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}

		value, ok := postingColumns[name]
		if !ok {
			return nil, fmt.Errorf("unknown --csv-columns entry %q: choose %s, %s, or a comma-separated list of %s",
				name, csvColumnsCore, csvColumnsExtended, strings.Join(slices.Sorted(maps.Keys(postingColumns)), ", "))
		}

		columns = append(columns, postingColumn{name: name, value: value})
	}

	if len(columns) == 0 {
		return nil, fmt.Errorf("--csv-columns %q selects no columns", spec)
	}

	return columns, nil
}

// newPostingPrinter returns a function that writes a posting in the requested
// format, plus a flush function to call when the stream is complete.
func newPostingPrinter(w io.Writer, output postingOutput) (emit func(*internal.JobPosting) error, flush func() error, err error) {
	switch {
	case output.asJSON:
		enc := json.NewEncoder(w)

		return func(j *internal.JobPosting) error {
			if err := enc.Encode(j); err != nil {
				return fmt.Errorf("writing JSON: %w", err)
			}

			return nil
		}, func() error { return nil }, nil

	case output.asCSV:
		cw := csv.NewWriter(w)

		columns := output.csvColumns
		if len(columns) == 0 {
			// A zero-valued postingOutput still has to produce the historic
			// eight columns, because that is what every existing caller means
			// by "CSV".
			if columns, err = resolveCSVColumns(csvColumnsCore); err != nil {
				return nil, nil, err
			}
		}

		if output.csvHeader {
			names := make([]string, len(columns))
			for i, column := range columns {
				names[i] = column.name
			}

			if err := cw.Write(names); err != nil {
				return nil, nil, fmt.Errorf("writing CSV header: %w", err)
			}
		}

		record := make([]string, len(columns))

		return func(j *internal.JobPosting) error {
				for i, column := range columns {
					record[i] = column.value(j)
				}

				if err := cw.Write(record); err != nil {
					return fmt.Errorf("writing CSV: %w", err)
				}

				return nil
			}, func() error {
				cw.Flush()

				return cw.Error()
			}, nil

	default:
		return func(j *internal.JobPosting) error {
			_, err := fmt.Fprintf(w, "company: %s title: %s location: %s%s url: %s\n",
				j.Company, j.Title, j.Location, describePostingDetail(j), j.URL)

			return err
		}, func() error { return nil }, nil
	}
}

// describePostingDetail renders the enrichment worth reading in the default
// human format, as leading-space " key: value" segments to sit inside the
// existing line.
//
// The rule is that a segment must tell the reader something the rest of the line
// does not. One posting has to stay one line: the default output is what people
// grep and eyeball at a few hundred results, and a crawl of 473,000 postings
// turned into five lines each is unreadable. So:
//
//   - Workplace type prints only for remote and hybrid, and only when the
//     location text does not already say it. "location: Remote workplace: remote"
//     is noise on the majority of remote postings.
//   - Employment type prints only when it is not full-time. Full-time is the
//     assumption a reader already has, so printing it adds a column of text that
//     changes nothing.
//   - The posted date prints as a date, not a timestamp. Boards publish
//     day-granularity truth even when they emit a clock time.
//
// Department, team, seniority, requisition and external ids and the source
// identity are deliberately absent. They are for machine consumers, which have
// --json and --csv-columns; on a terminal they would double the line length to
// restate what the title and company usually already imply.
func describePostingDetail(j *internal.JobPosting) string {
	var detail strings.Builder

	if workplace := j.WorkplaceType; workplace == internal.WorkplaceTypeRemote || workplace == internal.WorkplaceTypeHybrid {
		if !strings.Contains(strings.ToLower(j.Location), string(workplace)) {
			fmt.Fprintf(&detail, " workplace: %s", workplace)
		}
	}

	if j.EmploymentType != internal.EmploymentTypeUnknown && j.EmploymentType != internal.EmploymentTypeFullTime {
		fmt.Fprintf(&detail, " type: %s", j.EmploymentType)
	}

	if !j.Compensation.IsZero() {
		fmt.Fprintf(&detail, " pay: %s", describeCompensation(j.Compensation))
	}

	if !j.PostedAt.IsZero() {
		fmt.Fprintf(&detail, " posted: %s", j.PostedAt.UTC().Format(time.DateOnly))
	}

	return detail.String()
}

// joinValues renders a normalized vocabulary for flag help, so the values a user
// is offered cannot drift from the values the filter accepts.
func joinValues[T ~string](values []T) string {
	names := make([]string, len(values))
	for i, value := range values {
		names[i] = string(value)
	}

	return strings.Join(names, ", ")
}

// parseEmploymentTypes validates --employment-type against the canonical
// vocabulary.
//
// Unknown values are rejected at parse time rather than passed through to match
// nothing. A filter that silently matches zero postings out of a 2,100-source
// crawl reads as "nobody is hiring", which is the failure this project treats as
// the worst one available.
func parseEmploymentTypes(values []string) ([]internal.EmploymentType, error) {
	var types []internal.EmploymentType

	for _, raw := range values {
		if strings.TrimSpace(raw) == "" {
			continue
		}

		typ, ok := internal.NormalizeEmploymentType(raw)
		if !ok {
			return nil, fmt.Errorf("invalid --employment-type %q: valid values are %s",
				raw, joinValues(internal.EmploymentTypeValues()))
		}

		types = append(types, typ)
	}

	return types, nil
}

// parseWorkplaceTypes validates --workplace-type against the canonical
// vocabulary.
func parseWorkplaceTypes(values []string) ([]internal.WorkplaceType, error) {
	var types []internal.WorkplaceType

	for _, raw := range values {
		if strings.TrimSpace(raw) == "" {
			continue
		}

		typ, ok := internal.NormalizeWorkplaceType(raw)
		if !ok {
			return nil, fmt.Errorf("invalid --workplace-type %q: valid values are %s",
				raw, joinValues(internal.WorkplaceTypeValues()))
		}

		types = append(types, typ)
	}

	return types, nil
}

// parsePostedSince interprets --posted-since as either an instant or an age.
//
// Both forms exist because both are what people mean: a report run against a
// fixed window wants a date, and a person checking what appeared this week wants
// "7d". Ages are resolved against now once, at flag-parse time, so a crawl that
// takes an hour does not shift its own cutoff underneath itself.
func parsePostedSince(value string, now time.Time) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}

	if instant, err := time.Parse(time.RFC3339, value); err == nil {
		return instant.UTC(), nil
	}

	if day, err := time.Parse(time.DateOnly, value); err == nil {
		return day, nil
	}

	if age, err := parseAge(value); err == nil {
		if age <= 0 {
			return time.Time{}, fmt.Errorf("invalid --posted-since %q: an age must be positive, and it is subtracted from now", value)
		}

		return now.Add(-age).UTC(), nil
	}

	return time.Time{}, fmt.Errorf(
		"invalid --posted-since %q: want a date (2026-01-31), an RFC 3339 timestamp, or an age such as 7d, 2w or 72h", value)
}

// parseAge parses a duration, extending [time.ParseDuration] with the day and
// week units it does not support. Nobody asks a job board for postings from the
// last "168h".
func parseAge(value string) (time.Duration, error) {
	units := []struct {
		suffix string
		unit   time.Duration
	}{
		{"d", 24 * time.Hour},
		{"w", 7 * 24 * time.Hour},
	}

	for _, u := range units {
		digits, ok := strings.CutSuffix(value, u.suffix)
		if !ok {
			continue
		}

		count, err := strconv.ParseFloat(digits, 64)
		if err != nil {
			return 0, fmt.Errorf("parsing age %q: %w", value, err)
		}

		return time.Duration(count * float64(u.unit)), nil
	}

	return time.ParseDuration(value)
}

// newCompaniesCommand builds the `companies` command.
func newCompaniesCommand() *cobra.Command {
	var (
		asJSON bool
		asCSV  bool
	)

	cmd := &cobra.Command{
		Use:     "companies",
		Short:   "List the companies that postings are searched from",
		Args:    cobra.NoArgs,
		Aliases: []string{"sources"},
		RunE: func(cmd *cobra.Command, args []string) error {
			companies := services.Companies()

			out := cmd.OutOrStdout()

			switch {
			case asJSON:
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")

				return enc.Encode(companies)

			case asCSV:
				cw := csv.NewWriter(out)
				defer cw.Flush()

				for _, company := range companies {
					if err := cw.Write([]string{company}); err != nil {
						return fmt.Errorf("writing CSV: %w", err)
					}
				}

				return cw.Error()

			default:
				for _, company := range companies {
					if _, err := fmt.Fprintln(out, company); err != nil {
						return err
					}
				}

				return nil
			}
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "output as a JSON array")
	cmd.Flags().BoolVar(&asCSV, "csv", false, "output one company per CSV row, with no header")
	cmd.MarkFlagsMutuallyExclusive("json", "csv")

	return cmd
}

// newTotalCommand builds the `total` command.
func newTotalCommand() *cobra.Command {
	var (
		flags        globalFlags
		allowPartial bool
		manifestPath string
	)

	cmd := &cobra.Command{
		Use:   "total",
		Short: "Count the job postings currently available",
		Long: "Count the job postings currently available.\n\n" +
			"Writes a single row of \"DATE POSTINGS COMPANIES STATUS\" to stdout and\n" +
			"a header to stderr, so the row can be appended straight to a record file.\n" +
			"STATUS is complete, or partial when --allow-partial records a deadline\n" +
			"snapshot that must remain visibly distinct from a completed crawl.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := flags.logger(cmd.ErrOrStderr())

			ctx, cancel := flags.crawlContext(cmd)
			defer cancel()

			client, err := flags.client(logger)
			if err != nil {
				return err
			}

			startedAt := time.Now().UTC()
			sources := services.SourcesMatching(nil)
			sourceJobs, sourceResults := services.Observe(sources, logger)

			jobs := internal.Dedupe(
				internal.AllWithConcurrency(
					ctx,
					client,
					flags.concurrency,
					sourceJobs...,
				),
			)

			var (
				perCompany = map[string]int{}
				total      int
				failed     int
			)

			for jobPosting, err := range jobs {
				if err != nil {
					failed++

					continue
				}

				perCompany[jobPosting.Company]++
				total++
			}

			// Truncation is decided from the crawl context, not from the
			// per-source errors. A single slow board hitting the HTTP client's
			// own timeout produces an error that wraps context.DeadlineExceeded
			// too, so inspecting individual errors would condemn a perfectly
			// complete crawl.
			truncated := ctx.Err() != nil
			status := "complete"
			if truncated {
				status = "partial"
			}

			finishedAt := time.Now().UTC()
			manifest := newCrawlManifest(
				startedAt,
				finishedAt,
				flags.timeout,
				status,
				total,
				len(perCompany),
				sourceResults(),
			)
			if manifestPath != "" {
				if err := writeCrawlManifest(manifestPath, manifest); err != nil {
					return err
				}
			}

			logger.InfoContext(ctx, "crawl finished",
				slog.Int("postings", total),
				slog.Int("companies", len(perCompany)),
				slog.Int("failed_sources", failed),
				slog.String("status", status),
				slog.Bool("truncated", truncated),
			)

			fmt.Fprintf(cmd.ErrOrStderr(), "DATE POSTINGS COMPANIES STATUS\n")
			fmt.Fprintf(cmd.OutOrStdout(), "%s %d %d %s\n",
				time.Now().Format("01/02/06"), total, len(perCompany), status)

			// Default to failing closed. Callers that deliberately retain a
			// deadline snapshot must opt in, and the fourth output field keeps
			// that observation visibly distinct from a complete crawl.
			if truncated && !allowPartial {
				return fmt.Errorf(
					"crawl did not finish within %s: counted %d postings from %d companies, but this is incomplete and must not be recorded",
					flags.timeout, total, len(perCompany))
			}

			if truncated {
				logger.WarnContext(ctx, "recording partial crawl by explicit request",
					slog.Int("postings", total),
					slog.Int("companies", len(perCompany)),
					slog.Duration("timeout", flags.timeout),
				)
			}

			return nil
		},
	}

	flags.register(cmd)
	cmd.Flags().BoolVar(&allowPartial, "allow-partial", false,
		"return a successful, explicitly partial row when the overall deadline is reached")
	cmd.Flags().StringVar(&manifestPath, "manifest", "",
		"write a versioned JSON crawl manifest to this path")

	return cmd
}

// sourceHealth is one company's result from a health check.
type sourceHealth struct {
	Company string `json:"company"`

	// Key is the identifier the ATS uses, a tenant URL or hostname where that
	// differs from the company name. Reported because a health check exists to be
	// acted on: fixing a broken Workday tenant means knowing which URL failed,
	// not just which company did.
	Key string `json:"key,omitempty"`

	Status   string `json:"status"`
	Postings int    `json:"postings"`
	Error    string `json:"error,omitempty"`

	// Capped reports that counting stopped at [healthSampleLimit], so Postings
	// is a floor rather than the true total.
	Capped bool `json:"capped,omitempty"`
}

// healthSampleLimit bounds how many postings a health check reads per source.
//
// A health check only needs to know whether a source still works, and some
// employers are enormous: FedEx alone publishes over 138,000 postings, which is
// more than a thousand sequential paginated requests. Counting all of them would
// make checking every source take hours and would dominate the run for the sake
// of a number nobody reads precisely.
const healthSampleLimit = 100

// Health statuses.
const (
	statusOK     = "ok"
	statusEmpty  = "empty"
	statusFailed = "failed"
)

// newHealthCommand builds the `health` command.
func newHealthCommand() *cobra.Command {
	var (
		flags     globalFlags
		asJSON    bool
		failsOnly bool
		companies []string
	)

	cmd := &cobra.Command{
		Use:   "health",
		Short: "Check every job source and report which ones are broken",
		Long: "Check every job source and report which ones are broken.\n\n" +
			"Job boards are retired constantly, and a crawl that silently skips\n" +
			"failures slowly stops covering the companies it claims to. This reports\n" +
			"each source as ok (postings returned), empty (reachable, but nothing\n" +
			"posted), or failed (unreachable).\n\n" +
			"An empty source is not broken; the company simply is not hiring.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := flags.logger(cmd.ErrOrStderr())

			ctx, cancel := flags.crawlContext(cmd)
			defer cancel()

			client, err := flags.client(logger)
			if err != nil {
				return err
			}

			sources := services.SourcesMatching(companies)
			if len(sources) == 0 {
				return fmt.Errorf("no known companies match %s", strings.Join(companies, ", "))
			}

			results := checkSources(ctx, client, sources, flags.concurrency)

			// Group by status so failures cluster together, then order by
			// company within a status for a stable, diffable report.
			slices.SortFunc(results, func(a, b sourceHealth) int {
				if c := cmp.Compare(a.Status, b.Status); c != 0 {
					return c
				}

				return cmp.Compare(strings.ToLower(a.Company), strings.ToLower(b.Company))
			})

			counts := map[string]int{}
			for _, r := range results {
				counts[r.Status]++
			}

			out := cmd.OutOrStdout()

			if asJSON {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")

				if err := enc.Encode(results); err != nil {
					return fmt.Errorf("writing JSON: %w", err)
				}
			} else {
				for _, r := range results {
					if failsOnly && r.Status != statusFailed {
						continue
					}

					// Show the ATS key alongside the name when they differ, so a
					// failure is actionable: fixing a broken Workday tenant means
					// knowing which URL failed, not just which company did.
					name := r.Company
					if r.Key != "" {
						name += " (" + r.Key + ")"
					}

					if r.Error != "" {
						fmt.Fprintf(out, "%-8s %-72s %s\n", r.Status, name, r.Error)
						continue
					}

					count := strconv.Itoa(r.Postings)
					if r.Capped {
						count += "+"
					}

					fmt.Fprintf(out, "%-8s %-72s %s postings\n", r.Status, name, count)
				}
			}

			fmt.Fprintf(cmd.ErrOrStderr(), "\n%d sources: %d ok, %d empty, %d failed\n",
				len(results), counts[statusOK], counts[statusEmpty], counts[statusFailed])

			return nil
		},
	}

	flags.register(cmd)

	cmd.Flags().BoolVar(&asJSON, "json", false, "output as a JSON array")
	cmd.Flags().BoolVar(&failsOnly, "failed-only", false, "only print sources that failed")
	cmd.Flags().StringSliceVar(&companies, "company", nil,
		"only check companies matching any of these terms")

	return cmd
}

// checkSources fetches every source and records how it behaved.
func checkSources(ctx context.Context, client *http.Client, sources []services.Source, concurrency int) []sourceHealth {
	if concurrency < 1 {
		concurrency = 1
	}

	var (
		results = make([]sourceHealth, len(sources))
		sem     = make(chan struct{}, concurrency)
		wg      sync.WaitGroup
	)

	for i, source := range sources {
		wg.Add(1)

		go func(i int, source services.Source) {
			defer wg.Done()

			sem <- struct{}{}
			defer func() { <-sem }()

			health := sourceHealth{Company: source.Company, Status: statusOK}
			if source.Key != source.Company {
				health.Key = source.Key
			}

			// A panicking adapter is a bug worth surfacing, but it must not take
			// down a check covering every source; the whole point of this
			// command is to find broken sources, so it has to survive them.
			// A real case: an adapter that ignored yield's return value panicked
			// the moment this command started stopping early at the count cap.
			defer func() {
				if r := recover(); r != nil {
					health.Status = statusFailed
					health.Error = fmt.Sprintf("job source panicked: %v", r)
					results[i] = health
				}
			}()

			for jobPosting, err := range source.Jobs(ctx, client) {
				if err != nil {
					if health.Error == "" {
						health.Error = err.Error()
					}

					continue
				}

				if jobPosting != nil {
					health.Postings++
				}

				// Stop once the source has clearly proven itself. Breaking out
				// tells the adapter to stop paginating, which is what keeps a
				// check of every source bounded.
				if health.Postings >= healthSampleLimit {
					health.Capped = true

					break
				}
			}

			switch {
			case health.Postings > 0:
				health.Status = statusOK
			case health.Error != "":
				health.Status = statusFailed
			default:
				health.Status = statusEmpty
			}

			results[i] = health
		}(i, source)
	}

	wg.Wait()

	return results
}

// describeCompensation renders a pay range for human-readable output, preferring
// the board's own summary when it supplied one.
func describeCompensation(c *internal.Compensation) string {
	if c.Summary != "" {
		return c.Summary
	}

	period := ""
	if c.Period != internal.PeriodUnknown {
		period = "/" + string(c.Period)
	}

	currency := c.Currency
	if currency != "" {
		currency += " "
	}

	switch {
	case c.Min > 0 && c.Max > 0 && c.Min != c.Max:
		return fmt.Sprintf("%s%s-%s%s", currency,
			strconv.FormatFloat(c.Min, 'f', -1, 64),
			strconv.FormatFloat(c.Max, 'f', -1, 64), period)
	case c.Max > 0:
		return fmt.Sprintf("%s%s%s", currency, strconv.FormatFloat(c.Max, 'f', -1, 64), period)
	default:
		return fmt.Sprintf("%s%s%s", currency, strconv.FormatFloat(c.Min, 'f', -1, 64), period)
	}
}
