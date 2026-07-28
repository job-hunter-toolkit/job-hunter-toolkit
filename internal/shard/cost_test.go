package shard

import (
	"testing"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/services"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func run(platform, key, status string, durationMS int64) services.SourceRun {
	return services.SourceRun{Platform: platform, Key: key, Company: key, Status: status, DurationMS: durationMS}
}

func manifestOf(runs ...services.SourceRun) Manifest {
	return NewManifest(time.Time{}, time.Time{}, time.Minute, StatusComplete, 0, 0, runs)
}

func TestEstimateCostsTakesTheMedianAcrossManifests(t *testing.T) {
	t.Parallel()

	// A median over several days is the roadmap's thrash cap: one anomalous
	// night cannot move the plan.
	costs := EstimateCosts([]Manifest{
		manifestOf(run("alpha", "a", "complete", 100)),
		manifestOf(run("alpha", "a", "complete", 120)),
		manifestOf(run("alpha", "a", "complete", 900_000)),
	})

	must.MapNotEmpty(t, costs)
	test.Eq(t, int64(120), costs[SourceRef{Platform: "alpha", Key: "a"}])
}

func TestEstimateCostsIgnoresRunsThatNeverRan(t *testing.T) {
	t.Parallel()

	costs := EstimateCosts([]Manifest{
		manifestOf(
			run("alpha", "a", "planned", 0),
			run("alpha", "b", "running", 0),
			run("alpha", "c", "truncated", 5_000),
		),
	})

	_, plannedSampled := costs[SourceRef{Platform: "alpha", Key: "a"}]
	test.False(t, plannedSampled)

	// A source still going when the deadline hit is expensive, not unknown.
	test.Eq(t, int64(5_000), costs[SourceRef{Platform: "alpha", Key: "c"}])
}

func TestEstimateCostsClampsAnomalies(t *testing.T) {
	t.Parallel()

	costs := EstimateCosts([]Manifest{
		manifestOf(
			run("alpha", "slow", "truncated", 10*60*60*1000),
			run("alpha", "fast", "complete", 0),
			run("alpha", "instant", "complete", 1),
		),
	})

	test.Eq(t, MaxSourceCostMS, costs[SourceRef{Platform: "alpha", Key: "slow"}])
	test.Eq(t, MinSourceCostMS, costs[SourceRef{Platform: "alpha", Key: "instant"}])

	// A zero duration is not a measurement, so it is not a sample.
	_, sampled := costs[SourceRef{Platform: "alpha", Key: "fast"}]
	test.False(t, sampled)
}

func TestEstimateCostsChargesUnknownSourcesTheMedianSource(t *testing.T) {
	t.Parallel()

	costs := EstimateCosts([]Manifest{
		manifestOf(
			run("alpha", "a", "complete", 100),
			run("alpha", "b", "complete", 200),
			run("alpha", "c", "complete", 300),
		),
	})

	test.Eq(t, int64(200), UnknownSourceCost(costs))
	test.Eq(t, DefaultSourceCostMS, UnknownSourceCost(nil))
}

func TestEstimateCostsReturnsNilWithoutTimings(t *testing.T) {
	t.Parallel()

	// Packing against noise is worse than packing against nothing, so an empty
	// estimate must fall back to the uniform model rather than to zeros.
	costs := EstimateCosts([]Manifest{manifestOf(run("alpha", "a", "planned", 0))})
	must.Nil(t, costs)

	plan, err := Build(syntheticSources(), Options{ShardCount: 2, Costs: costs})
	must.NoError(t, err)
	test.Eq(t, CostModelUniform, plan.CostModel)
}

// TestBuildBalancesByMeasuredCost proves the estimate actually changes the
// plan: uniform weighting puts the three cheap alpha sources together because
// they outnumber the two beta tenants, while measured durations put the one
// expensive tenant on a runner of its own.
func TestBuildBalancesByMeasuredCost(t *testing.T) {
	t.Parallel()

	sources := syntheticSources()

	uniform, err := Build(sources, Options{ShardCount: 2})
	must.NoError(t, err)
	test.Eq(t, CostModelUniform, uniform.CostModel)

	costs := EstimateCosts([]Manifest{
		manifestOf(
			run("alpha", "a", "complete", 1_000),
			run("alpha", "b", "complete", 1_000),
			run("alpha", "c", "complete", 1_000),
			run("beta", "t1.example.com", "complete", 600_000),
			run("beta", "t2.example.com", "complete", 1_000),
		),
	})

	weighted, err := Build(sources, Options{ShardCount: 2, Costs: costs})
	must.NoError(t, err)
	test.Eq(t, CostModelDuration, weighted.CostModel)

	shardOf := func(plan Plan, ref SourceRef) int {
		for _, shard := range plan.Shards {
			for _, candidate := range shard.Sources {
				if candidate.identity() == ref {
					return shard.Index
				}
			}
		}

		t.Fatalf("source %s is in no shard", ref)

		return -1
	}

	expensive := SourceRef{Platform: "beta", Key: "t1.example.com"}

	// The expensive tenant is alone; everything else shares the other shard.
	sole := shardOf(weighted, expensive)
	test.Eq(t, 1, len(weighted.Shards[sole].Sources))
	test.Eq(t, int64(600_000), weighted.Shards[sole].EstimatedMS)

	// And the plan really did change shape versus uniform weighting.
	test.NotEq(t, uniform.PlanID, weighted.PlanID)
}
