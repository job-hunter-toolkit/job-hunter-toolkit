package schedule

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/services"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/shard"
)

// PlanSchemaVersion is the version of the schedule plan structure.
const PlanSchemaVersion = 1

// Lane is which of the two admission queues a source competes in.
type Lane uint8

const (
	// LaneAging is the fairness queue: strict FIFO by LastAttempt, holding every
	// source at least Policy.ObligeAt stale. It has a reserved share of each
	// group's capacity, and that reservation is the only reason a slow platform
	// is not starved forever by a cheap one.
	LaneAging Lane = iota

	// LaneValue is the density queue: highest postings-refreshed per second of
	// backend time first.
	LaneValue
)

// String renders the lane for plans and test failures.
func (l Lane) String() string {
	if l == LaneAging {
		return "aging"
	}

	return "value"
}

// MarshalText writes the lane by name, so a dumped plan says which queue chose
// each source rather than making a reader map 0 and 1 back to a policy.
func (l Lane) MarshalText() ([]byte, error) { return []byte(l.String()), nil }

// UnmarshalText reads a lane by name. An unrecognised value reads as the value
// lane, which is the conservative direction: it claims no fairness reservation.
func (l *Lane) UnmarshalText(text []byte) error {
	if string(text) == "aging" {
		*l = LaneAging

		return nil
	}

	*l = LaneValue

	return nil
}

// Reasons a source was not admitted.
const (
	// ReasonBackoff means the source has failed consecutively and its retry
	// interval has not elapsed.
	ReasonBackoff = "backoff"

	// ReasonOversize means the source alone is predicted to cost more than its
	// affinity group's entire per-run capacity, so no admission pass can ever
	// take it.
	//
	// This is the one hole in the fairness guarantee and it is reported rather
	// than hidden. Admitting it anyway would blow the budget and then be declined
	// by the dispatch gate, which advances nothing and starves it just the same,
	// only invisibly. It is a real signal: either the budget is too small for
	// that backend's slot count, or a single source has grown past what one run
	// can do and that is adapter work, not scheduling work.
	ReasonOversize = "oversize"

	// ReasonFresh means the source is inside its freshness target. Only an
	// unbounded (daemon) plan defers for this reason; a bounded run would rather
	// spend leftover budget than idle.
	ReasonFresh = "fresh"

	// ReasonGroupBudget means the source's affinity group is already booked to
	// its capacity for this run. This is the common one, and it is the whole
	// point: 970 Personio tenants behind one 4-slot key are ~243 sequential
	// rounds no matter what any scheduler decides, so a budget that admitted them
	// all would just truncate most of them.
	ReasonGroupBudget = "group_budget"

	// ReasonGlobalBudget means the run's worker-seconds are booked.
	ReasonGlobalBudget = "global_budget"
)

// StatusDeferred is the terminal source lifecycle status for work the dispatch
// gate declined to start.
//
// It is not a failure and it is not a truncation: nothing was requested. Folding
// it changes nothing at all in the state — not even LastAttempt, because
// advancing the aging clock of a source that never ran is unbounded starvation
// dressed as fairness. See Plan.Gate and Fold.
const StatusDeferred = "deferred"

// Item is one source admitted to a plan.
type Item struct {
	Source  SourceID `json:"source"`
	Company string   `json:"company,omitempty"`
	Group   string   `json:"group"`

	// PredictedMS is what the plan expects this source to cost. The dispatch gate
	// spends it against real elapsed time, and the next fold replaces it with
	// fact.
	PredictedMS int64 `json:"predicted_ms"`

	Score      int64 `json:"score"`
	Lane       Lane  `json:"lane"`
	StaleMilli int32 `json:"stale_milli"`

	// Rank is the position in execution order, which is not selection order. See
	// [Plan.Items].
	Rank int `json:"rank"`
}

// Deferral records why one source was not planned. Every unplanned source gets
// one: a plan that silently omits work is indistinguishable from a plan that
// lost it.
type Deferral struct {
	Source SourceID `json:"source"`
	Group  string   `json:"group,omitempty"`
	Reason string   `json:"reason"`
}

