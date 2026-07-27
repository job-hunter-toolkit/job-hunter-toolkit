package shard

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/services"
)

// Artifact file names a shard writes and a merge expects.
//
// The convention is fixed rather than read out of the manifest. A merge
// downloads artifacts produced by other machines, so a path that came from
// inside one of those files is attacker- and accident-controlled input; the
// index is not.
func ManifestFileName(index int) string { return fmt.Sprintf("shard-%d.json", index) }

// PostingsFileName is [ManifestFileName] for a shard's postings stream.
func PostingsFileName(index int) string { return fmt.Sprintf("shard-%d.ndjson", index) }

// ShardArtifacts is one shard's contribution to a merge.
type ShardArtifacts struct {
	Index        int
	ManifestName string
	Manifest     Manifest

	PostingsName string
	OpenPostings func() (io.ReadCloser, error)
}

// MergeOptions configures [Merge].
type MergeOptions struct {
	// AllowPartial downgrades an incomplete shard from a refusal to a total
	// labelled partial. It never turns an incomplete crawl into a complete one,
	// and it does not relax any other check: a missing, mismatched or
	// short-written shard is still refused, because the resulting number would
	// be not merely low but unattributable.
	AllowPartial bool
}

// ShardSummary reports one shard's contribution, for the workflow summary.
type ShardSummary struct {
	Index  int    `json:"index"`
	Status string `json:"status"`

	// Postings is this shard's own count, before global deduplication. It is
	// reported so a human can see how far the per-shard numbers are from the
	// merged total; adding these up is the specific mistake this package
	// exists to prevent.
	Postings   int   `json:"postings"`
	Sources    int   `json:"sources"`
	Unfinished int   `json:"unfinished_sources"`
	DurationMS int64 `json:"duration_ms"`
}

// MergeResult is a merged crawl.
type MergeResult struct {
	// Manifest is a whole-crawl manifest: it carries no shard stamp, and its
	// Postings and Companies are globally deduplicated.
	Manifest Manifest

	// CrossShardDuplicates is how many postings arrived under an identity some
	// other shard had already produced. It is the measured size of the error a
	// naive sum would have made.
	CrossShardDuplicates int

	// PostingsPerPlatform counts deduplicated postings by ATS.
	PostingsPerPlatform map[string]int

	Shards []ShardSummary
}

