package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/corpus"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/services"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/jobposting"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/query"
	"github.com/spf13/cobra"
)

// newCorpusCommand builds the `corpus` command group: the plumbing between a
// crawl's artifacts and a published .jhtc generation.
//
// internal/corpus holds the whole algorithm — identity, closure, the columnar
// format — and until this command existed nothing fed it. The verbs are wiring,
// not logic: `crawl` produces the postings.ndjson + crawl-manifest.json pair a
// fold consumes, `apply` folds that pair into a generation, `inspect`, `query`
// and `verify` read one back. Anything that decides whether a posting lives or
// dies stays in internal/corpus, where it is tested against the invariants.
func newCorpusCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "corpus",
		Short: "Build, publish, and read the persistent posting corpus",
		Long: "Build, publish, and read the persistent posting corpus.\n\n" +
			"A crawl answers \"what is on the boards right now\"; the corpus is the\n" +
			"record over time — when each posting appeared, and when the evidence says\n" +
			"it closed. `corpus crawl` writes a full-posting NDJSON stream plus the\n" +
			"crawl manifest, `corpus apply` folds that pair into the previous\n" +
			"generation, and `inspect`, `query` and `verify` read a generation back.\n\n" +
			"A generation is rewritten whole, never updated in place, and a source\n" +
			"that failed, truncated, or went unvisited can never make its previously\n" +
			"seen postings look removed — that rule lives in internal/corpus and this\n" +
			"command only wires it up.",
	}

	cmd.AddCommand(
		newCorpusCrawlCommand(),
		newCorpusApplyCommand(),
		newCorpusInspectCommand(),
		newCorpusQueryCommand(),
		newCorpusVerifyCommand(),
	)

	return cmd
}

// newCorpusCrawlCommand builds `corpus crawl`.
//
// It exists because no other command produces what `corpus apply` needs: `total
// --manifest` writes the manifest but discards the postings, and `shard run`
// streams identity-only PostingRecords (key, company, platform) because the
// merge needs nothing more. The corpus needs the full posting. Rather than
// change what those commands emit — one feeds the nightly's guarded record row,
// the other a pinned merge contract — this verb reuses their two existing
// shapes unchanged: the postings file is the frozen `postings --json` NDJSON
// contract, and the manifest is the versioned shard.Manifest that
// corpus.DecodeManifestSources already decodes.
func newCorpusCrawlCommand() *cobra.Command {
	var (
		flags        globalFlags
		companies    []string
		postingsPath string
		manifestPath string
		allowPartial bool
	)

	cmd := &cobra.Command{
		Use:   "crawl",
		Short: "Crawl and write the postings + manifest pair that `corpus apply` folds",
		Long: "Crawl and write the postings + manifest pair that `corpus apply` folds.\n\n" +
			"Writes every deduplicated posting as newline-delimited JSON (the same\n" +
			"records `postings --json` emits) and a versioned crawl manifest recording\n" +
			"per-source outcomes. Both files are always written, including when the\n" +
			"crawl fails or hits its deadline, so an apply can still fold whatever was\n" +
			"seen — the manifest's per-source statuses are what stop a truncated run\n" +
			"from closing anything.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCorpusCrawl(cmd, &flags, companies, postingsPath, manifestPath, allowPartial)
		},
	}

	flags.register(cmd)
	cmd.Flags().StringArrayVar(&companies, "company", nil,
		"limit the crawl to companies matching this term; repeat to add more. The manifest lists only what was attempted, so a narrowed crawl can never close postings from sources it skipped")
	cmd.Flags().StringVar(&postingsPath, "postings", "postings.ndjson",
		"write the full posting stream to this path as NDJSON")
	cmd.Flags().StringVar(&manifestPath, "manifest", "crawl-manifest.json",
		"write the versioned crawl manifest to this path")
	cmd.Flags().BoolVar(&allowPartial, "allow-partial", false,
		"exit successfully when the crawl reaches its deadline; the manifest still records partial, and closure still refuses every non-complete source")

	return cmd
}