// GroupBudget is one affinity group's arithmetic, kept so a plan can explain
// itself without recomputing the registry.
type GroupBudget struct {
	Key string `json:"key"`

	// ParallelismMilli is the group's measured concurrency in thousandths, or
	// Policy.ColdParallel when no run has measured it yet.
	ParallelismMilli int32 `json:"parallelism_milli"`

	// Measured distinguishes the two. A cold group's capacity is an assumption,
	// and assumptions that look like measurements are how a scheduler quietly
	// becomes a curated table.
	Measured bool `json:"measured"`

	CapacityMS int64 `json:"capacity_ms"`
	ReserveMS  int64 `json:"reserve_ms"`
	PlannedMS  int64 `json:"planned_ms"`
	Sources    int   `json:"sources"`
}

// Plan is an ordered work list that fits a budget.
type Plan struct {
	SchemaVersion int    `json:"schema_version"`
	PlanID        string `json:"plan_id"`

	// PlannedFor is Options.Now truncated to Policy.Tick: the instant the plan
	// assumed, recorded so the plan says what it assumed.
	PlannedFor time.Time `json:"planned_for"`

	Budget  Budget `json:"budget"`
	Workers int    `json:"workers"`
	Shards  int    `json:"shards"`

	// GlobalCapacityMS is budget x workers x shards x fill, the second of the two
	// constraints in the budget arithmetic.
	GlobalCapacityMS int64 `json:"global_capacity_ms"`
	PlannedMS        int64 `json:"planned_ms"`

	Items    []Item        `json:"items"`
	Deferred []Deferral    `json:"deferred"`
	Groups   []GroupBudget `json:"groups"`
}

// unlimitedMS stands in for an unbounded capacity. It is far enough below
// math.MaxInt64 that adding a clamped source cost to it cannot overflow.
const unlimitedMS int64 = math.MaxInt64 / 4

// candidate is one source with everything Build needs to rank it.
type candidate struct {
	id      SourceID
	company string
	group   string

	cost  int64
	yield int64
	stale int32
	score int64
	lane  Lane

	lastAttempt time.Time
	attempted   bool
}

