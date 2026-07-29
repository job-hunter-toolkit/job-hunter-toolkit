package schedule

import (
	"fmt"
	"hash/fnv"
	"slices"
	"testing"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/services"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/shard"
)

// The seams this package is designed around, exercised here rather than
// described:
//
//   - Options.Now is a value, so the clock is a variable in a loop.
//   - Build, Fold and Reconcile are pure, so a test is a fixture in and a struct
//     out.
//   - Fold consumes a shard.Manifest, so the whole feedback loop — plan, run,
//     fold, plan again — is drivable with no network and no process.
//   - The cost oracle lives here, never in the scheduler. That is what makes a
//     200-run starvation property assertable in milliseconds.

// Oracle is the simulated world: what a source would cost, return, and whether
// it would fail. It exists only in tests.
type Oracle func(SourceID) (time.Duration, int, error)

// platformShape is one synthetic platform, shaped by the per-platform means in
// docs/measurements/2026-07-28-crawl.md. These model scheduler behaviour, not
// crawl wall time.
type platformShape struct {
	platform string
	sources  int
	cost     time.Duration
	postings int
	slots    int
}

// measuredShape is the 07/28 registry in miniature: two cheap high-yield
// platforms and two of the SMB platforms that were 61% of the crawl time for
// 8.3% of the postings. The ratios are what matter, not the absolute counts.
func measuredShape() []platformShape {
	return []platformShape{
		{platform: "greenhouse", sources: 200, cost: 300 * time.Millisecond, postings: 65, slots: 4},
		{platform: "jibe", sources: 40, cost: 12 * time.Second, postings: 1670, slots: 4},
		{platform: "personio", sources: 400, cost: 2500 * time.Millisecond, postings: 12, slots: 4},
		{platform: "teamtailor", sources: 300, cost: 2400 * time.Millisecond, postings: 25, slots: 4},
	}
}

// registryOf builds synthetic sources. Keys are board slugs, so
// shard.AffinityKeys resolves no host and every platform is one group — which is
// exactly the shape of the platforms this scheduler exists for.
func registryOf(shapes []platformShape) []services.Source {
	var sources []services.Source

	for _, shape := range shapes {
		for i := range shape.sources {
			key := fmt.Sprintf("%s-%04d", shape.platform, i)
			sources = append(sources, services.Source{
				Platform: shape.platform,
				Key:      key,
				Company:  key,
			})
		}
	}

	return sources
}

func slotsOf(shapes []platformShape) map[string]int {
	slots := map[string]int{}
	for _, shape := range shapes {
		slots["platform:"+shape.platform] = shape.slots
	}

	return slots
}

// oracleOf returns a deterministic cost model: each source's cost is its
// platform's mean spread over a +/-40% band by a hash of its key, so the plan
// has something to sort by and the spread is identical on every architecture and
// every run. No math/rand, because a flaky fairness test is worse than none.
func oracleOf(shapes []platformShape, failing map[SourceID]bool) Oracle {
	byPlatform := map[string]platformShape{}
	for _, shape := range shapes {
		byPlatform[shape.platform] = shape
	}

	return func(id SourceID) (time.Duration, int, error) {
		shape, ok := byPlatform[id.Platform]
		if !ok {
			return time.Second, 1, nil
		}

		spread := int64(hashOf(id)%81) - 40 // -40 .. +40
		cost := shape.cost + time.Duration(int64(shape.cost)*spread/100)

		if failing[id] {
			return cost, 0, fmt.Errorf("simulated permanent failure for %s", id)
		}

		return cost, shape.postings, nil
	}
}

func hashOf(id SourceID) uint32 {
	digest := fnv.New32a()
	_, _ = digest.Write([]byte(id.Platform))
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write([]byte(id.Key))

	return digest.Sum32()
}

// execution configures the simulated executor.
type execution struct {
	// slots is each affinity group's real concurrency. The simulator schedules
	// each group onto that many slots, which is what makes the StartedAt and
	// FinishedAt stamps realistic enough for FoldGroups to learn from.
	slots map[string]int

	// completeRanks caps how far down the emit order the executor gets. Items
	// beyond it are recorded deferred, which is what a dispatch gate declining
	// work looks like in a manifest. Zero means the whole plan runs.
	completeRanks int
}