// Merge combines every shard of a plan into one total, or refuses.
//
// It fails closed. A total is produced only when every shard of the plan is
// present exactly once, was built from the same plan, source set, commit and
// manifest schema, crawled exactly the sources it was assigned, finished (or
// AllowPartial was set), and wrote as many postings as its manifest claims.
//
// Counting is a global union of posting identities, never a sum of per-shard
// totals. The same posting URL can arrive through two integrations — a company
// registered on both Greenhouse and Ashby is not hypothetical here — so adding
// shard counts inflates the record with duplicates that a single-process crawl
// would have collapsed, and the inflation grows silently with coverage.
func Merge(plan Plan, shards []ShardArtifacts, opts MergeOptions) (MergeResult, error) {
	if err := plan.Validate(); err != nil {
		return MergeResult{}, fmt.Errorf("merge shards: %w", err)
	}

	byIndex, err := indexShards(plan, shards)
	if err != nil {
		return MergeResult{}, err
	}

	var (
		seen         = make(map[[PostingKeyBytes]byte]struct{})
		companies    = make(map[string]struct{})
		perPlatform  = map[string]int{}
		duplicates   int
		allSources   []services.SourceRun
		summaries    = make([]ShardSummary, 0, len(byIndex))
		incomplete   []string
		startedAt    time.Time
		finishedAt   time.Time
		budget       time.Duration
		anyStartedAt bool
	)

	for index := 0; index < plan.ShardCount; index++ {
		artifact := byIndex[index]

		if err := checkShardStamp(plan, artifact); err != nil {
			return MergeResult{}, err
		}

		if err := checkShardCoverage(plan, artifact); err != nil {
			return MergeResult{}, err
		}

		if !artifact.Manifest.Complete() {
			incomplete = append(incomplete, describeIncomplete(artifact))
		}

		read, err := mergePostings(artifact, seen, companies, perPlatform, &duplicates)
		if err != nil {
			return MergeResult{}, err
		}

		if read != artifact.Manifest.Postings {
			return MergeResult{}, fmt.Errorf(
				"merge shards: shard %d wrote %d postings to %s but its manifest claims %d: the postings stream is truncated, so its contribution cannot be trusted",
				index, read, artifact.PostingsName, artifact.Manifest.Postings)
		}

		allSources = append(allSources, artifact.Manifest.Sources...)
		summaries = append(summaries, ShardSummary{
			Index:      index,
			Status:     artifact.Manifest.Status,
			Postings:   artifact.Manifest.Postings,
			Sources:    len(artifact.Manifest.Sources),
			Unfinished: len(artifact.Manifest.UnfinishedSources()),
			DurationMS: artifact.Manifest.DurationMS,
		})

		if !artifact.Manifest.StartedAt.IsZero() {
			if !anyStartedAt || artifact.Manifest.StartedAt.Before(startedAt) {
				startedAt = artifact.Manifest.StartedAt
				anyStartedAt = true
			}
		}

		if artifact.Manifest.FinishedAt.After(finishedAt) {
			finishedAt = artifact.Manifest.FinishedAt
		}

		// Shards run in parallel, so the crawl's budget is the longest shard
		// budget, not their sum.
		if parsed, err := time.ParseDuration(artifact.Manifest.Timeout); err == nil && parsed > budget {
			budget = parsed
		}
	}

	status := StatusComplete
	if len(incomplete) > 0 {
		if !opts.AllowPartial {
			return MergeResult{}, fmt.Errorf(
				"merge shards: %d of %d shards did not finish (%s): a partial crawl must not be merged into a total that looks complete",
				len(incomplete), plan.ShardCount, strings.Join(incomplete, "; "))
		}

		status = StatusPartial
	}

	slices.SortFunc(allSources, func(a, b services.SourceRun) int {
		if c := strings.Compare(a.Platform, b.Platform); c != 0 {
			return c
		}

		return strings.Compare(a.Key, b.Key)
	})

	manifest := NewManifest(
		startedAt,
		finishedAt,
		budget,
		status,
		len(seen),
		len(companies),
		allSources,
	)

	return MergeResult{
		Manifest:             manifest,
		CrossShardDuplicates: duplicates,
		PostingsPerPlatform:  perPlatform,
		Shards:               summaries,
	}, nil
}

// indexShards checks that the supplied artifacts are exactly the plan's shards.
func indexShards(plan Plan, shards []ShardArtifacts) (map[int]ShardArtifacts, error) {
	byIndex := make(map[int]ShardArtifacts, len(shards))

	for _, artifact := range shards {
		if artifact.Index < 0 || artifact.Index >= plan.ShardCount {
			return nil, fmt.Errorf(
				"merge shards: artifact %q claims shard %d, which is outside the plan's %d shards",
				artifact.ManifestName, artifact.Index, plan.ShardCount)
		}

		if existing, ok := byIndex[artifact.Index]; ok {
			return nil, fmt.Errorf(
				"merge shards: shard %d was supplied twice (%q and %q): merging both would count its postings twice",
				artifact.Index, existing.ManifestName, artifact.ManifestName)
		}

		byIndex[artifact.Index] = artifact
	}

	var missing []string

	for index := 0; index < plan.ShardCount; index++ {
		if _, ok := byIndex[index]; !ok {
			missing = append(missing, fmt.Sprintf("%d (%d sources)", index, len(plan.Shards[index].Sources)))
		}
	}

	if len(missing) > 0 {
		return nil, fmt.Errorf(
			"merge shards: %d of %d shards are missing: %s. A total that silently omits them would be recorded as a real drop in the job market",
			len(missing), plan.ShardCount, strings.Join(missing, ", "))
	}

	return byIndex, nil
}

