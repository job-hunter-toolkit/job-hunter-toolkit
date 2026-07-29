package corpus

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"slices"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/jobposting"
)

// MaxRuns is how many run records the corpus keeps. Ninety days of nightly runs
// is enough to answer "how has this source behaved" and small enough that the
// file stays diffable, which is the trade docs/design/corpus-format.md §8.3 makes
// for every growing artifact: bounded and slightly lossy is the version that
// survives.
const MaxRuns = 90

// RunRecord is one run's entry in runs.ndjson.
//
// It is a summary rather than the crawl manifest: the per-source numbers the
// scheduler needs are folded into [SourceState], and keeping 90 full manifests
// would be tens of megabytes to record that nothing changed.
type RunRecord struct {
	RunAt   time.Time `json:"run_at"`
	Writer  string    `json:"writer,omitempty"`
	Partial bool      `json:"partial,omitempty"`

	Sources    int `json:"sources"`
	Qualifying int `json:"qualifying"`
	Postings   int `json:"postings"`

	Churn Churn `json:"churn"`
}

// Churn is what one run did to the corpus.
//
// docs/design/corpus-format.md §8.2 is explicit that churn is the one number in
// the whole design that is assumed rather than measured — it needs two
// consecutive runs and does not have them — and that Apply should print it so
// the measurement arrives on its own. This is that number.
type Churn struct {
	Appeared  int `json:"appeared"`
	Changed   int `json:"changed"`
	Unchanged int `json:"unchanged"`
	Missing   int `json:"missing"`
	Closed    int `json:"closed"`
	Reopened  int `json:"reopened"`
	Lapsed    int `json:"lapsed"`

	// Dropped counts run postings the identity ladder could not tell apart, from
	// [Identities.Collisions]. It is reported rather than hidden because a
	// non-zero value means the corpus is representing two real postings as one.
	Dropped int `json:"dropped,omitempty"`

	// Rejected counts run postings whose source was absent from the run's manifest.
	Rejected int `json:"rejected,omitempty"`
}

// RunInput is one crawl's contribution.
//
// Sources is not optional: it is the only evidence that a source was visited,
// and closure refuses to touch anything without it. A posting arriving from a
// source with no [SourceRun] means the run lied about its coverage, and [Apply]
// says so rather than guessing.
type RunInput struct {
	// RunAt is the run's single clock reading. Every timestamp this run writes is
	// this value, truncated to a second in UTC. It is a field rather than a call
	// to time.Now because the whole determinism story rests on there being exactly
	// one clock reading below the run boundary.
	RunAt time.Time

	// Sources is the run's manifest, one entry per integration attempted.
	Sources []SourceRun

	// Postings is the run's deduplicated posting stream.
	Postings jobposting.Seq

	// Retired names sources that have left the registry. They are marked retired
	// rather than closed: deleting a source from the registry is not evidence that
	// its employer withdrew every job, so its rows freeze at their last known
	// observation and lapse on the ordinary schedule.
	Retired []jobposting.PostingSource

	// Partial records that the crawl did not finish, and Writer identifies the
	// producing binary. Both are copied into the manifest.
	Partial bool
	Writer  string
}

// Generation is the corpus [Apply] computed, before it is published.
//
// Separating the fold from the write is what makes the fold testable without a
// filesystem, and it is what lets a caller inspect the churn before deciding to
// publish at all.
type Generation struct {
	Manifest Manifest
	Rows     []Row
	Sources  []SourceState
	Runs     []RunRecord

	// Verdicts records why each source in the run did or did not qualify as
	// evidence of absence, in the run's source order. Nothing else in the corpus
	// records a refusal, and a refusal is exactly the thing an operator needs to
	// see.
	Verdicts map[jobposting.PostingSource]Verdict

	Churn Churn
}