func runCorpusCrawl(
	cmd *cobra.Command,
	flags *globalFlags,
	companies []string,
	postingsPath string,
	manifestPath string,
	allowPartial bool,
) error {
	logger := flags.logger(cmd.ErrOrStderr())

	sources := services.SourcesMatching(companies)
	if len(sources) == 0 {
		return fmt.Errorf("no known companies match %s", strings.Join(companies, ", "))
	}

	postingsFile, err := os.Create(postingsPath)
	if err != nil {
		return fmt.Errorf("create postings %q: %w", postingsPath, err)
	}
	defer postingsFile.Close()

	ctx, cancel := flags.crawlContext(cmd)
	defer cancel()

	client, err := flags.client(logger)
	if err != nil {
		return err
	}

	startedAt := time.Now().UTC()
	sourceJobs, sourceResults := services.Observe(sources, logger)

	logger.InfoContext(ctx, "corpus crawl started", slog.Int("sources", len(sources)))

	encoder := json.NewEncoder(postingsFile)

	var (
		perCompany = map[string]struct{}{}
		written    int
		failed     int
		writeErr   error
	)

	jobs := internal.Dedupe(
		internal.AllWithConcurrency(ctx, client, flags.concurrency, sourceJobs...),
	)

	for jobPosting, err := range jobs {
		if err != nil {
			failed++

			continue
		}

		if writeErr = encoder.Encode(jobPosting); writeErr != nil {
			break
		}

		written++
		perCompany[jobPosting.Company] = struct{}{}
	}

	closeErr := postingsFile.Close()

	truncated := ctx.Err() != nil
	status := "complete"
	if truncated {
		status = "partial"
	}

	manifest := newCrawlManifest(
		startedAt,
		time.Now().UTC(),
		flags.timeout,
		status,
		written,
		len(perCompany),
		sourceResults(),
	)

	// The manifest is written before any error is returned, mirroring `shard
	// run`: a crawl that dies without one leaves an apply nothing to name, and
	// the per-source statuses in it are exactly what the closure rules need to
	// refuse the sources that failed.
	if err := writeCrawlManifest(manifestPath, manifest); err != nil {
		return err
	}

	fmt.Fprintf(cmd.ErrOrStderr(), "corpus crawl: %d postings from %d companies, %d source errors, %s\n",
		written, len(perCompany), failed, status)

	switch {
	case writeErr != nil:
		return fmt.Errorf("write postings %q: %w", postingsPath, writeErr)
	case closeErr != nil:
		return fmt.Errorf("close postings %q: %w", postingsPath, closeErr)
	}

	if truncated && !allowPartial {
		return fmt.Errorf(
			"crawl did not finish within %s: wrote %d postings, but the run is partial and --allow-partial was not given",
			flags.timeout, written)
	}

	return nil
}

// newCorpusApplyCommand builds `corpus apply`.
func newCorpusApplyCommand() *cobra.Command {
	var (
		corpusDir    string
		outputDir    string
		postingsPath string
		manifestPath string
		runAtFlag    string
		writer       string
		retire       bool
		dryRun       bool
	)

	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Fold a crawl's postings and manifest into the next corpus generation",
		Long: "Fold a crawl's postings and manifest into the next corpus generation.\n\n" +
			"Reads the previous generation from --corpus (an empty or missing\n" +
			"directory starts generation 1), folds the run in under the closure\n" +
			"policy, and writes the next generation to --output — whole, never in\n" +
			"place, with the manifest last as the commit point. The churn report goes\n" +
			"to stderr; a machine-readable summary of the new generation goes to\n" +
			"stdout as JSON.\n\n" +
			"The run's clock reading defaults to the crawl manifest's finished_at\n" +
			"rather than to now, so the same inputs always produce byte-identical\n" +
			"output — which is also what makes a re-run of a failed publish safe.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCorpusApply(cmd, corpusApplyOptions{
				corpusDir:    corpusDir,
				outputDir:    outputDir,
				postingsPath: postingsPath,
				manifestPath: manifestPath,
				runAt:        runAtFlag,
				writer:       writer,
				retire:       retire,
				dryRun:       dryRun,
			})
		},
	}

	cmd.Flags().StringVar(&corpusDir, "corpus", "",
		"directory holding the previous generation; empty or missing starts a new corpus")
	cmd.Flags().StringVar(&outputDir, "output", "",
		"directory to write the next generation to; defaults to --corpus. Writing to a fresh directory and swapping it in is what keeps a killed apply from leaving a torn generation")
	cmd.Flags().StringVar(&postingsPath, "postings", "postings.ndjson",
		"the crawl's full-posting NDJSON stream, as `corpus crawl` writes it")
	cmd.Flags().StringVar(&manifestPath, "manifest", "crawl-manifest.json",
		"the crawl's manifest; its per-source statuses are the only evidence closure will accept")
	cmd.Flags().StringVar(&runAtFlag, "run-at", "",
		"RFC 3339 clock reading for the run; defaults to the manifest's finished_at")
	cmd.Flags().StringVar(&writer, "writer", "",
		"writer identity recorded in the generation; defaults to corpus-apply@<commit>")
	cmd.Flags().BoolVar(&retire, "retire-missing", false,
		"mark corpus sources that have left this binary's registry as retired; their rows freeze rather than close")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false,
		"fold and report the churn without writing a generation")

	return cmd
}

