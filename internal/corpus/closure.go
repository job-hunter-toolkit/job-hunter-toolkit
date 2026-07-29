package corpus

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/jobposting"
)

// Source run statuses, mirroring internal/services/observe.go. They are strings
// rather than an enum because they arrive as strings in a manifest written by
// another binary, possibly an older one.
const (
	StatusComplete  = "complete"
	StatusFailed    = "failed"
	StatusTruncated = "truncated"
	StatusStopped   = "stopped"
	StatusPlanned   = "planned"
)

// MaxTrailing is how many qualifying posting counts a source keeps.
//
// Seven is a week, matching internal/schedule.MaxSamples and internal/shard's
// cost estimate, and for the same reason: a median over a week already absorbs
// one anomalous day, so a single bad night does not move the volume-drop guard.
// It is duplicated rather than imported because internal/schedule is wired to a
// different half of the system and this package must not depend on it.
const MaxTrailing = 7

// Policy holds every tunable that can end a posting's life.
//
// All of the defaults are deliberately conservative, because the two errors are
// not symmetric: a delayed close is cosmetic and self-correcting, and a wrong
// close destroys history that re-crawling cannot recover. docs/design/corpus-format.md
// §11 is explicit that these numbers are judgement rather than measurement, and
// that they should be revisited once a fortnight of runs exists — not before.
type Policy struct {
	// EmptyStreak is how many consecutive qualifying-shaped empty runs a source
	// that normally publishes postings must produce before an empty run is
	// allowed to close anything. On the 07/28 crawl, 174 sources returned zero
	// postings and 166 of them reported status complete; a complete-and-empty run
	// is indistinguishable from the outside between "this company is not hiring"
	// and "the adapter's pagination broke on a response shape it has not seen".
	EmptyStreak int

	// MinRatio is the fraction of a source's trailing median a run must reach
	// before it may close anything. This is the partial-page and broken-pagination
	// case, and it is the guard the roadmap wants moved out of MAX_FAILED_SOURCE_PCT
	// in YAML and into Go — applied per source, where the evidence is, instead of
	// per run.
	MinRatio float64

	// MissingRuns is how many qualifying runs a posting must be absent from before
	// it closes. Two independent runs must agree before a row's life ends.
	MissingRuns int

	// LapseAfter is how long a source can go without a qualifying run before its
	// rows are reported lapsed rather than open. This is what stops a bounded run
	// from either lying about freshness or growing the open set without limit.
	LapseAfter time.Duration

	// FreshnessTarget is how long a source can go without a qualifying run before
	// its rows are reported stale. It matches internal/schedule's DefaultTarget,
	// because "stale" here has to mean the same thing the scheduler means by it or
	// the two halves of the budget model disagree in public.
	FreshnessTarget time.Duration
}

// Policy defaults. See the field comments for why each is what it is.
const (
	DefaultEmptyStreak     = 3
	DefaultMinRatio        = 0.25
	DefaultMissingRuns     = 2
	DefaultLapseAfter      = 90 * 24 * time.Hour
	DefaultFreshnessTarget = 24 * time.Hour
)

// DefaultPolicy returns the conservative defaults.
func DefaultPolicy() Policy {
	return Policy{
		EmptyStreak:     DefaultEmptyStreak,
		MinRatio:        DefaultMinRatio,
		MissingRuns:     DefaultMissingRuns,
		LapseAfter:      DefaultLapseAfter,
		FreshnessTarget: DefaultFreshnessTarget,
	}
}

// withDefaults fills in every unset field, so a caller can pass a zero Policy
// and a caller that sets one field does not silently disable the other four.
func (p Policy) withDefaults() Policy {
	if p.EmptyStreak <= 0 {
		p.EmptyStreak = DefaultEmptyStreak
	}

	if p.MinRatio <= 0 {
		p.MinRatio = DefaultMinRatio
	}

	if p.MissingRuns <= 0 {
		p.MissingRuns = DefaultMissingRuns
	}

	if p.LapseAfter <= 0 {
		p.LapseAfter = DefaultLapseAfter
	}

	if p.FreshnessTarget <= 0 {
		p.FreshnessTarget = DefaultFreshnessTarget
	}

	return p
}

// Reasons a run does or does not qualify as evidence of absence.
const (
	ReasonOK          = "ok"
	ReasonStatus      = "status:"
	ReasonErrors      = "errors"
	ReasonEmptyStreak = "empty-streak-too-short"
	ReasonVolumeDrop  = "volume-drop"
)

