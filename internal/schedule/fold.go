package schedule

import (
	"slices"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/httpx"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/services"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/shard"
)

// Source lifecycle statuses this package interprets. They mirror
// internal/services/observe.go, which owns them.
const (
	statusComplete  = "complete"
	statusFailed    = "failed"
	statusTruncated = "truncated"
	statusStopped   = "stopped"
)

// Fold folds one crawl manifest into the state, returning a new store.
//
// The input store is never mutated, so a caller can always compare before with
// after — which is how the fairness tests assert that a deferred source did not
// age.
//
// Per source status:
//
//	complete   LastAttempt set, LastSuccess set, duration and postings pushed,
//	           failures reset to zero
//	failed     LastAttempt set, duration pushed, failures incremented
//	truncated  LastAttempt set, duration pushed, failures unchanged
//	stopped    LastAttempt set, duration pushed, failures unchanged
//	deferred   nothing at all
//	planned    nothing at all
//	running    nothing at all
//
// Three rules there are worth stating on their own.
//
// Our impatience is not the board's fault. "truncated" and "stopped" mean we ran
// out of budget or a consumer broke early. Counting either as a failure would
// put a slow-but-healthy source into exponential back-off because of a
// scheduling decision we made, which turns the expensive tail of a run into a
// permanent blind spot.
//
// Their durations still count, exactly as internal/shard/cost.go already
// decides: they are lower bounds, and treating a lower bound as no information
// is how the most expensive sources end up looking cheap.
//
// A failed source's posting count is never pushed. A zero from an outage would
// drag the yield median down and demote a healthy source for a week.
func Fold(store *Store, manifest shard.Manifest, now time.Time) *Store {
	next := store.Clone()
	stamp := formatStamp(now)

	for _, run := range manifest.Sources {
		id := SourceID{Platform: run.Platform, Key: run.Key}
		if id.Platform == "" || id.Key == "" {
			continue
		}

		switch run.Status {
		case statusComplete, statusFailed, statusTruncated, statusStopped:
		default:
			// planned, running, deferred and anything a future binary invents:
			// nothing was learned, so nothing is recorded. Advancing LastAttempt
			// for a source that never ran resets the aging clock of work that was
			// never done, which is unbounded starvation dressed as fairness.
			continue
		}

		state, _ := next.Source(id)
		state.Platform = run.Platform
		state.Key = run.Key

		if run.Company != "" {
			state.Company = run.Company
		}

		state.LastAttempt = stamp

		if run.DurationMS > 0 {
			state.DurationMS = pushSample(state.DurationMS, clampDurationSample(run.DurationMS))
		}

		switch run.Status {
		case statusComplete:
			state.LastSuccess = stamp
			state.Postings = pushSample(state.Postings, clampPostingSample(run.Postings))
			state.ConsecutiveFailures = 0
			state.ErrorClass = ""

		case statusFailed:
			state.ConsecutiveFailures++
			state.ErrorClass = run.ErrorClass
		}

		// A source that reported anything at all is evidently still registered.
		state.Retired = false
		state.RetiredAt = ""

		next.PutSource(state)
	}

	next.WrittenAt = now.UTC()

	return next
}

// FoldGroups folds measured affinity-group parallelism out of manifests.
//
// The estimate is
//
//	parallelism(g) = max over runs of  sum(duration of g's sources) / (last finish - first start)
//
// which needs no new manifest field: services.SourceRun already carries
// StartedAt and FinishedAt. Three properties make max the right estimator rather
// than a median. It is mathematically a lower bound on the concurrency actually
// achieved, since total busy time over a span cannot exceed slots x span, so it
// can never over-estimate. A run that happened to schedule only two sources of a
// group under-estimates it, and taking the max across runs lets a later, busier
// run correct that. And it absorbs everything a semaphore count would miss —
// httpx's pacing interval, 429 cooldowns, retry sleeps — because it measures
// achieved throughput rather than permission.
//
// The alternative was a curated per-platform concurrency table for the planner.
// That is a second opinion about what httpx already knows, guaranteed to drift,
// and the drift shows up as a rate-limit problem, which is the hardest kind to
// diagnose.
//
// registry is required because affinity is derived per platform all-or-nothing
// (see shard.AffinityKeys); computing it from a manifest's subset of the
// registry could put the same source in a different group than the planner did.
func FoldGroups(store *Store, registry []services.Source, manifests []shard.Manifest, now time.Time) *Store {
	next := store.Clone()

	groups := groupsOf(registry, defaultPerHostLimit())
	stamp := formatStamp(now)

	for _, manifest := range manifests {
		type window struct {
			busyMS int64
			first  time.Time
			last   time.Time
			runs   int
		}

		windows := map[string]*window{}

		for _, run := range manifest.Sources {
			if run.DurationMS <= 0 || run.StartedAt.IsZero() || run.FinishedAt.IsZero() {
				continue
			}

			key, ok := groups[SourceID{Platform: run.Platform, Key: run.Key}]
			if !ok {
				continue
			}

			observed, found := windows[key]
			if !found {
				observed = &window{first: run.StartedAt, last: run.FinishedAt}
				windows[key] = observed
			}

			observed.busyMS += run.DurationMS
			observed.runs++

			if run.StartedAt.Before(observed.first) {
				observed.first = run.StartedAt
			}

			if run.FinishedAt.After(observed.last) {
				observed.last = run.FinishedAt
			}
		}

		// Sorted so the store is written in a fixed order regardless of map
		// iteration, even though every update here is idempotent.
		keys := make([]string, 0, len(windows))
		for key := range windows {
			keys = append(keys, key)
		}

		slices.Sort(keys)

		for _, key := range keys {
			observed := windows[key]

			spanMS := observed.last.Sub(observed.first).Milliseconds()
			if spanMS <= 0 {
				continue
			}

			parallelism := observed.busyMS * 1000 / spanMS
			if parallelism < int64(MinParallelismMilli) {
				// A group can always run one source at a time, so anything below
				// this is idle time inside the sample rather than a narrower
				// backend.
				parallelism = int64(MinParallelismMilli)
			}

			state, _ := next.Group(key)
			state.Key = key
			state.Samples++
			state.ObservedAt = stamp

			if int32(parallelism) > state.ParallelismMilli {
				state.ParallelismMilli = int32(parallelism)
			}

			next.PutGroup(state)
		}
	}

	next.WrittenAt = now.UTC()

	return next
}

