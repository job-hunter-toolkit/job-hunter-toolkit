// Package shard splits one crawl across several processes without letting the
// split change what the crawl means.
//
// A full crawl of ~2,131 companies does not finish on a single GitHub runner: a
// real run on 07/26/26 recorded "473404 1772 partial" after 350 minutes and was
// still incomplete. More wall clock on one machine is not the fix, and neither
// is naive parallelism. Most platforms here are one shared backend — all 647
// Greenhouse boards are boards-api.greenhouse.io, all 418 Ashby boards are
// api.ashbyhq.com — and every shard is a separate process with a private
// [httpx] limiter, so splitting one of those across two runners does not halve
// its time, it doubles the pressure that backend feels. This package therefore
// plans by service affinity (see [AffinityKeys]) and takes its parallelism from
// the platforms whose tenants really are on independent hosts.
//
// The three capabilities are deliberately separate: [Build] produces a
// reproducible plan, a crawl runs one shard of it, and [Merge] reassembles the
// shards while refusing to invent a total it cannot justify.
package shard

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/services"
)

// PlanSchemaVersion is the version of the shard plan file format.
//
// A merge refuses a plan it does not recognise rather than guessing, because
// the failure mode of a misread plan is a total that looks fine and is wrong.
const PlanSchemaVersion = 1

// Cost models a plan can be built with.
const (
	// CostModelUniform weights every source equally. It is the deterministic
	// fallback when no prior manifest is supplied.
	CostModelUniform = "uniform"

	// CostModelDuration weights each source by a rolling estimate of its
	// measured duration, taken from prior crawl manifests.
	CostModelDuration = "duration_ms"
)

// SourceRef names one crawlable source in a plan.
//
// Platform and Key are the identity; Company is carried for humans reading the
// plan and for the workflow summary, and is never used to resolve a ref. Source,
// company and ATS identity are separate concepts, and conflating them is what
// once put raw Workday tenant URLs into the user-facing company list.
type SourceRef struct {
	Platform string `json:"platform"`
	Key      string `json:"key"`
	Company  string `json:"company,omitempty"`
}

// identity returns the two fields that decide whether two refs are the same
// source.
func (r SourceRef) identity() SourceRef {
	return SourceRef{Platform: r.Platform, Key: r.Key}
}

// String renders the ref for error messages.
func (r SourceRef) String() string {
	return r.Platform + "/" + r.Key
}

// Shard is one process's worth of a planned crawl.
type Shard struct {
	Index int `json:"index"`

	// AffinityKeys are the backends this shard is solely responsible for. Every
	// key appears in exactly one shard, which is what keeps each shared
	// backend's limiter and 429 cooldown globally effective for the run.
	AffinityKeys []string `json:"affinity_keys"`

	// EstimatedMS is the plan's cost estimate for this shard, in the units of
	// the plan's cost model. Under CostModelUniform it is a source count.
	EstimatedMS int64 `json:"estimated_ms"`

	Sources []SourceRef `json:"sources"`
}

// Plan assigns every source of a crawl to exactly one shard.
type Plan struct {
	SchemaVersion int `json:"schema_version"`

	// PlanID fingerprints the shard assignment. A crawl stamps it into its
	// manifest and the merge refuses shards that do not all carry the same one,
	// which is how two runners working from different plans are caught before
	// their counts are added together.
	PlanID string `json:"plan_id"`

	// SourceSetID fingerprints the full source registry the plan was built
	// from. A shard verifies it against its own binary before crawling, so a
	// runner with a stale build fails loudly instead of silently skipping the
	// sources it has never heard of.
	SourceSetID string `json:"source_set_id"`

	// Commit is the VCS revision of the planning binary when the build stamped
	// one. It is informational: SourceSetID is the check that actually works,
	// because it holds even for a binary built with -buildvcs=false.
	Commit string `json:"commit,omitempty"`

	CreatedAt   time.Time `json:"created_at,omitzero"`
	ShardCount  int       `json:"shard_count"`
	CostModel   string    `json:"cost_model"`
	SourceCount int       `json:"source_count"`
	Shards      []Shard   `json:"shards"`
}