// checkShardStamp verifies that a shard was produced from this plan by a
// compatible binary.
func checkShardStamp(plan Plan, artifact ShardArtifacts) error {
	manifest := artifact.Manifest

	if manifest.SchemaVersion != ManifestSchemaVersion {
		return fmt.Errorf(
			"merge shards: shard %d manifest %q has schema version %d, want %d: its fields may not mean what this binary thinks they mean",
			artifact.Index, artifact.ManifestName, manifest.SchemaVersion, ManifestSchemaVersion)
	}

	if manifest.Shard == nil {
		return fmt.Errorf(
			"merge shards: shard %d manifest %q carries no shard stamp: it is a whole-crawl manifest, and merging it would double-count sources some other shard also crawled",
			artifact.Index, artifact.ManifestName)
	}

	stamp := manifest.Shard

	if stamp.Index != artifact.Index {
		return fmt.Errorf(
			"merge shards: manifest %q is shard %d but was supplied as shard %d",
			artifact.ManifestName, stamp.Index, artifact.Index)
	}

	if stamp.Count != plan.ShardCount {
		return fmt.Errorf(
			"merge shards: shard %d ran against a %d-shard plan but this plan has %d shards",
			artifact.Index, stamp.Count, plan.ShardCount)
	}

	if stamp.PlanID != plan.PlanID {
		return fmt.Errorf(
			"merge shards: shard %d ran plan %q but this is plan %q: two plans can assign the same source to different shards, so their results cannot be combined",
			artifact.Index, stamp.PlanID, plan.PlanID)
	}

	if stamp.SourceSetID != plan.SourceSetID {
		return fmt.Errorf(
			"merge shards: shard %d saw source set %q but the plan was built from %q: the shards did not agree on what a full crawl is",
			artifact.Index, stamp.SourceSetID, plan.SourceSetID)
	}

	if stamp.Commit != plan.Commit {
		return fmt.Errorf(
			"merge shards: shard %d was built from commit %q but the plan from %q: adapters can change what they return between commits",
			artifact.Index, orNone(stamp.Commit), orNone(plan.Commit))
	}

	return nil
}

// checkShardCoverage verifies that a shard crawled exactly its assignment.
//
// Both directions matter. A source the shard skipped is missing coverage a
// naive merge would report as postings disappearing from the market. A source
// the shard crawled without being assigned it means two processes were talking
// to that backend at once, which is the pressure increase the whole affinity
// design exists to prevent — and it is worth failing the run to surface.
func checkShardCoverage(plan Plan, artifact ShardArtifacts) error {
	planned := make(map[SourceRef]struct{}, len(plan.Shards[artifact.Index].Sources))
	for _, ref := range plan.Shards[artifact.Index].Sources {
		planned[ref.identity()] = struct{}{}
	}

	var extra []string

	for _, run := range artifact.Manifest.Sources {
		ref := SourceRef{Platform: run.Platform, Key: run.Key}

		if _, ok := planned[ref]; !ok {
			extra = append(extra, ref.String())

			continue
		}

		delete(planned, ref)
	}

	if len(extra) > 0 {
		slices.Sort(extra)

		return fmt.Errorf(
			"merge shards: shard %d crawled %d sources it was not assigned (%s): another shard was assigned them, so that backend saw two concurrent crawlers",
			artifact.Index, len(extra), strings.Join(truncateList(extra, 10), ", "))
	}

	if len(planned) > 0 {
		missing := make([]string, 0, len(planned))
		for ref := range planned {
			missing = append(missing, ref.String())
		}

		slices.Sort(missing)

		return fmt.Errorf(
			"merge shards: shard %d omitted %d of its planned sources (%s): the plan was not covered",
			artifact.Index, len(missing), strings.Join(truncateList(missing, 10), ", "))
	}

	return nil
}