type corpusApplyOptions struct {
	corpusDir    string
	outputDir    string
	postingsPath string
	manifestPath string
	runAt        string
	writer       string
	retire       bool
	dryRun       bool
}

// corpusApplySummary is `corpus apply`'s stdout, one JSON object. It exists so
// a workflow can guard on the numbers — generation, rows, open — without
// parsing prose, the same way total.txt is the nightly's machine channel.
type corpusApplySummary struct {
	Generation    int64        `json:"generation"`
	RunAt         time.Time    `json:"run_at"`
	Writer        string       `json:"writer,omitempty"`
	Partial       bool         `json:"partial,omitempty"`
	Rows          int          `json:"rows"`
	Open          int          `json:"open"`
	Stale         int          `json:"stale"`
	Closed        int          `json:"closed"`
	Lapsed        int          `json:"lapsed"`
	Sources       int          `json:"sources"`
	Qualifying    int          `json:"qualifying"`
	Refused       int          `json:"refused"`
	Churn         corpus.Churn `json:"churn"`
	ContentDigest string       `json:"content_digest,omitempty"`
	DryRun        bool         `json:"dry_run,omitempty"`
}

func runCorpusApply(cmd *cobra.Command, opts corpusApplyOptions) error {
	ctx := cmd.Context()

	base, baseGeneration, err := openCorpusOrEmpty(ctx, opts.corpusDir)
	if err != nil {
		return err
	}

	manifest, err := readCrawlManifestForApply(opts.manifestPath)
	if err != nil {
		return err
	}

	runAt := manifest.finishedAt
	if opts.runAt != "" {
		if runAt, err = time.Parse(time.RFC3339, opts.runAt); err != nil {
			return fmt.Errorf("invalid --run-at %q: %w", opts.runAt, err)
		}
	}

	writer := opts.writer
	if writer == "" {
		writer = "corpus-apply"
		if commit := buildCommit(); commit != "" {
			writer += "@" + commit
		}
	}

	var retired []jobposting.PostingSource
	if opts.retire {
		retired = retiredSources(base)
	}

	postingsFile, err := os.Open(opts.postingsPath)
	if err != nil {
		return fmt.Errorf("open postings %q: %w", opts.postingsPath, err)
	}
	defer postingsFile.Close()

	generation, err := corpus.Apply(ctx, base, corpus.RunInput{
		RunAt:    runAt,
		Sources:  manifest.sources,
		Postings: decodePostings(postingsFile, opts.postingsPath),
		Retired:  retired,
		Partial:  manifest.partial,
		Writer:   writer,
	}, corpus.Policy{})
	if err != nil {
		return err
	}

	if !opts.dryRun {
		outputDir := opts.outputDir
		if outputDir == "" {
			outputDir = opts.corpusDir
		}

		if outputDir == "" {
			return errors.New("corpus apply: no --output and no --corpus to write the generation to")
		}

		if err := generation.WriteTo(ctx, corpus.DirPublisher{Dir: outputDir}); err != nil {
			return err
		}
	}

	writeChurnReport(cmd.ErrOrStderr(), baseGeneration, generation)

	summary := corpusApplySummary{
		Generation:    generation.Manifest.Generation,
		RunAt:         generation.Manifest.RunAt,
		Writer:        generation.Manifest.Writer,
		Partial:       generation.Manifest.Partial,
		Rows:          generation.Manifest.Rows,
		Open:          generation.Manifest.Open,
		Stale:         generation.Manifest.Stale,
		Closed:        generation.Manifest.Closed,
		Lapsed:        generation.Manifest.Lapsed,
		Sources:       generation.Manifest.Sources,
		Qualifying:    qualifyingCount(generation),
		Refused:       len(generation.Verdicts) - qualifyingCount(generation),
		Churn:         generation.Churn,
		ContentDigest: generation.Manifest.ContentDigest,
		DryRun:        opts.dryRun,
	}

	encoder := json.NewEncoder(cmd.OutOrStdout())

	return encoder.Encode(summary)
}

