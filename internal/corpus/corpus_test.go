package corpus

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/jobposting"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestRowRoundTripsEveryField(t *testing.T) {
	t.Parallel()

	// Every field the corpus stores, including the ones whose absence is a fact:
	// a nil *bool is not false, a nil *Compensation is not an empty one, and a
	// zero time is not 1970.
	yes := true
	no := false

	src := source("greenhouse", "acme")

	want := []Row{
		{
			ID:        ID(src, BasisExternal, "1"),
			Basis:     BasisExternal,
			DedupeKey: "abc",
			FirstSeen: day(0),
			LastSeen:  day(1),
			Missing:   3,
			Reopens:   2,
			Closed:    &Closure{LastSeen: day(1), ConfirmedAt: day(2), Reason: ReasonAbsent},
			Posting: jobposting.JobPosting{
				Source:         src,
				Company:        "acme",
				URL:            "https://example.com/1",
				Title:          "Engineer",
				Location:       "Remote",
				Department:     "Engineering",
				Team:           "Platform",
				EmploymentType: jobposting.EmploymentTypeFullTime,
				WorkplaceType:  jobposting.WorkplaceTypeRemote,
				Seniority:      "Staff",
				RequisitionID:  "REQ-1",
				ExternalID:     "1",
				Remote:         &yes,
				PostedAt:       time.Date(2026, 3, 1, 12, 30, 45, 123_000_000, time.UTC),
				UpdatedAt:      time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC),
				Compensation: &jobposting.Compensation{
					Min:        120000.5,
					Max:        180000,
					Currency:   "USD",
					Period:     jobposting.PeriodYear,
					Summary:    "$120K – $180K • Offers Equity",
					Provenance: jobposting.ProvenanceEmployer,
				},
			},
		},
		{
			// The all-absent row: no closure, no compensation, no remote flag, no
			// dates. Every one of those has to come back absent rather than zeroed.
			ID:        ID(src, BasisDescriptor, "2"),
			Basis:     BasisDescriptor,
			DedupeKey: "def",
			FirstSeen: day(0),
			Posting:   jobposting.JobPosting{Source: src, Company: "acme"},
		},
		{
			// A board that has a pay field and left it blank is not a board with no
			// pay field, and Remote explicitly false is not Remote absent.
			ID:        ID(src, BasisURL, "3"),
			Basis:     BasisURL,
			DedupeKey: "ghi",
			FirstSeen: day(0),
			Posting: jobposting.JobPosting{
				Source:       src,
				Remote:       &no,
				Compensation: &jobposting.Compensation{},
			},
		},
	}

	builder, err := buildTable(want)
	must.NoError(t, err)

	table := writeAndOpen(t, builder)

	got, err := readRows(table)
	must.NoError(t, err)

	must.SliceLen(t, len(want), got)

	for i := range want {
		test.Eq(t, want[i].ID, got[i].ID)
		test.Eq(t, want[i].Basis, got[i].Basis)
		test.Eq(t, want[i].DedupeKey, got[i].DedupeKey)
		test.Eq(t, want[i].FirstSeen, got[i].FirstSeen)
		test.Eq(t, want[i].LastSeen, got[i].LastSeen)
		test.Eq(t, want[i].Missing, got[i].Missing)
		test.Eq(t, want[i].Reopens, got[i].Reopens)
		test.Eq(t, want[i].Closed, got[i].Closed)
		test.Eq(t, want[i].Posting, got[i].Posting)
	}
}

func writeAndOpen(t *testing.T, builder *Builder) *Table {
	t.Helper()

	dir := t.TempDir()

	file, err := os.Create(filepath.Join(dir, PostingsFile))
	must.NoError(t, err)

	_, err = builder.WriteTo(file)
	must.NoError(t, err)
	must.NoError(t, file.Close())

	store := DirStore{Dir: dir}

	size, err := store.Size(t.Context(), PostingsFile)
	must.NoError(t, err)

	table, err := OpenTable(&storeReaderAt{ctx: t.Context(), store: store, name: PostingsFile}, size)
	must.NoError(t, err)

	return table
}

