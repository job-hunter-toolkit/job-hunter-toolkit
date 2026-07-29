package schedule

import (
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/httpx"
)

// Policy defaults. Every one of these is a knob with a measured consequence, and
// two of them (ObligeAt, ObligeShare) are honest guesses that nobody has swept.
const (
	// DefaultTarget is how often a source should be refreshed. Daily is what a
	// job hunter needs and what the nightly already attempts.
	DefaultTarget = 24 * time.Hour

	// DefaultTick quantises Options.Now. A workflow retried inside the same hour
	// produces the identical plan, so a re-run does not reshuffle work already
	// under way. A daemon sets this to a minute.
	DefaultTick = time.Hour

	// DefaultObligeAt is the staleness at which a source stops competing on
	// value and joins the FIFO aging lane: three times its target.
	DefaultObligeAt int32 = 3000

	// DefaultObligeShare is the percent of each group's capacity reserved for the
	// aging lane. It is what stops a permanently expensive platform from being
	// starved by cheap ones, and it doubles as the cold-start reserve cap:
	// never-attempted sources sort first in the aging lane, so a registry that
	// doubles overnight — which this one did — would otherwise freeze every
	// existing source's refresh for days.
	DefaultObligeShare int32 = 50

	// DefaultFill is the percent of a group's theoretical capacity that may be
	// admitted. The last source admitted should finish before the deadline: a
	// source truncated by our own deadline costs its full duration and cannot
	// advance last_seen, so it is strictly worse than not starting it.
	DefaultFill int32 = 90

	// DefaultRetireAfter is how long a source absent from the registry keeps its
	// history before the record is dropped.
	DefaultRetireAfter = 90 * 24 * time.Hour

	// MaxStaleMilli caps the staleness multiplier at four targets overdue. Without
	// a cap, one source neglected for a year outranks everything forever.
	MaxStaleMilli int32 = 4000

	// MaxBackoffShift caps exponential back-off at 2^6 targets, about 64 days at
	// the default target. Back-off must not silently become retirement: a board
	// that 503s for a month and then comes back has to be noticed.
	MaxBackoffShift = 6

	// MinParallelismMilli is the floor for a group's measured parallelism. A
	// group can always run one source at a time, so an observation below this is
	// idle time in the sample rather than a narrower backend.
	MinParallelismMilli int32 = 1000
)

// Budget is how much work one run may do.
type Budget struct {
	// Wall is the run's time budget. Zero with Unbounded false means "no wall
	// budget": every eligible source is admitted and the plan orders rather than
	// selects, which is migration step 2 of docs/crawl-budget-model.md — useful
	// on its own and trivially revertible.
	Wall time.Duration

	// Requests is specified and deliberately not implementable yet.
	// services.SourceRun carries no request count, which
	// docs/architecture-roadmap.md already lists as missing. When that field
	// lands, a request budget is the same greedy pass with cost in requests and
	// no parallelism divisor, because requests do not get cheaper by being
	// concurrent. Until then Build rejects it rather than approximating it, since
	// an approximated politeness budget is the one kind of guess this project
	// cannot afford.
	Requests int

	// Unbounded is the daemon's mode: refresh forever against a freshness target
	// rather than a deadline. Capacity is infinite and everything at least one
	// target stale is admitted, in the same order. Same code, no second
	// scheduler.
	Unbounded bool
}

// bounded reports whether the budget selects as well as orders.
func (b Budget) bounded() bool { return !b.Unbounded && b.Wall > 0 }

// Policy is the scheduling configuration. It is configuration, not state: a run
// rewrites the state file, and configuration that a run can silently change is
// not configuration.
type Policy struct {
	// Target is the freshness target. PerPlatform overrides it by platform.
	Target      time.Duration
	PerPlatform map[string]time.Duration

	// Tick quantises Options.Now, see DefaultTick.
	Tick time.Duration

	// ObligeAt, ObligeShare and Fill are percent/milli knobs; see the Default
	// constants above for what each one buys.
	ObligeAt    int32
	ObligeShare int32
	Fill        int32

	// ColdParallel is the assumed concurrency of an affinity group with no
	// measurement yet, in thousandths. It defaults to httpx.DefaultPerHostLimit,
	// which is derived from the limiter that enforces it rather than invented,
	// and is the modal value in httpx's table.
	ColdParallel int32

	// RetireAfter is how long a source absent from the registry keeps its record.
	RetireAfter time.Duration
}

// withDefaults returns the policy with every unset field filled in. Callers pass
// a zero Policy and get the documented defaults.
func (p Policy) withDefaults() Policy {
	if p.Target <= 0 {
		p.Target = DefaultTarget
	}

	if p.Tick <= 0 {
		p.Tick = DefaultTick
	}

	if p.ObligeAt <= 0 {
		p.ObligeAt = DefaultObligeAt
	}

	if p.ObligeShare <= 0 || p.ObligeShare > 100 {
		p.ObligeShare = DefaultObligeShare
	}

	if p.Fill <= 0 || p.Fill > 100 {
		p.Fill = DefaultFill
	}

	if p.ColdParallel <= 0 {
		p.ColdParallel = int32(httpx.DefaultPerHostLimit) * 1000
	}

	if p.RetireAfter <= 0 {
		p.RetireAfter = DefaultRetireAfter
	}

	return p
}

// targetFor returns the freshness target for one platform.
//
// PerPlatform is read by key only and never ranged over, so its map iteration
// order cannot reach a plan.
func (p Policy) targetFor(platform string) time.Duration {
	if target, ok := p.PerPlatform[platform]; ok && target > 0 {
		return target
	}

	return p.Target
}

// Options configures [Build].
type Options struct {
	// Now is the planning instant. It is a field rather than a clock call
	// because that is the entire testing seam: there is no clock interface, no
	// package-level timeNow variable, and no need for either. Build truncates it
	// to Policy.Tick and records the result in Plan.PlannedFor, so the plan says
	// what it assumed.
	Now time.Time

	Budget Budget
	Policy Policy

	// Workers is the crawl's --concurrency. It enters only the global capacity
	// term.
	Workers int

	// Shards is 1 unless the plan will be handed to shard.Build. More shards
	// raise the global term and leave every per-group term untouched, which is
	// docs/crawl-budget-model.md's "sharding buys latency, not budget" expressed
	// as arithmetic.
	Shards int

	// PerHostLimit is passed through to shard.AffinityKeys. Zero uses
	// httpx.DefaultPerHostLimit.
	PerHostLimit int
}