// Options configures [Build].
type Options struct {
	// ShardCount is how many shards to produce. It must be at least 1.
	ShardCount int

	// PerHostLimit is passed to httpx when deriving affinity keys. Zero uses
	// httpx.DefaultPerHostLimit.
	PerHostLimit int

	// Costs is an optional per-source cost estimate, keyed by source identity.
	// A nil or empty map selects CostModelUniform.
	Costs map[SourceRef]int64

	// Commit records the planning binary's revision, when known.
	Commit string

	// SourceSetID overrides the fingerprint stamped into the plan.
	//
	// It exists because a plan may deliberately cover a subset of the registry
	// (`shard plan --company ...`), while the thing worth fingerprinting is
	// always the whole registry the binary carries: that is what a shard runner
	// can independently recompute, and disagreeing about it is what "the plan
	// and the binary are not the same build" actually looks like. Empty means
	// fingerprint the planned sources.
	SourceSetID string
}

// Build assigns sources to ShardCount shards.
//
// The result is a pure function of its inputs: no clock, no map iteration
// order, no randomness. Same sources and same options always produce the same
// plan, including the same PlanID, so a re-plan on a retried workflow run does
// not silently re-shuffle a crawl that is already half done.
//
// Assignment is longest-processing-time-first bin packing over affinity groups,
// not over sources. Groups are the indivisible unit — see [AffinityKeys] — so
// the largest single group is a hard floor on the critical path. With
// Greenhouse's 647 sources on one backend that floor is real and sharding
// cannot lower it; what sharding buys is that the other ~1,500 sources stop
// queueing behind it, plus fault isolation and a merge-time integrity proof.
func Build(sources []services.Source, opts Options) (Plan, error) {
	if opts.ShardCount < 1 {
		return Plan{}, fmt.Errorf("build shard plan: shard count %d is invalid: a crawl needs at least one shard", opts.ShardCount)
	}

	if len(sources) == 0 {
		return Plan{}, fmt.Errorf("build shard plan: no sources to plan: refusing to emit a plan that would crawl nothing")
	}

	perHostLimit := opts.PerHostLimit
	if perHostLimit < 1 {
		perHostLimit = defaultPerHostLimit()
	}

	refs, err := refsFrom(sources)
	if err != nil {
		return Plan{}, err
	}

	// Group by affinity, keeping every group's members in a deterministic order.
	var (
		keys   = AffinityKeys(sources, perHostLimit)
		groups = map[AffinityKey][]SourceRef{}
	)

	for i, ref := range refs {
		groups[keys[i]] = append(groups[keys[i]], ref)
	}

	costModel := CostModelUniform
	if len(opts.Costs) > 0 {
		costModel = CostModelDuration
	}

	unknownCost := UnknownSourceCost(opts.Costs)

	type bin struct {
		key   AffinityKey
		cost  int64
		items []SourceRef
	}

	bins := make([]bin, 0, len(groups))

	for key, items := range groups {
		slices.SortFunc(items, compareRefs)

		var cost int64
		for _, ref := range items {
			cost += sourceCost(opts.Costs, ref, costModel, unknownCost)
		}

		bins = append(bins, bin{key: key, cost: cost, items: items})
	}

	// Descending cost, then key, so ties never depend on map order.
	slices.SortFunc(bins, func(a, b bin) int {
		if a.cost != b.cost {
			return int(b.cost - a.cost)
		}

		return strings.Compare(string(a.key), string(b.key))
	})

	shards := make([]Shard, opts.ShardCount)
	for i := range shards {
		shards[i] = Shard{Index: i}
	}

	for _, packed := range bins {
		target := 0
		for i := 1; i < len(shards); i++ {
			if shards[i].EstimatedMS < shards[target].EstimatedMS {
				target = i
			}
		}

		shards[target].AffinityKeys = append(shards[target].AffinityKeys, string(packed.key))
		shards[target].EstimatedMS += packed.cost
		shards[target].Sources = append(shards[target].Sources, packed.items...)
	}

	for i := range shards {
		slices.Sort(shards[i].AffinityKeys)
		slices.SortFunc(shards[i].Sources, compareRefs)
	}

	sourceSetID := opts.SourceSetID
	if sourceSetID == "" {
		sourceSetID = SourceSetID(sources)
	}

	plan := Plan{
		SchemaVersion: PlanSchemaVersion,
		SourceSetID:   sourceSetID,
		Commit:        opts.Commit,
		ShardCount:    opts.ShardCount,
		CostModel:     costModel,
		SourceCount:   len(refs),
		Shards:        shards,
	}
	plan.PlanID = planID(plan)

	return plan, nil
}

