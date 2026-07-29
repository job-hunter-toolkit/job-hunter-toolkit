// Package schedule turns a budget and what previous runs measured into an
// ordered work list, and folds a finished run's manifest back into that state.
//
// The crawler's original execution model had two outcomes: one process attempted
// every source once, and the result was either complete or partial. The 07/28/26
// measurement (docs/measurements/2026-07-28-crawl.md) is what makes a third
// outcome necessary. That run crawled 8,235 sources in 877 seconds, and the cost
// was wildly uneven: eight SMB platforms were 51% of the registry and 61% of the
// crawl time for 8.3% of the postings, because each of their tenants is one small
// board behind a shared limiter key. Teamtailor cost 486 seconds per thousand
// postings; Greenhouse cost 4.7. A run that treats those the same spends most of
// its budget on almost none of the corpus.
//
// So a run becomes a bounded amount of work against an accumulated corpus:
// refresh as many sources as the budget allows, most valuable first, stop
// cleanly, and keep what it got. docs/crawl-budget-model.md decides that model
// and docs/design/budget-scheduler.md specifies this mechanism.
//
// Three functions and one file:
//
//   - [Build] is a pure function from (registry, [Store], [Options]) to a [Plan].
//   - [Plan.Gate] decorates a crawl so it declines work it cannot finish rather
//     than being killed mid-source.
//   - [FoldAll] is a pure function from (Store, registry, manifests) to the next
//     Store, which [Encode] writes as one deterministic JSON Lines file.
//
// Nothing here starts a goroutine, reads a clock, or makes a request. Time is a
// field ([Options.Now]), arithmetic is integer, and every iteration order is
// sorted, so the same state and the same budget produce the same plan — byte for
// byte, on every architecture the portability job builds.
//
// This package composes with [shard] rather than duplicating it. Affinity comes
// from [shard.AffinityKeys], which asks [httpx] — there is no second table of
// per-platform concurrency to drift from the limiter that actually enforces it.
// The politeness ceiling stays in httpx: scheduling decides what to fetch, never
// how hard.
package schedule

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// SchemaName and SchemaVersion identify the state file format.
//
// The version is bumped only for a change of meaning. Additive fields never bump
// it, because readers ignore unknown fields and an older binary must keep
// working against a newer file.
const (
	SchemaName    = "job-hunter-toolkit/scheduler-state"
	SchemaVersion = 1
)

// Record kinds, the first field of every line.
const (
	kindHeader = "header"
	kindSource = "source"
	kindGroup  = "group"
)

// MaxSamples is how many trailing observations a source keeps.
//
// Seven is a week. The estimate is the median of them, which is the same choice
// internal/shard/cost.go makes and for the same reason: a median over a week
// already absorbs one anomalous day, so the plan does not reshuffle every time
// one board has a bad night. An EWMA would move on that night; there is no case
// for two estimators.
const MaxSamples = 7

// SourceID is the stable integration identity: platform plus ATS key.
//
// It is exactly the pair [shard.SourceRef] and [services.SourceRun] already key
// on. Company is display text and is never part of identity — conflating the two
// is what once put raw Workday tenant URLs into the user-facing company list.
type SourceID struct {
	Platform string
	Key      string
}

// String renders the id for errors and log lines.
func (id SourceID) String() string { return id.Platform + "/" + id.Key }

func compareIDs(a, b SourceID) int {
	if c := strings.Compare(a.Platform, b.Platform); c != 0 {
		return c
	}

	return strings.Compare(a.Key, b.Key)
}

// SourceState is what one source carries between runs.
//
// Everything here except Group and RetiredAt is already emitted by
// `total --manifest`; this is deliberately the smallest possible delta on the
// manifest that exists, which is what docs/crawl-budget-model.md means by "the
// inputs exist; nothing consumes them across runs".
type SourceState struct {
	Platform string `json:"platform"`
	Key      string `json:"key"`
	Company  string `json:"company,omitempty"`

	// Group is the affinity key this source was last planned under. It is a
	// cache of [shard.AffinityKeys] recorded so a status command can explain a
	// plan without recomputing the registry. [Build] recomputes it and never
	// trusts it.
	Group string `json:"group,omitempty"`

	// LastAttempt advances whenever the crawler actually started this source.
	// LastSuccess advances only on a complete run.
	//
	// Staleness is measured from LastSuccess; back-off is measured from
	// LastAttempt. Conflating them is a real bug, not a nicety: a permanently
	// failing source has no LastSuccess, so an age measured from it grows without
	// bound and the back-off gate eventually stops holding at all.
	//
	// Both are RFC3339 in UTC. Text rather than time.Time so the file diffs
	// readably and so a hand-written fixture is obvious.
	LastAttempt string `json:"last_attempt,omitempty"`
	LastSuccess string `json:"last_success,omitempty"`

	// DurationMS and Postings are trailing samples, oldest first, capped at
	// [MaxSamples]. Durations are clamped on the way in (see [pushDuration]) so
	// one stalled run cannot dictate every future plan.
	DurationMS []int32 `json:"duration_ms,omitempty"`
	Postings   []int32 `json:"postings,omitempty"`

	ConsecutiveFailures int32  `json:"consecutive_failures,omitempty"`
	ErrorClass          string `json:"error_class,omitempty"`

	// Retired marks a source that left the registry. The record is kept so a
	// temporarily removed adapter does not lose its history, and dropped once it
	// has been gone for Policy.RetireAfter.
	Retired   bool   `json:"retired,omitempty"`
	RetiredAt string `json:"retired_at,omitempty"`
}

