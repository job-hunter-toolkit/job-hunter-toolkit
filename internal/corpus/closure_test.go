package corpus

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/jobposting"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestQualifies(t *testing.T) {
	t.Parallel()

	// A source with a week of history at ~100 postings a night. The median is what
	// both volume guards compare against.
	history := SourceState{
		Source:   source("greenhouse", "acme"),
		Trailing: []int{98, 101, 100, 99, 102, 100, 100},
	}

	cases := []struct {
		name       string
		run        SourceRun
		state      SourceState
		wantOK     bool
		wantReason string
	}{
		{
			name:       "a complete run with a normal count qualifies",
			run:        SourceRun{Status: StatusComplete, Postings: 100},
			state:      history,
			wantOK:     true,
			wantReason: ReasonOK,
		},
		{
			// jibe/fedex and jibe/dollargeneral ended truncated on 07/28 holding
			// 177,296 postings between them — 21.1% of the run. Under a budget model
			// hitting the deadline mid-source is the designed behaviour.
			name:       "a truncated run never qualifies",
			run:        SourceRun{Status: StatusTruncated, Postings: 103_196},
			state:      history,
			wantReason: ReasonStatus + StatusTruncated,
		},
		{
			// Seven of the 07/28 failures were Workday tenants with one shared
			// error: a platform-side event, not seven companies withdrawing every
			// job at once.
			name:       "a failed run never qualifies",
			run:        SourceRun{Status: StatusFailed, Postings: 0},
			state:      history,
			wantReason: ReasonStatus + StatusFailed,
		},
		{
			name:       "a stopped run never qualifies",
			run:        SourceRun{Status: StatusStopped, Postings: 100},
			state:      history,
			wantReason: ReasonStatus + StatusStopped,
		},
		{
			name:       "a planned run never qualifies",
			run:        SourceRun{Status: StatusPlanned},
			state:      history,
			wantReason: ReasonStatus + StatusPlanned,
		},
		{
			// Belt and braces against a future adapter that swallows an error and
			// still reports complete.
			name:       "errors disqualify even a complete run",
			run:        SourceRun{Status: StatusComplete, Postings: 100, Errors: 1},
			state:      history,
			wantReason: ReasonErrors,
		},
		{
			name:       "a first empty run from a busy source does not qualify",
			run:        SourceRun{Status: StatusComplete, Postings: 0},
			state:      history,
			wantReason: ReasonEmptyStreak,
		},
		{
			name:       "a second empty run still does not qualify",
			run:        SourceRun{Status: StatusComplete, Postings: 0},
			state:      SourceState{Trailing: history.Trailing, EmptyStreak: 1},
			wantOK:     false,
			wantReason: ReasonEmptyStreak,
		},
		{
			// Three consecutive quiet runs is the point at which "this company is
			// not hiring" becomes the better explanation than "the adapter's
			// pagination broke on a response shape it has not seen".
			name:       "the third empty run qualifies",
			run:        SourceRun{Status: StatusComplete, Postings: 0},
			state:      SourceState{Trailing: history.Trailing, EmptyStreak: 2},
			wantOK:     true,
			wantReason: ReasonOK,
		},
		{
			name:       "a run far below the trailing median does not qualify",
			run:        SourceRun{Status: StatusComplete, Postings: 24},
			state:      history,
			wantReason: ReasonVolumeDrop,
		},
		{
			name:       "a run exactly at the ratio qualifies",
			run:        SourceRun{Status: StatusComplete, Postings: 25},
			state:      history,
			wantOK:     true,
			wantReason: ReasonOK,
		},
		{
			// A source with no history has no baseline, so neither volume guard can
			// fire. Its first run qualifies, which is harmless: there is nothing to
			// close.
			name:       "a source with no history qualifies on its first run",
			run:        SourceRun{Status: StatusComplete, Postings: 0},
			state:      SourceState{},
			wantOK:     true,
			wantReason: ReasonOK,
		},
		{
			name:       "a growing source qualifies",
			run:        SourceRun{Status: StatusComplete, Postings: 100_000},
			state:      history,
			wantOK:     true,
			wantReason: ReasonOK,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			got := Qualifies(c.run, c.state, Policy{})

			test.Eq(t, c.wantOK, got.Qualifies)
			test.Eq(t, c.wantReason, got.Reason)
		})
	}
}

