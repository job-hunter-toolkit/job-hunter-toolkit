package shard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/services"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// posting builds a posting the way an adapter would, so the merge fixtures
// exercise the same identity function a real crawl does.
func posting(company, url string) *internal.JobPosting {
	return &internal.JobPosting{
		Company: company,
		URL:     url,
		Title:   "Engineer",
		Source:  internal.PostingSource{Platform: "alpha"},
	}
}

type shardFixture struct {
	// status is the crawl status the shard reports.
	status string

	// sourceStatus is the lifecycle state every source in the shard reports.
	sourceStatus string

	// postings are written to the NDJSON stream.
	postings []*internal.JobPosting

	// claimed overrides the manifest's posting count, to simulate a stream that
	// was cut short.
	claimed *int

	// extraSources adds refs the shard was never assigned.
	extraSources []SourceRef

	// dropSources removes this many of the shard's planned sources from the
	// manifest.
	dropSources int

	// mutate is applied to the manifest just before it is written.
	mutate func(*Manifest)
}

func writeShard(t *testing.T, dir string, plan Plan, index int, fixture shardFixture) {
	t.Helper()

	if fixture.status == "" {
		fixture.status = StatusComplete
	}

	if fixture.sourceStatus == "" {
		fixture.sourceStatus = "complete"
	}

	refs := plan.Shards[index].Sources
	refs = refs[:len(refs)-fixture.dropSources]

	runs := make([]services.SourceRun, 0, len(refs)+len(fixture.extraSources))
	for _, ref := range append(append([]SourceRef{}, refs...), fixture.extraSources...) {
		runs = append(runs, services.SourceRun{
			Platform:   ref.Platform,
			Key:        ref.Key,
			Company:    ref.Company,
			Status:     fixture.sourceStatus,
			DurationMS: 10,
		})
	}

	postingsPath := filepath.Join(dir, PostingsFileName(index))

	file, err := os.Create(postingsPath)
	must.NoError(t, err)

	writer := NewPostingWriter(file)
	for _, job := range fixture.postings {
		must.NoError(t, writer.Write(job))
	}

	must.NoError(t, writer.Flush())
	must.NoError(t, file.Close())

	companies := map[string]struct{}{}
	for _, job := range fixture.postings {
		companies[job.Company] = struct{}{}
	}

	started := time.Date(2026, 7, 27, 1, 0, 0, 0, time.UTC)
	manifest := NewManifest(started, started.Add(time.Minute), 75*time.Minute,
		fixture.status, writer.Written(), len(companies), runs)

	if fixture.claimed != nil {
		manifest.Postings = *fixture.claimed
	}

	manifest.Shard = &ShardStamp{
		Index:       index,
		Count:       plan.ShardCount,
		PlanID:      plan.PlanID,
		SourceSetID: plan.SourceSetID,
		Commit:      plan.Commit,
	}

	if fixture.mutate != nil {
		fixture.mutate(&manifest)
	}

	must.NoError(t, WriteManifest(filepath.Join(dir, ManifestFileName(index)), manifest))
}

// twoShardPlan splits the synthetic registry so shard 0 holds the slug platform
// and shard 1 holds the two tenant hosts.
func twoShardPlan(t *testing.T) Plan {
	t.Helper()

	plan, err := Build(syntheticSources(), Options{ShardCount: 2, Commit: "deadbeef"})
	must.NoError(t, err)

	return plan
}

// TestMergeDeduplicatesAcrossShards is the invariant the roadmap calls out by
// name: a posting URL can arrive through two integrations, so a total must be a
// union of identities, never a sum of shard counts.
func TestMergeDeduplicatesAcrossShards(t *testing.T) {
	t.Parallel()

	var (
		dir  = t.TempDir()
		plan = twoShardPlan(t)
		both = posting("Acme", "https://jobs.example.com/acme/1")
	)

	writeShard(t, dir, plan, 0, shardFixture{postings: []*internal.JobPosting{both}})
	writeShard(t, dir, plan, 1, shardFixture{postings: []*internal.JobPosting{both}})

	result, err := MergeDir(dir, plan, MergeOptions{})
	must.NoError(t, err)

	// Summing would say 2.
	test.Eq(t, 1, result.Manifest.Postings)
	test.Eq(t, 1, result.Manifest.Companies)
	test.Eq(t, 1, result.CrossShardDuplicates)
	test.Eq(t, 1, result.Shards[0].Postings)
	test.Eq(t, 1, result.Shards[1].Postings)
	test.Eq(t, StatusComplete, result.Manifest.Status)

	// The merged manifest is a whole-crawl manifest, so nothing downstream can
	// mistake it for a shard and merge it again.
	must.Nil(t, result.Manifest.Shard)
	test.Eq(t, ManifestSchemaVersion, result.Manifest.SchemaVersion)
	test.Eq(t, len(syntheticSources()), len(result.Manifest.Sources))
}

