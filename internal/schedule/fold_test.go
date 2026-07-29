package schedule

import (
	"testing"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/services"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/shard"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

var foldNow = time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)

func manifestOf(runs ...services.SourceRun) shard.Manifest {
	return shard.NewManifest(foldNow, foldNow.Add(time.Minute), time.Minute, shard.StatusComplete, 0, 0, runs)
}

func sourceRun(platform, key, status string, durationMS int64, postings int) services.SourceRun {
	return services.SourceRun{
		Platform:   platform,
		Key:        key,
		Company:    key,
		Status:     status,
		StartedAt:  foldNow,
		FinishedAt: foldNow.Add(time.Duration(durationMS) * time.Millisecond),
		DurationMS: durationMS,
		Postings:   postings,
	}
}

func TestFoldRecordsACompleteRun(t *testing.T) {
	t.Parallel()

	next := Fold(NewStore(), manifestOf(sourceRun("greenhouse", "stripe", statusComplete, 300, 65)), foldNow)

	state, ok := next.Source(SourceID{Platform: "greenhouse", Key: "stripe"})
	must.True(t, ok)

	test.Eq(t, formatStamp(foldNow), state.LastAttempt)
	test.Eq(t, formatStamp(foldNow), state.LastSuccess)
	test.Eq(t, []int32{300}, state.DurationMS)
	test.Eq(t, []int32{65}, state.Postings)
	test.Eq(t, int32(0), state.ConsecutiveFailures)
}

func TestFoldByStatus(t *testing.T) {
	t.Parallel()

	// The three rules worth their own sentences:
	//
	// Our impatience is not the board's fault, so truncated and stopped never
	// increment the failure count — putting a slow-but-healthy source into
	// exponential back-off because of a scheduling decision we made is how the
	// expensive tail of a run becomes a permanent blind spot.
	//
	// Their durations still count, exactly as internal/shard/cost.go decides:
	// they are lower bounds, and treating a lower bound as no information is how
	// the most expensive sources end up looking cheap.
	//
	// A failed source's postings are never pushed, because a zero from an outage
	// would drag the yield median down and demote the source for a week.
	before := NewStore()
	before.PutSource(SourceState{
		Platform:            "greenhouse",
		Key:                 "s",
		DurationMS:          []int32{100},
		Postings:            []int32{50},
		ConsecutiveFailures: 2,
	})

	cases := map[string]struct {
		status          string
		wantSuccess     bool
		wantDurations   []int32
		wantPostings    []int32
		wantConsecutive int32
	}{
		"complete":  {statusComplete, true, []int32{100, 400}, []int32{50, 7}, 0},
		"failed":    {statusFailed, false, []int32{100, 400}, []int32{50}, 3},
		"truncated": {statusTruncated, false, []int32{100, 400}, []int32{50}, 2},
		"stopped":   {statusStopped, false, []int32{100, 400}, []int32{50}, 2},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			next := Fold(before, manifestOf(sourceRun("greenhouse", "s", tc.status, 400, 7)), foldNow)

			state, ok := next.Source(SourceID{Platform: "greenhouse", Key: "s"})
			must.True(t, ok)

			test.Eq(t, formatStamp(foldNow), state.LastAttempt)
			test.Eq(t, tc.wantSuccess, state.LastSuccess != "")
			test.Eq(t, tc.wantDurations, state.DurationMS)
			test.Eq(t, tc.wantPostings, state.Postings)
			test.Eq(t, tc.wantConsecutive, state.ConsecutiveFailures)
		})
	}
}

func TestFoldIgnoresRunsThatNeverRan(t *testing.T) {
	t.Parallel()

	// deferred changes nothing at all — not even LastAttempt. Advancing the aging
	// clock of a source that never ran is unbounded starvation dressed as
	// fairness, and it is the reason deferred must also stay out of
	// shard/cost.go's cost samples: a zero duration would make deferred sources
	// look free and re-admit them forever.
	before := NewStore()
	before.PutSource(SourceState{Platform: "greenhouse", Key: "s", LastAttempt: "2026-07-01T00:00:00Z"})

	next := Fold(before, manifestOf(
		services.SourceRun{Platform: "greenhouse", Key: "s", Status: StatusDeferred},
		services.SourceRun{Platform: "greenhouse", Key: "p", Status: "planned"},
		services.SourceRun{Platform: "greenhouse", Key: "r", Status: "running"},
	), foldNow)

	state, ok := next.Source(SourceID{Platform: "greenhouse", Key: "s"})
	must.True(t, ok)
	test.Eq(t, "2026-07-01T00:00:00Z", state.LastAttempt)

	_, planned := next.Source(SourceID{Platform: "greenhouse", Key: "p"})
	test.False(t, planned)

	_, running := next.Source(SourceID{Platform: "greenhouse", Key: "r"})
	test.False(t, running)
}

