package schedule

import (
	"testing"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/services"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/shard"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// These run against the real source registry rather than a synthetic shape, so
// the numbers in the report are measured on the thing that will actually be
// scheduled.

func TestBuildPlansTheRealRegistry(t *testing.T) {
	t.Parallel()

	registry := services.Builtin
	must.SliceNotEmpty(t, registry)

	plan, err := Build(registry, NewStore(), Options{
		Now:     planningNow,
		Budget:  Budget{Wall: 15 * time.Minute},
		Workers: 32,
	})
	must.NoError(t, err)

	t.Logf("registry: %d sources, %d affinity groups", len(registry), len(plan.Groups))
	t.Logf("cold start at a 15-minute budget: %d admitted, %d deferred, %d ms planned against %d ms global capacity",
		len(plan.Items), len(plan.Deferred), plan.PlannedMS, plan.GlobalCapacityMS)

	test.Eq(t, len(registry), len(plan.Items)+len(plan.Deferred))

	// Every planned source resolves, and the plan composes with shard.Build.
	selected, err := plan.Sources(registry)
	must.NoError(t, err)
	test.SliceLen(t, len(plan.Items), selected)
}

func TestRealRegistryPlanIsDeterministic(t *testing.T) {
	t.Parallel()

	registry := services.Builtin

	opts := Options{
		Now:     planningNow,
		Budget:  Budget{Wall: 15 * time.Minute},
		Workers: 32,
	}

	first, err := Build(registry, NewStore(), opts)
	must.NoError(t, err)

	for range 4 {
		got, err := Build(permute(registry, 3), NewStore(), opts)
		must.NoError(t, err)

		test.Eq(t, first.PlanID, got.PlanID)
	}
}

func TestRealRegistryGroupCapacityHoldsForEverySharedBackend(t *testing.T) {
	t.Parallel()

	// The invariant that matters most: no affinity group is ever admitted more
	// work than budget x its own measured parallelism x fill. Sharding raises the
	// global term and this one not at all, because more runners are not
	// permission to press a shared backend harder — that ceiling lives in httpx
	// and scheduling must never reach for it.
	registry := services.Builtin

	plan, err := Build(registry, NewStore(), Options{
		Now:     planningNow,
		Budget:  Budget{Wall: 10 * time.Minute},
		Workers: 32,
		Shards:  8,
	})
	must.NoError(t, err)

	planned := map[string]int64{}
	for _, item := range plan.Items {
		planned[item.Group] += item.PredictedMS
	}

	for _, group := range plan.Groups {
		test.LessEq(t, group.CapacityMS, planned[group.Key],
			test.Sprintf("group %s planned %d ms against capacity %d", group.Key, planned[group.Key], group.CapacityMS))
	}
}

func TestRealRegistryStateFileSize(t *testing.T) {
	t.Parallel()

	// The file is read whole, written whole and reviewed in a diff, so its size
	// is a design constraint rather than a curiosity. It is deliberately not
	// committed to git: a state file changing daily is tens of megabytes of git
	// objects a year in every clone and every CI checkout.
	registry := services.Builtin

	manifest := shard.NewManifest(planningNow, planningNow.Add(time.Minute), time.Minute, shard.StatusComplete, 0, 0, runsFor(registry))

	store := NewStore()
	for range MaxSamples {
		store = Fold(store, manifest, planningNow)
	}

	store = Reconcile(store, registry, planningNow, Policy{})

	counter := &countingWriter{}
	must.NoError(t, Encode(counter, store))

	t.Logf("state file: %d sources, %d bytes (%.2f MiB), %d bytes/row",
		store.Len(), counter.n, float64(counter.n)/(1024*1024), counter.n/max(store.Len(), 1))

	test.Eq(t, len(registry), store.Len())
	test.LessEq(t, 4*1024*1024, counter.n, test.Sprint("the state file has outgrown what a whole-file read and a human diff can carry"))
}

func runsFor(registry []services.Source) []services.SourceRun {
	runs := make([]services.SourceRun, 0, len(registry))

	for i, source := range registry {
		runs = append(runs, services.SourceRun{
			Platform:   source.Platform,
			Key:        source.Key,
			Company:    source.Company,
			Status:     statusComplete,
			StartedAt:  planningNow,
			FinishedAt: planningNow.Add(time.Duration(100+i%900) * time.Millisecond),
			DurationMS: int64(100 + i%900),
			Postings:   i % 500,
		})
	}

	return runs
}

type countingWriter struct{ n int }

func (w *countingWriter) Write(p []byte) (int, error) {
	w.n += len(p)

	return len(p), nil
}

func BenchmarkBuildRealRegistry(b *testing.B) {
	registry := services.Builtin

	manifest := shard.NewManifest(planningNow, planningNow.Add(time.Minute), time.Minute, shard.StatusComplete, 0, 0, runsFor(registry))
	store := Fold(NewStore(), manifest, planningNow)

	opts := Options{
		Now:     planningNow.Add(30 * time.Hour),
		Budget:  Budget{Wall: 15 * time.Minute},
		Workers: 32,
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if _, err := Build(registry, store, opts); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncodeRealRegistry(b *testing.B) {
	registry := services.Builtin

	manifest := shard.NewManifest(planningNow, planningNow.Add(time.Minute), time.Minute, shard.StatusComplete, 0, 0, runsFor(registry))
	store := Fold(NewStore(), manifest, planningNow)

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if err := Encode(&countingWriter{}, store); err != nil {
			b.Fatal(err)
		}
	}
}