func TestMergeCountsDistinctPostingsAndCompaniesGlobally(t *testing.T) {
	t.Parallel()

	var (
		dir  = t.TempDir()
		plan = twoShardPlan(t)
	)

	writeShard(t, dir, plan, 0, shardFixture{postings: []*internal.JobPosting{
		posting("Acme", "https://jobs.example.com/acme/1"),
		posting("Acme", "https://jobs.example.com/acme/2"),
	}})
	writeShard(t, dir, plan, 1, shardFixture{postings: []*internal.JobPosting{
		posting("Acme", "https://jobs.example.com/acme/2"),
		posting("Globex", "https://jobs.example.com/globex/1"),
	}})

	result, err := MergeDir(dir, plan, MergeOptions{})
	must.NoError(t, err)

	test.Eq(t, 3, result.Manifest.Postings)
	test.Eq(t, 2, result.Manifest.Companies)
	test.Eq(t, 1, result.CrossShardDuplicates)
	test.Eq(t, map[string]int{"alpha": 3}, result.PostingsPerPlatform)
}

func TestMergeRefusesAMissingShard(t *testing.T) {
	t.Parallel()

	var (
		dir  = t.TempDir()
		plan = twoShardPlan(t)
	)

	writeShard(t, dir, plan, 0, shardFixture{postings: []*internal.JobPosting{posting("Acme", "https://x/1")}})

	_, err := MergeDir(dir, plan, MergeOptions{})
	must.ErrorContains(t, err, "1 of 2 shards are missing")

	// --allow-partial is about a shard that ran out of time, not about a shard
	// that never reported at all.
	_, err = MergeDir(dir, plan, MergeOptions{AllowPartial: true})
	must.ErrorContains(t, err, "shards are missing")
}

func TestMergeRefusesAMissingPostingsStream(t *testing.T) {
	t.Parallel()

	var (
		dir  = t.TempDir()
		plan = twoShardPlan(t)
	)

	writeShard(t, dir, plan, 0, shardFixture{postings: []*internal.JobPosting{posting("Acme", "https://x/1")}})
	writeShard(t, dir, plan, 1, shardFixture{})
	must.NoError(t, os.Remove(filepath.Join(dir, PostingsFileName(1))))

	_, err := MergeDir(dir, plan, MergeOptions{})
	must.ErrorContains(t, err, "no postings stream")
}

func TestMergeRefusesATruncatedShard(t *testing.T) {
	t.Parallel()

	var (
		dir  = t.TempDir()
		plan = twoShardPlan(t)
	)

	writeShard(t, dir, plan, 0, shardFixture{postings: []*internal.JobPosting{posting("Acme", "https://x/1")}})
	writeShard(t, dir, plan, 1, shardFixture{status: StatusPartial})

	_, err := MergeDir(dir, plan, MergeOptions{})
	must.ErrorContains(t, err, "did not finish")

	// Opting in must still label the result partial, never complete.
	result, err := MergeDir(dir, plan, MergeOptions{AllowPartial: true})
	must.NoError(t, err)
	test.Eq(t, StatusPartial, result.Manifest.Status)
}

func TestMergeRefusesAShardWithSourcesThatNeverFinished(t *testing.T) {
	t.Parallel()

	var (
		dir  = t.TempDir()
		plan = twoShardPlan(t)
	)

	// A shard whose context happened to be clean but that never scheduled some
	// of its sources still did not do what it promised.
	writeShard(t, dir, plan, 0, shardFixture{postings: []*internal.JobPosting{posting("Acme", "https://x/1")}})
	writeShard(t, dir, plan, 1, shardFixture{sourceStatus: "planned"})

	_, err := MergeDir(dir, plan, MergeOptions{})
	must.ErrorContains(t, err, "did not finish")
	must.ErrorContains(t, err, "2 sources still unfinished")
}

