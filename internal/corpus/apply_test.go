package corpus

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"iter"
	"strconv"
	"testing"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/jobposting"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// memPublisher and memStore are a corpus in a map, which is all the Store and
// Publisher contracts need — no filesystem, no lock, no syscall. It is also the
// shape a browser's IndexedDB backend has, so exercising it here is exercising
// the browser path.
type memStore struct{ objects map[string][]byte }

func newMemStore() *memStore { return &memStore{objects: map[string][]byte{}} }

func (m *memStore) Size(_ context.Context, name string) (int64, error) {
	body, ok := m.objects[name]
	if !ok {
		return 0, notExist(name)
	}

	return int64(len(body)), nil
}

func (m *memStore) ReadAt(_ context.Context, name string, p []byte, off int64) (int, error) {
	body, ok := m.objects[name]
	if !ok {
		return 0, notExist(name)
	}

	return bytes.NewReader(body).ReadAt(p, off)
}

func (m *memStore) Create(_ context.Context, name string) (io.WriteCloser, error) {
	return &memWriter{store: m, name: name}, nil
}

func (m *memStore) Commit(_ context.Context, manifest Manifest) error {
	body, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}

	m.objects[ManifestFile] = append(body, '\n')

	return nil
}

var (
	_ Store     = (*memStore)(nil)
	_ Publisher = (*memStore)(nil)
)

// notExist is what a Store must return for a missing object, so that readRuns
// can tell "this corpus has no run history yet" from "the object store broke".
func notExist(name string) error {
	return fmt.Errorf("corpus: %s: %w", name, fs.ErrNotExist)
}

type memWriter struct {
	store *memStore
	name  string
	buf   bytes.Buffer
}

func (w *memWriter) Write(p []byte) (int, error) { return w.buf.Write(p) }

func (w *memWriter) Close() error {
	w.store.objects[w.name] = bytes.Clone(w.buf.Bytes())

	return nil
}

// runAt is the single clock reading every test run uses, so no test reads a
// clock either.
var day0 = time.Date(2026, 7, 20, 3, 0, 0, 0, time.UTC)

func day(n int) time.Time { return day0.AddDate(0, 0, n) }

func seq(postings ...*jobposting.JobPosting) jobposting.Seq {
	return func(yield func(*jobposting.JobPosting, error) bool) {
		for _, p := range postings {
			if !yield(p, nil) {
				return
			}
		}
	}
}

func completeRun(src jobposting.PostingSource, postings int) SourceRun {
	return SourceRun{Platform: src.Platform, Key: src.Key, Company: src.Key, Status: StatusComplete, Postings: postings}
}

// applyTo folds a run and republishes, returning the reopened corpus. Going
// through the codec on every step is deliberate: a rule that holds in memory and
// not on disk is not a rule the corpus has.
func applyTo(t *testing.T, base *Corpus, in RunInput, policy Policy) (*Corpus, *Generation) {
	t.Helper()

	ctx := t.Context()

	generation, err := Apply(ctx, base, in, policy)
	must.NoError(t, err)

	store := newMemStore()
	must.NoError(t, generation.WriteTo(ctx, store))

	reopened, err := Open(ctx, store)
	must.NoError(t, err)
	must.NoError(t, Verify(ctx, reopened))

	return reopened, generation
}

func rowsOf(t *testing.T, c *Corpus) map[string]Row {
	t.Helper()

	out := map[string]Row{}

	for row, err := range c.Rows(t.Context()) {
		must.NoError(t, err)
		out[row.ID] = row
	}

	return out
}