// openCorpusOrEmpty opens the generation in dir, or returns an empty corpus
// when dir is unset or holds no manifest yet.
//
// Only a missing manifest means "first generation". Any other failure — a
// corrupt file, a version this reader must not read — is an error, because
// silently starting over would discard every FirstSeen the corpus exists to
// hold.
func openCorpusOrEmpty(ctx context.Context, dir string) (*corpus.Corpus, int64, error) {
	if dir == "" {
		return corpus.Empty(), 0, nil
	}

	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		return corpus.Empty(), 0, nil
	}

	store := corpus.DirStore{Dir: dir}

	if _, err := store.Size(ctx, corpus.ManifestFile); errors.Is(err, fs.ErrNotExist) {
		return corpus.Empty(), 0, nil
	}

	opened, err := corpus.Open(ctx, store)
	if err != nil {
		return nil, 0, fmt.Errorf("open corpus %q: %w", dir, err)
	}

	return opened, opened.Manifest().Generation, nil
}

// applyManifest is the slice of a crawl manifest an apply needs: the sources
// array (through corpus.DecodeManifestSources, so the two cannot disagree),
// plus the status and clock the RunInput carries.
type applyManifest struct {
	sources    []corpus.SourceRun
	partial    bool
	finishedAt time.Time
}

func readCrawlManifestForApply(path string) (applyManifest, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return applyManifest{}, fmt.Errorf("read crawl manifest %q: %w", path, err)
	}

	sources, err := corpus.DecodeManifestSources(strings.NewReader(string(raw)))
	if err != nil {
		return applyManifest{}, fmt.Errorf("%s: %w", path, err)
	}

	var envelope struct {
		Status     string    `json:"status"`
		FinishedAt time.Time `json:"finished_at"`
	}

	if err := json.Unmarshal(raw, &envelope); err != nil {
		return applyManifest{}, fmt.Errorf("decode crawl manifest %q: %w", path, err)
	}

	if envelope.FinishedAt.IsZero() {
		return applyManifest{}, fmt.Errorf(
			"crawl manifest %q has no finished_at; pass --run-at to supply the run's clock reading", path)
	}

	return applyManifest{
		sources: sources,
		// Anything other than complete is folded as partial. The distinction the
		// closure rules actually consume is per source, from the sources array;
		// this flag is only the honest label on the generation's manifest.
		partial:    envelope.Status != shardStatusComplete,
		finishedAt: envelope.FinishedAt,
	}, nil
}

// shardStatusComplete mirrors shard.StatusComplete without making this file's
// reader chase the import: the string is a published contract (the fourth
// column of jobs_record.txt) and pinned by tests there.
const shardStatusComplete = "complete"

// decodePostings streams a `postings --json` NDJSON file as a jobposting.Seq.
//
// json.Decoder rather than a line scanner: JSON values self-delimit, so there
// is no line-length cap to size against a posting nobody has seen yet.
func decodePostings(r io.Reader, name string) jobposting.Seq {
	return func(yield func(*jobposting.JobPosting, error) bool) {
		decoder := json.NewDecoder(r)

		for line := 1; ; line++ {
			var posting jobposting.JobPosting

			if err := decoder.Decode(&posting); err != nil {
				if errors.Is(err, io.EOF) {
					return
				}

				yield(nil, fmt.Errorf("decode postings %q record %d: %w", name, line, err))

				return
			}

			if !yield(&posting, nil) {
				return
			}
		}
	}
}

// retiredSources reports base corpus sources that this binary's registry no
// longer contains. Retirement freezes rows rather than closing them: deleting
// a source from the registry is not evidence its employer withdrew every job.
func retiredSources(base *corpus.Corpus) []jobposting.PostingSource {
	registry := map[jobposting.PostingSource]struct{}{}
	for _, source := range services.SourcesMatching(nil) {
		registry[jobposting.PostingSource{Platform: source.Platform, Key: source.Key}] = struct{}{}
	}

	var retired []jobposting.PostingSource

	for _, state := range base.Sources() {
		if state.Retired {
			continue
		}

		if _, present := registry[state.Source]; !present {
			retired = append(retired, state.Source)
		}
	}

	return retired
}

