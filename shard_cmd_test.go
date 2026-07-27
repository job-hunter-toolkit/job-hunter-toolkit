package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/services"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/shard"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
	"github.com/spf13/cobra"
)

// shardRoot builds a root command carrying only the shard commands.
//
// It deliberately does not use newRootCommand: these tests must pass whether or
// not the shard group has been wired into the root yet, so that wiring it is a
// one-line change to main.go rather than a change that also has to fix tests.
func shardRoot() *cobra.Command {
	root := &cobra.Command{Use: "job-hunter-toolkit", SilenceUsage: true}
	root.AddCommand(newShardCommand())

	return root
}

// runShard executes a shard command, returning stdout, stderr and the error.
func runShard(t *testing.T, args ...string) (string, string, error) {
	t.Helper()

	var stdout, stderr bytes.Buffer

	cmd := shardRoot()
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)

	err := cmd.ExecuteContext(t.Context())

	return stdout.String(), stderr.String(), err
}

func TestShardPlanCommandIsDeterministic(t *testing.T) {
	t.Parallel()

	// The nightly re-runs. If a re-plan reshuffled sources, a retried shard
	// would crawl a different slice than the artifacts already uploaded, and
	// the merge would refuse a run that was actually fine.
	var (
		first  = filepath.Join(t.TempDir(), "plan.json")
		second = filepath.Join(t.TempDir(), "plan.json")
		matrix = filepath.Join(t.TempDir(), "matrix.json")
	)

	stdout, stderr, err := runShard(t, "shard", "plan", "--shards=4", "--output="+first, "--github-matrix="+matrix)
	must.NoError(t, err)

	// stdout is the machine channel and carries only the matrix.
	test.Eq(t, "[0,1,2,3]\n", stdout)
	test.StrContains(t, stderr, "4 shards")

	_, _, err = runShard(t, "shard", "plan", "--shards=4", "--output="+second)
	must.NoError(t, err)

	firstPlan, err := shard.ReadPlan(first)
	must.NoError(t, err)

	secondPlan, err := shard.ReadPlan(second)
	must.NoError(t, err)

	test.Eq(t, firstPlan.PlanID, secondPlan.PlanID)
	must.Eq(t, firstPlan.Shards, secondPlan.Shards)
	test.Eq(t, shard.CostModelUniform, firstPlan.CostModel)

	matrixBytes, err := readShardFile(matrix)
	must.NoError(t, err)
	test.Eq(t, "[0,1,2,3]\n", matrixBytes)
}

func TestShardPlanCommandCoversTheWholeRegistry(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "plan.json")

	_, _, err := runShard(t, "shard", "plan", "--shards=6", "--output="+path)
	must.NoError(t, err)

	plan, err := shard.ReadPlan(path)
	must.NoError(t, err)

	all := services.SourcesMatching(nil)
	test.Eq(t, len(all), plan.SourceCount)
	test.Eq(t, shard.SourceSetID(all), plan.SourceSetID)
}

func TestShardPlanCommandWeightsByPriorManifests(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	all := services.SourcesMatching(nil)

	runs := make([]services.SourceRun, 0, len(all))
	for i, source := range all {
		runs = append(runs, services.SourceRun{
			Platform:   source.Platform,
			Key:        source.Key,
			Company:    source.Company,
			Status:     "complete",
			DurationMS: int64(1 + i%97),
		})
	}

	prior := filepath.Join(dir, "prior.json")
	must.NoError(t, writeCrawlManifest(prior,
		newCrawlManifest(time.Now(), time.Now(), time.Hour, "complete", 1, 1, runs)))

	path := filepath.Join(dir, "plan.json")

	_, _, err := runShard(t, "shard", "plan", "--shards=4", "--output="+path, "--prior="+prior)
	must.NoError(t, err)

	plan, err := shard.ReadPlan(path)
	must.NoError(t, err)
	test.Eq(t, shard.CostModelDuration, plan.CostModel)
}

func TestShardPlanCommandSurvivesAnUnusablePriorManifest(t *testing.T) {
	t.Parallel()

	// Losing yesterday's artifact makes the plan less balanced. Refusing to
	// plan at all would stop the crawl, which is much worse.
	dir := t.TempDir()
	path := filepath.Join(dir, "plan.json")

	_, stderr, err := runShard(t, "shard", "plan", "--shards=2", "--output="+path,
		"--prior="+filepath.Join(dir, "does-not-exist.json"))
	must.NoError(t, err)
	test.StrContains(t, stderr, "ignoring prior manifest")

	plan, err := shard.ReadPlan(path)
	must.NoError(t, err)
	test.Eq(t, shard.CostModelUniform, plan.CostModel)
}

func TestShardPlanCommandRejectsAnEmptySourceSelection(t *testing.T) {
	t.Parallel()

	_, _, err := runShard(t, "shard", "plan", "--shards=2",
		"--output="+filepath.Join(t.TempDir(), "plan.json"),
		"--company=definitely-not-a-real-company-xyzzy")
	must.ErrorContains(t, err, "refusing to plan a crawl of nothing")
}

