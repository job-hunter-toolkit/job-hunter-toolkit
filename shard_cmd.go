package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/services"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/shard"
	"github.com/spf13/cobra"
)

// newShardCommand builds the `shard` command group.
//
// A full crawl does not finish on one GitHub runner: the 07/26/26 baseline run
// recorded 473,404 postings from 1,772 companies after 350 minutes and was
// still incomplete. `shard plan`, `shard run` and `shard merge` split that
// crawl over several runners without letting the split change what the numbers
// mean — see internal/shard for why the split has to follow service affinity
// rather than source counts.
func newShardCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "shard",
		Short: "Plan, run, and merge a crawl split across several machines",
		Long: "Plan, run, and merge a crawl split across several machines.\n\n" +
			"Shards are assigned by service affinity, not by source count: every\n" +
			"source that talks to a given shared backend lands in the same shard so\n" +
			"that backend's rate limiter and 429 cooldown stay effective for the\n" +
			"whole run. Parallelism comes from the platforms whose tenants really\n" +
			"are on separate hosts.",
	}

	cmd.AddCommand(
		newShardPlanCommand(),
		newShardRunCommand(),
		newShardMergeCommand(),
	)

	return cmd
}

// newShardPlanCommand builds the `shard plan` command.
func newShardPlanCommand() *cobra.Command {
	var (
		shards      int
		output      string
		matrixPath  string
		priorPaths  []string
		companyTerm []string
	)

	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Produce a deterministic shard plan",
		Long: "Produce a deterministic shard plan.\n\n" +
			"Writes the plan to --output and the shard indexes to stdout as a JSON\n" +
			"array, which is what a GitHub Actions matrix expands with fromJSON. A\n" +
			"human-readable summary goes to stderr.\n\n" +
			"The same inputs always produce the same plan, including the same plan\n" +
			"id, so re-planning a retried workflow run does not reshuffle a crawl\n" +
			"that is already under way. Supply --prior to weight the packing by\n" +
			"measured per-source duration from previous crawl manifests; with none,\n" +
			"every source is weighted equally.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			sources := services.SourcesMatching(companyTerm)
			if len(sources) == 0 {
				return fmt.Errorf("no sources match %v: refusing to plan a crawl of nothing", companyTerm)
			}

			costs, err := loadPriorCosts(cmd.ErrOrStderr(), priorPaths)
			if err != nil {
				return err
			}

			plan, err := shard.Build(sources, shard.Options{
				ShardCount: shards,
				Costs:      costs,
				Commit:     buildCommit(),
				// Always the whole registry, even when --company narrows the
				// plan: a shard runner can recompute this and nothing else, so
				// it is what proves the plan and the runner are the same build.
				SourceSetID: shard.SourceSetID(services.SourcesMatching(nil)),
			})
			if err != nil {
				return err
			}

			plan.CreatedAt = time.Now().UTC()

			if err := shard.WritePlan(output, plan); err != nil {
				return err
			}

			if matrixPath != "" {
				if err := writeShardMatrixFile(matrixPath, plan.MatrixIndexes()); err != nil {
					return fmt.Errorf("write shard matrix %q: %w", matrixPath, err)
				}
			}

			// stdout is the machine-readable channel, so it carries exactly the
			// matrix and nothing else. Anything friendlier belongs on stderr.
			encoded, err := json.Marshal(plan.MatrixIndexes())
			if err != nil {
				return fmt.Errorf("encode shard matrix: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s\n", encoded)
			writePlanSummary(cmd.ErrOrStderr(), plan, output)

			return nil
		},
	}

	cmd.Flags().IntVar(&shards, "shards", 4, "number of shards to split the crawl into")
	cmd.Flags().StringVar(&output, "output", "shard-plan.json", "write the shard plan to this path")
	cmd.Flags().StringVar(&matrixPath, "github-matrix", "",
		"also write the shard indexes as a JSON array to this path")
	cmd.Flags().StringArrayVar(&priorPaths, "prior", nil,
		"crawl manifest from a previous run to weight the plan by measured duration; repeat for a rolling estimate")
	cmd.Flags().StringArrayVar(&companyTerm, "company", nil,
		"limit the plan to companies matching this term; repeat to add more")

	return cmd
}