// sourceCost returns the planner's weight for one source.
func sourceCost(costs map[SourceRef]int64, ref SourceRef, model string, unknown int64) int64 {
	if model == CostModelUniform {
		return 1
	}

	if cost, ok := costs[ref.identity()]; ok {
		return cost
	}

	// A source with no history still costs something. Charging it zero would let
	// a shard collect every new source in the registry for free.
	return unknown
}

func compareRefs(a, b SourceRef) int {
	if c := strings.Compare(a.Platform, b.Platform); c != 0 {
		return c
	}

	return strings.Compare(a.Key, b.Key)
}

// refsFrom converts sources to refs, rejecting a registry that names the same
// source twice.
//
// A duplicate would be assigned to one shard and counted once, which is
// harmless, or split across two, which is not. Either way the plan's "every
// source exactly once" coverage proof stops meaning anything, so the planner
// refuses rather than silently picking an interpretation.
func refsFrom(sources []services.Source) ([]SourceRef, error) {
	var (
		refs = make([]SourceRef, 0, len(sources))
		seen = make(map[SourceRef]struct{}, len(sources))
	)

	for _, source := range sources {
		ref := SourceRef{Platform: source.Platform, Key: source.Key, Company: source.Company}

		if _, ok := seen[ref.identity()]; ok {
			return nil, fmt.Errorf("build shard plan: source %s is registered more than once: a plan cannot promise to crawl it exactly once", ref)
		}

		seen[ref.identity()] = struct{}{}
		refs = append(refs, ref)
	}

	return refs, nil
}

// SourceSetID fingerprints a source registry.
//
// It covers identity only — platform and ATS key — so that renaming a company's
// display string does not invalidate every shard mid-run, while adding,
// removing or re-keying a source does.
func SourceSetID(sources []services.Source) string {
	identities := make([]string, 0, len(sources))
	for _, source := range sources {
		identities = append(identities, source.Platform+"\x00"+source.Key)
	}

	slices.Sort(identities)

	digest := sha256.New()
	fmt.Fprintf(digest, "source-set/v%d\n", PlanSchemaVersion)

	for _, identity := range identities {
		fmt.Fprintf(digest, "%s\n", identity)
	}

	return hex.EncodeToString(digest.Sum(nil)[:16])
}

// planID fingerprints a shard assignment.
//
// Deliberately not covered: CreatedAt, Commit, CostModel and company display
// names. Two plans that put the same sources in the same shards are
// interchangeable no matter how or when they were derived, and a merge that
// rejected them would fail closed on a difference that cannot affect a count.
func planID(plan Plan) string {
	digest := sha256.New()
	fmt.Fprintf(digest, "shard-plan/v%d\nshards=%d\n", PlanSchemaVersion, plan.ShardCount)

	for _, shard := range plan.Shards {
		for _, ref := range shard.Sources {
			fmt.Fprintf(digest, "%d\t%s\t%s\n", shard.Index, ref.Platform, ref.Key)
		}
	}

	return hex.EncodeToString(digest.Sum(nil)[:16])
}

// Validate reports whether a decoded plan is internally consistent.
//
// A plan arrives at a shard and at the merge as a downloaded artifact, so it is
// treated as untrusted input: a truncated or hand-edited plan must fail here
// rather than produce a crawl that quietly skips sources.
func (p Plan) Validate() error {
	if p.SchemaVersion != PlanSchemaVersion {
		return fmt.Errorf("shard plan schema version %d is not supported: this binary reads version %d", p.SchemaVersion, PlanSchemaVersion)
	}

	if p.ShardCount < 1 || len(p.Shards) != p.ShardCount {
		return fmt.Errorf("shard plan declares %d shards but carries %d", p.ShardCount, len(p.Shards))
	}

	seen := make(map[SourceRef]int, p.SourceCount)
	total := 0

	for i, shard := range p.Shards {
		if shard.Index != i {
			return fmt.Errorf("shard plan entry %d has index %d: shards must be listed in index order", i, shard.Index)
		}

		for _, ref := range shard.Sources {
			if ref.Platform == "" || ref.Key == "" {
				return fmt.Errorf("shard plan shard %d contains a source with an empty platform or key", i)
			}

			if other, ok := seen[ref.identity()]; ok {
				return fmt.Errorf("shard plan assigns source %s to shards %d and %d: a source crawled twice is counted twice and doubles that backend's request rate", ref, other, i)
			}

			seen[ref.identity()] = i
			total++
		}
	}

	if p.SourceCount != total {
		return fmt.Errorf("shard plan declares %d sources but assigns %d", p.SourceCount, total)
	}

	if got := planID(p); got != p.PlanID {
		return fmt.Errorf("shard plan id %q does not match its own assignment (recomputed %q): the plan was edited after it was built", p.PlanID, got)
	}

	return nil
}