// Apply folds one run into base and returns the next generation.
//
// The algorithm, and the one line that carries the whole safety property:
//
//  1. Decide, per source, whether this run qualifies as evidence of absence.
//  2. Partition the run's postings by source and resolve identity per source,
//     with collision demotion.
//  3. Merge base rows with observed rows:
//     both sides            -> reset missing, refresh the posting, reopen if closed
//     run only              -> a new row, first_seen = RunAt
//     base only, qualified  -> missing++, close at MissingRuns
//     base only, unqualified-> UNTOUCHED
//  4. Lapse anything whose source has gone quiet for LapseAfter.
//  5. Roll the source states forward.
//
// Step 3's last line is the whole safety property, in one place. Because the
// verdict is read per source and never from the run's or a shard's aggregate
// status, the known `shard merge` bug that reports a shard complete when every
// source in it failed cannot close a single row: those sources are failed and
// refused individually.
func Apply(ctx context.Context, base *Corpus, in RunInput, policy Policy) (*Generation, error) {
	if base == nil {
		base = Empty()
	}

	policy = policy.withDefaults()

	if in.RunAt.IsZero() {
		return nil, errors.New("corpus: RunInput.RunAt is required; the corpus takes one clock reading per run")
	}

	runAt := in.RunAt.UTC().Truncate(time.Second)

	runs := make(map[jobposting.PostingSource]SourceRun, len(in.Sources))
	verdicts := make(map[jobposting.PostingSource]Verdict, len(in.Sources))

	for _, run := range in.Sources {
		source := run.Source()

		if _, duplicate := runs[source]; duplicate {
			return nil, fmt.Errorf("corpus: source %s/%s appears twice in the run manifest",
				source.Platform, source.Key)
		}

		state, _ := base.Source(source)
		runs[source] = run
		verdicts[source] = Qualifies(run, state, policy)
	}

	observed, churn, err := partition(ctx, in.Postings, runs)
	if err != nil {
		return nil, err
	}

	baseRows, err := readRows(base.table)
	if err != nil {
		return nil, err
	}

	rows, demoted, err := merge(base, baseRows, observed, runs, verdicts, runAt, policy, &churn)
	if err != nil {
		return nil, err
	}

	sources := rollForward(base, in, runs, verdicts, demoted, runAt)
	next := newCorpus(Manifest{Policy: policy}, sources, nil)

	lapse(next, rows, runAt, policy, &churn)
	sortRows(rows)

	generation := &Generation{
		Rows:     rows,
		Sources:  sources,
		Verdicts: verdicts,
		Churn:    churn,
	}

	generation.finish(base, in, runAt, policy, next)

	return generation, nil
}

// finish counts the states, fills the manifest and appends the run record.
func (g *Generation) finish(base *Corpus, in RunInput, runAt time.Time, policy Policy, next *Corpus) {
	openKeys := map[string]struct{}{}

	var stale, closed, lapsed int

	openPerSource := map[jobposting.PostingSource]int{}

	for _, row := range g.Rows {
		switch next.State(row, runAt) {
		case StateOpen:
			// The headline number is distinct dedupe keys, not rows: an employer on
			// two ATSs has two rows and one job, and this is the same global union
			// `shard merge` computes rather than a sum over anything.
			openKeys[row.DedupeKey] = struct{}{}
			openPerSource[row.Posting.Source]++
		case StateStale:
			stale++
		case StateClosed:
			closed++
		case StateLapsed:
			lapsed++
		}
	}

	for i := range g.Sources {
		g.Sources[i].Open = openPerSource[g.Sources[i].Source]
	}

	qualifying := 0
	for _, verdict := range g.Verdicts {
		if verdict.Qualifies {
			qualifying++
		}
	}

	postings := 0
	for _, run := range in.Sources {
		postings += run.Postings
	}

	g.Runs = append(slices.Clone(base.runs), RunRecord{
		RunAt:      runAt,
		Writer:     in.Writer,
		Partial:    in.Partial,
		Sources:    len(in.Sources),
		Qualifying: qualifying,
		Postings:   postings,
		Churn:      g.Churn,
	})

	if len(g.Runs) > MaxRuns {
		g.Runs = g.Runs[len(g.Runs)-MaxRuns:]
	}

	g.Manifest = Manifest{
		FormatVersion:    FormatVersion,
		MinReaderVersion: MinReaderVersion,
		IdentityVersion:  IdentityVersion,
		Generation:       base.manifest.Generation + 1,
		RunAt:            runAt,
		Writer:           in.Writer,
		Policy:           policy,
		Rows:             len(g.Rows),
		Sources:          len(g.Sources),
		Open:             len(openKeys),
		Stale:            stale,
		Closed:           closed,
		Lapsed:           lapsed,
		Partial:          in.Partial,
	}
}