// Verdict is why a run may or may not be used as evidence that a posting is
// gone. The reason is carried even when it qualifies, so a caller logging one
// never has to reconstruct it.
type Verdict struct {
	Qualifies bool
	Reason    string
}

// SourceRun is one source's contribution to one crawl, as the corpus needs it.
//
// It mirrors services.SourceRun field for field and JSON tag for JSON tag,
// deliberately rather than importing it: internal/services is the ATS adapters
// and pulls in net/http and the whole crawler, and a corpus reader that runs in
// a browser must link none of that. The duplication is eight fields and it is
// checked by decoding a real crawl manifest in the tests.
type SourceRun struct {
	Platform   string    `json:"platform"`
	Key        string    `json:"key"`
	Company    string    `json:"company"`
	Status     string    `json:"status"`
	StartedAt  time.Time `json:"started_at,omitzero"`
	FinishedAt time.Time `json:"finished_at,omitzero"`
	DurationMS int64     `json:"duration_ms,omitzero"`
	Postings   int       `json:"postings"`
	Errors     int       `json:"errors"`
	ErrorClass string    `json:"error_class,omitempty"`
}

// Source returns the run's integration identity.
func (r SourceRun) Source() jobposting.PostingSource {
	return jobposting.PostingSource{Platform: r.Platform, Key: r.Key}
}

// DecodeManifestSources reads the sources array out of a crawl manifest.
//
// It decodes only that one field, so it accepts a whole-crawl manifest, a shard
// manifest, and any future manifest that keeps the key — which is what lets this
// package consume internal/shard's output without importing internal/shard. A
// manifest with no sources array is an error rather than an empty run: the
// difference between "no source was visited" and "this is not a manifest" is
// exactly the distinction closure must not get wrong.
func DecodeManifestSources(r io.Reader) ([]SourceRun, error) {
	var envelope struct {
		Sources *[]SourceRun `json:"sources"`
	}

	if err := json.NewDecoder(r).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("corpus: decode manifest: %w", err)
	}

	if envelope.Sources == nil {
		return nil, fmt.Errorf("corpus: manifest has no %q array", "sources")
	}

	return *envelope.Sources, nil
}

// SourceState is what the corpus remembers about one integration between runs.
type SourceState struct {
	Source  jobposting.PostingSource `json:"source"`
	Company string                   `json:"company,omitempty"`

	// Retired marks a source that has left the registry. Deleting a source from
	// internal/services must not close its postings, so a retired source freezes
	// its rows at their last known observation and reports them lapsed once
	// LapseAfter elapses, rather than closing them as absent.
	Retired bool `json:"retired,omitempty"`

	LastAttempt time.Time `json:"last_attempt,omitzero"`

	// LastQualifying is the run that was allowed to close this source's postings.
	// It is NOT the last successful run, and conflating the two is how the
	// invariant this package exists to protect gets broken by accident. It is also
	// the value [Corpus.LastSeen] derives an open row's last observation from.
	LastQualifying time.Time `json:"last_qualifying,omitzero"`

	LastDurationMS int64 `json:"last_duration_ms,omitempty"`
	LastPostings   int   `json:"last_postings,omitempty"`

	// Trailing holds the last MaxTrailing qualifying posting counts, oldest
	// first. Its median is what the volume-drop guard compares against.
	Trailing []int `json:"trailing,omitempty"`

	// EmptyStreak counts consecutive qualifying-shaped runs that returned nothing.
	EmptyStreak int `json:"empty_streak,omitempty"`

	// ConsecutiveFailures is one of the three inputs the budget scheduler needs
	// that currently die with the run. LastDurationMS and LastPostings are the
	// other two; the corpus is where they finally survive it.
	ConsecutiveFailures int `json:"consecutive_failures,omitempty"`

	// RequisitionUnsafe is sticky. A source that has ever demoted requisition
	// identity never promotes it again, so a day on which every requisition
	// happened to be distinct cannot re-promote a field that collided yesterday.
	RequisitionUnsafe bool `json:"requisition_unsafe,omitempty"`

	// Open is the number of rows this source currently has in state Open, kept so
	// a consumer can answer "how much does this source contribute" without reading
	// the postings file at all.
	Open int `json:"open"`
}