// simulate turns a plan into a manifest by consulting the oracle, with no
// network, no goroutines and no wall clock.
func simulate(plan Plan, oracle Oracle, start time.Time, exec execution) shard.Manifest {
	free := map[string][]time.Time{}

	runs := make([]services.SourceRun, 0, len(plan.Items))

	var (
		last     = start
		postings int
	)

	for _, item := range plan.Items {
		if exec.completeRanks > 0 && item.Rank >= exec.completeRanks {
			runs = append(runs, services.SourceRun{
				Platform: item.Source.Platform,
				Key:      item.Source.Key,
				Company:  item.Company,
				Status:   StatusDeferred,
			})

			continue
		}

		slots := exec.slots[item.Group]
		if slots < 1 {
			slots = 1
		}

		if len(free[item.Group]) == 0 {
			free[item.Group] = make([]time.Time, slots)
			for i := range free[item.Group] {
				free[item.Group][i] = start
			}
		}

		// Earliest free slot in this group, which is list scheduling and is what
		// a semaphore of that width actually does.
		pick := 0
		for i := range free[item.Group] {
			if free[item.Group][i].Before(free[item.Group][pick]) {
				pick = i
			}
		}

		cost, yield, err := oracle(item.Source)

		startedAt := free[item.Group][pick]
		finishedAt := startedAt.Add(cost)
		free[item.Group][pick] = finishedAt

		if finishedAt.After(last) {
			last = finishedAt
		}

		status := statusComplete
		errorClass := ""
		errors := 0

		if err != nil {
			status = statusFailed
			errorClass = "simulated"
			errors = 1
			yield = 0
		}

		postings += yield

		runs = append(runs, services.SourceRun{
			Platform:   item.Source.Platform,
			Key:        item.Source.Key,
			Company:    item.Company,
			Status:     status,
			StartedAt:  startedAt,
			FinishedAt: finishedAt,
			DurationMS: cost.Milliseconds(),
			Postings:   yield,
			Errors:     errors,
			ErrorClass: errorClass,
		})
	}

	return shard.NewManifest(start, last, last.Sub(start), shard.StatusComplete, postings, len(runs), runs)
}

// loop drives plan/run/fold repeatedly against a fake clock, which is a variable
// rather than an interface because Options.Now is a field.
type loop struct {
	registry []services.Source
	oracle   Oracle
	exec     execution
	opts     Options

	store *Store
	now   time.Time

	// attempts counts how many times each source actually ran, which is the
	// quantity every fairness property is stated in.
	attempts map[SourceID]int
	plans    []Plan

	// postings is the total refreshed across every run, which is the metric the
	// fairness lane is paid for in.
	postings int
}

func newLoop(t *testing.T, shapes []platformShape, opts Options, exec execution, failing map[SourceID]bool) *loop {
	t.Helper()

	return &loop{
		registry: registryOf(shapes),
		oracle:   oracleOf(shapes, failing),
		exec:     exec,
		opts:     opts,
		store:    NewStore(),
		now:      time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC),
		attempts: map[SourceID]int{},
	}
}

func (l *loop) run(t *testing.T, runs int, step time.Duration) {
	t.Helper()

	for range runs {
		opts := l.opts
		opts.Now = l.now

		plan, err := Build(l.registry, l.store, opts)
		if err != nil {
			t.Fatalf("build plan at %s: %v", l.now, err)
		}

		manifest := simulate(plan, l.oracle, l.now, l.exec)
		l.postings += manifest.Postings

		for _, run := range manifest.Sources {
			if run.Status == StatusDeferred {
				continue
			}

			l.attempts[SourceID{Platform: run.Platform, Key: run.Key}]++
		}

		l.store = FoldAll(l.store, l.registry, []shard.Manifest{manifest}, l.now, l.opts.Policy)
		l.plans = append(l.plans, plan)
		l.now = l.now.Add(step)
	}
}

// neverRun returns the sources the loop never actually attempted, sorted.
func (l *loop) neverRun() []SourceID {
	var missing []SourceID

	for _, source := range l.registry {
		id := SourceID{Platform: source.Platform, Key: source.Key}
		if l.attempts[id] == 0 {
			missing = append(missing, id)
		}
	}

	slices.SortFunc(missing, compareIDs)

	return missing
}

// span is the least- and most-refreshed source of one platform across a loop.
type span struct{ min, max int }

func (s span) String() string { return fmt.Sprintf("%d..%d", s.min, s.max) }

// refreshesPerPlatform is the shape every fairness claim is really about: not
// how many postings were refreshed, but whether the least-favoured source on
// each platform is rotating or abandoned.
func (l *loop) refreshesPerPlatform() map[string]span {
	out := map[string]span{}

	for _, source := range l.registry {
		count := l.attempts[SourceID{Platform: source.Platform, Key: source.Key}]

		current, seen := out[source.Platform]
		if !seen {
			out[source.Platform] = span{min: count, max: count}

			continue
		}

		current.min = min(current.min, count)
		current.max = max(current.max, count)
		out[source.Platform] = current
	}

	return out
}