func TestMergeRefusesADifferentPlan(t *testing.T) {
	t.Parallel()

	var (
		dir  = t.TempDir()
		plan = twoShardPlan(t)
	)

	writeShard(t, dir, plan, 0, shardFixture{})
	writeShard(t, dir, plan, 1, shardFixture{mutate: func(m *Manifest) {
		m.Shard.PlanID = "0000000000000000"
	}})

	_, err := MergeDir(dir, plan, MergeOptions{})
	must.ErrorContains(t, err, "cannot be combined")
}

func TestMergeRefusesADifferentShardCount(t *testing.T) {
	t.Parallel()

	var (
		dir  = t.TempDir()
		plan = twoShardPlan(t)
	)

	writeShard(t, dir, plan, 0, shardFixture{})
	writeShard(t, dir, plan, 1, shardFixture{mutate: func(m *Manifest) {
		m.Shard.Count = 4
	}})

	_, err := MergeDir(dir, plan, MergeOptions{})
	must.ErrorContains(t, err, "4-shard plan")
}

func TestMergeRefusesADifferentSourceSet(t *testing.T) {
	t.Parallel()

	var (
		dir  = t.TempDir()
		plan = twoShardPlan(t)
	)

	writeShard(t, dir, plan, 0, shardFixture{})
	writeShard(t, dir, plan, 1, shardFixture{mutate: func(m *Manifest) {
		m.Shard.SourceSetID = "1111111111111111"
	}})

	_, err := MergeDir(dir, plan, MergeOptions{})
	must.ErrorContains(t, err, "did not agree on what a full crawl is")
}

func TestMergeRefusesADifferentCommit(t *testing.T) {
	t.Parallel()

	var (
		dir  = t.TempDir()
		plan = twoShardPlan(t)
	)

	writeShard(t, dir, plan, 0, shardFixture{})
	writeShard(t, dir, plan, 1, shardFixture{mutate: func(m *Manifest) {
		m.Shard.Commit = "cafebabe"
	}})

	_, err := MergeDir(dir, plan, MergeOptions{})
	must.ErrorContains(t, err, "built from commit")
}

func TestMergeRefusesADifferentSchemaVersion(t *testing.T) {
	t.Parallel()

	var (
		dir  = t.TempDir()
		plan = twoShardPlan(t)
	)

	writeShard(t, dir, plan, 0, shardFixture{})
	writeShard(t, dir, plan, 1, shardFixture{mutate: func(m *Manifest) {
		m.SchemaVersion = ManifestSchemaVersion + 1
	}})

	_, err := MergeDir(dir, plan, MergeOptions{})
	must.ErrorContains(t, err, "schema version")
}

func TestMergeRefusesAWholeCrawlManifest(t *testing.T) {
	t.Parallel()

	var (
		dir  = t.TempDir()
		plan = twoShardPlan(t)
	)

	writeShard(t, dir, plan, 0, shardFixture{})
	writeShard(t, dir, plan, 1, shardFixture{mutate: func(m *Manifest) {
		m.Shard = nil
	}})

	_, err := MergeDir(dir, plan, MergeOptions{})
	must.ErrorContains(t, err, "no shard stamp")
}

func TestMergeRefusesAShardThatCrawledSomeoneElsesSources(t *testing.T) {
	t.Parallel()

	var (
		dir  = t.TempDir()
		plan = twoShardPlan(t)
	)

	// Two processes on one backend is the pressure increase the affinity plan
	// exists to prevent, so it must fail the run rather than pass unnoticed.
	stolen := plan.Shards[0].Sources[0]

	writeShard(t, dir, plan, 0, shardFixture{})
	writeShard(t, dir, plan, 1, shardFixture{extraSources: []SourceRef{stolen}})

	_, err := MergeDir(dir, plan, MergeOptions{})
	must.ErrorContains(t, err, "sources it was not assigned")
}

func TestMergeRefusesAShardThatSkippedItsSources(t *testing.T) {
	t.Parallel()

	var (
		dir  = t.TempDir()
		plan = twoShardPlan(t)
	)

	writeShard(t, dir, plan, 0, shardFixture{dropSources: 1})
	writeShard(t, dir, plan, 1, shardFixture{})

	_, err := MergeDir(dir, plan, MergeOptions{})
	must.ErrorContains(t, err, "omitted 1 of its planned sources")
}