func TestApplyFoldsAFirstRunIntoAnEmptyCorpus(t *testing.T) {
	t.Parallel()

	src := source("greenhouse", "acme")

	corpus, generation := applyTo(t, Empty(), RunInput{
		RunAt:   day(0),
		Sources: []SourceRun{completeRun(src, 2)},
		Postings: seq(
			posting(src, "ext-1", "", "https://example.com/1", "Engineer", "Remote"),
			posting(src, "ext-2", "", "https://example.com/2", "Designer", "Berlin"),
		),
	}, Policy{})

	test.Eq(t, 2, corpus.Manifest().Rows)
	test.Eq(t, 2, corpus.Manifest().Open)
	test.Eq(t, int64(1), corpus.Manifest().Generation)
	test.Eq(t, 2, generation.Churn.Appeared)

	for _, row := range rowsOf(t, corpus) {
		test.Eq(t, day(0), row.FirstSeen)
		test.Nil(t, row.Closed)
		test.Eq(t, 0, row.Missing)

		// The observation is derived rather than stored, which is what keeps an
		// open row's bytes identical between generations.
		test.True(t, row.LastSeen.IsZero())
		test.Eq(t, day(0), corpus.LastSeen(row))
		test.Eq(t, StateOpen, corpus.State(row, day(0)))
	}

	state, ok := corpus.Source(src)
	must.True(t, ok)
	test.Eq(t, day(0), state.LastQualifying)
	test.Eq(t, day(0), state.LastAttempt)
	test.Eq(t, []int{2}, state.Trailing)
	test.Eq(t, 2, state.Open)
}

func TestApplyOpenCountIsTheGlobalUnionOfDedupeKeys(t *testing.T) {
	t.Parallel()

	// The corpus's headline number is what jobs_record.txt has always published:
	// distinct dedupe keys, never a sum. Two integrations publishing one URL is
	// one job and two rows.
	greenhouse := source("greenhouse", "acme")
	ashby := source("ashby", "acme")

	shared := "https://acme.example/careers/1"

	corpus, _ := applyTo(t, Empty(), RunInput{
		RunAt:   day(0),
		Sources: []SourceRun{completeRun(greenhouse, 1), completeRun(ashby, 1)},
		Postings: seq(
			posting(greenhouse, "gh-1", "", shared, "Engineer", "Remote"),
			posting(ashby, "ab-1", "", shared, "Engineer", "Remote"),
		),
	}, Policy{})

	test.Eq(t, 2, corpus.Manifest().Rows)
	test.Eq(t, 1, corpus.Manifest().Open)
}

func TestApplyNeverClosesAnythingFromAFailedSource(t *testing.T) {
	t.Parallel()

	// docs/architecture-roadmap.md states this as an invariant: "a failed source
	// cannot make all of its previously seen jobs look removed". Seven of the
	// 07/28 failures were Workday tenants sharing one platform-side error.
	src := source("workday", "tenant")

	corpus, _ := applyTo(t, Empty(), RunInput{
		RunAt:    day(0),
		Sources:  []SourceRun{completeRun(src, 1)},
		Postings: seq(posting(src, "ext-1", "", "https://example.com/1", "Engineer", "Remote")),
	}, Policy{})

	// Ten consecutive failures. Not one of them may touch the row.
	for i := 1; i <= 10; i++ {
		var generation *Generation

		corpus, generation = applyTo(t, corpus, RunInput{
			RunAt: day(i),
			Sources: []SourceRun{{
				Platform: src.Platform, Key: src.Key, Status: StatusFailed,
				Errors: 1, ErrorClass: "workdayStatusError",
			}},
			Postings: seq(),
		}, Policy{})

		test.Eq(t, 0, generation.Churn.Closed)
		test.Eq(t, 0, generation.Churn.Missing)
		test.False(t, generation.Verdicts[src].Qualifies)
	}

	rows := rowsOf(t, corpus)
	must.MapLen(t, 1, rows)

	for _, row := range rows {
		test.Nil(t, row.Closed)
		test.Eq(t, 0, row.Missing)

		// LastQualifying never advanced, so the derived observation is still the
		// only run that ever qualified.
		test.Eq(t, day(0), corpus.LastSeen(row))
	}

	state, _ := corpus.Source(src)
	test.Eq(t, 10, state.ConsecutiveFailures)
	test.Eq(t, day(10), state.LastAttempt)
	test.Eq(t, day(0), state.LastQualifying)
}