func TestPublishAndOpenThroughADirectory(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	src := source("greenhouse", "acme")

	generation, err := Apply(t.Context(), Empty(), RunInput{
		RunAt:    day(0),
		Writer:   "job-hunter-toolkit test",
		Sources:  []SourceRun{completeRun(src, 1)},
		Postings: seq(posting(src, "ext-1", "", "https://example.com/1", "Engineer", "Remote")),
	}, Policy{})
	must.NoError(t, err)

	must.NoError(t, generation.WriteTo(t.Context(), DirPublisher{Dir: dir}))

	for _, name := range []string{PostingsFile, SourcesFile, RunsFile, ManifestFile} {
		_, err := os.Stat(filepath.Join(dir, name))
		must.NoError(t, err, must.Sprintf("%s should exist", name))
	}

	corpus, err := Open(t.Context(), DirStore{Dir: dir})
	must.NoError(t, err)
	must.NoError(t, Verify(t.Context(), corpus))

	test.Eq(t, 1, corpus.Manifest().Rows)
	test.Eq(t, "job-hunter-toolkit test", corpus.Manifest().Writer)
	test.Eq(t, FormatVersion, corpus.Manifest().FormatVersion)
	test.Eq(t, IdentityVersion, corpus.Manifest().IdentityVersion)
	test.SliceLen(t, 1, corpus.Runs())

	// sources.json is meant to be reviewable in a diff, which means indented JSON
	// and a stable order rather than a columnar blob.
	body, err := os.ReadFile(filepath.Join(dir, SourcesFile))
	must.NoError(t, err)
	test.StrContains(t, string(body), "\n  {\n")
}

func TestOpenRefusesACorpusThatNeedsANewerReader(t *testing.T) {
	t.Parallel()

	// Fail closed. A plausible wrong answer is worse than an error, which is the
	// same discipline `shard merge` already applies to schema mismatches.
	dir := t.TempDir()
	src := source("greenhouse", "acme")

	generation, err := Apply(t.Context(), Empty(), RunInput{
		RunAt:    day(0),
		Sources:  []SourceRun{completeRun(src, 1)},
		Postings: seq(posting(src, "ext-1", "", "https://example.com/1", "Engineer", "Remote")),
	}, Policy{})
	must.NoError(t, err)

	generation.Manifest.MinReaderVersion = FormatVersion + 1
	must.NoError(t, generation.WriteTo(t.Context(), DirPublisher{Dir: dir}))

	_, err = Open(t.Context(), DirStore{Dir: dir})
	must.ErrorIs(t, err, ErrReaderTooOld)
}

func TestOpenIgnoresUnknownManifestFields(t *testing.T) {
	t.Parallel()

	// Additive changes must not bump MinReaderVersion, so a v1 reader opening a
	// corpus that only added fields has to get correct answers about everything it
	// understands. encoding/json does this by default and corpus reads must never
	// set DisallowUnknownFields.
	dir := t.TempDir()
	src := source("greenhouse", "acme")

	generation, err := Apply(t.Context(), Empty(), RunInput{
		RunAt:    day(0),
		Sources:  []SourceRun{completeRun(src, 1)},
		Postings: seq(posting(src, "ext-1", "", "https://example.com/1", "Engineer", "Remote")),
	}, Policy{})
	must.NoError(t, err)
	must.NoError(t, generation.WriteTo(t.Context(), DirPublisher{Dir: dir}))

	path := filepath.Join(dir, ManifestFile)

	var raw map[string]any

	body, err := os.ReadFile(path)
	must.NoError(t, err)
	must.NoError(t, json.Unmarshal(body, &raw))

	raw["something_a_later_version_added"] = []string{"a", "b"}

	body, err = json.Marshal(raw)
	must.NoError(t, err)
	must.NoError(t, os.WriteFile(path, body, 0o600))

	corpus, err := Open(t.Context(), DirStore{Dir: dir})
	must.NoError(t, err)
	test.Eq(t, 1, corpus.Manifest().Rows)
}

func TestVerifyCatchesATamperedPostingsFile(t *testing.T) {
	t.Parallel()

	// The digest is what a client checks after a range request, because GitHub
	// Pages has a documented history of returning a range of a re-compressed body
	// that decodes to garbage.
	dir := t.TempDir()
	src := source("greenhouse", "acme")

	generation, err := Apply(t.Context(), Empty(), RunInput{
		RunAt:   day(0),
		Sources: []SourceRun{completeRun(src, 2)},
		Postings: seq(
			posting(src, "ext-1", "", "https://example.com/1", "Engineer", "Remote"),
			posting(src, "ext-2", "", "https://example.com/2", "Designer", "Berlin"),
		),
	}, Policy{})
	must.NoError(t, err)
	must.NoError(t, generation.WriteTo(t.Context(), DirPublisher{Dir: dir}))

	corpus, err := Open(t.Context(), DirStore{Dir: dir})
	must.NoError(t, err)
	must.NoError(t, Verify(t.Context(), corpus))

	path := filepath.Join(dir, ManifestFile)

	var manifest Manifest

	body, err := os.ReadFile(path)
	must.NoError(t, err)
	must.NoError(t, json.Unmarshal(body, &manifest))

	manifest.ContentDigest = "0000000000000000000000000000000000000000000000000000000000000000"

	body, err = json.Marshal(manifest)
	must.NoError(t, err)
	must.NoError(t, os.WriteFile(path, body, 0o600))

	corpus, err = Open(t.Context(), DirStore{Dir: dir})
	must.NoError(t, err)
	test.Error(t, Verify(t.Context(), corpus))
}