// loadPriorCosts reads prior manifests into a cost estimate.
//
// A prior that cannot be read is a warning, not a failure. The estimate is an
// optimisation: losing it makes the plan less balanced, while refusing to plan
// because yesterday's artifact expired would stop the crawl entirely.
func loadPriorCosts(stderr io.Writer, paths []string) (map[shard.SourceRef]int64, error) {
	if len(paths) == 0 {
		return nil, nil
	}

	manifests := make([]shard.Manifest, 0, len(paths))

	for _, path := range paths {
		manifest, err := shard.ReadManifest(path)
		if err != nil {
			fmt.Fprintf(stderr, "warning: ignoring prior manifest %s: %v\n", path, err)

			continue
		}

		manifests = append(manifests, manifest)
	}

	costs := shard.EstimateCosts(manifests)
	if costs == nil {
		fmt.Fprintf(stderr, "warning: no usable per-source durations in %d prior manifest(s); weighting every source equally\n", len(paths))
	}

	return costs, nil
}

func writePlanSummary(stderr io.Writer, plan shard.Plan, output string) {
	fmt.Fprintf(stderr, "wrote %s: plan %s, source set %s, %d sources, %d shards, cost model %s\n",
		output, plan.PlanID, plan.SourceSetID, plan.SourceCount, plan.ShardCount, plan.CostModel)

	for _, planned := range plan.Shards {
		if len(planned.Sources) == 0 {
			// An empty shard is a runner that will start, resolve nothing, and
			// finish. Harmless, but it means --shards is larger than the number
			// of independent backends, which is worth saying out loud.
			fmt.Fprintf(stderr, "  shard %d: empty (fewer independent backends than shards)\n", planned.Index)

			continue
		}

		fmt.Fprintf(stderr, "  shard %d: %d sources, %d backends, estimate %d\n",
			planned.Index, len(planned.Sources), len(planned.AffinityKeys), planned.EstimatedMS)
	}
}

