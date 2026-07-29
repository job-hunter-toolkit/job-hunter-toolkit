package schedule

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/services"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/shard"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

var planningNow = time.Date(2026, 7, 28, 9, 30, 0, 0, time.UTC)

// seededStore gives every source of a shape a plausible history, so tests
// exercise the estimator rather than the cold-start path.
func seededStore(shapes []platformShape, lastSuccess time.Time) *Store {
	store := NewStore()
	oracle := oracleOf(shapes, nil)

	for _, source := range registryOf(shapes) {
		id := SourceID{Platform: source.Platform, Key: source.Key}
		cost, yield, _ := oracle(id)

		store.PutSource(SourceState{
			Platform:    id.Platform,
			Key:         id.Key,
			Company:     source.Company,
			LastAttempt: formatStamp(lastSuccess),
			LastSuccess: formatStamp(lastSuccess),
			DurationMS:  []int32{int32(cost.Milliseconds())},
			Postings:    []int32{int32(yield)},
		})
	}

	return store
}

func TestBuildRejectsAnEmptyRegistry(t *testing.T) {
	t.Parallel()

	_, err := Build(nil, NewStore(), Options{Now: planningNow, Workers: 8})
	test.Error(t, err)
}

func TestBuildRejectsADuplicateSource(t *testing.T) {
	t.Parallel()

	// The same rule shard.Build enforces: a plan that cannot promise to refresh a
	// source exactly once cannot be merged into a total anybody should believe.
	sources := []services.Source{
		{Platform: "greenhouse", Key: "stripe"},
		{Platform: "greenhouse", Key: "stripe"},
	}

	_, err := Build(sources, NewStore(), Options{Now: planningNow, Workers: 8})
	test.ErrorContains(t, err, "registered more than once")
}

func TestBuildRejectsARequestBudget(t *testing.T) {
	t.Parallel()

	// Specified and deliberately not implementable: services.SourceRun carries no
	// request count, and approximating a politeness budget from durations is the
	// one guess this project cannot afford.
	_, err := Build(registryOf(measuredShape()), NewStore(), Options{
		Now:     planningNow,
		Budget:  Budget{Wall: time.Minute, Requests: 1000},
		Workers: 8,
	})
	test.ErrorContains(t, err, "request budget")
}

func TestBuildRejectsABoundedPlanWithNoWorkers(t *testing.T) {
	t.Parallel()

	// The global capacity term is budget x workers x shards. Defaulting workers
	// silently would under-admit by a factor of the concurrency, which looks like
	// a scheduler that is simply bad rather than one that was misconfigured.
	_, err := Build(registryOf(measuredShape()), NewStore(), Options{
		Now:    planningNow,
		Budget: Budget{Wall: time.Minute},
	})
	test.ErrorContains(t, err, "Workers")
}

func TestDeterministicUnderInputPermutation(t *testing.T) {
	t.Parallel()

	shapes := measuredShape()
	store := seededStore(shapes, planningNow.Add(-30*time.Hour))
	registry := registryOf(shapes)

	opts := Options{
		Now:     planningNow,
		Budget:  Budget{Wall: 3 * time.Minute},
		Workers: 24,
	}

	first, err := Build(registry, store, opts)
	must.NoError(t, err)
	must.SliceNotEmpty(t, first.Items)

	// A deterministic-looking plan that depends on registry order is the classic
	// way "the retried run did not reshuffle" stops being true.
	for shuffle := range 20 {
		permuted := permute(registry, shuffle)

		got, err := Build(permuted, store, opts)
		must.NoError(t, err)

		test.Eq(t, first.PlanID, got.PlanID)
		test.Eq(t, first.Items, got.Items)
		test.Eq(t, first.Deferred, got.Deferred)
		test.Eq(t, first.Groups, got.Groups)
	}
}

// permute shuffles deterministically. A seeded LCG rather than math/rand,
// because a shuffle that differs between runs turns a determinism test into a
// flaky one and this suite exists to catch exactly that class of bug.
func permute[T any](values []T, seed int) []T {
	out := slices.Clone(values)

	state := uint64(seed)*6364136223846793005 + 1442695040888963407

	for i := len(out) - 1; i > 0; i-- {
		state = state*6364136223846793005 + 1442695040888963407
		j := int((state >> 33) % uint64(i+1))
		out[i], out[j] = out[j], out[i]
	}

	return out
}