// ShardSources returns the refs assigned to one shard index.
func (p Plan) ShardSources(index int) ([]SourceRef, error) {
	if index < 0 || index >= len(p.Shards) {
		return nil, fmt.Errorf("shard index %d is outside the plan's %d shards", index, len(p.Shards))
	}

	return p.Shards[index].Sources, nil
}

// Resolve returns the sources of one shard, in the order they appear in all.
//
// Order is taken from all rather than from the plan on purpose: the registry
// hands back a platform round-robin so a crawl's opening wave spreads over
// backends instead of hammering one, and re-sorting a shard by platform would
// throw that away.
//
// It fails closed on any ref the binary cannot resolve. That is the check that
// catches a runner crawling a plan built by a different build: the shard would
// otherwise report a clean, complete manifest for a subset of what it promised.
func (p Plan) Resolve(index int, all []services.Source) ([]services.Source, error) {
	refs, err := p.ShardSources(index)
	if err != nil {
		return nil, err
	}

	wanted := make(map[SourceRef]struct{}, len(refs))
	for _, ref := range refs {
		wanted[ref.identity()] = struct{}{}
	}

	resolved := make([]services.Source, 0, len(refs))

	for _, source := range all {
		identity := SourceRef{Platform: source.Platform, Key: source.Key}
		if _, ok := wanted[identity]; !ok {
			continue
		}

		delete(wanted, identity)
		resolved = append(resolved, source)
	}

	if len(wanted) > 0 {
		missing := make([]string, 0, len(wanted))
		for ref := range wanted {
			missing = append(missing, ref.String())
		}

		slices.Sort(missing)

		return nil, fmt.Errorf(
			"resolve shard %d: %d planned sources are not registered in this binary (%s): the plan and the binary disagree, so this shard cannot crawl what it promised",
			index, len(missing), strings.Join(missing, ", "))
	}

	return resolved, nil
}

// WritePlan writes a plan to path atomically.
func WritePlan(path string, plan Plan) error {
	return writeJSONAtomic(path, ".shard-plan-*.json", plan, "shard plan")
}

// ReadPlan reads and validates a plan from path.
func ReadPlan(path string) (Plan, error) {
	file, err := os.Open(path)
	if err != nil {
		return Plan{}, fmt.Errorf("open shard plan %q: %w", path, err)
	}
	defer file.Close()

	return DecodePlan(file, path)
}

// DecodePlan reads and validates a plan from r. name is used in errors.
func DecodePlan(r io.Reader, name string) (Plan, error) {
	var plan Plan

	dec := json.NewDecoder(r)
	if err := dec.Decode(&plan); err != nil {
		return Plan{}, fmt.Errorf("decode shard plan %q: %w", name, err)
	}

	if err := plan.Validate(); err != nil {
		return Plan{}, fmt.Errorf("shard plan %q is not usable: %w", name, err)
	}

	return plan, nil
}

// MatrixIndexes returns [0, 1, ... ShardCount-1], the value a GitHub Actions
// matrix expands with fromJSON.
func (p Plan) MatrixIndexes() []int {
	indexes := make([]int, p.ShardCount)
	for i := range indexes {
		indexes[i] = i
	}

	return indexes
}

// writeJSONAtomic encodes value to a temporary file beside path and renames it
// into place, so a reader never observes a half-written artifact.
func writeJSONAtomic(path, pattern string, value any, what string) error {
	temp, err := os.CreateTemp(filepath.Dir(path), pattern)
	if err != nil {
		return fmt.Errorf("create %s beside %q: %w", what, path, err)
	}

	tempPath := temp.Name()
	defer func() {
		// A successful rename makes this a harmless no-op. On failure, do not
		// leave a misleading partial file behind.
		_ = os.Remove(tempPath)
	}()

	enc := json.NewEncoder(temp)
	enc.SetIndent("", "  ")

	if err := enc.Encode(value); err != nil {
		_ = temp.Close()

		return fmt.Errorf("encode %s %q: %w", what, path, err)
	}

	if err := temp.Close(); err != nil {
		return fmt.Errorf("close %s %q: %w", what, path, err)
	}

	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("publish %s %q: %w", what, path, err)
	}

	return nil
}

func sortStrings(values []string) { slices.Sort(values) }