// newShardRunCommand builds the `shard run` command.
func newShardRunCommand() *cobra.Command {
	var (
		flags        globalFlags
		planPath     string
		index        int
		outputDir    string
		manifestPath string
		postingsPath string
		allowPartial bool
	)

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Crawl one shard of a plan",
		Long: "Crawl one shard of a plan.\n\n" +
			"Writes postings as newline-delimited JSON and a versioned manifest\n" +
			"recording what the shard attempted, what completed, what failed, and\n" +
			"whether it ran out of time. Both files are always written, including\n" +
			"when the shard fails, so the merge can say precisely what is missing\n" +
			"instead of finding nothing at all.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := flags.logger(cmd.ErrOrStderr())

			plan, err := shard.ReadPlan(planPath)
			if err != nil {
				return err
			}

			all := services.SourcesMatching(nil)

			// Two independent checks, because they fail differently. The source
			// set id catches a runner whose binary knows a different world than
			// the planner did, even when the plan's own sources all happen to
			// resolve. Resolve catches the specific sources this shard cannot
			// crawl, and names them.
			if got := shard.SourceSetID(all); got != plan.SourceSetID {
				return fmt.Errorf(
					"shard %d: this binary registers source set %s but plan %s was built from %s: refusing to crawl a shard of a plan that describes a different crawl",
					index, got, plan.PlanID, plan.SourceSetID)
			}

			sources, err := plan.Resolve(index, all)
			if err != nil {
				return err
			}

			manifestPath, postingsPath := shardOutputPaths(outputDir, index, manifestPath, postingsPath)

			postingsFile, err := os.Create(postingsPath)
			if err != nil {
				return fmt.Errorf("create shard postings %q: %w", postingsPath, err)
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

			logger.InfoContext(ctx, "shard started",
				slog.Int("shard", index),
				slog.Int("shards", plan.ShardCount),
				slog.String("plan_id", plan.PlanID),
				slog.Int("sources", len(sources)),
			)

			writer := shard.NewPostingWriter(postingsFile)

			var (
				companies = map[string]struct{}{}
				failed    int
				writeErr  error
			)

			jobs := internal.Dedupe(
				internal.AllWithConcurrency(ctx, client, flags.concurrency, sourceJobs...),
			)

			for jobPosting, err := range jobs {
				if err != nil {
					failed++

					continue
				}

				if writeErr = writer.Write(jobPosting); writeErr != nil {
					break
				}

				companies[jobPosting.Company] = struct{}{}
			}

			// Flush before deciding anything, so the manifest's count and the
			// bytes on disk describe the same set of postings. The merge
			// compares them and refuses a mismatch.
			flushErr := writer.Flush()
			closeErr := postingsFile.Close()

			truncated := ctx.Err() != nil
			status := shard.StatusComplete
			if truncated {
				status = shard.StatusPartial
			}

			manifest := newCrawlManifest(
				startedAt,
				time.Now().UTC(),
				flags.timeout,
				status,
				writer.Written(),
				len(companies),
				sourceResults(),
			)
			manifest.Shard = &shard.ShardStamp{
				Index:       index,
				Count:       plan.ShardCount,
				PlanID:      plan.PlanID,
				SourceSetID: plan.SourceSetID,
				Commit:      buildCommit(),
			}

			// The manifest is written before any error is returned. A shard that
			// dies without one is indistinguishable from a shard that never
			// started, and the merge would have nothing to name.
			if err := writeCrawlManifest(manifestPath, manifest); err != nil {
				return err
			}

			logger.InfoContext(ctx, "shard finished",
				slog.Int("shard", index),
				slog.Int("postings", writer.Written()),
				slog.Int("companies", len(companies)),
				slog.Int("failed_sources", failed),
				slog.String("status", status),
			)

			fmt.Fprintf(cmd.ErrOrStderr(), "shard %d/%d: %d postings, %d companies, %d source errors, %s\n",
				index, plan.ShardCount, writer.Written(), len(companies), failed, status)

			switch {
			case writeErr != nil:
				return fmt.Errorf("shard %d: write postings: %w", index, writeErr)
			case flushErr != nil:
				return fmt.Errorf("shard %d: %w", index, flushErr)
			case closeErr != nil:
				return fmt.Errorf("shard %d: close postings %q: %w", index, postingsPath, closeErr)
			}

			if truncated && !allowPartial {
				return fmt.Errorf(
					"shard %d did not finish within %s: counted %d postings from %d companies, but this is incomplete and must not be merged as a complete crawl",
					index, flags.timeout, writer.Written(), len(companies))
			}

			if truncated {
				logger.WarnContext(ctx, "shard reached its deadline; manifest records it as partial",
					slog.Int("shard", index),
					slog.Duration("timeout", flags.timeout),
					slog.Int("unfinished_sources", len(manifest.UnfinishedSources())),
				)
			}

			return nil
		},
	}

	flags.register(cmd)
	cmd.Flags().StringVar(&planPath, "plan", "shard-plan.json", "shard plan to crawl")
	cmd.Flags().IntVar(&index, "shard", 0, "index of the shard to crawl")
	cmd.Flags().StringVar(&outputDir, "output-dir", ".",
		"directory for this shard's manifest and postings, named shard-N.json and shard-N.ndjson")
	cmd.Flags().StringVar(&manifestPath, "manifest", "", "override the manifest path")
	cmd.Flags().StringVar(&postingsPath, "postings", "", "override the postings path")
	cmd.Flags().BoolVar(&allowPartial, "allow-partial", false,
		"exit successfully when the shard reaches its deadline; the manifest still records it as partial and the merge still refuses it unless told otherwise")

	return cmd
}

// shardOutputPaths resolves where a shard writes, preferring explicit overrides
// over the conventional names the merge looks for.
func shardOutputPaths(dir string, index int, manifestPath, postingsPath string) (string, string) {
	if manifestPath == "" {
		manifestPath = filepath.Join(dir, shard.ManifestFileName(index))
	}

	if postingsPath == "" {
		postingsPath = filepath.Join(dir, shard.PostingsFileName(index))
	}

	return manifestPath, postingsPath
}

func writeShardMatrixFile(path string, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode %q: %w", path, err)
	}

	return os.WriteFile(path, append(encoded, '\n'), 0o644)
}

// sortedPlatformNames returns a map's keys in a stable order, so summaries do not
// reorder themselves between runs.
func sortedPlatformNames[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}

	slices.Sort(keys)

	return keys
}