func TestDeterministicUnderClockJitterWithinTick(t *testing.T) {
	t.Parallel()

	// Options.Now is truncated to Policy.Tick, so a workflow retried inside the
	// same hour produces the identical plan rather than reshuffling work already
	// under way.
	shapes := measuredShape()
	store := seededStore(shapes, planningNow.Add(-30*time.Hour))
	registry := registryOf(shapes)

	base := Options{
		Now:     time.Date(2026, 7, 28, 9, 0, 0, 0, time.UTC),
		Budget:  Budget{Wall: 3 * time.Minute},
		Workers: 24,
	}

	want, err := Build(registry, store, base)
	must.NoError(t, err)

	for _, drift := range []time.Duration{time.Second, 17 * time.Minute, 59*time.Minute + 59*time.Second} {
		opts := base
		opts.Now = base.Now.Add(drift)

		got, err := Build(registry, store, opts)
		must.NoError(t, err)

		test.Eq(t, want.PlanID, got.PlanID)
		test.Eq(t, want.PlannedFor, got.PlannedFor)
	}

	// And the next tick is allowed to differ; a plan frozen forever is not the
	// property being claimed.
	opts := base
	opts.Now = base.Now.Add(time.Hour)

	next, err := Build(registry, store, opts)
	must.NoError(t, err)
	test.NotEq(t, want.PlannedFor, next.PlannedFor)
}

// goldenPlanID pins the plan of a fixed fixture.
//
// It is the cross-architecture and cross-release guard. The score is integer
// arithmetic precisely so this can be exact: the Go spec permits an
// implementation to fuse a multiply-add into a single rounding and arm64 does,
// so a float score would be a plan that can legitimately differ between the four
// targets the portability job builds.
const goldenPlanID = "9e9f9184c6103341427faaac52d8b3ca"

func TestGoldenPlanID(t *testing.T) {
	t.Parallel()

	plan := goldenPlan(t)

	test.Eq(t, goldenPlanID, plan.PlanID)
}

func goldenPlan(t *testing.T) Plan {
	t.Helper()

	shapes := measuredShape()
	store := seededStore(shapes, planningNow.Add(-30*time.Hour))

	plan, err := Build(registryOf(shapes), store, Options{
		Now:     planningNow,
		Budget:  Budget{Wall: 4 * time.Minute},
		Workers: 24,
	})
	must.NoError(t, err)

	return plan
}

func TestMonotoneInBudget(t *testing.T) {
	t.Parallel()

	// More time must never take work away. A budget knob that can drop a source
	// by being raised is a knob nobody can reason about.
	shapes := measuredShape()
	store := seededStore(shapes, planningNow.Add(-30*time.Hour))
	registry := registryOf(shapes)

	var previous []SourceID

	for _, wall := range []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 30 * time.Minute} {
		plan, err := Build(registry, store, Options{Now: planningNow, Budget: Budget{Wall: wall}, Workers: 24})
		must.NoError(t, err)

		admitted := map[SourceID]bool{}
		for _, item := range plan.Items {
			admitted[item.Source] = true
		}

		for _, id := range previous {
			if !admitted[id] {
				t.Fatalf("budget %s dropped %s, which a smaller budget had admitted", wall, id)
			}
		}

		previous = previous[:0]
		for _, item := range plan.Items {
			previous = append(previous, item.Source)
		}
	}
}

func TestGroupBudgetRespected(t *testing.T) {
	t.Parallel()

	// Per group, the sum of predictions must fit budget x parallelism x fill.
	// This is the constraint that makes admission mean anything: 970 Personio
	// tenants behind one 4-slot key are ~243 sequential rounds no matter what,
	// and admitting them all would just truncate most of them.
	shapes := measuredShape()
	store := seededStore(shapes, planningNow.Add(-30*time.Hour))

	plan, err := Build(registryOf(shapes), store, Options{
		Now:     planningNow,
		Budget:  Budget{Wall: 2 * time.Minute},
		Workers: 24,
	})
	must.NoError(t, err)

	planned := map[string]int64{}
	for _, item := range plan.Items {
		planned[item.Group] += item.PredictedMS
	}

	must.MapNotEmpty(t, planned)

	for _, group := range plan.Groups {
		test.LessEq(t, group.CapacityMS, planned[group.Key],
			test.Sprintf("group %s planned %d ms against a capacity of %d", group.Key, planned[group.Key], group.CapacityMS))
		test.Eq(t, planned[group.Key], group.PlannedMS)

		// The measured 07/28 capacity, restated: 2 min x 4 slots x 90%.
		want := (2 * time.Minute).Milliseconds() * int64(group.ParallelismMilli) / 1000 * int64(DefaultFill) / 100
		test.Eq(t, want, group.CapacityMS)
	}

	test.LessEq(t, plan.GlobalCapacityMS, plan.PlannedMS)
}