// ID returns the state's source identity.
func (s SourceState) ID() SourceID { return SourceID{Platform: s.Platform, Key: s.Key} }

// LastAttemptTime parses LastAttempt. ok is false when it is unset or unparseable,
// which both mean "never attempted as far as this file can prove".
func (s SourceState) LastAttemptTime() (t time.Time, ok bool) { return parseStamp(s.LastAttempt) }

// LastSuccessTime parses LastSuccess, with the same contract as LastAttemptTime.
func (s SourceState) LastSuccessTime() (t time.Time, ok bool) { return parseStamp(s.LastSuccess) }

// clone deep-copies a record and canonicalises it.
//
// An empty sample series and an absent one are the same fact, and the file can
// only spell the absent one — omitempty drops "postings":[] on the way out, so a
// store holding a non-nil empty slice would not survive its own round trip.
// Found by the fuzz target, which is exactly the sort of thing a fuzz target on
// a downloaded artifact is for.
func (s SourceState) clone() SourceState {
	s.DurationMS = emptyToNil(slices.Clone(s.DurationMS))
	s.Postings = emptyToNil(slices.Clone(s.Postings))

	return s
}

func emptyToNil(values []int32) []int32 {
	if len(values) == 0 {
		return nil
	}

	return values
}

// GroupState is one affinity group's measured capacity.
//
// ParallelismMilli is how many sources of the group ran concurrently in the
// busiest run observed, in thousandths (4000 means four at a time). It is
// measured from manifests rather than read from a curated table, because a
// second opinion about what httpx already knows is guaranteed to drift and the
// drift looks like a rate-limit problem. See [FoldGroups].
type GroupState struct {
	Key              string `json:"key"`
	ParallelismMilli int32  `json:"parallelism_milli"`
	ObservedAt       string `json:"observed_at,omitempty"`
	Samples          int32  `json:"samples,omitempty"`
}

// Store is the in-memory form of one state file.
//
// The maps are unexported so that nothing can range over them into an artifact;
// every accessor that returns more than one record returns it sorted. That is
// not fussiness — map iteration order is the classic way a "deterministic" plan
// stops being one.
type Store struct {
	// WrittenAt and Writer are stamped into the header. They are fields rather
	// than clock and build lookups so that Encode is a pure function of the
	// Store, which is what makes the file byte-deterministic and reviewable.
	WrittenAt time.Time
	Writer    string

	sources map[SourceID]SourceState
	groups  map[string]GroupState
}

// NewStore returns an empty store: the cold-start state, in which every source
// is maximally stale and no cost is known.
func NewStore() *Store {
	return &Store{
		sources: map[SourceID]SourceState{},
		groups:  map[string]GroupState{},
	}
}

func (s *Store) init() {
	if s.sources == nil {
		s.sources = map[SourceID]SourceState{}
	}

	if s.groups == nil {
		s.groups = map[string]GroupState{}
	}
}

// Len reports how many source records the store holds, retired ones included.
func (s *Store) Len() int {
	if s == nil {
		return 0
	}

	return len(s.sources)
}

// GroupLen reports how many affinity groups have a measured parallelism.
func (s *Store) GroupLen() int {
	if s == nil {
		return 0
	}

	return len(s.groups)
}

// Source returns one source's state.
func (s *Store) Source(id SourceID) (SourceState, bool) {
	if s == nil {
		return SourceState{}, false
	}

	state, ok := s.sources[id]

	return state.clone(), ok
}

// PutSource stores one source's state, replacing any previous record.
func (s *Store) PutSource(state SourceState) {
	s.init()
	s.sources[state.ID()] = state.clone()
}

// DeleteSource removes a source's record entirely. Used for retirement expiry;
// a source that merely failed is never deleted, because losing its failure
// history is how a dead board gets hammered again.
func (s *Store) DeleteSource(id SourceID) {
	if s == nil {
		return
	}

	delete(s.sources, id)
}