func TestMergeRefusesAShortPostingsStream(t *testing.T) {
	t.Parallel()

	var (
		dir     = t.TempDir()
		plan    = twoShardPlan(t)
		claimed = 5
	)

	writeShard(t, dir, plan, 0, shardFixture{
		postings: []*internal.JobPosting{posting("Acme", "https://x/1")},
		claimed:  &claimed,
	})
	writeShard(t, dir, plan, 1, shardFixture{})

	_, err := MergeDir(dir, plan, MergeOptions{})
	must.ErrorContains(t, err, "postings stream is truncated")
}

func TestMergeRefusesACorruptPostingsStream(t *testing.T) {
	t.Parallel()

	var (
		dir  = t.TempDir()
		plan = twoShardPlan(t)
	)

	writeShard(t, dir, plan, 0, shardFixture{postings: []*internal.JobPosting{posting("Acme", "https://x/1")}})
	writeShard(t, dir, plan, 1, shardFixture{})

	must.NoError(t, os.WriteFile(filepath.Join(dir, PostingsFileName(1)), []byte("{not json}\n"), 0o600))

	_, err := MergeDir(dir, plan, MergeOptions{})
	must.ErrorContains(t, err, "decode postings")
}

func TestMergeRefusesADuplicateShardIndex(t *testing.T) {
	t.Parallel()

	plan := twoShardPlan(t)

	artifact := ShardArtifacts{Index: 0, ManifestName: "a.json"}
	_, err := Merge(plan, []ShardArtifacts{artifact, artifact}, MergeOptions{})
	must.ErrorContains(t, err, "supplied twice")
}

func TestMergeRefusesAnOutOfRangeShardIndex(t *testing.T) {
	t.Parallel()

	plan := twoShardPlan(t)

	_, err := Merge(plan, []ShardArtifacts{{Index: 9, ManifestName: "a.json"}}, MergeOptions{})
	must.ErrorContains(t, err, "outside the plan's 2 shards")
}

func TestMergeRefusesAnInvalidPlan(t *testing.T) {
	t.Parallel()

	plan := twoShardPlan(t)
	plan.PlanID = "tampered"

	_, err := Merge(plan, nil, MergeOptions{})
	must.ErrorContains(t, err, "does not match its own assignment")
}

func TestMergeReportsTheLongestShardBudget(t *testing.T) {
	t.Parallel()

	var (
		dir  = t.TempDir()
		plan = twoShardPlan(t)
	)

	// Shards run in parallel, so the crawl's budget is the longest shard's, not
	// the sum, and a summary that added them would claim 150 minutes of budget
	// for a 75-minute wall clock.
	writeShard(t, dir, plan, 0, shardFixture{})
	writeShard(t, dir, plan, 1, shardFixture{mutate: func(m *Manifest) {
		m.Timeout = (90 * time.Minute).String()
	}})

	result, err := MergeDir(dir, plan, MergeOptions{})
	must.NoError(t, err)
	test.Eq(t, (90 * time.Minute).String(), result.Manifest.Timeout)
}

func TestMergedManifestSummarizesEveryShardsSources(t *testing.T) {
	t.Parallel()

	var (
		dir  = t.TempDir()
		plan = twoShardPlan(t)
	)

	writeShard(t, dir, plan, 0, shardFixture{})
	writeShard(t, dir, plan, 1, shardFixture{})

	result, err := MergeDir(dir, plan, MergeOptions{})
	must.NoError(t, err)

	test.Eq(t, len(syntheticSources()), result.Manifest.SourceCounts["complete"])
	test.Eq(t, 2, len(result.Shards))

	// Sources are ordered so the workflow summary is stable between runs.
	var names []string
	for _, source := range result.Manifest.Sources {
		names = append(names, source.Platform+"/"+source.Key)
	}

	must.Eq(t, []string{
		"alpha/a", "alpha/b", "alpha/c",
		"beta/t1.example.com", "beta/t2.example.com",
	}, names)
}

func TestMergeRejectsAPathTraversalInAPostingsName(t *testing.T) {
	t.Parallel()

	// The merge derives artifact paths from the shard index, never from a
	// string inside a downloaded manifest, so a hostile manifest has nowhere to
	// put a path. This pins that: naming is a pure function of the index.
	test.Eq(t, "shard-3.json", ManifestFileName(3))
	test.Eq(t, "shard-3.ndjson", PostingsFileName(3))
	test.False(t, strings.Contains(ManifestFileName(3), ".."))
}