// Build turns a registry, persisted state and a budget into an ordered work
// list.
//
// It is a pure function: no clock, no I/O, no goroutines, no map iteration into
// output. Same sources and same state and same budget always produce the same
// plan including the same PlanID, so a retried workflow run does not reshuffle
// work already under way — the same property shard.Build already guarantees and
// for the same reason.
//
// The budget is two constraints rather than one, and that is the single most
// consequential decision here. Sources in different affinity groups do not
// contend; sources in the same one contend at exactly the concurrency httpx
// grants that key. Charging a group's four-way-parallel time against a
// single-threaded clock is the intuitive model and it leaves most of the budget
// unspent, so admission checks:
//
//	per group g:  sum(predicted) <= budget x parallelism(g) x fill
//	globally:     sum(predicted) <= budget x workers x shards x fill
//
// Selection is a greedy pass over two lanes. The aging lane is FIFO by
// LastAttempt and holds a reserved share of every group's capacity; the value
// lane is postings-refreshed per second of backend time. Ranking purely by
// value refreshes more postings per run and abandons slow platforms entirely,
// which is the failure the reserved share exists to prevent — see
// [Plan.Items] for why execution order is a third, separate question.
func Build(sources []services.Source, store *Store, opts Options) (Plan, error) {
	if len(sources) == 0 {
		return Plan{}, fmt.Errorf("build schedule: no sources to plan: refusing to emit a plan that would crawl nothing")
	}

	if opts.Budget.Requests != 0 {
		return Plan{}, fmt.Errorf(
			"build schedule: a request budget is not implementable yet: services.SourceRun records no request count, " +
				"and approximating a politeness budget from durations is exactly the guess this project cannot afford")
	}

	workers := opts.Workers
	shards := max(opts.Shards, 1)

	if opts.Budget.bounded() && workers < 1 {
		return Plan{}, fmt.Errorf("build schedule: a wall budget of %s needs Workers >= 1: the global capacity term is budget x workers x shards", opts.Budget.Wall)
	}

	workers = max(workers, 1)

	policy := opts.Policy.withDefaults()
	now := opts.Now.UTC().Truncate(policy.Tick)

	perHostLimit := opts.PerHostLimit
	if perHostLimit < 1 {
		perHostLimit = defaultPerHostLimit()
	}

	if err := checkDuplicates(sources); err != nil {
		return Plan{}, err
	}

	var (
		keys       = shard.AffinityKeys(sources, perHostLimit)
		est        = newEstimator(store)
		candidates = make([]candidate, 0, len(sources))
		deferred   = make([]Deferral, 0)
	)

	for i, source := range sources {
		id := SourceID{Platform: source.Platform, Key: source.Key}
		group := string(keys[i])

		// SourceState.Retired is deliberately not consulted. Build only ever sees
		// the registry, so a source in front of it is registered by definition; a
		// stale retired flag is bookkeeping for Reconcile to clear, not a reason
		// to skip work the caller asked for.
		state, known := store.Source(id)

		cand := candidate{
			id:      id,
			company: source.Company,
			group:   group,
			cost:    est.cost(source.Platform, state, known),
			yield:   est.yield(source.Platform, state, known),
		}

		cand.lastAttempt, cand.attempted = state.LastAttemptTime()

		target := policy.targetFor(source.Platform)
		cand.stale = staleness(now, state, target)

		// Postings refreshed per second of backend time, weighted by how overdue
		// the refresh is. Integer throughout: the Go spec lets an implementation
		// fuse a multiply-add into one rounding and arm64 does, so a float score
		// is a plan that can differ by architecture on a project whose CI builds
		// four of them.
		cand.score = cand.yield * int64(cand.stale) * 1000 / cand.cost

		if cand.stale >= policy.ObligeAt {
			cand.lane = LaneAging
		} else {
			cand.lane = LaneValue
		}

		if known && !eligible(now, state, target) {
			deferred = append(deferred, Deferral{Source: id, Group: group, Reason: ReasonBackoff})

			continue
		}

		if opts.Budget.Unbounded && cand.stale < 1000 {
			deferred = append(deferred, Deferral{Source: id, Group: group, Reason: ReasonFresh})

			continue
		}

		candidates = append(candidates, cand)
	}

	plan := admit(candidates, store, policy, opts.Budget, workers, shards)

	// Non-nil even when empty, so a serialised plan says "nothing was deferred"
	// rather than "the deferral list is missing".
	plan.Deferred = append(deferred, plan.Deferred...)

	slices.SortFunc(plan.Deferred, func(a, b Deferral) int { return compareIDs(a.Source, b.Source) })

	plan.SchemaVersion = PlanSchemaVersion
	plan.PlannedFor = now
	plan.Budget = opts.Budget
	plan.Workers = workers
	plan.Shards = shards
	plan.PlanID = planID(plan)

	return plan, nil
}

// staleness returns how overdue a refresh is, in thousandths of the target,
// capped at MaxStaleMilli.
//
// Measured from LastSuccess, never from LastAttempt: an attempt that failed did
// not refresh anything, and treating it as if it had is how a permanently broken
// source stops looking stale and quietly disappears from every plan.
func staleness(now time.Time, state SourceState, target time.Duration) int32 {
	success, ok := state.LastSuccessTime()
	if !ok {
		// Never succeeded, so maximally stale: a new source ranks with the most
		// neglected work rather than behind it, and is tried rather than starved.
		return MaxStaleMilli
	}

	age := now.Sub(success)
	if age <= 0 {
		return 0
	}

	milli := age.Milliseconds() * 1000 / target.Milliseconds()
	if milli > int64(MaxStaleMilli) {
		return MaxStaleMilli
	}

	return int32(milli)
}

// eligible reports whether a repeatedly failing source has waited long enough.
//
// Measured from LastAttempt, not LastSuccess. A permanently failing source has
// no LastSuccess at all, so an interval measured from it grows without bound and
// the gate stops holding — the source ends up attempted on nearly every run
// forever. Timing it from the last attempt is what makes back-off actually back
// off.
//
// The shift caps at MaxBackoffShift so a dead board is still retried about every
// 64 days. Back-off must not silently become retirement.
func eligible(now time.Time, state SourceState, target time.Duration) bool {
	if state.ConsecutiveFailures <= 0 {
		return true
	}

	attempt, ok := state.LastAttemptTime()
	if !ok {
		// Failures recorded with no attempt timestamp is a state file we cannot
		// reason about. Try the source: a wasted attempt is cheaper than an
		// invisible permanent skip.
		return true
	}

	shift := min(int(state.ConsecutiveFailures), MaxBackoffShift)

	return now.Sub(attempt) >= target*time.Duration(int64(1)<<shift)
}

