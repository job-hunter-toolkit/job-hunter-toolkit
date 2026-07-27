package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/shard"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// planAndRunOneShard produces a real one-shard plan and crawls it against a
// deadline that cannot be met, which is how this suite stays hermetic: no
// request survives an already-expired context.
func planAndRunOneShard(t *testing.T) (string, string) {
	t.Helper()

	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.json")

	_, _, err := runShard(t, "shard", "plan", "--shards=1", "--output="+planPath, "--company=oxide")
	must.NoError(t, err)

	_, _, err = runShard(t, "shard", "run",
		"--plan="+planPath, "--shard=0", "--output-dir="+dir, "--timeout=1ns", "--allow-partial")
	must.NoError(t, err)

	return dir, planPath
}

// TestShardMergeCommandEmitsTheRecordRow pins the merge to the same output
// contract as `total`: the row on stdout, the header on stderr, four fields, so
// a sharded crawl appends to jobs_record.txt exactly like an unsharded one.
func TestShardMergeCommandEmitsTheRecordRow(t *testing.T) {
	t.Parallel()

	dir, planPath := planAndRunOneShard(t)
	mergedPath := filepath.Join(dir, "crawl-manifest.json")

	stdout, stderr, err := runShard(t, "shard", "merge",
		"--plan="+planPath, "--shards-dir="+dir, "--manifest="+mergedPath, "--allow-partial")
	must.NoError(t, err)

	rows := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
	must.Len(t, 1, rows, must.Sprint("stdout must carry exactly one row"))

	fields := strings.Fields(rows[0])
	must.Len(t, 4, fields)
	test.Eq(t, shard.StatusPartial, fields[3])

	test.StrContains(t, stderr, "DATE POSTINGS COMPANIES STATUS")
	test.StrContains(t, stderr, "before global deduplication")

	merged, err := shard.ReadManifest(mergedPath)
	must.NoError(t, err)

	// The merged manifest is a whole-crawl manifest. Nothing downstream may
	// mistake it for a shard and fold it into another total.
	must.Nil(t, merged.Shard)
	test.Eq(t, crawlManifestSchemaVersion, merged.SchemaVersion)
	test.Eq(t, shard.StatusPartial, merged.Status)
	test.SliceNotEmpty(t, merged.Sources)
}

func TestShardMergeCommandFailsClosedOnAPartialShard(t *testing.T) {
	t.Parallel()

	dir, planPath := planAndRunOneShard(t)

	stdout, _, err := runShard(t, "shard", "merge", "--plan="+planPath, "--shards-dir="+dir)
	must.ErrorContains(t, err, "did not finish")

	// Nothing is printed to stdout, so a caller redirecting the row into
	// jobs_record.txt cannot append a line from a refused merge.
	test.Eq(t, "", stdout)
}

func TestShardMergeCommandFailsClosedOnAMissingShard(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.json")

	_, _, err := runShard(t, "shard", "plan", "--shards=2", "--output="+planPath, "--company=oxide")
	must.NoError(t, err)

	_, _, err = runShard(t, "shard", "run",
		"--plan="+planPath, "--shard=0", "--output-dir="+dir, "--timeout=1ns", "--allow-partial")
	must.NoError(t, err)

	// Shard 1 never reported. --allow-partial covers a shard that ran out of
	// time, not one that is absent: a total missing a whole runner's worth of
	// sources would be recorded as the job market shrinking.
	for _, extra := range [][]string{nil, {"--allow-partial"}} {
		args := append([]string{"shard", "merge", "--plan=" + planPath, "--shards-dir=" + dir}, extra...)

		stdout, _, err := runShard(t, args...)
		must.ErrorContains(t, err, "shards are missing")
		test.Eq(t, "", stdout)
	}
}

func TestShardMergeCommandFailsClosedOnAnotherPlan(t *testing.T) {
	t.Parallel()

	dir, _ := planAndRunOneShard(t)

	// A plan with a different shard count is a different plan, and its shard 0
	// covers different sources than the artifacts on disk.
	otherPlan := filepath.Join(dir, "other-plan.json")

	_, _, err := runShard(t, "shard", "plan", "--shards=3", "--output="+otherPlan, "--company=oxide")
	must.NoError(t, err)

	_, _, err = runShard(t, "shard", "merge", "--plan="+otherPlan, "--shards-dir="+dir, "--allow-partial")
	must.ErrorContains(t, err, "missing")
}

func TestShardMergeCommandRefusesAnAbsentPlan(t *testing.T) {
	t.Parallel()

	_, _, err := runShard(t, "shard", "merge",
		"--plan="+filepath.Join(t.TempDir(), "absent.json"), "--shards-dir="+t.TempDir())
	must.ErrorContains(t, err, "open shard plan")
}