// Sources returns every source record sorted by (platform, key).
func (s *Store) Sources() []SourceState {
	if s == nil {
		return nil
	}

	out := make([]SourceState, 0, len(s.sources))
	for _, state := range s.sources {
		out = append(out, state.clone())
	}

	slices.SortFunc(out, func(a, b SourceState) int { return compareIDs(a.ID(), b.ID()) })

	return out
}

// Group returns one affinity group's measured capacity.
func (s *Store) Group(key string) (GroupState, bool) {
	if s == nil {
		return GroupState{}, false
	}

	group, ok := s.groups[key]

	return group, ok
}

// PutGroup stores one affinity group's measured capacity.
func (s *Store) PutGroup(group GroupState) {
	s.init()
	s.groups[group.Key] = group
}

// Groups returns every group record sorted by key.
func (s *Store) Groups() []GroupState {
	if s == nil {
		return nil
	}

	out := make([]GroupState, 0, len(s.groups))
	for _, group := range s.groups {
		out = append(out, group)
	}

	slices.SortFunc(out, func(a, b GroupState) int { return strings.Compare(a.Key, b.Key) })

	return out
}

// Clone returns a deep copy. Fold and its siblings never mutate their input, so
// a caller can always compare before against after.
func (s *Store) Clone() *Store {
	if s == nil {
		return NewStore()
	}

	out := &Store{
		WrittenAt: s.WrittenAt,
		Writer:    s.Writer,
		sources:   make(map[SourceID]SourceState, len(s.sources)),
		groups:    maps.Clone(s.groups),
	}

	for id, state := range s.sources {
		out.sources[id] = state.clone()
	}

	if out.groups == nil {
		out.groups = map[string]GroupState{}
	}

	return out
}

// Wire records. Kind is first so a line is self-describing before it is parsed
// into anything, and the embedded structs keep field order equal to declaration
// order, which is what makes the output diff-stable.

type headerLine struct {
	Kind      string `json:"kind"`
	Schema    string `json:"schema"`
	Version   int    `json:"version"`
	WrittenAt string `json:"written_at,omitempty"`
	Writer    string `json:"writer,omitempty"`
}

type sourceLine struct {
	Kind string `json:"kind"`

	SourceState
}

type groupLine struct {
	Kind string `json:"kind"`

	GroupState
}

// Encode writes the store as JSON Lines: header, then sources sorted by
// (platform, key), then groups sorted by key.
//
// Byte-deterministic for the same store, which is the whole point of the format.
// It makes the file reviewable in a diff, safe to hash into a cache key, and
// makes a golden fixture an exact test rather than an approximate one.
func Encode(w io.Writer, store *Store) error {
	buffered := bufio.NewWriter(w)

	enc := json.NewEncoder(buffered)
	enc.SetEscapeHTML(false)

	header := headerLine{Kind: kindHeader, Schema: SchemaName, Version: SchemaVersion}
	if store != nil {
		header.Writer = store.Writer

		if !store.WrittenAt.IsZero() {
			header.WrittenAt = formatStamp(store.WrittenAt)
		}
	}

	if err := enc.Encode(header); err != nil {
		return fmt.Errorf("encode scheduler state header: %w", err)
	}

	for _, state := range store.Sources() {
		if err := enc.Encode(sourceLine{Kind: kindSource, SourceState: state}); err != nil {
			return fmt.Errorf("encode scheduler state for source %s: %w", state.ID(), err)
		}
	}

	for _, group := range store.Groups() {
		if err := enc.Encode(groupLine{Kind: kindGroup, GroupState: group}); err != nil {
			return fmt.Errorf("encode scheduler state for group %q: %w", group.Key, err)
		}
	}

	return buffered.Flush()
}

// ErrFutureVersion reports a state file written by a newer binary.
//
// It is not a crawl-stopping error. An older binary re-running against newer
// state must degrade to a cold start, because the nightly is the only live
// verification this project has and refusing to crawl loses the day's data.
var ErrFutureVersion = errors.New("scheduler state was written by a newer schema version")

// Origin says where a store came from, for the manifest's state_source field.
type Origin string

const (
	// OriginState means a usable state file was read.
	OriginState Origin = "state"

	// OriginCold means there was no usable state. Every source is maximally
	// stale, no cost is known, and the plan degrades to an ordered full pass
	// bounded by the budget — which is today's behaviour, so a cold start is a
	// worse plan and never a wrong one.
	OriginCold Origin = "cold"
)