// admit runs the greedy two-lane admission pass and fixes execution order.
func admit(
	candidates []candidate,
	store *Store,
	policy Policy,
	budget Budget,
	workers, shards int,
) Plan {
	books := map[string]*groupBook{}

	wallMS := int64(0)
	if budget.Wall > 0 {
		wallMS = budget.Wall.Milliseconds()
	}

	bounded := budget.bounded()

	for _, cand := range candidates {
		if _, ok := books[cand.group]; ok {
			continue
		}

		parallelism := policy.ColdParallel
		measured := false

		if group, found := store.Group(cand.group); found && group.ParallelismMilli > 0 {
			parallelism = group.ParallelismMilli
			measured = true
		}

		capacity := unlimitedMS
		if bounded {
			capacity = wallMS * int64(parallelism) / 1000 * int64(policy.Fill) / 100
		}

		book := &groupBook{budget: GroupBudget{
			Key:              cand.group,
			ParallelismMilli: parallelism,
			Measured:         measured,
			CapacityMS:       capacity,
			ReserveMS:        capacity / 100 * int64(policy.ObligeShare),
		}}

		if !bounded {
			book.budget.ReserveMS = capacity
		}

		books[cand.group] = book
	}

	globalCapacity := unlimitedMS
	if bounded {
		globalCapacity = wallMS * int64(workers) * int64(shards) / 100 * int64(policy.Fill)
	}

	var (
		plannedMS int64
		deferred  []Deferral
	)

	// Two ordered lanes. Aging is FIFO by LastAttempt — never-attempted first,
	// which is why the reserve doubles as a cold-start cap: a registry that
	// doubles overnight floods this lane, and without the cap it would freeze
	// every existing source's refresh for days.
	var aging, value []candidate

	for _, cand := range candidates {
		// A source that alone exceeds its group's whole capacity can never be
		// taken by any pass. Say so, rather than leaving it to fall out of every
		// run with a budget reason that implies it might fit next time.
		if bounded && cand.cost > books[cand.group].budget.CapacityMS {
			deferred = append(deferred, Deferral{Source: cand.id, Group: cand.group, Reason: ReasonOversize})

			continue
		}

		if cand.lane == LaneAging {
			aging = append(aging, cand)
		} else {
			value = append(value, cand)
		}
	}

	slices.SortFunc(aging, compareAging)
	slices.SortFunc(value, compareValue)

	take := func(cand candidate, groupLimit int64) bool {
		book := books[cand.group]

		if book.budget.PlannedMS+cand.cost > groupLimit {
			return false
		}

		if plannedMS+cand.cost > globalCapacity {
			return false
		}

		book.budget.PlannedMS += cand.cost
		book.budget.Sources++
		book.items = append(book.items, cand)
		plannedMS += cand.cost

		return true
	}

	// Pass one: the aging lane against its reserved share.
	var overflow []candidate

	for _, cand := range aging {
		if !take(cand, books[cand.group].budget.ReserveMS) {
			overflow = append(overflow, cand)
		}
	}

	// Pass two: aging candidates that did not fit the reserve, against whatever
	// is left of the full group capacity.
	for _, cand := range overflow {
		if !take(cand, books[cand.group].budget.CapacityMS) {
			deferred = append(deferred, deferralFor(cand, books[cand.group].budget, plannedMS, globalCapacity))
		}
	}

	// Pass three: the value lane against the remainder.
	for _, cand := range value {
		if !take(cand, books[cand.group].budget.CapacityMS) {
			deferred = append(deferred, deferralFor(cand, books[cand.group].budget, plannedMS, globalCapacity))
		}
	}

	return Plan{
		GlobalCapacityMS: globalCapacity,
		PlannedMS:        plannedMS,
		Items:            emitOrder(books),
		Deferred:         deferred,
		Groups:           groupBudgets(books),
	}
}

// deferralFor names which of the two constraints actually bound.
func deferralFor(cand candidate, budget GroupBudget, plannedMS, globalCapacity int64) Deferral {
	reason := ReasonGroupBudget
	if plannedMS+cand.cost > globalCapacity {
		reason = ReasonGlobalBudget
	}

	return Deferral{Source: cand.id, Group: budget.Key, Reason: reason}
}