func TestApplyNeverClosesAnythingFromATruncatedSource(t *testing.T) {
	t.Parallel()

	// jibe/fedex ended truncated on 07/28 holding 103,196 postings. A rule that
	// closed what was absent from the latest run would have retired most of it the
	// first time the crawl hit its deadline mid-source.
	src := source("jibe", "fedex")

	postings := make([]*jobposting.JobPosting, 20)
	for i := range postings {
		postings[i] = posting(src, "ext-"+strconv.Itoa(i), "", "https://fedex.example/"+strconv.Itoa(i), "Driver", "Memphis")
	}

	corpus, _ := applyTo(t, Empty(), RunInput{
		RunAt:    day(0),
		Sources:  []SourceRun{completeRun(src, len(postings))},
		Postings: seq(postings...),
	}, Policy{})

	// A truncated run that got through only the first two postings.
	for i := 1; i <= 5; i++ {
		var generation *Generation

		corpus, generation = applyTo(t, corpus, RunInput{
			RunAt: day(i),
			Sources: []SourceRun{{
				Platform: src.Platform, Key: src.Key, Status: StatusTruncated, Postings: 2,
			}},
			Postings: seq(postings[0], postings[1]),
		}, Policy{})

		test.Eq(t, 0, generation.Churn.Closed)
		test.Eq(t, 0, generation.Churn.Missing)
	}

	test.Eq(t, len(postings), corpus.Manifest().Rows)
	test.Eq(t, 0, corpus.Manifest().Closed)
}

func TestApplyNeverClosesAnythingFromASourceTheRunNeverVisited(t *testing.T) {
	t.Parallel()

	// This is the budget model's normal case: most sources are not visited in any
	// given run, and absence from a run is not evidence of anything.
	visited := source("greenhouse", "visited")
	skipped := source("greenhouse", "skipped")

	corpus, _ := applyTo(t, Empty(), RunInput{
		RunAt:   day(0),
		Sources: []SourceRun{completeRun(visited, 1), completeRun(skipped, 1)},
		Postings: seq(
			posting(visited, "v-1", "", "https://example.com/v1", "Engineer", "Remote"),
			posting(skipped, "s-1", "", "https://example.com/s1", "Engineer", "Remote"),
		),
	}, Policy{})

	for i := 1; i <= 5; i++ {
		corpus, _ = applyTo(t, corpus, RunInput{
			RunAt:    day(i),
			Sources:  []SourceRun{completeRun(visited, 1)},
			Postings: seq(posting(visited, "v-1", "", "https://example.com/v1", "Engineer", "Remote")),
		}, Policy{})
	}

	test.Eq(t, 2, corpus.Manifest().Rows)
	test.Eq(t, 0, corpus.Manifest().Closed)

	for _, row := range rowsOf(t, corpus) {
		test.Nil(t, row.Closed)
	}
}

func TestApplyClosesOnlyAfterMissingRunsQualifyingRuns(t *testing.T) {
	t.Parallel()

	src := source("greenhouse", "acme")

	stay := posting(src, "keep", "", "https://example.com/keep", "Engineer", "Remote")
	going := posting(src, "gone", "", "https://example.com/gone", "Designer", "Berlin")

	corpus, _ := applyTo(t, Empty(), RunInput{
		RunAt:    day(0),
		Sources:  []SourceRun{completeRun(src, 2)},
		Postings: seq(stay, going),
	}, Policy{})

	goneID := ID(src, BasisExternal, "gone")

	// First qualifying run without it: missing, not closed. Two independent runs
	// must agree before a row's life ends.
	corpus, generation := applyTo(t, corpus, RunInput{
		RunAt:    day(1),
		Sources:  []SourceRun{completeRun(src, 1)},
		Postings: seq(stay),
	}, Policy{})

	test.Eq(t, 1, generation.Churn.Missing)
	test.Eq(t, 0, generation.Churn.Closed)

	row := rowsOf(t, corpus)[goneID]
	must.Nil(t, row.Closed)
	test.Eq(t, 1, row.Missing)

	// The last observation is frozen at day 0 — the run that saw it — and not at
	// day 1, which is the run that did not. Getting this wrong is how a closing
	// interval silently becomes a lie.
	test.Eq(t, day(0), corpus.LastSeen(row))

	corpus, generation = applyTo(t, corpus, RunInput{
		RunAt:    day(2),
		Sources:  []SourceRun{completeRun(src, 1)},
		Postings: seq(stay),
	}, Policy{})

	test.Eq(t, 1, generation.Churn.Closed)

	row = rowsOf(t, corpus)[goneID]
	must.NotNil(t, row.Closed)
	test.Eq(t, ReasonAbsent, row.Closed.Reason)
	test.Eq(t, day(0), row.Closed.LastSeen)
	test.Eq(t, day(2), row.Closed.ConfirmedAt)
	test.Eq(t, StateClosed, corpus.State(row, day(2)))

	test.Eq(t, 1, corpus.Manifest().Open)
	test.Eq(t, 1, corpus.Manifest().Closed)
}

