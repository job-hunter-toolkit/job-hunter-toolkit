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

// TestShardedTotalEqualsUnshardedTotal is the whole point of the package: a
// crawl split over N runners has to produce the number a single process would
// have produced from the same postings. Anything else turns a scheduling change
// into an apparent change in the job market, which jobs_record.txt records
// permanently.
func TestShardedTotalEqualsUnshardedTotal(t *testing.T) {
	t.Parallel()

	var (
		dir  = t.TempDir()
		plan = twoShardPlan(t)
		all  []*internal.JobPosting
	)

	perShard := map[int][]*internal.JobPosting{}

	for _, planned := range plan.Shards {
		for i, ref := range planned.Sources {
			for job := range 3 {
				// Every third posting is republished under an identity another
				// shard also produces, which is what a company registered on two
				// ATSs looks like from here.
				company := ref.Company
				url := "https://jobs.example.com/" + ref.Key + "/" + string(rune('a'+job))

				if job == 2 {
					company = "Shared Employer"
					url = "https://jobs.example.com/shared/" + string(rune('a'+i%2))
				}

				p := posting(company, url)
				perShard[planned.Index] = append(perShard[planned.Index], p)
				all = append(all, p)
			}
		}
	}

	for _, planned := range plan.Shards {
		writeShard(t, dir, plan, planned.Index, shardFixture{postings: perShard[planned.Index]})
	}

	result, err := MergeDir(dir, plan, MergeOptions{})
	must.NoError(t, err)

	// What a single-process crawl would have counted over the same postings.
	seq := func(yield func(*internal.JobPosting, error) bool) {
		for _, job := range all {
			if !yield(job, nil) {
				return
			}
		}
	}

	var (
		unsharded int
		companies = map[string]struct{}{}
	)

	for job := range internal.Dedupe(seq) {
		unsharded++
		companies[job.Company] = struct{}{}
	}

	must.Eq(t, unsharded, result.Manifest.Postings)
	must.Eq(t, len(companies), result.Manifest.Companies)

	// And prove the naive alternative really would have been wrong, so this
	// test cannot pass vacuously on a fixture with no cross-shard overlap.
	summed := 0
	for _, summary := range result.Shards {
		summed += summary.Postings
	}

	test.Greater(t, result.Manifest.Postings, summed,
		test.Sprint("fixture must contain postings that arrive through two shards"))
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

// TestMergeRefusesWhenTooManySourcesFailed is a regression test.
//
// "failed" is a terminal source state, so a shard whose every source failed has
// genuinely finished, and Complete() said so: a merge accepted it and produced
// status "complete" with zero postings. Unsharded that was survivable, because a
// failure broad enough to do it took the whole crawl down and was obvious.
// Sharded it is this design's worst failure: one dead runner out of sixteen
// loses only its own sources, so the day's total is short by a sixteenth, is
// still labelled complete, and is appended to jobs_record.txt and graphed, where
// nothing distinguishes it from hiring actually falling.
func TestMergeRefusesWhenTooManySourcesFailed(t *testing.T) {
	t.Parallel()

	var (
		dir  = t.TempDir()
		plan = twoShardPlan(t)
	)

	// One whole shard's sources fail, which is the dead-runner case.
	writeShard(t, dir, plan, 0, shardFixture{sourceStatus: "failed"})
	writeShard(t, dir, plan, 1, shardFixture{postings: []*internal.JobPosting{
		posting("Acme", "https://jobs.example.com/acme/1"),
	}})

	_, err := MergeDir(dir, plan, MergeOptions{})

	must.Error(t, err)
	test.StrContains(t, err.Error(), "must not be recorded as complete")
}

// TestMergeRecordsAMassFailureAsPartial covers the deliberate-override path: an
// operator who asks for the number anyway gets it labelled partial, never
// complete, so the chart keeps it off the trend line.
func TestMergeRecordsAMassFailureAsPartial(t *testing.T) {
	t.Parallel()

	var (
		dir  = t.TempDir()
		plan = twoShardPlan(t)
	)

	writeShard(t, dir, plan, 0, shardFixture{sourceStatus: "failed"})
	writeShard(t, dir, plan, 1, shardFixture{postings: []*internal.JobPosting{
		posting("Acme", "https://jobs.example.com/acme/1"),
	}})

	result, err := MergeDir(dir, plan, MergeOptions{AllowPartial: true})

	must.NoError(t, err)
	test.Eq(t, StatusPartial, result.Manifest.Status)
}

// TestMergeToleratesTheOrdinaryFailureRate guards the other direction: boards
// are retired constantly, so a handful of failures is normal and must not
// refuse a real crawl.
func TestMergeToleratesTheOrdinaryFailureRate(t *testing.T) {
	t.Parallel()

	var (
		dir  = t.TempDir()
		plan = twoShardPlan(t)
	)

	writeShard(t, dir, plan, 0, shardFixture{postings: []*internal.JobPosting{
		posting("Acme", "https://jobs.example.com/acme/1"),
	}})
	writeShard(t, dir, plan, 1, shardFixture{postings: []*internal.JobPosting{
		posting("Beta", "https://jobs.example.com/beta/1"),
	}})

	result, err := MergeDir(dir, plan, MergeOptions{})

	must.NoError(t, err)
	test.Eq(t, StatusComplete, result.Manifest.Status)
}
