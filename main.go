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
	timeout     time.Duration
	concurrency int
	logLevel    string
	proxies     []string
}

// register attaches the crawl flags to a command.
func (g *globalFlags) register(cmd *cobra.Command) {
	cmd.Flags().DurationVar(&g.timeout, "timeout", time.Hour, "overall time budget for the crawl")
	cmd.Flags().IntVar(&g.concurrency, "concurrency", internal.DefaultConcurrency,
		"number of job sources to fetch at once")
	cmd.Flags().StringVar(&g.logLevel, "log-level", "warn", "log verbosity: debug, info, warn, or error")
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

	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: level}))
}

// client builds the shared crawler client, validating an explicit proxy before
// any source work starts. The default transport already honors the standard
// HTTP_PROXY, HTTPS_PROXY, and NO_PROXY environment variables.
func (g *globalFlags) client(logger *slog.Logger) (*http.Client, error) {
	opts := []httpx.Option{httpx.WithLogger(logger)}

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
		flags     globalFlags
		asJSON    bool
		asCSV     bool
		noDedupe  bool
		filter    internal.Filter
		showStats bool
	)

	cmd := &cobra.Command{
		Use:   "postings",
		Short: "Find job postings from various companies",
		Long: "Find job postings from various companies.\n\n" +
			"Filters combine as you would expect: values within a flag are OR-ed,\n" +
			"and different flags are AND-ed. Matching is case-insensitive substring\n" +
			"matching against the text the job board publishes.",
		Example: "  # Remote application security roles\n" +
			"  job-hunter-toolkit postings --remote --title security --title appsec\n\n" +
			"  # Everything at a few companies, as JSON\n" +
			"  job-hunter-toolkit postings --company stripe --company cloudflare --json",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := flags.logger(cmd.ErrOrStderr())

			emit, flush, err := newPostingPrinter(cmd.OutOrStdout(), asJSON, asCSV)
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

// newPostingPrinter returns a function that writes a posting in the requested
// format, plus a flush function to call when the stream is complete.
func newPostingPrinter(w io.Writer, asJSON, asCSV bool) (emit func(*internal.JobPosting) error, flush func() error, err error) {
	switch {
	case asJSON:
		enc := json.NewEncoder(w)

		return func(j *internal.JobPosting) error {
			if err := enc.Encode(j); err != nil {
				return fmt.Errorf("writing JSON: %w", err)
			}

			return nil
		}, func() error { return nil }, nil

	case asCSV:
		cw := csv.NewWriter(w)

		return func(j *internal.JobPosting) error {
				// Pay columns are appended rather than inserted, so anything
				// reading the original four fields keeps working. They are empty
				// when the employer disclosed nothing, which is the common case.
				var payMin, payMax, currency, period string

				if !j.Compensation.IsZero() {
					if j.Compensation.Min > 0 {
						payMin = strconv.FormatFloat(j.Compensation.Min, 'f', -1, 64)
					}

					if j.Compensation.Max > 0 {
						payMax = strconv.FormatFloat(j.Compensation.Max, 'f', -1, 64)
					}

					currency = j.Compensation.Currency
					period = string(j.Compensation.Period)
				}

				record := []string{j.Company, j.Title, j.Location, j.URL, payMin, payMax, currency, period}

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
			pay := ""
			if !j.Compensation.IsZero() {
				pay = " pay: " + describeCompensation(j.Compensation)
			}

			_, err := fmt.Fprintf(w, "company: %s title: %s location: %s%s url: %s\n",
				j.Company, j.Title, j.Location, pay, j.URL)

			return err
		}, func() error { return nil }, nil
	}
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
	var flags globalFlags

	cmd := &cobra.Command{
		Use:   "total",
		Short: "Count the job postings currently available",
		Long: "Count the job postings currently available.\n\n" +
			"Writes a single row of \"DATE POSTINGS COMPANIES\" to stdout and a header\n" +
			"to stderr, so the row can be appended straight to a record file.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := flags.logger(cmd.ErrOrStderr())

			ctx, cancel := flags.crawlContext(cmd)
			defer cancel()

			client, err := flags.client(logger)
			if err != nil {
				return err
			}

			jobs := internal.Dedupe(
				internal.AllWithConcurrency(
					ctx,
					client,
					flags.concurrency,
					services.JobsFuncs(services.SourcesMatching(nil))...,
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

			logger.InfoContext(ctx, "crawl finished",
				slog.Int("postings", total),
				slog.Int("companies", len(perCompany)),
				slog.Int("failed_sources", failed),
				slog.Bool("truncated", truncated),
			)

			fmt.Fprintf(cmd.ErrOrStderr(), "DATE POSTINGS COMPANIES\n")
			fmt.Fprintf(cmd.OutOrStdout(), "%s %d %d\n",
				time.Now().Format("01/02/06"), total, len(perCompany))

			// Recording a truncated crawl as a data point would corrupt the
			// long-running posting trend, which is this project's only historical
			// record. Fail loudly instead, so the caller can discard the row.
			if truncated {
				return fmt.Errorf(
					"crawl did not finish within %s: counted %d postings from %d companies, but this is incomplete and must not be recorded",
					flags.timeout, total, len(perCompany))
			}

			return nil
		},
	}

	flags.register(cmd)

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