// Reconcile aligns the state with the registry.
//
// A source that has left the registry keeps its record, marked retired, so a
// temporarily removed adapter does not lose its history; the record is dropped
// once it has been gone for Policy.RetireAfter. A retired source that comes back
// is un-retired with its history intact.
func Reconcile(store *Store, registry []services.Source, now time.Time, policy Policy) *Store {
	policy = policy.withDefaults()

	next := store.Clone()

	registered := make(map[SourceID]services.Source, len(registry))
	for _, source := range registry {
		registered[SourceID{Platform: source.Platform, Key: source.Key}] = source
	}

	stamp := formatStamp(now)

	for _, state := range next.Sources() {
		id := state.ID()

		if _, ok := registered[id]; ok {
			if state.Retired {
				state.Retired = false
				state.RetiredAt = ""
				next.PutSource(state)
			}

			continue
		}

		if !state.Retired {
			state.Retired = true
			state.RetiredAt = stamp
			next.PutSource(state)

			continue
		}

		retiredAt, ok := parseStamp(state.RetiredAt)
		if !ok {
			// Retired with no legible timestamp: stamp it now rather than keep it
			// forever or drop history we cannot date.
			state.RetiredAt = stamp
			next.PutSource(state)

			continue
		}

		if now.Sub(retiredAt) > policy.RetireAfter {
			next.DeleteSource(id)
		}
	}

	next.WrittenAt = now.UTC()

	return next
}

// FoldAll is what a sharded merge calls: N shard manifests, one fold, one
// writer.
//
// N shards must never be N writers. Each shard sees only its own slice, so a
// shard writing state would either clobber the others or need a merge of its
// own; the merge job already has every manifest in one place and is the only
// place that can fold them once.
func FoldAll(store *Store, registry []services.Source, manifests []shard.Manifest, now time.Time, policy Policy) *Store {
	next := store.Clone()

	for _, manifest := range manifests {
		next = Fold(next, manifest, now)
	}

	next = FoldGroups(next, registry, manifests, now)
	next = Reconcile(next, registry, now, policy)

	// Cache each source's current affinity group so a status command can explain
	// a plan without recomputing the registry. Build recomputes it and never
	// trusts this.
	for id, key := range groupsOf(registry, defaultPerHostLimit()) {
		state, ok := next.Source(id)
		if !ok || state.Group == key {
			continue
		}

		state.Group = key
		next.PutSource(state)
	}

	next.WrittenAt = now.UTC()

	return next
}

// groupsOf maps every registry source to its affinity key.
func groupsOf(registry []services.Source, perHostLimit int) map[SourceID]string {
	keys := shard.AffinityKeys(registry, perHostLimit)

	groups := make(map[SourceID]string, len(registry))
	for i, source := range registry {
		groups[SourceID{Platform: source.Platform, Key: source.Key}] = string(keys[i])
	}

	return groups
}

// pushSample appends one observation and keeps the newest MaxSamples, oldest
// first.
func pushSample(samples []int32, value int32) []int32 {
	out := append(slices.Clone(samples), value)
	if len(out) > MaxSamples {
		out = slices.Clone(out[len(out)-MaxSamples:])
	}

	return out
}

// clampDurationSample bounds a sample on the way into the file, using
// internal/shard's constants, so the stored series can never overflow int32 and
// one stalled run cannot dominate a median.
func clampDurationSample(durationMS int64) int32 {
	return int32(min(max(durationMS, shard.MinSourceCostMS), shard.MaxSourceCostMS))
}

func clampPostingSample(postings int) int32 {
	if postings < 0 {
		return 0
	}

	if postings > int(^uint32(0)>>1) {
		return int32(^uint32(0) >> 1)
	}

	return int32(postings)
}

// defaultPerHostLimit reads httpx's own default rather than restating it, so the
// planner and the limiter cannot disagree about what a backend allows.
func defaultPerHostLimit() int { return httpx.DefaultPerHostLimit }
