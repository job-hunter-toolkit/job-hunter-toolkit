package main

import (
	"fmt"
	"io"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/shard"
	"github.com/spf13/cobra"
)

// newShardMergeCommand builds the `shard merge` command.
//
// It emits the same "DATE POSTINGS COMPANIES STATUS" contract as `total`: the
// row on stdout, the header on stderr, so the row can be appended straight to
// jobs_record.txt. A sharded crawl has to be indistinguishable from an
// unsharded one at that boundary, or the record's history stops being one
// series.
func newShardMergeCommand() *cobra.Command {
	var (
		planPath     string
		shardsDir    string
		manifestPath string
		allowPartial bool
	)

	cmd := &cobra.Command{
		Use:   "merge",
		Short: "Combine every shard of a plan into one verified total",
		Long: "Combine every shard of a plan into one verified total.\n\n" +
			"Writes a single row of \"DATE POSTINGS COMPANIES STATUS\" to stdout and\n" +
			"a header to stderr, exactly like `total`.\n\n" +
			"Postings are counted as a global union of posting identities, never as\n" +
			"a sum of per-shard counts: the same posting URL can arrive through two\n" +
			"integrations, so summing inflates the record with duplicates a\n" +
			"single-process crawl would have collapsed.\n\n" +
			"The merge fails closed. It refuses to produce a total when a shard is\n" +
			"missing, was built from a different plan, source set or commit, uses a\n" +
			"different manifest schema, crawled sources it was not assigned, omitted\n" +
			"sources it was, wrote fewer postings than its manifest claims, or did\n" +
			"not finish.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			plan, err := shard.ReadPlan(planPath)
			if err != nil {
				return err
			}

			result, err := shard.MergeDir(shardsDir, plan, shard.MergeOptions{AllowPartial: allowPartial})
			if err != nil {
				return err
			}

			if manifestPath != "" {
				if err := writeCrawlManifest(manifestPath, result.Manifest); err != nil {
					return err
				}
			}

			writeMergeSummary(cmd.ErrOrStderr(), plan, result)

			fmt.Fprintf(cmd.ErrOrStderr(), "DATE POSTINGS COMPANIES STATUS\n")
			fmt.Fprintf(cmd.OutOrStdout(), "%s %d %d %s\n",
				time.Now().Format("01/02/06"),
				result.Manifest.Postings,
				result.Manifest.Companies,
				result.Manifest.Status)

			if result.Manifest.Status != shard.StatusComplete {
				// Reaching here at all means --allow-partial was given, so this
				// is not a refusal; it is the warning that the fourth column has
				// to stay visibly distinct from a completed crawl.
				fmt.Fprintf(cmd.ErrOrStderr(),
					"warning: recording a partial crawl by explicit request; %d of %d shards did not finish\n",
					countUnfinishedShards(result), plan.ShardCount)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&planPath, "plan", "shard-plan.json", "shard plan the shards were crawled from")
	cmd.Flags().StringVar(&shardsDir, "shards-dir", ".",
		"directory holding every shard's shard-N.json and shard-N.ndjson")
	cmd.Flags().StringVar(&manifestPath, "manifest", "",
		"write the merged whole-crawl manifest to this path")
	cmd.Flags().BoolVar(&allowPartial, "allow-partial", false,
		"merge shards that reached their deadline, labelling the total partial; it never labels an incomplete crawl complete, and it does not excuse a missing or mismatched shard")

	return cmd
}

// writeMergeSummary prints the numbers the roadmap asks a sharded run to show,
// most importantly the gap between the per-shard counts and the merged total.
// That gap is the size of the error a naive sum would have made, and printing
// it is how the invariant stays visible rather than merely tested.
func writeMergeSummary(stderr io.Writer, plan shard.Plan, result shard.MergeResult) {
	raw := 0
	for _, summary := range result.Shards {
		raw += summary.Postings
	}

	fmt.Fprintf(stderr, "merged plan %s across %d shards\n", plan.PlanID, plan.ShardCount)

	for _, summary := range result.Shards {
		fmt.Fprintf(stderr, "  shard %d: %s, %d sources (%d unfinished), %d postings, %.1fs\n",
			summary.Index, summary.Status, summary.Sources, summary.Unfinished,
			summary.Postings, float64(summary.DurationMS)/1000)
	}

	fmt.Fprintf(stderr, "  postings: %d before global deduplication, %d after (%d duplicates across shards)\n",
		raw, result.Manifest.Postings, result.CrossShardDuplicates)

	for _, platform := range sortedKeys(result.PostingsPerPlatform) {
		name := platform
		if name == "" {
			name = "(unattributed)"
		}

		fmt.Fprintf(stderr, "  %s: %d postings\n", name, result.PostingsPerPlatform[platform])
	}
}

func countUnfinishedShards(result shard.MergeResult) int {
	count := 0

	for _, summary := range result.Shards {
		if summary.Status != shard.StatusComplete || summary.Unfinished > 0 {
			count++
		}
	}

	return count
}