// mergePostings folds one shard's postings stream into the global sets.
func mergePostings(
	artifact ShardArtifacts,
	seen map[[PostingKeyBytes]byte]struct{},
	companies map[string]struct{},
	perPlatform map[string]int,
	duplicates *int,
) (int, error) {
	if artifact.OpenPostings == nil {
		return 0, fmt.Errorf("merge shards: shard %d has no postings stream", artifact.Index)
	}

	stream, err := artifact.OpenPostings()
	if err != nil {
		return 0, fmt.Errorf("merge shards: open shard %d postings: %w", artifact.Index, err)
	}
	defer stream.Close()

	return readPostings(stream, artifact.PostingsName, func(record PostingRecord) error {
		key, err := decodePostingKey(record.Key)
		if err != nil {
			return fmt.Errorf("merge shards: shard %d postings %q: %w", artifact.Index, artifact.PostingsName, err)
		}

		if _, ok := seen[key]; ok {
			*duplicates++

			return nil
		}

		seen[key] = struct{}{}
		companies[record.Company] = struct{}{}
		perPlatform[record.Platform]++

		return nil
	})
}

func decodePostingKey(encoded string) ([PostingKeyBytes]byte, error) {
	var key [PostingKeyBytes]byte

	if len(encoded) != PostingKeyBytes*2 {
		return key, fmt.Errorf("posting key %q is %d characters, want %d", encoded, len(encoded), PostingKeyBytes*2)
	}

	for i := 0; i < PostingKeyBytes; i++ {
		high, lowErr := hexNibble(encoded[i*2])
		low, highErr := hexNibble(encoded[i*2+1])

		if err := errors.Join(lowErr, highErr); err != nil {
			return key, fmt.Errorf("posting key %q is not hexadecimal: %w", encoded, err)
		}

		key[i] = high<<4 | low
	}

	return key, nil
}

func hexNibble(c byte) (byte, error) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', nil
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, nil
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, nil
	}

	return 0, fmt.Errorf("invalid hex digit %q", string(rune(c)))
}

// MergeDir merges the shard artifacts written under dir by [ManifestFileName]
// and [PostingsFileName].
func MergeDir(dir string, plan Plan, opts MergeOptions) (MergeResult, error) {
	shards := make([]ShardArtifacts, 0, plan.ShardCount)

	for index := 0; index < plan.ShardCount; index++ {
		var (
			manifestPath = filepath.Join(dir, ManifestFileName(index))
			postingsPath = filepath.Join(dir, PostingsFileName(index))
		)

		manifest, err := ReadManifest(manifestPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				// Keep going so the error names every missing shard at once
				// rather than one per re-run of a six-hour workflow.
				continue
			}

			return MergeResult{}, fmt.Errorf("merge shards: %w", err)
		}

		if _, err := os.Stat(postingsPath); err != nil {
			return MergeResult{}, fmt.Errorf(
				"merge shards: shard %d has a manifest but no postings stream at %q: %w",
				index, postingsPath, err)
		}

		shards = append(shards, ShardArtifacts{
			Index:        index,
			ManifestName: manifestPath,
			Manifest:     manifest,
			PostingsName: postingsPath,
			OpenPostings: func() (io.ReadCloser, error) { return os.Open(postingsPath) },
		})
	}

	return Merge(plan, shards, opts)
}

func describeIncomplete(artifact ShardArtifacts) string {
	unfinished := artifact.Manifest.UnfinishedSources()

	return fmt.Sprintf("shard %d status %s with %d sources still unfinished",
		artifact.Index, orNone(artifact.Manifest.Status), len(unfinished))
}

func truncateList(values []string, limit int) []string {
	if len(values) <= limit {
		return values
	}

	return append(slices.Clone(values[:limit]), fmt.Sprintf("and %d more", len(values)-limit))
}

func orNone(value string) string {
	if value == "" {
		return "(unset)"
	}

	return value
}