func TestApplyDoesNotAdvanceMissingOnANonQualifyingRun(t *testing.T) {
	t.Parallel()

	// A source that alternates between a good run and a bad one must take two
	// *qualifying* runs to close, not two runs.
	src := source("greenhouse", "acme")

	stay := posting(src, "keep", "", "https://example.com/keep", "Engineer", "Remote")
	going := posting(src, "gone", "", "https://example.com/gone", "Designer", "Berlin")

	corpus, _ := applyTo(t, Empty(), RunInput{
		RunAt:    day(0),
		Sources:  []SourceRun{completeRun(src, 2)},
		Postings: seq(stay, going),
	}, Policy{})

	corpus, _ = applyTo(t, corpus, RunInput{
		RunAt:    day(1),
		Sources:  []SourceRun{completeRun(src, 1)},
		Postings: seq(stay),
	}, Policy{})

	for i := 2; i <= 6; i++ {
		var generation *Generation

		corpus, generation = applyTo(t, corpus, RunInput{
			RunAt:    day(i),
			Sources:  []SourceRun{{Platform: src.Platform, Key: src.Key, Status: StatusFailed, Errors: 1}},
			Postings: seq(),
		}, Policy{})

		test.Eq(t, 0, generation.Churn.Closed)
	}

	row := rowsOf(t, corpus)[ID(src, BasisExternal, "gone")]
	test.Nil(t, row.Closed)
	test.Eq(t, 1, row.Missing)
}

func TestApplyResetsMissingOnAnyObservation(t *testing.T) {
	t.Parallel()

	src := source("greenhouse", "acme")

	stay := posting(src, "keep", "", "https://example.com/keep", "Engineer", "Remote")
	flapping := posting(src, "flap", "", "https://example.com/flap", "Designer", "Berlin")

	corpus, _ := applyTo(t, Empty(), RunInput{
		RunAt:    day(0),
		Sources:  []SourceRun{completeRun(src, 2)},
		Postings: seq(stay, flapping),
	}, Policy{})

	corpus, _ = applyTo(t, corpus, RunInput{
		RunAt:    day(1),
		Sources:  []SourceRun{completeRun(src, 1)},
		Postings: seq(stay),
	}, Policy{})

	corpus, _ = applyTo(t, corpus, RunInput{
		RunAt:    day(2),
		Sources:  []SourceRun{completeRun(src, 2)},
		Postings: seq(stay, flapping),
	}, Policy{})

	row := rowsOf(t, corpus)[ID(src, BasisExternal, "flap")]
	test.Eq(t, 0, row.Missing)
	test.True(t, row.LastSeen.IsZero())
	test.Eq(t, day(2), corpus.LastSeen(row))

	corpus, _ = applyTo(t, corpus, RunInput{
		RunAt:    day(3),
		Sources:  []SourceRun{completeRun(src, 1)},
		Postings: seq(stay),
	}, Policy{})

	row = rowsOf(t, corpus)[ID(src, BasisExternal, "flap")]
	test.Eq(t, 1, row.Missing)
	test.Nil(t, row.Closed)
}