func TestGlobalBudgetBindsWhenWorkersAreFew(t *testing.T) {
	t.Parallel()

	// Sharding raises the global term and leaves every per-group term untouched,
	// which is "sharding buys latency, not budget" expressed as arithmetic.
	shapes := measuredShape()
	store := seededStore(shapes, planningNow.Add(-30*time.Hour))
	registry := registryOf(shapes)

	one, err := Build(registry, store, Options{Now: planningNow, Budget: Budget{Wall: time.Minute}, Workers: 2, Shards: 1})
	must.NoError(t, err)

	four, err := Build(registry, store, Options{Now: planningNow, Budget: Budget{Wall: time.Minute}, Workers: 2, Shards: 4})
	must.NoError(t, err)

	test.Eq(t, one.GlobalCapacityMS*4, four.GlobalCapacityMS)
	test.Less(t, len(four.Items), len(one.Items))

	// Per-group capacity is identical: more runners are not permission to press
	// a shared backend harder.
	byKey := map[string]GroupBudget{}
	for _, group := range one.Groups {
		byKey[group.Key] = group
	}

	for _, group := range four.Groups {
		test.Eq(t, byKey[group.Key].CapacityMS, group.CapacityMS)
	}

	var reason bool

	for _, deferral := range one.Deferred {
		if deferral.Reason == ReasonGlobalBudget {
			reason = true
		}
	}

	test.True(t, reason, test.Sprint("a two-worker minute should defer something for global budget"))
}

func TestEveryUnplannedSourceIsExplained(t *testing.T) {
	t.Parallel()

	// A plan that silently omits work is indistinguishable from one that lost it.
	shapes := measuredShape()
	store := seededStore(shapes, planningNow.Add(-30*time.Hour))
	registry := registryOf(shapes)

	plan, err := Build(registry, store, Options{Now: planningNow, Budget: Budget{Wall: 90 * time.Second}, Workers: 24})
	must.NoError(t, err)

	accounted := map[SourceID]bool{}
	for _, item := range plan.Items {
		accounted[item.Source] = true
	}

	for _, deferral := range plan.Deferred {
		must.False(t, accounted[deferral.Source], must.Sprintf("%s is both planned and deferred", deferral.Source))
		test.StrNotEqFold(t, "", deferral.Reason)

		accounted[deferral.Source] = true
	}

	test.Eq(t, len(registry), len(accounted))
	test.True(t, slices.IsSortedFunc(plan.Deferred, func(a, b Deferral) int { return compareIDs(a.Source, b.Source) }))
}

func TestUnknownSourcesAreOptimisticInValueAndHonestInCost(t *testing.T) {
	t.Parallel()

	// A new source must be tried rather than starved, so its value is the
	// platform's 75th percentile and its staleness is the cap. Its cost is the
	// platform median and never optimistic: optimism in cost is a lie to the
	// budget that converts admitted work into truncated sources, which cost their
	// full duration and refresh nothing.
	store := NewStore()
	store.PutSource(SourceState{Platform: "greenhouse", Key: "known-a", DurationMS: []int32{300}, Postings: []int32{10}, LastSuccess: formatStamp(planningNow)})
	store.PutSource(SourceState{Platform: "greenhouse", Key: "known-b", DurationMS: []int32{500}, Postings: []int32{90}, LastSuccess: formatStamp(planningNow)})

	registry := []services.Source{
		{Platform: "greenhouse", Key: "known-a"},
		{Platform: "greenhouse", Key: "known-b"},
		{Platform: "greenhouse", Key: "brand-new"},
	}

	plan, err := Build(registry, store, Options{Now: planningNow, Budget: Budget{Wall: 10 * time.Minute}, Workers: 8})
	must.NoError(t, err)

	fresh, ok := plan.Lookup(SourceID{Platform: "greenhouse", Key: "brand-new"})
	must.True(t, ok)

	test.Eq(t, MaxStaleMilli, fresh.StaleMilli)
	test.Eq(t, LaneAging, fresh.Lane)

	// Platform median of {300, 500}; the p75 yield of {10, 90} is 90.
	test.Eq(t, int64(400), fresh.PredictedMS)
	test.Eq(t, int64(90)*int64(MaxStaleMilli)*1000/400, fresh.Score)
}