func qualifyingCount(generation *corpus.Generation) int {
	count := 0

	for _, verdict := range generation.Verdicts {
		if verdict.Qualifies {
			count++
		}
	}

	return count
}

// writeChurnReport prints what the fold did, including every reason a source
// was refused as evidence of absence. The refusals matter most: nothing else
// in the corpus records one, and a refusal is exactly the thing an operator
// needs to see before trusting a night's closures.
func writeChurnReport(w io.Writer, baseGeneration int64, generation *corpus.Generation) {
	churn := generation.Churn
	manifest := generation.Manifest

	fmt.Fprintf(w, "corpus apply: generation %d -> %d at %s\n",
		baseGeneration, manifest.Generation, manifest.RunAt.Format(time.RFC3339))
	fmt.Fprintf(w, "  rows %d: open %d, stale %d, closed %d, lapsed %d\n",
		manifest.Rows, manifest.Open, manifest.Stale, manifest.Closed, manifest.Lapsed)
	fmt.Fprintf(w, "  churn: appeared %d, changed %d, unchanged %d, missing %d, closed %d, reopened %d, lapsed %d\n",
		churn.Appeared, churn.Changed, churn.Unchanged, churn.Missing, churn.Closed, churn.Reopened, churn.Lapsed)

	if churn.Dropped > 0 || churn.Rejected > 0 {
		fmt.Fprintf(w, "  dropped %d (identity collisions), rejected %d (source absent from manifest)\n",
			churn.Dropped, churn.Rejected)
	}

	refusals := map[string]int{}

	for _, verdict := range generation.Verdicts {
		if !verdict.Qualifies {
			refusals[verdict.Reason]++
		}
	}

	if len(refusals) == 0 {
		fmt.Fprintf(w, "  every visited source qualified as evidence of absence\n")

		return
	}

	parts := make([]string, 0, len(refusals))
	for _, reason := range slices.Sorted(maps.Keys(refusals)) {
		parts = append(parts, fmt.Sprintf("%s=%d", reason, refusals[reason]))
	}

	fmt.Fprintf(w, "  refused as evidence of absence: %s\n", strings.Join(parts, " "))
}

