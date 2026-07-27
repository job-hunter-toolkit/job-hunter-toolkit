package shard

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/services"
)

// ManifestSchemaVersion is the version of the crawl manifest file format.
//
// Version 2 adds the optional "shard" object and changes nothing else. Every
// version 1 field keeps its name, type and meaning, which matters because the
// nightly workflow's Python summarizer reads this file directly and a breaking
// change there reds the job and destroys the day's data row. The bump is for a
// change of meaning, not of shape: in a version 1 manifest "postings" is a
// whole crawl, and in a version 2 manifest carrying a "shard" object it is one
// shard's contribution before global deduplication. A reader that adds those
// numbers up gets a wrong total, so it must be able to tell.
const ManifestSchemaVersion = 2

// Crawl statuses. They are also the fourth column of the jobs record, so the
// strings are a published contract, not an internal detail.
const (
	// StatusComplete means every planned source reached a terminal state within
	// the time budget.
	StatusComplete = "complete"

	// StatusPartial means the crawl hit its deadline. A partial crawl is never
	// recorded or graphed as complete.
	StatusPartial = "partial"
)

// Source lifecycle states that mean a source is finished with, mirroring
// internal/services/observe.go. A manifest listing anything else was written
// before its crawl finished.
var terminalSourceStatuses = map[string]bool{
	"complete":  true,
	"failed":    true,
	"truncated": true,
	"stopped":   true,
}

// ShardStamp identifies which shard of which plan produced a manifest.
//
// It is absent from a whole-crawl manifest, which is how a reader tells a
// summable-once total from one shard's slice of it.
type ShardStamp struct {
	Index int `json:"index"`
	Count int `json:"count"`

	// PlanID and SourceSetID are copied from the plan the shard was run
	// against. The merge requires every shard to agree on both before it will
	// combine them.
	PlanID      string `json:"plan_id"`
	SourceSetID string `json:"source_set_id"`

	// Commit is the shard binary's VCS revision when its build stamped one.
	Commit string `json:"commit,omitempty"`
}

// Manifest is the versioned record of one crawl, or of one shard of one crawl.
//
// Posting counts are before global URL deduplication, so shard manifests must
// be merged through [Merge] rather than added together: the same posting URL
// can arrive through two integrations, and summing is exactly the failure mode
// docs/architecture-roadmap.md calls out.
type Manifest struct {
	SchemaVersion int                  `json:"schema_version"`
	StartedAt     time.Time            `json:"started_at"`
	FinishedAt    time.Time            `json:"finished_at"`
	DurationMS    int64                `json:"duration_ms"`
	Timeout       string               `json:"timeout"`
	Status        string               `json:"status"`
	Postings      int                  `json:"postings"`
	Companies     int                  `json:"companies"`
	SourceCounts  map[string]int       `json:"source_counts"`
	Shard         *ShardStamp          `json:"shard,omitempty"`
	Sources       []services.SourceRun `json:"sources"`
}

// NewManifest builds a manifest from a finished crawl.
func NewManifest(
	startedAt time.Time,
	finishedAt time.Time,
	timeout time.Duration,
	status string,
	postings int,
	companies int,
	sources []services.SourceRun,
) Manifest {
	counts := map[string]int{}
	for _, source := range sources {
		counts[source.Status]++
	}

	return Manifest{
		SchemaVersion: ManifestSchemaVersion,
		StartedAt:     startedAt,
		FinishedAt:    finishedAt,
		DurationMS:    finishedAt.Sub(startedAt).Milliseconds(),
		Timeout:       timeout.String(),
		Status:        status,
		Postings:      postings,
		Companies:     companies,
		SourceCounts:  counts,
		Sources:       sources,
	}
}

// Complete reports whether the manifest describes a crawl that finished.
//
// Both conditions are required. Status alone is decided from the crawl context
// and would miss a source left in "planned" or "running" because the process
// died before it was scheduled; the source scan alone would miss a crawl whose
// context expired after the last source had already stopped.
func (m Manifest) Complete() bool {
	if m.Status != StatusComplete {
		return false
	}

	for _, source := range m.Sources {
		if !terminalSourceStatuses[source.Status] {
			return false
		}
	}

	return true
}

// UnfinishedSources returns the sources that never reached a terminal state.
func (m Manifest) UnfinishedSources() []services.SourceRun {
	var unfinished []services.SourceRun

	for _, source := range m.Sources {
		if !terminalSourceStatuses[source.Status] {
			unfinished = append(unfinished, source)
		}
	}

	return unfinished
}

// WriteManifest writes a manifest to path atomically.
func WriteManifest(path string, manifest Manifest) error {
	return writeJSONAtomic(path, ".crawl-manifest-*.json", manifest, "crawl manifest")
}

// ReadManifest reads a manifest from path without interpreting it. Validation
// belongs to the caller, because what counts as usable differs between a merge
// (which fails closed) and a cost estimate (which skips what it cannot use).
func ReadManifest(path string) (Manifest, error) {
	file, err := os.Open(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("open crawl manifest %q: %w", path, err)
	}
	defer file.Close()

	return DecodeManifest(file, path)
}

// DecodeManifest decodes a manifest from r. name is used in errors.
func DecodeManifest(r io.Reader, name string) (Manifest, error) {
	var manifest Manifest

	if err := json.NewDecoder(r).Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode crawl manifest %q: %w", name, err)
	}

	return manifest, nil
}