func TestBackoffDefersARepeatedlyFailingSource(t *testing.T) {
	t.Parallel()

	// Timed from LastAttempt, not LastSuccess. A permanently failing source has
	// no LastSuccess, so an interval measured from it grows without bound and the
	// gate stops holding at all.
	store := NewStore()
	store.PutSource(SourceState{
		Platform:            "greenhouse",
		Key:                 "dead",
		LastAttempt:         formatStamp(planningNow.Add(-30 * time.Hour)),
		ConsecutiveFailures: 3,
		ErrorClass:          "timeout",
	})

	registry := []services.Source{{Platform: "greenhouse", Key: "dead"}}

	plan, err := Build(registry, store, Options{Now: planningNow, Budget: Budget{Wall: time.Hour}, Workers: 8})
	must.NoError(t, err)

	// 2^3 targets is 192 hours; 30 hours is not enough.
	test.SliceEmpty(t, plan.Items)
	must.SliceLen(t, 1, plan.Deferred)
	test.Eq(t, ReasonBackoff, plan.Deferred[0].Reason)

	// Past the interval it comes back. Back-off must not silently become
	// retirement: a board that 503s for a month and then returns has to be
	// noticed.
	later, err := Build(registry, store, Options{Now: planningNow.Add(200 * time.Hour), Budget: Budget{Wall: time.Hour}, Workers: 8})
	must.NoError(t, err)
	test.SliceLen(t, 1, later.Items)
}

func TestUnboundedPlanSkipsFreshSourcesOnly(t *testing.T) {
	t.Parallel()

	// The daemon's mode: same code, no second scheduler. Capacity is infinite and
	// everything at least one target stale is admitted, in the same order.
	store := NewStore()
	store.PutSource(SourceState{Platform: "greenhouse", Key: "fresh", LastSuccess: formatStamp(planningNow.Add(-time.Hour)), DurationMS: []int32{300}, Postings: []int32{10}})
	store.PutSource(SourceState{Platform: "greenhouse", Key: "stale", LastSuccess: formatStamp(planningNow.Add(-40 * time.Hour)), DurationMS: []int32{300}, Postings: []int32{10}})

	registry := []services.Source{
		{Platform: "greenhouse", Key: "fresh"},
		{Platform: "greenhouse", Key: "stale"},
	}

	plan, err := Build(registry, store, Options{Now: planningNow, Budget: Budget{Unbounded: true}})
	must.NoError(t, err)

	must.SliceLen(t, 1, plan.Items)
	test.Eq(t, "stale", plan.Items[0].Source.Key)
	must.SliceLen(t, 1, plan.Deferred)
	test.Eq(t, ReasonFresh, plan.Deferred[0].Reason)
}

func TestNoWallBudgetOrdersWithoutSelecting(t *testing.T) {
	t.Parallel()

	// Migration step 2 of docs/crawl-budget-model.md: staleness-first ordering,
	// still attempting every source. Immediately useful, trivially revertible,
	// and it changes no output.
	shapes := measuredShape()
	registry := registryOf(shapes)
	store := seededStore(shapes, planningNow.Add(-30*time.Hour))

	plan, err := Build(registry, store, Options{Now: planningNow})
	must.NoError(t, err)

	test.SliceLen(t, len(registry), plan.Items)
	test.SliceEmpty(t, plan.Deferred)

	// Empty, not absent: a serialised plan must say "nothing was deferred"
	// rather than leave a reader to guess whether the list went missing.
	test.True(t, plan.Deferred != nil)
	test.True(t, plan.Items != nil)
}

