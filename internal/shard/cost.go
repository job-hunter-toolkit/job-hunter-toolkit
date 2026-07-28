package shard

import (
	"slices"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/httpx"
)

// Bounds on a single source's planned cost, in milliseconds.
const (
	// DefaultSourceCostMS is charged to a source with no usable history. It is
	// the median of every sample in the supplied manifests when there is one,
	// and this value only when there is not, so it matters solely for a plan
	// built from manifests that contain no timings at all.
	DefaultSourceCostMS int64 = 1_000

	// MinSourceCostMS keeps a source that finished instantly from being free.
	// A shard packed full of zero-cost sources still has to open every one of
	// those connections.
	MinSourceCostMS int64 = 1

	// MaxSourceCostMS clamps one anomalous observation. The 07/26/26 baseline
	// run stalled ~216 Workday tenants for two minutes each on a semaphore leak
	// and truncated them all; a manifest from a run like that must not be able
	// to dictate the shape of every future plan. Thirty minutes is also past the
	// point where a plan can help: a single source that costs more than a shard
	// budget is a source-level problem, not a packing problem.
	MaxSourceCostMS int64 = 30 * 60 * 1000
)

// costSampleStatuses are the source lifecycle states whose recorded duration
// says something about how expensive that source is.
//
// "planned" is excluded because it never ran. "running" is excluded because its
// duration field is zero, not small. "truncated" and "stopped" are included
// even though their durations are lower bounds: a source that was still going
// when the deadline hit is expensive, and treating that as no information is
// how the most expensive sources end up spread evenly instead of isolated.
var costSampleStatuses = map[string]bool{
	"complete":  true,
	"failed":    true,
	"truncated": true,
	"stopped":   true,
}

// EstimateCosts derives a per-source cost estimate from prior crawl manifests.
//
// Per source it takes the median of that source's usable samples, which is the
// roadmap's "rolling duration estimate" and its thrash cap in one: a median over
// several days cannot be moved far by a single anomalous day, so the plan does
// not reshuffle every time one board has a bad night. Sources absent from every
// manifest are charged the median of all samples, so a newly added company is
// costed like a typical company rather than like nothing.
//
// It returns nil when the manifests carry no usable timings at all, which makes
// [Build] fall back to CostModelUniform rather than pack against noise.
func EstimateCosts(manifests []Manifest) map[SourceRef]int64 {
	samples := map[SourceRef][]int64{}

	var all []int64

	for _, manifest := range manifests {
		for _, run := range manifest.Sources {
			if !costSampleStatuses[run.Status] || run.DurationMS <= 0 {
				continue
			}

			ref := SourceRef{Platform: run.Platform, Key: run.Key}
			cost := clampCost(run.DurationMS)

			samples[ref] = append(samples[ref], cost)
			all = append(all, cost)
		}
	}

	if len(all) == 0 {
		return nil
	}

	fallback := median(all)

	costs := make(map[SourceRef]int64, len(samples)+1)
	for ref, values := range samples {
		costs[ref] = median(values)
	}

	// A zero-valued ref can never match a real source (a source always has a
	// platform), so it is a safe carrier for "the cost of an unknown source".
	costs[SourceRef{}] = fallback

	return costs
}

// UnknownSourceCost returns the cost [Build] will charge a source that the
// estimate does not cover.
func UnknownSourceCost(costs map[SourceRef]int64) int64 {
	if fallback, ok := costs[SourceRef{}]; ok {
		return fallback
	}

	return DefaultSourceCostMS
}

func clampCost(value int64) int64 {
	return min(max(value, MinSourceCostMS), MaxSourceCostMS)
}

func median(values []int64) int64 {
	sorted := slices.Clone(values)
	slices.Sort(sorted)

	middle := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[middle]
	}

	// Averaging the two middle samples keeps an even-length series from
	// silently preferring the cheaper of the pair.
	return (sorted[middle-1] + sorted[middle]) / 2
}

func defaultPerHostLimit() int { return httpx.DefaultPerHostLimit }