func TestQualifiesAgainstTheShapeOfTheRealCrawl(t *testing.T) {
	t.Parallel()

	// A reconstruction of the 07/28 manifest's *shape*, not the manifest itself:
	// 3,685 sources, 3,675 complete, 8 failed, 2 truncated, and 174 sources that
	// returned zero postings of which 166 reported complete
	// (docs/measurements/2026-07-28-crawl.md and docs/design/corpus-format.md §2).
	//
	// corpus-format.md §2.2 reports closureproto refusing 176 of 3,685 source
	// runs against the real file: 2 truncated, 8 failed, 166 empty-streak. This
	// asserts the same arithmetic falls out of this implementation.
	const (
		total     = 3685
		failed    = 8
		truncated = 2
		emptyOK   = 166
	)

	runs := make([]SourceRun, 0, total)
	states := make(map[string]SourceState, total)

	add := func(key, status string, postings int, trailing []int) {
		runs = append(runs, SourceRun{Platform: "p", Key: key, Status: status, Postings: postings})
		states[key] = SourceState{Trailing: trailing}
	}

	busy := []int{100, 100, 100, 100, 100, 100, 100}

	for i := 0; i < truncated; i++ {
		add("truncated-"+strconv.Itoa(i), StatusTruncated, 103_196, busy)
	}

	for i := 0; i < failed; i++ {
		// internal/services sets failed whenever Errors > 0, so a failed run in a
		// real manifest carries one.
		runs = append(runs, SourceRun{Platform: "p", Key: "failed-" + strconv.Itoa(i), Status: StatusFailed, Errors: 1})
		states["failed-"+strconv.Itoa(i)] = SourceState{Trailing: busy}
	}

	for i := 0; i < emptyOK; i++ {
		add("empty-"+strconv.Itoa(i), StatusComplete, 0, busy)
	}

	for i := len(runs); i < total; i++ {
		add("ok-"+strconv.Itoa(i), StatusComplete, 100, busy)
	}

	must.SliceLen(t, total, runs)

	reasons := map[string]int{}
	qualifying := 0

	for _, run := range runs {
		verdict := Qualifies(run, states[run.Key], Policy{})
		if verdict.Qualifies {
			qualifying++

			continue
		}

		reasons[verdict.Reason]++
	}

	test.Eq(t, total-176, qualifying)
	test.Eq(t, truncated, reasons[ReasonStatus+StatusTruncated])
	test.Eq(t, failed, reasons[ReasonStatus+StatusFailed])
	test.Eq(t, emptyOK, reasons[ReasonEmptyStreak])
}

func TestTrailingMedian(t *testing.T) {
	t.Parallel()

	cases := []struct {
		trailing []int
		want     int
	}{
		{nil, 0},
		{[]int{5}, 5},
		{[]int{5, 9}, 7},
		{[]int{9, 5}, 7},
		{[]int{1, 2, 3, 4, 5}, 3},
		// A median over a week absorbs one anomalous day, which is the whole
		// reason it is not a mean.
		{[]int{100, 100, 100, 0, 100, 100, 100}, 100},
		{[]int{100, 100, 100, 900_000, 100, 100, 100}, 100},
	}

	for _, c := range cases {
		test.Eq(t, c.want, SourceState{Trailing: c.trailing}.TrailingMedian(),
			test.Sprintf("trailing %v", c.trailing))
	}
}

func TestPolicyDefaultsFillEveryFieldIndependently(t *testing.T) {
	t.Parallel()

	// A caller that sets one knob must not silently disable the other four.
	got := Policy{MissingRuns: 5}.withDefaults()

	test.Eq(t, 5, got.MissingRuns)
	test.Eq(t, DefaultEmptyStreak, got.EmptyStreak)
	test.Eq(t, DefaultMinRatio, got.MinRatio)
	test.Eq(t, DefaultLapseAfter, got.LapseAfter)
	test.Eq(t, DefaultFreshnessTarget, got.FreshnessTarget)

	test.Eq(t, DefaultPolicy(), Policy{}.withDefaults())
}