func TestEmitOrderInterleavesGroups(t *testing.T) {
	t.Parallel()

	// A worker parked on a semaphore is occupied, not idle, so 400 Personio
	// sources at the head of the list park most of the pool on one 4-slot key.
	// The opening wave must touch every backend.
	shapes := measuredShape()
	store := seededStore(shapes, planningNow.Add(-30*time.Hour))

	plan, err := Build(registryOf(shapes), store, Options{
		Now:     planningNow,
		Budget:  Budget{Wall: 10 * time.Minute},
		Workers: 24,
	})
	must.NoError(t, err)
	must.SliceNotEmpty(t, plan.Items)

	groups := map[string]bool{}
	for _, group := range plan.Groups {
		if group.Sources > 0 {
			groups[group.Key] = true
		}
	}

	opening := map[string]bool{}
	for _, item := range plan.Items[:len(groups)] {
		opening[item.Group] = true
	}

	test.Eq(t, len(groups), len(opening), test.Sprintf("the first %d items should cover every planned group, got %v", len(groups), opening))
}

func TestAgingLaneLeadsItsGroupInEmitOrder(t *testing.T) {
	t.Parallel()

	// Aging items win dispatch order as well as admission. Without that a
	// truncated run never reaches the tail of any group's order, and whatever
	// sits there is starved for as long as the truncation fraction holds.
	store := NewStore()

	var registry []services.Source

	for i := range 40 {
		key := fmt.Sprintf("s%02d", i)
		registry = append(registry, services.Source{Platform: "greenhouse", Key: key})

		// Half fresh (value lane), half four days stale (aging lane).
		success := planningNow.Add(-time.Hour)
		if i%2 == 0 {
			success = planningNow.Add(-96 * time.Hour)
		}

		store.PutSource(SourceState{
			Platform:    "greenhouse",
			Key:         key,
			LastAttempt: formatStamp(success),
			LastSuccess: formatStamp(success),
			DurationMS:  []int32{int32(100 + i*10)},
			Postings:    []int32{10},
		})
	}

	plan, err := Build(registry, store, Options{Now: planningNow, Budget: Budget{Wall: 10 * time.Minute}, Workers: 8})
	must.NoError(t, err)

	sawValue := false

	for _, item := range plan.Items {
		if item.Lane == LaneValue {
			sawValue = true

			continue
		}

		must.False(t, sawValue, must.Sprintf("aging item %s ranked %d after a value item", item.Source, item.Rank))
	}

	test.True(t, sawValue)
}

func TestValueLaneIsLongestProcessingTimeFirst(t *testing.T) {
	t.Parallel()

	// LPT within a group minimises that group's makespan. It applies to the value
	// lane only: the aging lane keeps FIFO order, because FIFO is the one
	// intra-group order that provably rotates when a run is truncated.
	store := NewStore()

	var registry []services.Source

	for i := range 20 {
		key := fmt.Sprintf("s%02d", i)
		registry = append(registry, services.Source{Platform: "greenhouse", Key: key})
		store.PutSource(SourceState{
			Platform:    "greenhouse",
			Key:         key,
			LastAttempt: formatStamp(planningNow.Add(-time.Hour)),
			LastSuccess: formatStamp(planningNow.Add(-time.Hour)),
			DurationMS:  []int32{int32(100 + i*10)},
			Postings:    []int32{10},
		})
	}

	plan, err := Build(registry, store, Options{Now: planningNow, Budget: Budget{Wall: 10 * time.Minute}, Workers: 8})
	must.NoError(t, err)
	must.SliceLen(t, 20, plan.Items)

	for i := 1; i < len(plan.Items); i++ {
		test.LessEq(t, plan.Items[i-1].PredictedMS, plan.Items[i].PredictedMS)
	}
}