// Decode reads a state file strictly, for tests, fuzzing and callers that want
// to know the file was bad. Prefer [Load] in a crawl.
//
// Unknown fields are ignored, so an additive field never needs a version bump.
// Unknown record kinds are skipped and reported in skipped, and are dropped on
// the next Encode: preserving them verbatim would conflict with sorted
// deterministic output, and the cost of dropping them is bounded because a
// missing record is well defined and simply means cold start for that source.
func Decode(r io.Reader) (store *Store, skipped int, err error) {
	store = NewStore()

	scanner := bufio.NewScanner(r)

	// State files are ~190 bytes a row today; a megabyte of slack is generous
	// and still bounded, which matters because this file arrives as a downloaded
	// artifact and is therefore untrusted input.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	line := 0
	sawHeader := false

	for scanner.Scan() {
		line++

		raw := scanner.Bytes()
		if len(strings.TrimSpace(string(raw))) == 0 {
			continue
		}

		var kind struct {
			Kind string `json:"kind"`
		}

		if err := json.Unmarshal(raw, &kind); err != nil {
			return nil, skipped, fmt.Errorf("decode scheduler state line %d: %w", line, err)
		}

		switch kind.Kind {
		case kindHeader:
			var header headerLine
			if err := json.Unmarshal(raw, &header); err != nil {
				return nil, skipped, fmt.Errorf("decode scheduler state header on line %d: %w", line, err)
			}

			if header.Schema != "" && header.Schema != SchemaName {
				return nil, skipped, fmt.Errorf("decode scheduler state line %d: schema %q is not %q", line, header.Schema, SchemaName)
			}

			if header.Version > SchemaVersion {
				return nil, skipped, fmt.Errorf("%w: file is version %d, this binary reads version %d", ErrFutureVersion, header.Version, SchemaVersion)
			}

			store.Writer = header.Writer

			if stamp, ok := parseStamp(header.WrittenAt); ok {
				store.WrittenAt = stamp
			}

			sawHeader = true

		case kindSource:
			var record sourceLine
			if err := json.Unmarshal(raw, &record); err != nil {
				return nil, skipped, fmt.Errorf("decode scheduler state source on line %d: %w", line, err)
			}

			if record.Platform == "" || record.Key == "" {
				return nil, skipped, fmt.Errorf("decode scheduler state line %d: source record has an empty platform or key", line)
			}

			store.PutSource(record.SourceState)

		case kindGroup:
			var record groupLine
			if err := json.Unmarshal(raw, &record); err != nil {
				return nil, skipped, fmt.Errorf("decode scheduler state group on line %d: %w", line, err)
			}

			if record.Key == "" {
				return nil, skipped, fmt.Errorf("decode scheduler state line %d: group record has an empty key", line)
			}

			store.PutGroup(record.GroupState)

		default:
			skipped++
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, skipped, fmt.Errorf("read scheduler state: %w", err)
	}

	if !sawHeader && (store.Len() > 0 || store.GroupLen() > 0) {
		return nil, skipped, errors.New("decode scheduler state: file has records but no header line")
	}

	return store, skipped, nil
}

// Load reads state from r, degrading to a cold start rather than failing.
//
// It never returns a nil store. The returned error is advisory: it explains why
// the store is cold and is never a reason to refuse to crawl. A missing,
// truncated, hand-mangled or future-versioned file all land here, and all mean
// the same thing — this run does not get to prioritise, exactly as every run
// before this package did not.
func Load(r io.Reader) (*Store, Origin, error) {
	store, _, err := Decode(r)
	if err != nil {
		return NewStore(), OriginCold, err
	}

	return store, OriginState, nil
}

// ReadFile loads state from path, degrading to a cold start. A missing file is
// the ordinary first run and is reported with a nil error.
func ReadFile(path string) (*Store, Origin, error) {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return NewStore(), OriginCold, nil
		}

		return NewStore(), OriginCold, fmt.Errorf("open scheduler state %q: %w", path, err)
	}

	defer file.Close()

	store, origin, err := Load(file)
	if err != nil {
		return store, origin, fmt.Errorf("read scheduler state %q: %w", path, err)
	}

	return store, origin, nil
}

// WriteFile writes the store to path atomically, so a reader never observes a
// half-written file. The same temp-and-rename shape internal/shard uses.
func WriteFile(path string, store *Store) error {
	temp, err := os.CreateTemp(filepath.Dir(path), ".scheduler-state-*.jsonl")
	if err != nil {
		return fmt.Errorf("create scheduler state beside %q: %w", path, err)
	}

	tempPath := temp.Name()
	defer func() {
		// A successful rename makes this a harmless no-op. On failure, do not
		// leave a misleading partial file behind.
		_ = os.Remove(tempPath)
	}()

	if err := Encode(temp, store); err != nil {
		_ = temp.Close()

		return err
	}

	if err := temp.Close(); err != nil {
		return fmt.Errorf("close scheduler state %q: %w", path, err)
	}

	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("publish scheduler state %q: %w", path, err)
	}

	return nil
}

func formatStamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func parseStamp(value string) (time.Time, bool) {
	if value == "" {
		return time.Time{}, false
	}

	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, false
	}

	return parsed.UTC(), true
}