func TestApplyReopensAClosedRowWithoutResettingFirstSeen(t *testing.T) {
	t.Parallel()

	// A board re-publishing a filled role and a close that was simply wrong both
	// land here, and neither justifies a fresh FirstSeen: the role's history is
	// the point of the corpus.
	src := source("greenhouse", "acme")

	stay := posting(src, "keep", "", "https://example.com/keep", "Engineer", "Remote")
	returning := posting(src, "back", "", "https://example.com/back", "Designer", "Berlin")

	corpus, _ := applyTo(t, Empty(), RunInput{
		RunAt:    day(0),
		Sources:  []SourceRun{completeRun(src, 2)},
		Postings: seq(stay, returning),
	}, Policy{})

	for i := 1; i <= 2; i++ {
		corpus, _ = applyTo(t, corpus, RunInput{
			RunAt:    day(i),
			Sources:  []SourceRun{completeRun(src, 1)},
			Postings: seq(stay),
		}, Policy{})
	}

	id := ID(src, BasisExternal, "back")
	must.NotNil(t, rowsOf(t, corpus)[id].Closed)

	corpus, generation := applyTo(t, corpus, RunInput{
		RunAt:    day(3),
		Sources:  []SourceRun{completeRun(src, 2)},
		Postings: seq(stay, returning),
	}, Policy{})

	test.Eq(t, 1, generation.Churn.Reopened)

	row := rowsOf(t, corpus)[id]
	test.Nil(t, row.Closed)
	test.Eq(t, 1, row.Reopens)
	test.Eq(t, day(0), row.FirstSeen)
	test.Eq(t, 0, row.Missing)
	test.Eq(t, StateOpen, corpus.State(row, day(3)))
}

func TestApplyStoresLastSeenOnlyWhenTheRunCouldNotClose(t *testing.T) {
	t.Parallel()

	// A non-qualifying run still observes postings, and that observation has to be
	// written down because nothing else records it. A qualifying run's observation
	// is derivable, so storing it would change every row's bytes every generation
	// for no information.
	src := source("greenhouse", "acme")
	p := posting(src, "ext-1", "", "https://example.com/1", "Engineer", "Remote")

	corpus, _ := applyTo(t, Empty(), RunInput{
		RunAt:    day(0),
		Sources:  []SourceRun{completeRun(src, 1)},
		Postings: seq(p),
	}, Policy{})

	corpus, _ = applyTo(t, corpus, RunInput{
		RunAt:    day(1),
		Sources:  []SourceRun{{Platform: src.Platform, Key: src.Key, Status: StatusTruncated, Postings: 1}},
		Postings: seq(p),
	}, Policy{})

	row := rowsOf(t, corpus)[ID(src, BasisExternal, "ext-1")]
	test.Eq(t, day(1), row.LastSeen)
	test.Eq(t, day(1), corpus.LastSeen(row))

	state, _ := corpus.Source(src)
	test.Eq(t, day(0), state.LastQualifying)
	test.Eq(t, day(1), state.LastAttempt)
}

func TestApplyLapsesRowsWhoseSourceWentQuiet(t *testing.T) {
	t.Parallel()

	// A lapsed row is dropped from the open count and archived without a closing
	// date, because nobody observed one.
	quiet := source("greenhouse", "quiet")
	active := source("greenhouse", "active")

	activePosting := posting(active, "a-1", "", "https://example.com/a1", "Engineer", "Remote")

	corpus, _ := applyTo(t, Empty(), RunInput{
		RunAt:   day(0),
		Sources: []SourceRun{completeRun(quiet, 1), completeRun(active, 1)},
		Postings: seq(
			posting(quiet, "q-1", "", "https://example.com/q1", "Engineer", "Remote"),
			activePosting,
		),
	}, Policy{})

	corpus, generation := applyTo(t, corpus, RunInput{
		RunAt:    day(120),
		Sources:  []SourceRun{completeRun(active, 1)},
		Postings: seq(activePosting),
	}, Policy{})

	test.Eq(t, 1, generation.Churn.Lapsed)

	row := rowsOf(t, corpus)[ID(quiet, BasisExternal, "q-1")]
	must.NotNil(t, row.Closed)
	test.Eq(t, ReasonLapsed, row.Closed.Reason)
	test.Eq(t, day(0), row.Closed.LastSeen)
	test.Eq(t, StateLapsed, corpus.State(row, day(120)))

	test.Eq(t, 1, corpus.Manifest().Open)
	test.Eq(t, 1, corpus.Manifest().Lapsed)
	test.Eq(t, 0, corpus.Manifest().Closed)
}