// partition groups a run's postings by source.
//
// A posting whose source is absent from the run manifest is rejected rather than
// admitted, because the manifest is the corpus's only record that a source was
// visited: admitting a posting from a source with no run would create rows that
// no future run can ever close.
func partition(
	ctx context.Context,
	postings jobposting.Seq,
	runs map[jobposting.PostingSource]SourceRun,
) (map[jobposting.PostingSource][]*jobposting.JobPosting, Churn, error) {
	observed := make(map[jobposting.PostingSource][]*jobposting.JobPosting, len(runs))

	var churn Churn

	if postings == nil {
		return observed, churn, nil
	}

	for posting, err := range postings {
		if err != nil {
			return nil, churn, fmt.Errorf("corpus: read run postings: %w", err)
		}

		if err := ctx.Err(); err != nil {
			return nil, churn, err
		}

		if posting == nil {
			continue
		}

		source := posting.Source
		if _, known := runs[source]; !known {
			churn.Rejected++

			continue
		}

		observed[source] = append(observed[source], posting)
	}

	return observed, churn, nil
}

// merge folds the run's observations into the base rows.
func merge(
	base *Corpus,
	baseRows []Row,
	observed map[jobposting.PostingSource][]*jobposting.JobPosting,
	runs map[jobposting.PostingSource]SourceRun,
	verdicts map[jobposting.PostingSource]Verdict,
	runAt time.Time,
	policy Policy,
	churn *Churn,
) ([]Row, map[jobposting.PostingSource]bool, error) {
	demoted := map[jobposting.PostingSource]bool{}

	byID := make(map[string]int, len(baseRows))
	for i := range baseRows {
		byID[baseRows[i].ID] = i
	}

	matched := make([]bool, len(baseRows))
	rows := make([]Row, 0, len(baseRows))

	// Sorted so that the order in which sources are folded — and therefore the
	// order new rows are appended in before the final sort — is a function of the
	// data rather than of Go's map iteration.
	for _, source := range sortedSources(observed) {
		state, _ := base.Source(source)
		qualifies := verdicts[source].Qualifies

		identities := Identify(source, observed[source], state.RequisitionUnsafe)
		churn.Dropped += identities.Collisions

		// Recorded here rather than recomputed later: resolving the ladder a second
		// time to ask the same question was measured as a full extra identity pass
		// over every posting in the run.
		if slices.Contains(identities.Demoted, BasisRequisition) {
			demoted[source] = true
		}

		for i, posting := range observed[source] {
			id := identities.IDs[i]
			if id == "" {
				continue
			}

			index, existing := byID[id]
			if existing && baseRows[index].Posting.Source != source {
				// Identity is scoped to the integration, so this is arithmetically
				// unreachable rather than merely unlikely. Reported instead of
				// silently overwriting another source's row.
				return nil, nil, fmt.Errorf("corpus: id %s collides across sources %s/%s and %s/%s",
					id, source.Platform, source.Key,
					baseRows[index].Posting.Source.Platform, baseRows[index].Posting.Source.Key)
			}

			if !existing {
				rows = append(rows, newRow(id, identities.Bases[i], posting, runAt, qualifies))
				churn.Appeared++

				continue
			}

			matched[index] = true
			refresh(&baseRows[index], identities.Bases[i], posting, runAt, qualifies, churn)
		}
	}

	for i := range baseRows {
		if !matched[i] {
			absent(base, &baseRows[i], runs, verdicts, runAt, policy, churn)
		}
	}

	return append(rows, baseRows...), demoted, nil
}

func newRow(id string, basis Basis, posting *jobposting.JobPosting, runAt time.Time, qualifies bool) Row {
	row := Row{
		ID:        id,
		Basis:     basis,
		DedupeKey: DedupeKey(posting),
		FirstSeen: runAt,
		Posting:   *posting,
	}

	stampLastSeen(&row, runAt, qualifies)

	return row
}

// refresh applies an observation to an existing row.
func refresh(row *Row, basis Basis, posting *jobposting.JobPosting, runAt time.Time, qualifies bool, churn *Churn) {
	if row.Closed != nil {
		// A board re-publishing a filled role and a close that was simply wrong
		// both land here. Neither justifies a fresh FirstSeen: the role's history
		// is the point of the corpus, so the reopen is counted on the row instead.
		row.Closed = nil
		row.Reopens++
		churn.Reopened++
	}

	if row.Missing != 0 {
		row.Missing = 0
	}

	if postingChanged(&row.Posting, posting) {
		churn.Changed++
	} else {
		churn.Unchanged++
	}

	row.Basis = basis
	row.DedupeKey = DedupeKey(posting)
	row.Posting = *posting

	stampLastSeen(row, runAt, qualifies)
}

// stampLastSeen writes the row's last observation, or deliberately does not.
//
// A qualifying run advances its source's LastQualifying to RunAt, so an observed
// row's last observation is derivable and storing it would change the row's
// bytes every generation for no information. A run that did not qualify does not
// advance anything, so the observation has to be written down or it is lost.
func stampLastSeen(row *Row, runAt time.Time, qualifies bool) {
	if qualifies {
		row.LastSeen = time.Time{}

		return
	}

	row.LastSeen = runAt
}