func TestDirStoreRefusesToEscapeItsDirectory(t *testing.T) {
	t.Parallel()

	// The manifest is downloaded from the internet, so every name that reaches
	// the filesystem is untrusted input.
	dir := t.TempDir()
	must.NoError(t, os.WriteFile(filepath.Join(dir, "secret"), []byte("x"), 0o600))

	nested := filepath.Join(dir, "corpus")
	must.NoError(t, os.MkdirAll(nested, 0o755))

	store := DirStore{Dir: nested}

	_, err := store.Size(t.Context(), "../secret")
	test.Error(t, err)

	_, err = store.Size(t.Context(), "/etc/passwd")
	test.Error(t, err)
}

func TestEmptyCorpusIsUsable(t *testing.T) {
	t.Parallel()

	corpus := Empty()

	test.Eq(t, 0, corpus.Manifest().Rows)
	test.SliceEmpty(t, corpus.Sources())
	test.SliceEmpty(t, corpus.Runs())

	for _, err := range corpus.Rows(t.Context()) {
		must.NoError(t, err)

		t.Fatal("an empty corpus yielded a row")
	}

	must.NoError(t, Verify(t.Context(), corpus))
}

func TestSortRowsIsTotalAndSourceMajor(t *testing.T) {
	t.Parallel()

	// Grouping a source's rows is what puts its shared URL prefix, company name
	// and location vocabulary inside the compressor's window.
	rows := []Row{
		{ID: "b", Posting: jobposting.JobPosting{Source: source("workday", "z")}},
		{ID: "a", Posting: jobposting.JobPosting{Source: source("ashby", "b")}},
		{ID: "c", Posting: jobposting.JobPosting{Source: source("ashby", "a")}},
		{ID: "a", Posting: jobposting.JobPosting{Source: source("workday", "z")}},
	}

	sortRows(rows)

	test.Eq(t, []string{"ashby/a/c", "ashby/b/a", "workday/z/a", "workday/z/b"},
		[]string{
			rows[0].Posting.Source.Platform + "/" + rows[0].Posting.Source.Key + "/" + rows[0].ID,
			rows[1].Posting.Source.Platform + "/" + rows[1].Posting.Source.Key + "/" + rows[1].ID,
			rows[2].Posting.Source.Platform + "/" + rows[2].Posting.Source.Key + "/" + rows[2].ID,
			rows[3].Posting.Source.Platform + "/" + rows[3].Posting.Source.Key + "/" + rows[3].ID,
		})
}

func TestReadRowsRejectsAClosureTimestampWithNoReason(t *testing.T) {
	t.Parallel()

	// closed_reason doubles as the presence flag for Closure, so a file carrying a
	// closing date and no reason is corrupt and must not silently resurrect a row.
	builder := NewBuilder(1)
	must.NoError(t, builder.AddStrings(colClosedWhy, []string{""}))
	must.NoError(t, builder.AddInts(colClosedAt, []int64{encodeTime(day(1))}))

	table := writeAndOpen(t, builder)

	_, err := readRows(table)
	must.ErrorIs(t, err, ErrFormat)
}

func TestReadRowsRejectsAnUnparseableCompensationFigure(t *testing.T) {
	t.Parallel()

	builder := NewBuilder(1)
	must.NoError(t, builder.AddInts(colComp, []int64{1}))
	must.NoError(t, builder.AddStrings(colCompMin, []string{"not a number"}))

	table := writeAndOpen(t, builder)

	_, err := readRows(table)
	must.ErrorIs(t, err, ErrFormat)
}

func TestEncodeFloatRoundTripsExactly(t *testing.T) {
	t.Parallel()

	for _, f := range []float64{0, 1, 0.1, 120000.5, 1e300, -42.125, 1.0 / 3.0} {
		got, err := decodeFloat(encodeFloat(f))
		must.NoError(t, err)
		test.Eq(t, f, got, test.Sprintf("value %v", f))
	}
}