func TestApplyRetiresRatherThanClosesASourceThatLeftTheRegistry(t *testing.T) {
	t.Parallel()

	// Deleting a source from the registry is not evidence that its employer
	// withdrew every job.
	gone := source("greenhouse", "gone")
	kept := source("greenhouse", "kept")

	keptPosting := posting(kept, "k-1", "", "https://example.com/k1", "Engineer", "Remote")

	corpus, _ := applyTo(t, Empty(), RunInput{
		RunAt:   day(0),
		Sources: []SourceRun{completeRun(gone, 1), completeRun(kept, 1)},
		Postings: seq(
			posting(gone, "g-1", "", "https://example.com/g1", "Engineer", "Remote"),
			keptPosting,
		),
	}, Policy{})

	corpus, _ = applyTo(t, corpus, RunInput{
		RunAt:    day(1),
		Sources:  []SourceRun{completeRun(kept, 1)},
		Postings: seq(keptPosting),
		Retired:  []jobposting.PostingSource{gone},
	}, Policy{})

	state, ok := corpus.Source(gone)
	must.True(t, ok)
	test.True(t, state.Retired)

	row := rowsOf(t, corpus)[ID(gone, BasisExternal, "g-1")]
	test.Nil(t, row.Closed)

	// It only leaves the open set once LapseAfter elapses, and then as retired
	// rather than closed.
	corpus, _ = applyTo(t, corpus, RunInput{
		RunAt:    day(120),
		Sources:  []SourceRun{completeRun(kept, 1)},
		Postings: seq(keptPosting),
		Retired:  []jobposting.PostingSource{gone},
	}, Policy{})

	row = rowsOf(t, corpus)[ID(gone, BasisExternal, "g-1")]
	must.NotNil(t, row.Closed)
	test.Eq(t, ReasonRetired, row.Closed.Reason)
	test.Eq(t, StateLapsed, corpus.State(row, day(120)))
}

func TestApplyRejectsAPostingFromASourceTheManifestDoesNotList(t *testing.T) {
	t.Parallel()

	// Admitting it would create rows that no future run can ever close, because
	// the manifest is the corpus's only record that a source was visited.
	listed := source("greenhouse", "listed")
	unlisted := source("greenhouse", "unlisted")

	corpus, generation := applyTo(t, Empty(), RunInput{
		RunAt:   day(0),
		Sources: []SourceRun{completeRun(listed, 1)},
		Postings: seq(
			posting(listed, "l-1", "", "https://example.com/l1", "Engineer", "Remote"),
			posting(unlisted, "u-1", "", "https://example.com/u1", "Engineer", "Remote"),
		),
	}, Policy{})

	test.Eq(t, 1, generation.Churn.Rejected)
	test.Eq(t, 1, corpus.Manifest().Rows)
}

func TestApplyRejectsADuplicateSourceInTheManifest(t *testing.T) {
	t.Parallel()

	src := source("greenhouse", "acme")

	_, err := Apply(t.Context(), Empty(), RunInput{
		RunAt:    day(0),
		Sources:  []SourceRun{completeRun(src, 1), completeRun(src, 2)},
		Postings: seq(),
	}, Policy{})

	test.Error(t, err)
}

func TestApplyRequiresAClockReading(t *testing.T) {
	t.Parallel()

	_, err := Apply(t.Context(), Empty(), RunInput{Postings: seq()}, Policy{})
	test.Error(t, err)
}