// absent handles a base row the run did not observe.
func absent(
	base *Corpus,
	row *Row,
	runs map[jobposting.PostingSource]SourceRun,
	verdicts map[jobposting.PostingSource]Verdict,
	runAt time.Time,
	policy Policy,
	churn *Churn,
) {
	if row.Closed != nil {
		return
	}

	source := row.Posting.Source

	if _, visited := runs[source]; !visited {
		// The source was not in this run at all. Under a budget model that is the
		// normal case for most sources in most runs, and it is not evidence of
		// anything.
		return
	}

	if !verdicts[source].Qualifies {
		return
	}

	// Freeze the last observation before the source's LastQualifying advances past
	// it. Derivation is correct only while the row was present in the source's
	// most recent qualifying run; from the moment it goes missing, the derived
	// answer would be this run — the very run that did not see it.
	if row.LastSeen.IsZero() {
		row.LastSeen = base.LastSeen(*row)
	}

	row.Missing++

	if row.Missing < policy.MissingRuns {
		churn.Missing++

		return
	}

	row.Closed = &Closure{
		LastSeen:    row.LastSeen,
		ConfirmedAt: runAt,
		Reason:      ReasonAbsent,
	}

	churn.Closed++
}

// lapse archives rows whose source has stopped being crawled.
//
// A lapsed row never carries a closing date, because nobody observed one. That
// distinction — "we do not know" against "it closed" — is the reason the corpus
// has four states rather than two, and it has to survive all the way into a UI.
func lapse(next *Corpus, rows []Row, runAt time.Time, policy Policy, churn *Churn) {
	for i := range rows {
		if rows[i].Closed != nil {
			continue
		}

		state, known := next.Source(rows[i].Posting.Source)

		reason := ReasonLapsed
		if known && state.Retired {
			reason = ReasonRetired
		}

		lastSeen := next.LastSeen(rows[i])

		switch {
		case !known || state.LastQualifying.IsZero():
			// A row whose source has never qualified has no interval to measure, so
			// it cannot lapse on elapsed time. It stays out of the open count via
			// [Corpus.State] instead.
			continue
		case runAt.Sub(state.LastQualifying) < policy.LapseAfter:
			continue
		}

		rows[i].Closed = &Closure{LastSeen: lastSeen, ConfirmedAt: runAt, Reason: reason}
		churn.Lapsed++
	}
}

// rollForward computes the next generation's source states.
func rollForward(
	base *Corpus,
	in RunInput,
	runs map[jobposting.PostingSource]SourceRun,
	verdicts map[jobposting.PostingSource]Verdict,
	demoted map[jobposting.PostingSource]bool,
	runAt time.Time,
) []SourceState {
	states := make(map[jobposting.PostingSource]SourceState, len(base.sources)+len(runs))
	for _, state := range base.sources {
		states[state.Source] = state
	}

	for _, source := range sortedSources(runs) {
		run := runs[source]

		state, ok := states[source]
		if !ok {
			state = SourceState{Source: source}
		}

		if run.Company != "" {
			state.Company = run.Company
		}

		state.LastAttempt = runAt
		state.LastDurationMS = run.DurationMS
		state.LastPostings = run.Postings

		if run.Status == StatusComplete && run.Errors == 0 {
			state.ConsecutiveFailures = 0

			// An empty run that is otherwise well-formed advances the streak even
			// when it does not qualify. That is what makes the streak a count of
			// consecutive quiet runs rather than a count of runs the guard let
			// through, which would never reach its own threshold.
			if run.Postings == 0 {
				state.EmptyStreak++
			} else {
				state.EmptyStreak = 0
			}
		} else {
			state.ConsecutiveFailures++
		}

		if verdicts[source].Qualifies {
			state.LastQualifying = runAt
			state.Trailing = append(state.Trailing, run.Postings)

			if len(state.Trailing) > MaxTrailing {
				state.Trailing = state.Trailing[len(state.Trailing)-MaxTrailing:]
			}
		}

		// A source that has ever demoted requisition identity never promotes it
		// again, so this only ever goes from false to true.
		if demoted[source] {
			state.RequisitionUnsafe = true
		}

		states[source] = state
	}

	for _, source := range in.Retired {
		state, ok := states[source]
		if !ok {
			continue
		}

		state.Retired = true
		states[source] = state
	}

	out := make([]SourceState, 0, len(states))
	for _, state := range states {
		out = append(out, state)
	}

	slices.SortFunc(out, func(a, b SourceState) int { return compareSources(a.Source, b.Source) })

	return out
}

