package schedule

import (
	"slices"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/shard"
)

// estimator answers "what will this source cost and what will it return" from
// the trailing samples in a [Store].
//
// It is the same estimator internal/shard/cost.go already applies to a directory
// of manifests — median of clamped trailing samples, with a fallback for sources
// it has never seen — moved onto state that persists across runs and reusing
// shard's own clamp constants so there is exactly one opinion about what a
// source costs. shard/cost.go medians samples it re-reads every time; this
// medians the same samples kept incrementally. Nothing here is a second cost
// model, and shard can consume this one directly through [Plan.Costs].
type estimator struct {
	platformCost  map[string]int64
	platformYield map[string]int64

	globalCost  int64
	globalYield int64

	hasGlobalCost  bool
	hasGlobalYield bool
}

// newEstimator builds the fallback tables once per [Build].
func newEstimator(store *Store) *estimator {
	est := &estimator{
		platformCost:  map[string]int64{},
		platformYield: map[string]int64{},
	}

	var (
		costByPlatform  = map[string][]int64{}
		yieldByPlatform = map[string][]int64{}
		allCosts        []int64
		allYields       []int64
	)

	// Sources() is sorted, so the sample slices are assembled in a fixed order
	// and the percentiles below cannot depend on map iteration.
	for _, state := range store.Sources() {
		for _, sample := range state.DurationMS {
			cost := clampCost(int64(sample))
			costByPlatform[state.Platform] = append(costByPlatform[state.Platform], cost)
			allCosts = append(allCosts, cost)
		}

		for _, sample := range state.Postings {
			yield := int64(max(sample, 0))
			yieldByPlatform[state.Platform] = append(yieldByPlatform[state.Platform], yield)
			allYields = append(allYields, yield)
		}
	}

	for platform, samples := range costByPlatform {
		est.platformCost[platform] = median(samples)
	}

	// A new board on a known platform is charged that platform's 75th percentile
	// yield, not its median: optimism belongs in the value term, where being
	// wrong costs one source's worth of budget, so a new source is tried rather
	// than starved.
	for platform, samples := range yieldByPlatform {
		est.platformYield[platform] = percentile(samples, 75)
	}

	if len(allCosts) > 0 {
		est.globalCost = median(allCosts)
		est.hasGlobalCost = true
	}

	if len(allYields) > 0 {
		est.globalYield = percentile(allYields, 75)
		est.hasGlobalYield = true
	}

	return est
}

// cost returns the predicted duration of one source in milliseconds.
//
// Cost is deliberately never optimistic. Optimism in cost is not conservatism,
// it is a lie to the budget: it admits work the run cannot finish and converts
// it into truncated sources, which cost their full duration and refresh nothing.
func (e *estimator) cost(platform string, state SourceState, known bool) int64 {
	if known && len(state.DurationMS) > 0 {
		samples := make([]int64, 0, len(state.DurationMS))
		for _, sample := range state.DurationMS {
			samples = append(samples, clampCost(int64(sample)))
		}

		return median(samples)
	}

	if cost, ok := e.platformCost[platform]; ok {
		return cost
	}

	if e.hasGlobalCost {
		return e.globalCost
	}

	return shard.DefaultSourceCostMS
}

// yield returns the predicted postings of one source.
func (e *estimator) yield(platform string, state SourceState, known bool) int64 {
	if known && len(state.Postings) > 0 {
		samples := make([]int64, 0, len(state.Postings))
		for _, sample := range state.Postings {
			samples = append(samples, int64(max(sample, 0)))
		}

		return median(samples)
	}

	if yield, ok := e.platformYield[platform]; ok {
		return yield
	}

	if e.hasGlobalYield {
		return e.globalYield
	}

	// One posting, not zero. A source charged zero value can never be admitted by
	// the value lane at all, which would make a brand-new registry with no
	// history rank entirely by lane order.
	return 1
}

// clampCost applies internal/shard's bounds, unchanged: one millisecond because
// a source that finished instantly still costs a connection, thirty minutes
// because one stalled run must not dictate the shape of every future plan.
func clampCost(value int64) int64 {
	return min(max(value, shard.MinSourceCostMS), shard.MaxSourceCostMS)
}

func median(values []int64) int64 {
	sorted := slices.Clone(values)
	slices.Sort(sorted)

	if len(sorted) == 0 {
		return 0
	}

	middle := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[middle]
	}

	// Averaging the two middle samples keeps an even-length series from silently
	// preferring the cheaper of the pair, which is internal/shard/cost.go's rule.
	return (sorted[middle-1] + sorted[middle]) / 2
}

// percentile returns the nearest-rank pth percentile, integer arithmetic only.
func percentile(values []int64, p int) int64 {
	if len(values) == 0 {
		return 0
	}

	sorted := slices.Clone(values)
	slices.Sort(sorted)

	index := (len(sorted)*p + 99) / 100
	if index < 1 {
		index = 1
	}

	if index > len(sorted) {
		index = len(sorted)
	}

	return sorted[index-1]
}