// TestShardRunCommandFailsClosedOnItsDeadline pins the shard half of the
// invariant: a crawl that ran out of time is never reported as a finished one,
// and it still leaves both artifacts behind so the merge can say what happened.
func TestShardRunCommandFailsClosedOnItsDeadline(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.json")

	_, _, err := runShard(t, "shard", "plan", "--shards=1", "--output="+planPath, "--company=oxide")
	must.NoError(t, err)

	// A timeout this small cannot finish any crawl, which is how the rest of
	// this suite keeps deadline behaviour hermetic.
	_, stderr, err := runShard(t, "shard", "run",
		"--plan="+planPath, "--shard=0", "--output-dir="+dir, "--timeout=1ns")
	must.ErrorContains(t, err, "must not be merged as a complete crawl")
	test.StrContains(t, stderr, "partial")

	manifest, err := shard.ReadManifest(filepath.Join(dir, shard.ManifestFileName(0)))
	must.NoError(t, err)

	plan, err := shard.ReadPlan(planPath)
	must.NoError(t, err)

	must.NotNil(t, manifest.Shard)
	test.Eq(t, 0, manifest.Shard.Index)
	test.Eq(t, 1, manifest.Shard.Count)
	test.Eq(t, plan.PlanID, manifest.Shard.PlanID)
	test.Eq(t, plan.SourceSetID, manifest.Shard.SourceSetID)
	test.Eq(t, shard.StatusPartial, manifest.Status)
	test.Eq(t, crawlManifestSchemaVersion, manifest.SchemaVersion)
	test.Eq(t, len(plan.Shards[0].Sources), len(manifest.Sources))

	// The postings stream exists even for a shard that produced nothing, so a
	// missing file always means a missing shard rather than an empty one.
	postings, err := readShardFile(filepath.Join(dir, shard.PostingsFileName(0)))
	must.NoError(t, err)
	test.Eq(t, "", postings)
}

func TestShardRunCommandCanRecordAnExplicitPartialShard(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.json")

	_, _, err := runShard(t, "shard", "plan", "--shards=1", "--output="+planPath, "--company=oxide")
	must.NoError(t, err)

	_, _, err = runShard(t, "shard", "run",
		"--plan="+planPath, "--shard=0", "--output-dir="+dir, "--timeout=1ns", "--allow-partial")
	must.NoError(t, err)

	manifest, err := shard.ReadManifest(filepath.Join(dir, shard.ManifestFileName(0)))
	must.NoError(t, err)
	test.Eq(t, shard.StatusPartial, manifest.Status)
	test.False(t, manifest.Complete())
}

func TestShardRunCommandRefusesAPlanFromAnotherBuild(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.json")

	// A plan whose source set fingerprint does not match this binary's registry
	// is a plan from a different build. Crawling it would produce a manifest
	// that looks complete for a shard that silently skipped whatever this
	// binary has never heard of.
	plan, err := shard.Build(services.SourcesMatching([]string{"oxide"}), shard.Options{
		ShardCount:  1,
		SourceSetID: "0123456789abcdef0123456789abcdef",
	})
	must.NoError(t, err)
	must.NoError(t, shard.WritePlan(planPath, plan))

	_, _, err = runShard(t, "shard", "run", "--plan="+planPath, "--shard=0", "--output-dir="+dir, "--timeout=1ns")
	must.ErrorContains(t, err, "describes a different crawl")
}

// TestShardRunCommandCompletesAnEmptyShard covers the case where --shards
// exceeds the number of independent backends. The runner has nothing to do, but
// it must still produce both artifacts and report complete, because a merge
// that treats "no work" and "no report" the same cannot tell a spare runner
// from a dead one.
func TestShardRunCommandCompletesAnEmptyShard(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.json")

	// oxide is a single source, so a two-shard plan leaves shard 1 empty.
	_, _, err := runShard(t, "shard", "plan", "--shards=2", "--output="+planPath, "--company=oxide")
	must.NoError(t, err)

	plan, err := shard.ReadPlan(planPath)
	must.NoError(t, err)
	must.SliceEmpty(t, plan.Shards[1].Sources)

	// No timeout trick here: a shard with no sources makes no requests at all,
	// so this is hermetic on its own.
	_, _, err = runShard(t, "shard", "run", "--plan="+planPath, "--shard=1", "--output-dir="+dir)
	must.NoError(t, err)

	manifest, err := shard.ReadManifest(filepath.Join(dir, shard.ManifestFileName(1)))
	must.NoError(t, err)
	test.Eq(t, shard.StatusComplete, manifest.Status)
	test.True(t, manifest.Complete())
	test.Eq(t, 0, manifest.Postings)
}

func TestShardRunCommandRefusesAnOutOfRangeShard(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.json")

	_, _, err := runShard(t, "shard", "plan", "--shards=2", "--output="+planPath, "--company=oxide")
	must.NoError(t, err)

	_, _, err = runShard(t, "shard", "run", "--plan="+planPath, "--shard=7", "--output-dir="+dir, "--timeout=1ns")
	must.ErrorContains(t, err, "outside the plan's 2 shards")
}

func TestShardRunCommandRefusesAMissingPlan(t *testing.T) {
	t.Parallel()

	_, _, err := runShard(t, "shard", "run",
		"--plan="+filepath.Join(t.TempDir(), "absent.json"), "--shard=0", "--timeout=1ns")
	must.ErrorContains(t, err, "open shard plan")
}

// readShardFile returns a file's exact contents. Nothing is trimmed: a test
// asserting "this shard produced no postings" has to see the file as written.
func readShardFile(path string) (string, error) {
	data, err := os.ReadFile(path)

	return string(data), err
}