func TestState(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	src := source("greenhouse", "acme")

	corpusWith := func(state SourceState) *Corpus {
		return newCorpus(Manifest{Policy: DefaultPolicy()}, []SourceState{state}, nil)
	}

	row := Row{Posting: jobposting.JobPosting{Source: src}}

	t.Run("open inside the freshness target", func(t *testing.T) {
		t.Parallel()

		c := corpusWith(SourceState{Source: src, LastQualifying: now.Add(-time.Hour)})
		test.Eq(t, StateOpen, c.State(row, now))
	})

	t.Run("stale past the freshness target", func(t *testing.T) {
		t.Parallel()

		c := corpusWith(SourceState{Source: src, LastQualifying: now.Add(-48 * time.Hour)})
		test.Eq(t, StateStale, c.State(row, now))
	})

	t.Run("lapsed past LapseAfter", func(t *testing.T) {
		t.Parallel()

		c := corpusWith(SourceState{Source: src, LastQualifying: now.Add(-100 * 24 * time.Hour)})
		test.Eq(t, StateLapsed, c.State(row, now))
	})

	t.Run("a source that never qualified cannot be called open", func(t *testing.T) {
		t.Parallel()

		c := corpusWith(SourceState{Source: src})
		test.Eq(t, StateLapsed, c.State(row, now))
	})

	t.Run("an unknown source cannot be called open", func(t *testing.T) {
		t.Parallel()

		c := corpusWith(SourceState{Source: source("ashby", "other")})
		test.Eq(t, StateLapsed, c.State(row, now))
	})

	t.Run("closed beats everything", func(t *testing.T) {
		t.Parallel()

		c := corpusWith(SourceState{Source: src, LastQualifying: now})
		closed := row
		closed.Closed = &Closure{Reason: ReasonAbsent, LastSeen: now.Add(-time.Hour), ConfirmedAt: now}

		test.Eq(t, StateClosed, c.State(closed, now))
	})

	t.Run("lapsed and retired are not closures", func(t *testing.T) {
		t.Parallel()

		// "We do not know" is a different answer from "it closed", and that
		// distinction has to survive into the UI.
		c := corpusWith(SourceState{Source: src, LastQualifying: now})

		for _, reason := range []string{ReasonLapsed, ReasonRetired} {
			archived := row
			archived.Closed = &Closure{Reason: reason, LastSeen: now.Add(-time.Hour), ConfirmedAt: now}

			test.Eq(t, StateLapsed, c.State(archived, now), test.Sprintf("reason %q", reason))
		}
	})
}

func TestStateString(t *testing.T) {
	t.Parallel()

	test.Eq(t, "open", StateOpen.String())
	test.Eq(t, "stale", StateStale.String())
	test.Eq(t, "closed", StateClosed.String())
	test.Eq(t, "lapsed", StateLapsed.String())
	test.True(t, strings.HasPrefix(State(9).String(), "state("))
}

func TestDecodeManifestSources(t *testing.T) {
	t.Parallel()

	// The corpus consumes internal/shard's manifest without importing it, so the
	// JSON tags of SourceRun are a contract with another package. This decodes a
	// literal manifest fragment in that package's shape.
	const manifest = `{
	  "schema_version": 2,
	  "status": "partial",
	  "postings": 780489,
	  "sources": [
	    {"platform":"greenhouse","key":"anthropic","company":"anthropic","status":"complete","postings":42,"errors":0,"duration_ms":1234},
	    {"platform":"jibe","key":"fedex","company":"fedex","status":"truncated","postings":103196,"errors":1,"error_class":"deadline"}
	  ]
	}`

	got, err := DecodeManifestSources(strings.NewReader(manifest))
	must.NoError(t, err)
	must.SliceLen(t, 2, got)

	test.Eq(t, source("greenhouse", "anthropic"), got[0].Source())
	test.Eq(t, StatusComplete, got[0].Status)
	test.Eq(t, 42, got[0].Postings)
	test.Eq(t, int64(1234), got[0].DurationMS)

	test.Eq(t, StatusTruncated, got[1].Status)
	test.Eq(t, 103196, got[1].Postings)
	test.Eq(t, 1, got[1].Errors)
	test.Eq(t, "deadline", got[1].ErrorClass)
}

func TestDecodeManifestSourcesRejectsAManifestWithNoSources(t *testing.T) {
	t.Parallel()

	// "No source was visited" and "this is not a manifest" are exactly the
	// distinction closure must not get wrong, so a missing array is an error
	// rather than an empty run.
	_, err := DecodeManifestSources(strings.NewReader(`{"schema_version":2}`))
	test.Error(t, err)

	got, err := DecodeManifestSources(strings.NewReader(`{"sources":[]}`))
	must.NoError(t, err)
	test.SliceEmpty(t, got)
}