func TestApplyPropagatesAPostingStreamError(t *testing.T) {
	t.Parallel()

	failing := func(yield func(*jobposting.JobPosting, error) bool) {
		yield(nil, context.DeadlineExceeded)
	}

	_, err := Apply(t.Context(), Empty(), RunInput{
		RunAt:    day(0),
		Sources:  []SourceRun{},
		Postings: iter.Seq2[*jobposting.JobPosting, error](failing),
	}, Policy{})

	must.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestApplyRecordsRequisitionUnsafePermanently(t *testing.T) {
	t.Parallel()

	src := source("greenhouse", "stripe")

	colliding := []*jobposting.JobPosting{
		posting(src, "", "See Opening ID", "https://example.com/1", "Engineer", "SF"),
		posting(src, "", "See Opening ID", "https://example.com/2", "Engineer", "NY"),
	}

	corpus, _ := applyTo(t, Empty(), RunInput{
		RunAt:    day(0),
		Sources:  []SourceRun{completeRun(src, 2)},
		Postings: seq(colliding...),
	}, Policy{})

	state, _ := corpus.Source(src)
	must.True(t, state.RequisitionUnsafe)

	// A later run whose requisitions happen to be distinct must not re-promote the
	// rung, or every row's identity would change and the corpus would report a
	// mass close and a mass open.
	distinct := []*jobposting.JobPosting{
		posting(src, "", "REQ-1", "https://example.com/1", "Engineer", "SF"),
		posting(src, "", "REQ-2", "https://example.com/2", "Engineer", "NY"),
	}

	corpus, generation := applyTo(t, corpus, RunInput{
		RunAt:    day(1),
		Sources:  []SourceRun{completeRun(src, 2)},
		Postings: seq(distinct...),
	}, Policy{})

	test.Eq(t, 0, generation.Churn.Appeared)
	test.Eq(t, 2, corpus.Manifest().Rows)

	for _, row := range rowsOf(t, corpus) {
		test.Eq(t, BasisURL, row.Basis)
		test.Eq(t, day(0), row.FirstSeen)
	}

	state, _ = corpus.Source(src)
	test.True(t, state.RequisitionUnsafe)
}

func TestApplyIsDeterministic(t *testing.T) {
	t.Parallel()

	// Same input, byte-identical output. The places this could leak are the
	// dictionary order, the source map's iteration order and the row sort, all of
	// which this exercises by shuffling nothing and asserting the bytes anyway —
	// Go randomizes map iteration on its own, so running the fold twice is the
	// test.
	build := func() []byte {
		sources := make([]SourceRun, 0, 12)
		postings := make([]*jobposting.JobPosting, 0, 120)

		for s := range 12 {
			src := source([]string{"greenhouse", "ashby", "workday"}[s%3], "tenant-"+strconv.Itoa(s))
			sources = append(sources, completeRun(src, 10))

			for p := range 10 {
				postings = append(postings, posting(src,
					"ext-"+strconv.Itoa(p), "REQ-"+strconv.Itoa(p),
					"https://example.com/"+strconv.Itoa(s)+"/"+strconv.Itoa(p),
					"Engineer "+strconv.Itoa(p), []string{"Remote", "Berlin", "SF"}[p%3]))
			}
		}

		generation, err := Apply(t.Context(), Empty(), RunInput{
			RunAt: day(0), Sources: sources, Postings: seq(postings...),
		}, Policy{})
		must.NoError(t, err)

		store := newMemStore()
		must.NoError(t, generation.WriteTo(t.Context(), store))

		return store.objects[PostingsFile]
	}

	first := build()
	for range 5 {
		test.Eq(t, first, build())
	}
}

func TestApplyLeavesAStillOpenRowsBytesUnchanged(t *testing.T) {
	t.Parallel()

	// The sizing decision the whole format rests on: a posting that is simply
	// still open produces byte-identical output every generation, so a client can
	// reuse a cached block and a delta stays proportional to churn rather than to
	// corpus size.
	src := source("greenhouse", "acme")

	postings := make([]*jobposting.JobPosting, 50)
	for i := range postings {
		postings[i] = posting(src, "ext-"+strconv.Itoa(i), "", "https://example.com/"+strconv.Itoa(i), "Engineer", "Remote")
	}

	corpus, first := applyTo(t, Empty(), RunInput{
		RunAt: day(0), Sources: []SourceRun{completeRun(src, len(postings))}, Postings: seq(postings...),
	}, Policy{})

	_, second := applyTo(t, corpus, RunInput{
		RunAt: day(1), Sources: []SourceRun{completeRun(src, len(postings))}, Postings: seq(postings...),
	}, Policy{})

	firstTable, err := buildTable(first.Rows)
	must.NoError(t, err)

	secondTable, err := buildTable(second.Rows)
	must.NoError(t, err)

	test.Eq(t, firstTable.ContentDigest(), secondTable.ContentDigest())
}

func TestApplyCountsChurn(t *testing.T) {
	t.Parallel()

	// docs/design/corpus-format.md §8.2 is explicit that churn is the one number
	// in the design that is assumed rather than measured, and that Apply should
	// report it so the measurement arrives on its own.
	src := source("greenhouse", "acme")

	unchanged := posting(src, "same", "", "https://example.com/same", "Engineer", "Remote")
	edited := posting(src, "edit", "", "https://example.com/edit", "Designer", "Berlin")

	corpus, _ := applyTo(t, Empty(), RunInput{
		RunAt:    day(0),
		Sources:  []SourceRun{completeRun(src, 2)},
		Postings: seq(unchanged, edited),
	}, Policy{})

	retitled := posting(src, "edit", "", "https://example.com/edit", "Senior Designer", "Berlin")
	fresh := posting(src, "new", "", "https://example.com/new", "Analyst", "Remote")

	_, generation := applyTo(t, corpus, RunInput{
		RunAt:    day(1),
		Sources:  []SourceRun{completeRun(src, 3)},
		Postings: seq(unchanged, retitled, fresh),
	}, Policy{})

	test.Eq(t, Churn{Appeared: 1, Changed: 1, Unchanged: 1}, generation.Churn)
}

func TestApplyKeepsAtMostMaxRunsOfHistory(t *testing.T) {
	t.Parallel()

	src := source("greenhouse", "acme")
	p := posting(src, "ext-1", "", "https://example.com/1", "Engineer", "Remote")

	corpus := Empty()
	for i := range MaxRuns + 5 {
		corpus, _ = applyTo(t, corpus, RunInput{
			RunAt: day(i), Sources: []SourceRun{completeRun(src, 1)}, Postings: seq(p),
		}, Policy{})
	}

	runs := corpus.Runs()
	must.SliceLen(t, MaxRuns, runs)
	test.Eq(t, day(MaxRuns+4), runs[len(runs)-1].RunAt)
	test.Eq(t, day(5), runs[0].RunAt)
}

func TestApplyKeepsAtMostMaxTrailingSamples(t *testing.T) {
	t.Parallel()

	src := source("greenhouse", "acme")
	p := posting(src, "ext-1", "", "https://example.com/1", "Engineer", "Remote")

	corpus := Empty()
	for i := range MaxTrailing + 4 {
		corpus, _ = applyTo(t, corpus, RunInput{
			RunAt: day(i), Sources: []SourceRun{completeRun(src, 1)}, Postings: seq(p),
		}, Policy{})
	}

	state, _ := corpus.Source(src)
	test.SliceLen(t, MaxTrailing, state.Trailing)
}

func TestApplyTruncatesTheClockToASecondInUTC(t *testing.T) {
	t.Parallel()

	src := source("greenhouse", "acme")

	corpus, _ := applyTo(t, Empty(), RunInput{
		RunAt:    time.Date(2026, 7, 20, 3, 0, 0, 999_999_999, time.FixedZone("x", 3600)),
		Sources:  []SourceRun{completeRun(src, 1)},
		Postings: seq(posting(src, "ext-1", "", "https://example.com/1", "Engineer", "Remote")),
	}, Policy{})

	want := time.Date(2026, 7, 20, 2, 0, 0, 0, time.UTC)
	test.Eq(t, want, corpus.Manifest().RunAt)

	for _, row := range rowsOf(t, corpus) {
		test.Eq(t, want, row.FirstSeen)
	}
}