// newCorpusInspectCommand builds `corpus inspect`.
func newCorpusInspectCommand() *cobra.Command {
	var (
		corpusDir string
		asJSON    bool
		runCount  int
	)

	cmd := &cobra.Command{
		Use:   "inspect",
		Short: "Report a generation's manifest, counts, and recent churn",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			opened, err := corpus.Open(ctx, corpus.DirStore{Dir: corpusDir})
			if err != nil {
				return err
			}

			manifest := opened.Manifest()
			runs := opened.Runs()

			if runCount > 0 && len(runs) > runCount {
				runs = runs[len(runs)-runCount:]
			}

			out := cmd.OutOrStdout()

			if asJSON {
				encoder := json.NewEncoder(out)
				encoder.SetIndent("", "  ")

				return encoder.Encode(struct {
					Manifest corpus.Manifest    `json:"manifest"`
					Runs     []corpus.RunRecord `json:"runs"`
				}{manifest, runs})
			}

			fmt.Fprintf(out, "generation %d, written %s by %s\n",
				manifest.Generation, manifest.RunAt.Format(time.RFC3339), cmp.Or(manifest.Writer, "unknown"))
			fmt.Fprintf(out, "format v%d (min reader v%d), identity v%d, digest %s\n",
				manifest.FormatVersion, manifest.MinReaderVersion, manifest.IdentityVersion, manifest.ContentDigest)

			partial := ""
			if manifest.Partial {
				partial = " (from a partial crawl)"
			}

			fmt.Fprintf(out, "%d rows over %d sources%s: open %d, stale %d, closed %d, lapsed %d\n",
				manifest.Rows, manifest.Sources, partial,
				manifest.Open, manifest.Stale, manifest.Closed, manifest.Lapsed)
			fmt.Fprintf(out, "policy: missing-runs %d, empty-streak %d, min-ratio %.2f, freshness %s, lapse %s\n",
				manifest.Policy.MissingRuns, manifest.Policy.EmptyStreak, manifest.Policy.MinRatio,
				manifest.Policy.FreshnessTarget, manifest.Policy.LapseAfter)

			if len(runs) > 0 {
				fmt.Fprintf(out, "last %d run(s):\n", len(runs))

				for _, run := range runs {
					fmt.Fprintf(out, "  %s: %d sources (%d qualifying), %d postings; +%d appeared, %d changed, %d closed, %d lapsed\n",
						run.RunAt.Format(time.RFC3339), run.Sources, run.Qualifying, run.Postings,
						run.Churn.Appeared, run.Churn.Changed, run.Churn.Closed, run.Churn.Lapsed)
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&corpusDir, "corpus", "", "directory holding the generation")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output the manifest and run history as JSON")
	cmd.Flags().IntVar(&runCount, "runs", 5, "how many recent runs to report; 0 for all retained")
	_ = cmd.MarkFlagRequired("corpus")

	return cmd
}

// newCorpusQueryCommand builds `corpus query`.
func newCorpusQueryCommand() *cobra.Command {
	var (
		corpusDir      string
		asJSON         bool
		states         []string
		limit          int
		offset         int
		asOf           string
		filter         query.Query
		employmentType []string
		workplaceType  []string
		postedSince    string
	)

	cmd := &cobra.Command{
		Use:   "query",
		Short: "Run the shared query vocabulary against a generation",
		Long: "Run the shared query vocabulary against a generation.\n\n" +
			"The filters are the same ones `postings` takes, applied to the stored\n" +
			"corpus instead of a live crawl, plus what only a corpus can answer: each\n" +
			"row's lifecycle state. By default only open postings are returned;\n" +
			"--state widens that to stale (source not crawled recently), closed\n" +
			"(absent from qualifying runs of its own source), and lapsed (source no\n" +
			"longer observed; no closing date exists or is invented).\n\n" +
			"Results are paginated with --limit and --offset in the corpus's stored\n" +
			"order, which is stable for a given generation.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			wanted, err := parseCorpusStates(states)
			if err != nil {
				return err
			}

			if limit < 0 || offset < 0 {
				return errors.New("--limit and --offset must not be negative")
			}

			now := time.Now().UTC()
			if asOf != "" {
				if now, err = time.Parse(time.RFC3339, asOf); err != nil {
					return fmt.Errorf("invalid --as-of %q: %w", asOf, err)
				}
			}

			if filter.EmploymentTypes, err = parseEmploymentTypes(employmentType); err != nil {
				return err
			}

			if filter.WorkplaceTypes, err = parseWorkplaceTypes(workplaceType); err != nil {
				return err
			}

			if filter.PostedSince, err = parsePostedSince(postedSince, now); err != nil {
				return err
			}

			return runCorpusQuery(cmd, corpusDir, filter, wanted, now, limit, offset, asJSON)
		},
	}

	cmd.Flags().StringVar(&corpusDir, "corpus", "", "directory holding the generation")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output matches as newline-delimited JSON rows")
	cmd.Flags().StringSliceVar(&states, "state", []string{"open"},
		"lifecycle states to include: open, stale, closed, lapsed, or all")
	cmd.Flags().IntVar(&limit, "limit", 50, "maximum matches to return; 0 returns them all")
	cmd.Flags().IntVar(&offset, "offset", 0, "matches to skip before returning any")
	cmd.Flags().StringVar(&asOf, "as-of", "",
		"RFC 3339 instant to evaluate lifecycle states at; defaults to now")

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
		"only postings publishing pay of at least this much per year; implies --has-pay")
	cmd.Flags().StringSliceVar(&filter.Departments, "department", nil,
		"only postings whose department or team contains any of these terms")
	cmd.Flags().StringSliceVar(&employmentType, "employment-type", nil,
		"only postings of these employment types ("+joinValues(internal.EmploymentTypeValues())+")")
	cmd.Flags().StringSliceVar(&workplaceType, "workplace-type", nil,
		"only postings of these workplace types ("+joinValues(internal.WorkplaceTypeValues())+")")
	cmd.Flags().StringVar(&postedSince, "posted-since", "",
		"only postings published at or after this point: a date, an RFC 3339 timestamp, or an age such as 7d")
	_ = cmd.MarkFlagRequired("corpus")

	return cmd
}