// emitOrder decides execution order, which is a different problem from
// selection and expensive to get wrong in a way the plan does not show.
//
// internal.AllWithConcurrency hands a source to a worker and the worker then
// blocks inside httpx on that service's semaphore. A worker parked on a
// semaphore is occupied, not idle, so putting 970 Personio sources at the head
// of the list parks most of the pool on one 4-slot key. The rule is therefore:
// order within a group, then interleave groups round-robin by descending load,
// so the opening wave spreads across backends. That is the same instinct
// shard.Plan.Resolve follows when it keeps the registry's platform round-robin
// instead of re-sorting.
//
// Within a group, aging-lane items go first, and inside the aging lane the order
// stays FIFO by LastAttempt rather than becoming longest-first. That detail is
// load-bearing. If a truncated run always stops at the same fraction of the
// list, any fixed intra-lane order that does not rotate starves whatever sits at
// the tail forever — and a source the gate declined never advances LastAttempt,
// so FIFO is the one order that provably rotates. The value lane inside a group
// is longest-processing-time first, which minimises that group's makespan.
func emitOrder(books map[string]*groupBook) []Item {
	keys := make([]string, 0, len(books))
	for key := range books {
		keys = append(keys, key)
	}

	slices.Sort(keys)

	queues := make([][]candidate, 0, len(keys))
	order := make([]string, 0, len(keys))

	for _, key := range keys {
		book := books[key]
		if len(book.items) == 0 {
			continue
		}

		items := slices.Clone(book.items)
		slices.SortFunc(items, compareEmit)

		queues = append(queues, items)
		order = append(order, key)
	}

	// Heaviest groups first, so the longest pole starts in the opening wave. Ties
	// break on key, never on map order.
	indexes := make([]int, len(queues))
	for i := range indexes {
		indexes[i] = i
	}

	slices.SortStableFunc(indexes, func(a, b int) int {
		loadA, loadB := books[order[a]].budget.PlannedMS, books[order[b]].budget.PlannedMS
		if loadA != loadB {
			if loadA > loadB {
				return -1
			}

			return 1
		}

		return strings.Compare(order[a], order[b])
	})

	total := 0
	for _, queue := range queues {
		total += len(queue)
	}

	items := make([]Item, 0, total)

	// Groups drop out of the rotation as they empty, so this is linear in the
	// number of items rather than rounds x groups. On the real registry that is
	// the difference between 1,492 rounds over 1,002 groups and one pass.
	active := indexes

	for round := 0; len(active) > 0; round++ {
		remaining := active[:0]

		for _, index := range active {
			queue := queues[index]
			if round >= len(queue) {
				continue
			}

			if round+1 < len(queue) {
				remaining = append(remaining, index)
			}

			cand := queue[round]
			items = append(items, Item{
				Source:      cand.id,
				Company:     cand.company,
				Group:       cand.group,
				PredictedMS: cand.cost,
				Score:       cand.score,
				Lane:        cand.lane,
				StaleMilli:  cand.stale,
				Rank:        len(items),
			})
		}

		active = remaining
	}

	return items
}

func groupBudgets(books map[string]*groupBook) []GroupBudget {
	out := make([]GroupBudget, 0, len(books))
	for _, book := range books {
		out = append(out, book.budget)
	}

	slices.SortFunc(out, func(a, b GroupBudget) int { return strings.Compare(a.Key, b.Key) })

	return out
}

// groupBook is declared at package scope so groupBudgets and emitOrder can share
// it with admit.
type groupBook struct {
	budget GroupBudget
	items  []candidate
}

func compareAging(a, b candidate) int {
	// Never attempted sorts first: it is the most overdue thing there is.
	if a.attempted != b.attempted {
		if !a.attempted {
			return -1
		}

		return 1
	}

	if a.attempted && !a.lastAttempt.Equal(b.lastAttempt) {
		if a.lastAttempt.Before(b.lastAttempt) {
			return -1
		}

		return 1
	}

	return compareIDs(a.id, b.id)
}

func compareValue(a, b candidate) int {
	if a.score != b.score {
		if a.score > b.score {
			return -1
		}

		return 1
	}

	return compareIDs(a.id, b.id)
}