func sortedSources[V any](m map[jobposting.PostingSource]V) []jobposting.PostingSource {
	out := make([]jobposting.PostingSource, 0, len(m))
	for source := range m {
		out = append(out, source)
	}

	slices.SortFunc(out, compareSources)

	return out
}

// postingChanged reports whether two observations of the same posting differ in
// anything the corpus stores. It is spelled out rather than delegated to
// reflect.DeepEqual so that adding a field to [jobposting.JobPosting] without
// adding it here is a visible omission rather than a silent behaviour change.
func postingChanged(old, latest *jobposting.JobPosting) bool {
	if old.Company != latest.Company ||
		old.URL != latest.URL ||
		old.Title != latest.Title ||
		old.Location != latest.Location ||
		old.Department != latest.Department ||
		old.Team != latest.Team ||
		old.EmploymentType != latest.EmploymentType ||
		old.WorkplaceType != latest.WorkplaceType ||
		old.Seniority != latest.Seniority ||
		old.RequisitionID != latest.RequisitionID ||
		old.ExternalID != latest.ExternalID ||
		old.Source != latest.Source ||
		!old.PostedAt.Equal(latest.PostedAt) ||
		!old.UpdatedAt.Equal(latest.UpdatedAt) {
		return true
	}

	if (old.Remote == nil) != (latest.Remote == nil) {
		return true
	}

	if old.Remote != nil && *old.Remote != *latest.Remote {
		return true
	}

	if (old.Compensation == nil) != (latest.Compensation == nil) {
		return true
	}

	return old.Compensation != nil && *old.Compensation != *latest.Compensation
}

// WriteTo publishes the generation.
//
// Order is load-bearing: the postings, then the sources, then the run history,
// and the manifest last. Nothing points at a half-written generation, so a
// writer that dies partway leaves the previous generation readable rather than
// leaving a corrupt one.
func (g *Generation) WriteTo(ctx context.Context, publisher Publisher) error {
	builder, err := buildTable(g.Rows)
	if err != nil {
		return err
	}

	g.Manifest.ContentDigest = builder.ContentDigest()
	g.Manifest.Rows = len(g.Rows)

	if err := writeObject(ctx, publisher, PostingsFile, func(w io.Writer) error {
		_, err := builder.WriteTo(w)

		return err
	}); err != nil {
		return err
	}

	if err := writeObject(ctx, publisher, SourcesFile, func(w io.Writer) error {
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")

		// Never nil: an empty corpus must serialize as [] rather than null, because
		// a reader distinguishing the two would be distinguishing nothing.
		sources := g.Sources
		if sources == nil {
			sources = []SourceState{}
		}

		return encoder.Encode(sources)
	}); err != nil {
		return err
	}

	if err := writeObject(ctx, publisher, RunsFile, func(w io.Writer) error {
		encoder := json.NewEncoder(w)

		for _, record := range g.Runs {
			if err := encoder.Encode(record); err != nil {
				return err
			}
		}

		return nil
	}); err != nil {
		return err
	}

	return publisher.Commit(ctx, g.Manifest)
}

func writeObject(ctx context.Context, publisher Publisher, name string, write func(io.Writer) error) error {
	file, err := publisher.Create(ctx, name)
	if err != nil {
		return fmt.Errorf("corpus: create %s: %w", name, err)
	}

	buffered := bufio.NewWriterSize(file, 128*1024)

	if err := write(buffered); err != nil {
		file.Close()

		return fmt.Errorf("corpus: write %s: %w", name, err)
	}

	if err := buffered.Flush(); err != nil {
		file.Close()

		return fmt.Errorf("corpus: flush %s: %w", name, err)
	}

	return file.Close()
}

// readRuns loads runs.ndjson. A corpus with no run history is not an error: the
// first generation has none.
func readRuns(ctx context.Context, store Store) ([]RunRecord, error) {
	raw, err := ReadFile(ctx, store, RunsFile)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}

		return nil, err
	}

	var records []RunRecord

	decoder := json.NewDecoder(bytes.NewReader(raw))
	for {
		var record RunRecord

		if err := decoder.Decode(&record); err != nil {
			if errors.Is(err, io.EOF) {
				return records, nil
			}

			return nil, fmt.Errorf("corpus: decode %s: %w", RunsFile, err)
		}

		records = append(records, record)
	}
}