// TrailingMedian is the volume-drop guard's baseline. It is a median rather than
// a mean so one anomalous day cannot move it, and it returns 0 for a source with
// no history — which is what makes the guard inert on a source's first run
// instead of refusing it.
func (s SourceState) TrailingMedian() int {
	if len(s.Trailing) == 0 {
		return 0
	}

	sorted := slices.Clone(s.Trailing)
	slices.Sort(sorted)

	middle := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[middle]
	}

	return (sorted[middle-1] + sorted[middle]) / 2
}

// Qualifies is the only place in the codebase permitted to decide that a source
// run counts as evidence that a posting is gone.
//
// The four conditions, in order, and what each one is defending against:
//
//  1. Status must be complete. truncated, stopped, failed and planned never
//     qualify. On the 07/28 crawl two sources ended truncated holding 177,296
//     postings — jibe/fedex and jibe/dollargeneral, 21.1% of the run's raw
//     output — and under a budget model hitting a deadline mid-source is the
//     designed behaviour, not an anomaly.
//  2. Errors must be zero. internal/services already sets failed whenever
//     Errors > 0, so this is belt-and-braces against a future adapter that
//     swallows one.
//  3. An empty run from a source that normally publishes postings qualifies only
//     after EmptyStreak consecutive empty runs.
//  4. A run returning less than MinRatio of the source's trailing median does not
//     qualify.
//
// Run against the real 07/28 manifest, this refuses 176 of 3,685 source runs: 2
// truncated, 8 failed and 166 empty-streak. The guard costs 4.8% of source runs
// one cycle of latency and protects a fifth of the corpus.
func Qualifies(run SourceRun, state SourceState, policy Policy) Verdict {
	policy = policy.withDefaults()

	if run.Status != StatusComplete {
		return Verdict{Reason: ReasonStatus + run.Status}
	}

	if run.Errors > 0 {
		return Verdict{Reason: ReasonErrors}
	}

	median := state.TrailingMedian()

	// A source with no history has no baseline, so neither volume guard can fire.
	// Its first run qualifies, which is correct: there is nothing to close.
	if median <= 0 {
		return Verdict{Qualifies: true, Reason: ReasonOK}
	}

	if run.Postings == 0 {
		// state.EmptyStreak counts the runs before this one, so this run is the
		// (EmptyStreak+1)th. Checked before the ratio guard because zero is also
		// below any ratio, and "the board went quiet" and "the parser broke" want
		// different reasons in the log.
		if state.EmptyStreak+1 < policy.EmptyStreak {
			return Verdict{Reason: ReasonEmptyStreak}
		}

		return Verdict{Qualifies: true, Reason: ReasonOK}
	}

	if float64(run.Postings) < policy.MinRatio*float64(median) {
		return Verdict{Reason: ReasonVolumeDrop}
	}

	return Verdict{Qualifies: true, Reason: ReasonOK}
}

// State is a posting's lifecycle state. It is computed, never stored: storing
// derived state invites two writers to disagree with each other, and under a
// budget model where most sources are not visited in most runs, "not closed"
// cannot be allowed to mean "open".
type State uint8

// The four states. Two would not be enough: "we do not know" is a different
// answer from "it closed", and that distinction has to survive into the UI.
const (
	// StateOpen means the posting was present in its source's most recent
	// qualifying run, and that run is inside the source's freshness target.
	StateOpen State = iota

	// StateStale means the source has not had a qualifying run within its
	// freshness target. The posting was open when anyone last looked, and nobody
	// has looked recently.
	StateStale

	// StateClosed means MissingRuns qualifying runs of the posting's own source
	// have failed to see it.
	StateClosed

	// StateLapsed means the source has had no qualifying run for LapseAfter. A
	// lapsed row is dropped from "currently open" counts and never carries a
	// closing date, because nobody observed one.
	StateLapsed
)

func (s State) String() string {
	switch s {
	case StateOpen:
		return "open"
	case StateStale:
		return "stale"
	case StateClosed:
		return "closed"
	case StateLapsed:
		return "lapsed"
	default:
		return fmt.Sprintf("state(%d)", uint8(s))
	}
}

// Closure reasons.
const (
	// ReasonAbsent is the only reason that carries a real closing interval: the
	// posting was there at LastSeen and gone by ConfirmedAt.
	ReasonAbsent = "absent"

	// ReasonLapsed means the source stopped being crawled. Nobody observed a
	// close.
	ReasonLapsed = "lapsed"

	// ReasonRetired means the source left the registry. Nobody observed a close.
	ReasonRetired = "retired"
)