func TestPlanComposesWithShardBuild(t *testing.T) {
	t.Parallel()

	// Schedule first, shard second, over one shared notion of affinity. The plan
	// must be one artifact for the merge's coverage proof, so scheduling inside
	// each shard is not an option.
	shapes := measuredShape()
	registry := registryOf(shapes)
	store := seededStore(shapes, planningNow.Add(-30*time.Hour))

	plan, err := Build(registry, store, Options{
		Now:     planningNow,
		Budget:  Budget{Wall: 5 * time.Minute},
		Workers: 24,
		Shards:  4,
	})
	must.NoError(t, err)

	selected, err := plan.Sources(registry)
	must.NoError(t, err)
	test.SliceLen(t, len(plan.Items), selected)

	shardPlan, err := shard.Build(selected, shard.Options{
		ShardCount:  4,
		Costs:       plan.Costs(),
		SourceSetID: shard.SourceSetID(registry),
	})
	must.NoError(t, err)
	must.NoError(t, shardPlan.Validate())

	test.Eq(t, shard.CostModelDuration, shardPlan.CostModel)
	test.Eq(t, len(plan.Items), shardPlan.SourceCount)

	// An affinity group lives in exactly one shard by construction, which is why
	// the per-group budget needs no adjustment and the composition is clean.
	owner := map[string]int{}

	for _, s := range shardPlan.Shards {
		for _, key := range s.AffinityKeys {
			if other, ok := owner[key]; ok {
				t.Fatalf("affinity key %s is in shards %d and %d", key, other, s.Index)
			}

			owner[key] = s.Index
		}
	}
}

func TestSourcesFailsClosedOnAnUnresolvableRef(t *testing.T) {
	t.Parallel()

	registry := []services.Source{{Platform: "greenhouse", Key: "stripe"}}

	plan, err := Build(registry, NewStore(), Options{Now: planningNow, Budget: Budget{Wall: time.Minute}, Workers: 4})
	must.NoError(t, err)

	_, err = plan.Sources(nil)
	test.ErrorContains(t, err, "not registered in this binary")
}

func TestOversizeSourcesAreNamedRatherThanSilentlyStarved(t *testing.T) {
	t.Parallel()

	// The one hole in the fairness guarantee, made visible. A source predicted to
	// cost more than its group's whole per-run capacity cannot be taken by any
	// admission pass, and the honest thing is to say which constraint is
	// impossible rather than report a budget reason that implies it might fit
	// next time. Admitting it anyway would blow the budget and then be declined
	// at dispatch, which advances nothing and starves it just the same, only
	// invisibly.
	store := NewStore()
	store.PutSource(SourceState{Platform: "workday", Key: "huge", DurationMS: []int32{20 * 60 * 1000}})
	store.PutSource(SourceState{Platform: "workday", Key: "normal", DurationMS: []int32{5_000}})

	registry := []services.Source{
		{Platform: "workday", Key: "huge"},
		{Platform: "workday", Key: "normal"},
	}

	// One minute x 4 slots x 90% is 216 seconds of group capacity; 20 minutes
	// does not fit and never will at this budget.
	plan, err := Build(registry, store, Options{Now: planningNow, Budget: Budget{Wall: time.Minute}, Workers: 32})
	must.NoError(t, err)

	must.SliceLen(t, 1, plan.Items)
	test.Eq(t, "normal", plan.Items[0].Source.Key)

	must.SliceLen(t, 1, plan.Deferred)
	test.Eq(t, "huge", plan.Deferred[0].Source.Key)
	test.Eq(t, ReasonOversize, plan.Deferred[0].Reason)

	// A budget large enough takes it, so the reason really is the budget and not
	// a permanent exclusion.
	roomy, err := Build(registry, store, Options{Now: planningNow, Budget: Budget{Wall: 10 * time.Minute}, Workers: 32})
	must.NoError(t, err)
	test.SliceLen(t, 2, roomy.Items)
}

func TestLaneMarshalsByName(t *testing.T) {
	t.Parallel()

	// A dumped plan should say which queue chose each source rather than making a
	// reader map 0 and 1 back to a policy.
	encoded, err := json.Marshal(Item{Source: SourceID{Platform: "greenhouse", Key: "s"}, Lane: LaneAging})
	must.NoError(t, err)
	test.StrContains(t, string(encoded), `"lane":"aging"`)

	var back Item
	must.NoError(t, json.Unmarshal(encoded, &back))
	test.Eq(t, LaneAging, back.Lane)

	value, err := json.Marshal(Item{Lane: LaneValue})
	must.NoError(t, err)
	test.StrContains(t, string(value), `"lane":"value"`)
}