func TestFoldDoesNotMutateItsInput(t *testing.T) {
	t.Parallel()

	before := NewStore()
	before.PutSource(SourceState{Platform: "greenhouse", Key: "s", DurationMS: []int32{100}})

	_ = Fold(before, manifestOf(sourceRun("greenhouse", "s", statusComplete, 400, 7)), foldNow)

	state, _ := before.Source(SourceID{Platform: "greenhouse", Key: "s"})
	test.Eq(t, []int32{100}, state.DurationMS)
	test.Eq(t, "", state.LastAttempt)
}

func TestFoldCapsTrailingSamples(t *testing.T) {
	t.Parallel()

	store := NewStore()

	for i := range 12 {
		store = Fold(store, manifestOf(sourceRun("greenhouse", "s", statusComplete, int64(100+i), i)), foldNow)
	}

	state, ok := store.Source(SourceID{Platform: "greenhouse", Key: "s"})
	must.True(t, ok)

	// Oldest first, newest last, a week deep.
	test.SliceLen(t, MaxSamples, state.DurationMS)
	test.Eq(t, int32(105), state.DurationMS[0])
	test.Eq(t, int32(111), state.DurationMS[MaxSamples-1])
}

func TestFoldClampsAnAnomalousDuration(t *testing.T) {
	t.Parallel()

	// internal/shard's bounds, unchanged. The 07/26 semaphore-leak run truncated
	// ~216 Workday tenants at two minutes each; one run like that must not
	// dictate every future plan, and an unclamped value would also overflow the
	// int32 the file stores.
	next := Fold(NewStore(), manifestOf(sourceRun("workday", "t", statusTruncated, 10*60*60*1000, 0)), foldNow)

	state, _ := next.Source(SourceID{Platform: "workday", Key: "t"})
	test.Eq(t, []int32{int32(shard.MaxSourceCostMS)}, state.DurationMS)
}

func TestFoldGroupsMeasuresParallelism(t *testing.T) {
	t.Parallel()

	// parallelism = busy time / span, which is mathematically a lower bound on
	// the concurrency actually achieved and therefore can never over-estimate.
	// Four sources of one second each inside a one-second span is four slots.
	registry := []services.Source{
		{Platform: "greenhouse", Key: "a"},
		{Platform: "greenhouse", Key: "b"},
		{Platform: "greenhouse", Key: "c"},
		{Platform: "greenhouse", Key: "d"},
	}

	var runs []services.SourceRun

	for _, source := range registry {
		runs = append(runs, services.SourceRun{
			Platform:   source.Platform,
			Key:        source.Key,
			Status:     statusComplete,
			StartedAt:  foldNow,
			FinishedAt: foldNow.Add(time.Second),
			DurationMS: 1000,
		})
	}

	next := FoldGroups(NewStore(), registry, []shard.Manifest{manifestOf(runs...)}, foldNow)

	group, ok := next.Group("platform:greenhouse")
	must.True(t, ok)
	test.Eq(t, int32(4000), group.ParallelismMilli)
	test.Eq(t, int32(1), group.Samples)
}

func TestFoldGroupsKeepsTheBusiestObservation(t *testing.T) {
	t.Parallel()

	// A run that scheduled only two sources of a group under-estimates it; taking
	// the max across runs lets a later, busier run correct that.
	registry := []services.Source{
		{Platform: "greenhouse", Key: "a"},
		{Platform: "greenhouse", Key: "b"},
	}

	thin := manifestOf(services.SourceRun{
		Platform: "greenhouse", Key: "a", Status: statusComplete,
		StartedAt: foldNow, FinishedAt: foldNow.Add(time.Second), DurationMS: 1000,
	})

	busy := manifestOf(
		services.SourceRun{
			Platform: "greenhouse", Key: "a", Status: statusComplete,
			StartedAt: foldNow, FinishedAt: foldNow.Add(time.Second), DurationMS: 1000,
		},
		services.SourceRun{
			Platform: "greenhouse", Key: "b", Status: statusComplete,
			StartedAt: foldNow, FinishedAt: foldNow.Add(time.Second), DurationMS: 1000,
		},
	)

	store := FoldGroups(NewStore(), registry, []shard.Manifest{busy}, foldNow)
	group, _ := store.Group("platform:greenhouse")
	test.Eq(t, int32(2000), group.ParallelismMilli)

	store = FoldGroups(store, registry, []shard.Manifest{thin}, foldNow.Add(time.Hour))
	group, _ = store.Group("platform:greenhouse")
	test.Eq(t, int32(2000), group.ParallelismMilli, test.Sprint("a thinner run must not lower a measured capacity"))
}

