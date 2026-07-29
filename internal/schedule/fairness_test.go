package schedule

import (
	"testing"
	"time"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// The fairness property this package guarantees, stated precisely:
//
//	Bounded-delay progress. Let s be enabled (not retired, not in back-off) in
//	affinity group G, and let B_G = budget x parallelism(G) x fill x ObligeShare
//	be G's per-run aging capacity. Once stale(s) crosses ObligeAt, s joins a
//	queue ordered by LastAttempt. A source's LastAttempt advances only when it
//	actually runs, so no already-queued source can move ahead of s, and each run
//	drains a prefix of that queue costing at least B_G (or the whole queue).
//	Therefore s is attempted within ceil((W_s + N) / B_G) runs, where W_s is the
//	predicted cost of the queue ahead of it on entry and N is the cost of sources
//	registered since.
//
// Three honesties about that. It bounds delay, not staleness: if a group's
// total demand exceeds its per-run capacity, some source must miss its freshness
// target and the guarantee degrades to a rotation period. It depends on
// never-attempted sources being finite — a registry that grows without bound
// starves everything, which is a registry problem the scheduler correctly
// refuses to hide. And it excludes a source whose own predicted cost exceeds its
// group's whole per-run capacity, which no pass can take; those are reported as
// ReasonOversize rather than quietly dropped, because the fix is a bigger budget
// or a cheaper adapter and neither is something a scheduler can do.
//
// Untestable scheduling is how starvation bugs survive, so these are properties
// over many simulated runs rather than assertions about one plan.

// fairPolicy shortens the freshness target so a hundred simulated hours cover
// many refresh cycles. Everything else is the shipped default.
func fairPolicy() Policy {
	return Policy{Target: 2 * time.Hour, Tick: time.Hour}
}

func TestNoStarvation(t *testing.T) {
	t.Parallel()

	// A budget that admits only a few percent of the registry per run. Every
	// source must still be visited, including the ones on the platform that costs
	// 45x more per posting than the cheapest.
	shapes := measuredShape()

	l := newLoop(t, shapes, Options{
		Budget:  Budget{Wall: 20 * time.Second},
		Policy:  fairPolicy(),
		Workers: 24,
	}, execution{slots: slotsOf(shapes)}, nil)

	l.run(t, 200, time.Hour)

	// The budget really was tight: a run that admitted everything would prove
	// nothing about fairness.
	test.Less(t, len(l.registry)/2, len(l.plans[0].Items),
		test.Sprintf("run 0 admitted %d of %d sources, which is not a tight budget", len(l.plans[0].Items), len(l.registry)))

	missing := l.neverRun()
	if len(missing) > 0 {
		t.Fatalf("%d of %d sources were never crawled in 200 runs, worst offenders: %v",
			len(missing), len(l.registry), missing[:min(len(missing), 10)])
	}

	// And the expensive platform is rotating rather than merely being touched
	// once. Ranking purely by value density abandons exactly this platform.
	perPlatform := map[string]int{}
	for id, count := range l.attempts {
		if perPlatform[id.Platform] == 0 || count < perPlatform[id.Platform] {
			perPlatform[id.Platform] = count
		}
	}

	for platform, fewest := range perPlatform {
		test.Greater(t, 2, fewest, test.Sprintf("%s: least-visited source ran only %d times in 200 runs", platform, fewest))
	}
}

func TestDeferredSourcesDoNotAge(t *testing.T) {
	t.Parallel()

	// The executor completes only the first half of every plan, which is what a
	// dispatch gate declining work looks like from the scheduler's side.
	//
	// This is the test that found the real bug. Longest-processing-time emit
	// order puts the cheapest sources at the tail of every group, so a run
	// truncated at the same fraction each time never reaches them — and if their
	// intra-lane order does not rotate, they are starved forever. It rotates
	// because a source the gate declined does not advance LastAttempt, and the
	// aging lane is ordered by LastAttempt.
	shapes := measuredShape()

	l := newLoop(t, shapes, Options{
		Budget:  Budget{Wall: 30 * time.Second},
		Policy:  fairPolicy(),
		Workers: 24,
	}, execution{slots: slotsOf(shapes)}, nil)

	// Half of the first plan, held fixed, so the truncation fraction is exactly
	// the adversarial one.
	first, err := Build(l.registry, l.store, Options{
		Now:     l.now,
		Budget:  l.opts.Budget,
		Policy:  l.opts.Policy,
		Workers: l.opts.Workers,
	})
	must.NoError(t, err)

	l.exec.completeRanks = len(first.Items) / 2
	must.Greater(t, 0, l.exec.completeRanks)

	l.run(t, 100, time.Hour)

	missing := l.neverRun()
	if len(missing) > 0 {
		t.Fatalf("%d sources never ran in 100 half-completed runs, e.g. %v", len(missing), missing[:min(len(missing), 10)])
	}
}

func TestDeferralDoesNotAdvanceTheAgingClock(t *testing.T) {
	t.Parallel()

	// The mechanism behind the property above, asserted directly rather than
	// inferred from 100 runs.
	shapes := []platformShape{{platform: "greenhouse", sources: 20, cost: time.Second, postings: 10, slots: 4}}

	l := newLoop(t, shapes, Options{
		Budget:  Budget{Wall: 10 * time.Second},
		Policy:  fairPolicy(),
		Workers: 8,
	}, execution{slots: slotsOf(shapes), completeRanks: 3}, nil)

	l.run(t, 1, time.Hour)

	ran, deferred := 0, 0

	for _, source := range l.registry {
		id := SourceID{Platform: source.Platform, Key: source.Key}

		state, ok := l.store.Source(id)
		if !ok || state.LastAttempt == "" {
			deferred++

			continue
		}

		ran++
	}

	test.Eq(t, 3, ran)
	test.Eq(t, len(l.registry)-3, deferred)
}

func TestBackoffBoundsAttempts(t *testing.T) {
	t.Parallel()

	// A permanently failing source must be neither hammered nor retired. With a
	// two-hour target and a run every hour, exponential back-off from LastAttempt
	// gives attempts at roughly runs 0, 2, 6, 14, 30 and 62.
	//
	// Timed from LastSuccess instead, a source that never succeeds has an age
	// that grows without bound, every interval eventually clears, and it is
	// attempted on nearly every run forever.
	shapes := []platformShape{{platform: "greenhouse", sources: 20, cost: 100 * time.Millisecond, postings: 10, slots: 4}}

	dead := SourceID{Platform: "greenhouse", Key: "greenhouse-0007"}

	l := newLoop(t, shapes, Options{
		Budget:  Budget{Wall: time.Minute},
		Policy:  fairPolicy(),
		Workers: 8,
	}, execution{slots: slotsOf(shapes)}, map[SourceID]bool{dead: true})

	l.run(t, 90, time.Hour)

	attempts := l.attempts[dead]

	test.GreaterEq(t, 3, attempts, test.Sprintf("a dead source attempted %d times in 90 runs is retirement, not back-off", attempts))
	test.LessEq(t, 12, attempts, test.Sprintf("a dead source attempted %d times in 90 runs is being hammered", attempts))

	// The healthy sources beside it were not affected.
	healthy := l.attempts[SourceID{Platform: "greenhouse", Key: "greenhouse-0000"}]
	test.GreaterEq(t, 40, healthy)

	state, ok := l.store.Source(dead)
	must.True(t, ok)
	test.Eq(t, int32(attempts), state.ConsecutiveFailures)
	test.Eq(t, "", state.LastSuccess)
}

func TestSlowPlatformIsNotStarvedByCheapOnes(t *testing.T) {
	t.Parallel()

	// The measured motivation, in miniature: Greenhouse is 65 postings for 0.3 s
	// and Personio is 12 postings for 2.5 s, so at equal staleness Greenhouse
	// scores about 45x higher. Ranking purely by that density refreshes more
	// postings per run and abandons three quarters of Personio permanently. The
	// aging lane's reserved share is what buys the difference, and this asserts
	// the trade is actually being made.
	shapes := measuredShape()

	l := newLoop(t, shapes, Options{
		Budget:  Budget{Wall: 15 * time.Second},
		Policy:  fairPolicy(),
		Workers: 24,
	}, execution{slots: slotsOf(shapes)}, nil)

	l.run(t, 60, time.Hour)

	byPlatform := map[string]int{}
	for id, count := range l.attempts {
		byPlatform[id.Platform] += count
	}

	for _, shape := range shapes {
		test.Greater(t, 0, byPlatform[shape.platform], test.Sprintf("%s never ran at all", shape.platform))
	}

	// Every source of the expensive SMB platforms was refreshed, not just the
	// cheap high-yield ones.
	for _, source := range l.registry {
		id := SourceID{Platform: source.Platform, Key: source.Key}
		must.Greater(t, 0, l.attempts[id], must.Sprintf("%s never ran in 60 runs", id))
	}
}

func TestPlanIsStableAcrossARetriedRun(t *testing.T) {
	t.Parallel()

	// A retried workflow run inside the same tick must not reshuffle work already
	// under way — the reason shard.Build is deterministic, applied to the layer
	// above it.
	shapes := measuredShape()

	l := newLoop(t, shapes, Options{
		Budget:  Budget{Wall: 20 * time.Second},
		Policy:  fairPolicy(),
		Workers: 24,
	}, execution{slots: slotsOf(shapes)}, nil)

	l.run(t, 5, time.Hour)

	opts := l.opts
	opts.Now = l.now

	first, err := Build(l.registry, l.store, opts)
	must.NoError(t, err)

	opts.Now = l.now.Add(41 * time.Minute)

	retry, err := Build(l.registry, l.store, opts)
	must.NoError(t, err)

	test.Eq(t, first.PlanID, retry.PlanID)
	test.Eq(t, first.Items, retry.Items)
}

func TestConvergesOnMeasuredCostAfterAColdStart(t *testing.T) {
	t.Parallel()

	// The first run after state loss is a calibration run: with no state at all
	// even the platform fallbacks are empty, so everything is charged the same
	// default and admission over-commits. That is acceptable because it is also
	// exactly what every run before this package did. What matters is that it
	// converges.
	shapes := measuredShape()

	l := newLoop(t, shapes, Options{
		Budget:  Budget{Wall: 30 * time.Second},
		Policy:  fairPolicy(),
		Workers: 24,
	}, execution{slots: slotsOf(shapes)}, nil)

	l.run(t, 4, time.Hour)

	oracle := oracleOf(shapes, nil)

	opts := l.opts
	opts.Now = l.now

	plan, err := Build(l.registry, l.store, opts)
	must.NoError(t, err)
	must.SliceNotEmpty(t, plan.Items)

	measured := 0

	for _, item := range plan.Items {
		state, ok := l.store.Source(item.Source)
		if !ok || len(state.DurationMS) == 0 {
			// Still unmeasured, so still charged its platform's median. Honest,
			// and the reason a bounded run's first pass over a new platform is a
			// calibration pass.
			continue
		}

		measured++

		want, _, _ := oracle(item.Source)
		test.Eq(t, want.Milliseconds(), item.PredictedMS,
			test.Sprintf("%s predicted %d ms against a true cost of %d", item.Source, item.PredictedMS, want.Milliseconds()))
	}

	test.Greater(t, len(plan.Items)/2, measured,
		test.Sprintf("only %d of %d planned sources had a measured cost after four runs", measured, len(plan.Items)))

	// And the group's real four-slot concurrency has been learned from manifests
	// rather than assumed.
	group, ok := l.store.Group("platform:personio")
	must.True(t, ok)
	test.GreaterEq(t, int32(3000), group.ParallelismMilli)
}

func TestFairnessLaneCostsPostingsAndBuysCoverage(t *testing.T) {
	t.Parallel()

	// The trade, measured rather than asserted. Ranking purely by value density
	// refreshes more postings per run and abandons the expensive platforms
	// entirely; the aging lane's reserved share buys coverage back and the
	// difference is the price paid knowingly.
	//
	// The counterfactual is expressed by raising ObligeAt above the staleness cap
	// so no source can ever enter the aging lane. That is a policy setting, not a
	// second code path, which is the point: there is one scheduler.
	// Few workers relative to the registry, so the global capacity term binds.
	// That is the regime where the two lanes actually differ: per-group capacity
	// alone already stops a cheap backend from eating an expensive one's slots,
	// and it is only when the run's own worker-seconds run out that value
	// ranking gets to abandon a whole platform.
	shapes := measuredShape()

	both := newLoop(t, shapes, Options{
		Budget:  Budget{Wall: time.Minute},
		Policy:  fairPolicy(),
		Workers: 4,
	}, execution{slots: slotsOf(shapes)}, nil)
	both.run(t, 60, time.Hour)

	valueOnly := fairPolicy()
	valueOnly.ObligeAt = MaxStaleMilli + 1

	density := newLoop(t, shapes, Options{
		Budget:  Budget{Wall: time.Minute},
		Policy:  valueOnly,
		Workers: 4,
	}, execution{slots: slotsOf(shapes)}, nil)
	density.run(t, 60, time.Hour)

	t.Logf("two lanes:  %d postings refreshed over 60 runs; refreshes per source %v", both.postings, both.refreshesPerPlatform())
	t.Logf("value only: %d postings refreshed over 60 runs; refreshes per source %v", density.postings, density.refreshesPerPlatform())

	// Coverage: every source rotates, on every platform, including the two that
	// cost the most per posting.
	test.SliceEmpty(t, both.neverRun())

	for platform, span := range both.refreshesPerPlatform() {
		test.Greater(t, 2, span.min, test.Sprintf("%s: least-refreshed source ran %d times in 60 runs", platform, span.min))
	}

	// Density: pure value ranking refreshes the cheap high-yield platforms tens
	// of times and abandons the expensive ones after the cold-start run that
	// measured them. The failure is total and silent rather than gradual, which
	// is what makes it worth a policy rather than a warning.
	abandoned := 0

	for _, platform := range []string{"personio", "teamtailor"} {
		if density.refreshesPerPlatform()[platform].max <= 1 {
			abandoned++
		}
	}

	test.Eq(t, 2, abandoned,
		test.Sprint("pure value ranking should abandon the expensive platforms; if it no longer does, the global budget is not binding and this test proves nothing"))

	// And it really is ahead on the metric it optimises, which is exactly why the
	// trade has to be made deliberately rather than discovered.
	test.Greater(t, both.postings, density.postings)
}

func TestColdStartIsACalibrationRun(t *testing.T) {
	t.Parallel()

	// With no state at all even the platform fallbacks are empty, so every source
	// is charged the same default and admission over-commits. The first run after
	// state loss is a calibration run — acceptable because it is also exactly
	// what every run before this package did. What must not happen is that it
	// stays that way.
	shapes := measuredShape()

	l := newLoop(t, shapes, Options{
		Budget:  Budget{Wall: 30 * time.Second},
		Policy:  fairPolicy(),
		Workers: 24,
	}, execution{slots: slotsOf(shapes)}, nil)

	l.run(t, 6, time.Hour)

	oracle := oracleOf(shapes, nil)

	ratio := func(plan Plan) float64 {
		var predicted, actual int64

		for _, item := range plan.Items {
			cost, _, _ := oracle(item.Source)
			predicted += item.PredictedMS
			actual += cost.Milliseconds()
		}

		if predicted == 0 {
			return 0
		}

		return float64(actual) / float64(predicted)
	}

	first := ratio(l.plans[0])
	last := ratio(l.plans[len(l.plans)-1])

	t.Logf("cold start under-charged the budget by %.2fx on run 0; by run %d the error is %.4fx",
		first, len(l.plans)-1, last)

	test.Greater(t, 1.5, first, test.Sprint("run 0 should visibly mis-predict, or this test is not measuring cold start"))
	test.InDelta(t, 1.0, last, 0.02)
}