func compareEmit(a, b candidate) int {
	if a.lane != b.lane {
		if a.lane == LaneAging {
			return -1
		}

		return 1
	}

	if a.lane == LaneAging {
		return compareAging(a, b)
	}

	// Longest-processing-time first inside the value lane.
	if a.cost != b.cost {
		if a.cost > b.cost {
			return -1
		}

		return 1
	}

	return compareIDs(a.id, b.id)
}

// planID fingerprints the ordered work list.
//
// Deliberately not covered: PlannedFor, predicted costs, scores and company
// display names. Two plans that run the same sources in the same order are
// interchangeable however they were derived, which is the same rule
// shard.planID follows.
func planID(plan Plan) string {
	digest := sha256.New()
	fmt.Fprintf(digest, "schedule-plan/v%d\nitems=%d\n", PlanSchemaVersion, len(plan.Items))

	for _, item := range plan.Items {
		fmt.Fprintf(digest, "%d\t%s\t%s\n", item.Rank, item.Source.Platform, item.Source.Key)
	}

	return hex.EncodeToString(digest.Sum(nil)[:16])
}

// checkDuplicates refuses a registry that names the same source twice, for the
// same reason shard.Build does: a plan that cannot promise to run a source
// exactly once cannot be merged into a total anybody should believe.
func checkDuplicates(sources []services.Source) error {
	seen := make(map[SourceID]struct{}, len(sources))

	for _, source := range sources {
		id := SourceID{Platform: source.Platform, Key: source.Key}

		if id.Platform == "" || id.Key == "" {
			return fmt.Errorf("build schedule: source %q has an empty platform or key", source.Company)
		}

		if _, ok := seen[id]; ok {
			return fmt.Errorf("build schedule: source %s is registered more than once: a plan cannot promise to refresh it exactly once", id)
		}

		seen[id] = struct{}{}
	}

	return nil
}

// Refs returns the planned sources as shard refs, in execution order.
func (p Plan) Refs() []shard.SourceRef {
	refs := make([]shard.SourceRef, 0, len(p.Items))
	for _, item := range p.Items {
		refs = append(refs, shard.SourceRef{Platform: item.Source.Platform, Key: item.Source.Key, Company: item.Company})
	}

	return refs
}

// Costs returns this plan's predictions keyed the way shard.Build reads them.
//
// The zero ref carries the fallback shard.UnknownSourceCost looks up, so a plan
// handed to shard.Build charges an unlisted source the plan's own median rather
// than a flat default.
func (p Plan) Costs() map[shard.SourceRef]int64 {
	costs := make(map[shard.SourceRef]int64, len(p.Items)+1)

	samples := make([]int64, 0, len(p.Items))

	for _, item := range p.Items {
		ref := shard.SourceRef{Platform: item.Source.Platform, Key: item.Source.Key}
		costs[ref] = item.PredictedMS
		samples = append(samples, item.PredictedMS)
	}

	if len(samples) > 0 {
		costs[shard.SourceRef{}] = median(samples)
	}

	return costs
}

// Sources resolves the plan against a registry, in execution order.
//
// It fails closed on any planned source the binary cannot resolve, the same
// check shard.Plan.Resolve makes: a runner working from a plan built by a
// different build would otherwise report a clean manifest for a subset of what
// it promised.
func (p Plan) Sources(all []services.Source) ([]services.Source, error) {
	byID := make(map[SourceID]services.Source, len(all))
	for _, source := range all {
		byID[SourceID{Platform: source.Platform, Key: source.Key}] = source
	}

	resolved := make([]services.Source, 0, len(p.Items))

	var missing []string

	for _, item := range p.Items {
		source, ok := byID[item.Source]
		if !ok {
			missing = append(missing, item.Source.String())

			continue
		}

		resolved = append(resolved, source)
	}

	if len(missing) > 0 {
		slices.Sort(missing)

		return nil, fmt.Errorf(
			"resolve schedule plan: %d planned sources are not registered in this binary (%s): the plan and the binary disagree, so this run cannot refresh what it promised",
			len(missing), strings.Join(missing, ", "))
	}

	return resolved, nil
}

// Lookup returns the plan's item for one source.
func (p Plan) Lookup(id SourceID) (Item, bool) {
	for _, item := range p.Items {
		if item.Source == id {
			return item, true
		}
	}

	return Item{}, false
}