// corpusQueryRow is one match in `corpus query --json` output: the posting
// plus the life the corpus knows about it.
type corpusQueryRow struct {
	ID        string                `json:"id"`
	State     string                `json:"state"`
	FirstSeen time.Time             `json:"first_seen"`
	LastSeen  time.Time             `json:"last_seen,omitzero"`
	Closed    *corpus.Closure       `json:"closed,omitempty"`
	Reopens   int                   `json:"reopens,omitempty"`
	Posting   jobposting.JobPosting `json:"posting"`
}

func runCorpusQuery(
	cmd *cobra.Command,
	corpusDir string,
	filter query.Query,
	wanted map[corpus.State]bool,
	now time.Time,
	limit int,
	offset int,
	asJSON bool,
) error {
	ctx := cmd.Context()

	opened, err := corpus.Open(ctx, corpus.DirStore{Dir: corpusDir})
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	encoder := json.NewEncoder(out)

	matched, emitted := 0, 0

	for row, err := range opened.Rows(ctx) {
		if err != nil {
			return err
		}

		state := opened.State(row, now)
		if !wanted[state] {
			continue
		}

		if !filter.Match(&row.Posting) {
			continue
		}

		matched++

		if matched <= offset {
			continue
		}

		if limit > 0 && emitted >= limit {
			// Keep counting so the total under the pagination is the real total,
			// but emit nothing further.
			continue
		}

		emitted++

		if asJSON {
			if err := encoder.Encode(corpusQueryRow{
				ID:        row.ID,
				State:     state.String(),
				FirstSeen: row.FirstSeen,
				LastSeen:  opened.LastSeen(row),
				Closed:    row.Closed,
				Reopens:   row.Reopens,
				Posting:   row.Posting,
			}); err != nil {
				return err
			}

			continue
		}

		posting := row.Posting
		if _, err := fmt.Fprintf(out, "%-6s company: %s title: %s location: %s first_seen: %s url: %s\n",
			state, posting.Company, posting.Title, posting.Location,
			row.FirstSeen.Format(time.DateOnly), posting.URL); err != nil {
			return err
		}
	}

	fmt.Fprintf(cmd.ErrOrStderr(), "%d match(es); showing %d after offset %d (generation %d as of %s)\n",
		matched, emitted, offset, opened.Manifest().Generation, now.Format(time.RFC3339))

	return nil
}

func parseCorpusStates(states []string) (map[corpus.State]bool, error) {
	byName := map[string]corpus.State{
		"open":   corpus.StateOpen,
		"stale":  corpus.StateStale,
		"closed": corpus.StateClosed,
		"lapsed": corpus.StateLapsed,
	}

	wanted := map[corpus.State]bool{}

	for _, raw := range states {
		name := strings.ToLower(strings.TrimSpace(raw))
		if name == "" {
			continue
		}

		if name == "all" {
			for _, state := range byName {
				wanted[state] = true
			}

			continue
		}

		state, ok := byName[name]
		if !ok {
			return nil, fmt.Errorf("invalid --state %q: valid values are open, stale, closed, lapsed, all", raw)
		}

		wanted[state] = true
	}

	if len(wanted) == 0 {
		return nil, errors.New("--state selects no lifecycle states")
	}

	return wanted, nil
}

// newCorpusVerifyCommand builds `corpus verify`.
func newCorpusVerifyCommand() *cobra.Command {
	var corpusDir string

	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Recompute a generation's content digest and check it against its manifest",
		Long: "Recompute a generation's content digest and check it against its manifest.\n\n" +
			"This is the expensive read — every column — and the one that catches a\n" +
			"truncated download, a torn publish, and a corrupted object. A publish\n" +
			"pipeline should run it against what it is about to serve, and fail\n" +
			"closed on any mismatch.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			opened, err := corpus.Open(ctx, corpus.DirStore{Dir: corpusDir})
			if err != nil {
				return err
			}

			if err := corpus.Verify(ctx, opened); err != nil {
				return err
			}

			manifest := opened.Manifest()
			fmt.Fprintf(cmd.OutOrStdout(), "ok: generation %d, %d rows, digest %s\n",
				manifest.Generation, manifest.Rows, manifest.ContentDigest)

			return nil
		},
	}

	cmd.Flags().StringVar(&corpusDir, "corpus", "", "directory holding the generation")
	_ = cmd.MarkFlagRequired("corpus")

	return cmd
}