func TestFoldGroupsFloorsAtOneSlot(t *testing.T) {
	t.Parallel()

	// A group can always run one source at a time, so an observation below one
	// slot is idle time inside the sample rather than a narrower backend.
	registry := []services.Source{{Platform: "greenhouse", Key: "a"}, {Platform: "greenhouse", Key: "b"}}

	sparse := manifestOf(
		services.SourceRun{
			Platform: "greenhouse", Key: "a", Status: statusComplete,
			StartedAt: foldNow, FinishedAt: foldNow.Add(time.Second), DurationMS: 1000,
		},
		services.SourceRun{
			Platform: "greenhouse", Key: "b", Status: statusComplete,
			StartedAt: foldNow.Add(time.Minute), FinishedAt: foldNow.Add(time.Minute + time.Second), DurationMS: 1000,
		},
	)

	next := FoldGroups(NewStore(), registry, []shard.Manifest{sparse}, foldNow)

	group, ok := next.Group("platform:greenhouse")
	must.True(t, ok)
	test.Eq(t, MinParallelismMilli, group.ParallelismMilli)
}

func TestReconcileRetiresAndRestores(t *testing.T) {
	t.Parallel()

	store := NewStore()
	store.PutSource(SourceState{Platform: "lever", Key: "gone", DurationMS: []int32{100}})
	store.PutSource(SourceState{Platform: "lever", Key: "here"})

	registry := []services.Source{{Platform: "lever", Key: "here"}}

	next := Reconcile(store, registry, foldNow, Policy{})

	gone, ok := next.Source(SourceID{Platform: "lever", Key: "gone"})
	must.True(t, ok)
	test.True(t, gone.Retired)
	test.Eq(t, formatStamp(foldNow), gone.RetiredAt)

	// A temporarily removed adapter must not lose its history.
	test.Eq(t, []int32{100}, gone.DurationMS)

	back := Reconcile(next, []services.Source{{Platform: "lever", Key: "here"}, {Platform: "lever", Key: "gone"}}, foldNow.Add(time.Hour), Policy{})

	restored, ok := back.Source(SourceID{Platform: "lever", Key: "gone"})
	must.True(t, ok)
	test.False(t, restored.Retired)
	test.Eq(t, "", restored.RetiredAt)
	test.Eq(t, []int32{100}, restored.DurationMS)
}

func TestReconcileDropsALongRetiredSource(t *testing.T) {
	t.Parallel()

	store := NewStore()
	store.PutSource(SourceState{Platform: "lever", Key: "ancient", Retired: true, RetiredAt: formatStamp(foldNow.Add(-200 * 24 * time.Hour))})

	next := Reconcile(store, []services.Source{{Platform: "greenhouse", Key: "x"}}, foldNow, Policy{})

	_, ok := next.Source(SourceID{Platform: "lever", Key: "ancient"})
	test.False(t, ok)
}

func TestFoldAllCachesTheAffinityGroup(t *testing.T) {
	t.Parallel()

	// The cache exists so a status command can explain a plan without recomputing
	// the registry. Build recomputes it and never trusts it.
	registry := []services.Source{{Platform: "greenhouse", Key: "stripe"}}

	next := FoldAll(NewStore(), registry, []shard.Manifest{
		manifestOf(sourceRun("greenhouse", "stripe", statusComplete, 300, 65)),
	}, foldNow, Policy{})

	state, ok := next.Source(SourceID{Platform: "greenhouse", Key: "stripe"})
	must.True(t, ok)
	test.Eq(t, "platform:greenhouse", state.Group)
}

func TestFoldAllIsOneWriterForManyShardManifests(t *testing.T) {
	t.Parallel()

	// N shards must never be N writers: each shard sees only its own slice, and
	// the merge is the only place that has every manifest at once.
	registry := []services.Source{
		{Platform: "greenhouse", Key: "a"},
		{Platform: "personio", Key: "b"},
	}

	next := FoldAll(NewStore(), registry, []shard.Manifest{
		manifestOf(sourceRun("greenhouse", "a", statusComplete, 300, 65)),
		manifestOf(sourceRun("personio", "b", statusComplete, 2500, 12)),
	}, foldNow, Policy{})

	test.Eq(t, 2, next.Len())
	test.Eq(t, foldNow, next.WrittenAt)
}

func TestFoldedStateFeedsBackIntoTheNextPlan(t *testing.T) {
	t.Parallel()

	// The loop the whole design rests on: plan, run, fold, plan again. A source
	// that just succeeded must stop outranking one that has not been refreshed.
	registry := []services.Source{
		{Platform: "greenhouse", Key: "a"},
		{Platform: "greenhouse", Key: "b"},
	}

	store := Fold(NewStore(), manifestOf(sourceRun("greenhouse", "a", statusComplete, 300, 65)), foldNow)

	plan, err := Build(registry, store, Options{Now: foldNow.Add(time.Hour), Budget: Budget{Wall: time.Minute}, Workers: 8})
	must.NoError(t, err)
	must.SliceLen(t, 2, plan.Items)

	refreshed, ok := plan.Lookup(SourceID{Platform: "greenhouse", Key: "a"})
	must.True(t, ok)

	untouched, ok := plan.Lookup(SourceID{Platform: "greenhouse", Key: "b"})
	must.True(t, ok)

	test.Less(t, MaxStaleMilli, refreshed.StaleMilli)
	test.Eq(t, MaxStaleMilli, untouched.StaleMilli)
	test.Less(t, refreshed.Rank, untouched.Rank)
}
