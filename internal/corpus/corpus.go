package corpus

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"os"
	"path"
	"path/filepath"
	"slices"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/jobposting"
)

// Format versions.
const (
	// FormatVersion is bumped for any change of shape. Adding an optional column
	// or an optional manifest field is not a change of shape: readers address
	// columns by name and ignore unknown JSON fields, so a v1 reader opening a
	// corpus that only added things gets correct answers about everything it
	// understands.
	FormatVersion = 1

	// MinReaderVersion is what a reader must support to read this corpus without
	// being silently wrong. A change that reuses a field name with a new meaning,
	// or changes what LastSeen means, or changes the closure policy's semantics,
	// bumps this and old readers refuse. Fail closed: a plausible wrong answer is
	// worse than an error, which is the same discipline `shard merge` already
	// applies to schema and commit mismatches.
	MinReaderVersion = 1
)

// File names inside a corpus directory.
//
// sources.json and runs.ndjson are deliberately not columnar. They are under a
// megabyte, they are read whole every time, and being reviewable in a diff is
// worth more than being fast.
const (
	PostingsFile = "corpus.jhtc"
	SourcesFile  = "sources.json"
	RunsFile     = "runs.ndjson"
	ManifestFile = "manifest.json"
)

// Manifest is the corpus's only entry point. Readers reach every other file
// through it, and a publisher writes it last: that is the commit point, and it
// is the only atomicity primitive available in a browser, where there is no
// rename and no file lock.
type Manifest struct {
	FormatVersion    int `json:"format_version"`
	MinReaderVersion int `json:"min_reader_version"`
	IdentityVersion  int `json:"identity_version"`

	// Generation increments once per published corpus.
	Generation int64 `json:"generation"`

	// RunAt is the single clock reading of the producing run. Every timestamp any
	// corpus writer emits during that run is this value.
	RunAt time.Time `json:"run_at,omitzero"`

	// Writer identifies the binary that produced this generation.
	Writer string `json:"writer,omitempty"`

	// Policy is recorded because the meaning of "closed" depends on it. A consumer
	// comparing two generations built under different policies is comparing two
	// different questions, and this is how it can see that.
	Policy Policy `json:"policy"`

	// ContentDigest is SHA-256 over the uncompressed column payloads, in footer
	// order. It is the corpus's identity: compress/flate's output is not promised
	// stable across Go releases, so hashing the file would make a toolchain
	// upgrade look like a data change.
	ContentDigest string `json:"content_digest"`

	Rows    int `json:"rows"`
	Sources int `json:"sources"`

	// Open is the number of distinct dedupe keys among rows in state Open. It is
	// the number this format owes jobs_record.txt, and it is `shard merge`'s
	// global union restricted to postings currently believed open — never a sum.
	Open   int `json:"open"`
	Stale  int `json:"stale"`
	Closed int `json:"closed"`
	Lapsed int `json:"lapsed"`

	// Partial records that the producing crawl did not finish. A partial crawl is
	// never recorded as complete; under a budget model partial is the normal case,
	// so it is a field rather than a failure.
	Partial bool `json:"partial,omitempty"`
}

// ErrReaderTooOld reports a corpus this build must not read.
var ErrReaderTooOld = errors.New("corpus: reader too old")

func (m Manifest) check() error {
	if m.MinReaderVersion > FormatVersion {
		return fmt.Errorf("%w: corpus requires reader version %d, this build supports %d",
			ErrReaderTooOld, m.MinReaderVersion, FormatVersion)
	}

	return nil
}

// Store is everything the corpus needs to read.
//
// It is [io.ReaderAt] over named objects rather than [io.Reader], because the
// whole value of a columnar file is the selective read: an aggregate over two
// columns must not pay for the other twenty-five, and one column is one
// contiguous byte range. os.DirFS, an HTTP client issuing Range requests and
// IndexedDB can all implement this; none of it assumes a file, a lock, an mmap
// or a syscall a browser lacks.
type Store interface {
	// Size reports the size of name in bytes.
	Size(ctx context.Context, name string) (int64, error)

	// ReadAt reads len(p) bytes from name starting at off, with [io.ReaderAt]'s
	// contract: a short read must return an error.
	ReadAt(ctx context.Context, name string, p []byte, off int64) (int, error)
}

// Publisher writes a generation. Commit writes the manifest, which is the only
// atomic step and the only thing readers follow, so a half-written generation is
// invisible because nothing points at it.
type Publisher interface {
	Create(ctx context.Context, name string) (io.WriteCloser, error)
	Commit(ctx context.Context, manifest Manifest) error
}

// ReadFile reads a whole object out of a store.
func ReadFile(ctx context.Context, store Store, name string) ([]byte, error) {
	size, err := store.Size(ctx, name)
	if err != nil {
		return nil, err
	}

	buf := make([]byte, size)
	if size == 0 {
		return buf, nil
	}

	if _, err := store.ReadAt(ctx, name, buf, 0); err != nil {
		return nil, err
	}

	return buf, nil
}

// DirStore reads a corpus from a directory.
type DirStore struct{ Dir string }

func (d DirStore) resolve(name string) (string, error) {
	// path.Clean plus the separator check keeps a manifest from naming
	// ../../etc/passwd. The corpus is downloaded from the internet, so every name
	// that reaches the filesystem is untrusted input.
	cleaned := path.Clean("/" + name)
	if cleaned == "/" {
		return "", fmt.Errorf("corpus: invalid object name %q", name)
	}

	return filepath.Join(d.Dir, filepath.FromSlash(cleaned[1:])), nil
}

// Size implements [Store].
func (d DirStore) Size(_ context.Context, name string) (int64, error) {
	resolved, err := d.resolve(name)
	if err != nil {
		return 0, err
	}

	info, err := os.Stat(resolved)
	if err != nil {
		return 0, err
	}

	return info.Size(), nil
}

// ReadAt implements [Store].
func (d DirStore) ReadAt(_ context.Context, name string, p []byte, off int64) (int, error) {
	resolved, err := d.resolve(name)
	if err != nil {
		return 0, err
	}

	file, err := os.Open(resolved)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	return io.ReadFull(io.NewSectionReader(file, off, int64(len(p))), p)
}

// DirPublisher writes a corpus into a directory.
//
// It writes each object in full before the next and the manifest last. It does
// not rename, does not lock and does not fsync: the corpus is derived state,
// rebuildable from the immutable shard artifacts, and the manifest-last rule is
// the only atomicity the browser target can offer, so making the filesystem path
// stronger than the browser path would mean two commit stories to reason about.
type DirPublisher struct{ Dir string }

// Create implements [Publisher].
func (d DirPublisher) Create(_ context.Context, name string) (io.WriteCloser, error) {
	store := DirStore(d)

	resolved, err := store.resolve(name)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
		return nil, err
	}

	return os.Create(resolved)
}

// Commit implements [Publisher].
func (d DirPublisher) Commit(ctx context.Context, manifest Manifest) error {
	file, err := d.Create(ctx, ManifestFile)
	if err != nil {
		return err
	}

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(manifest); err != nil {
		file.Close()

		return err
	}

	return file.Close()
}

// Corpus is a published generation, opened for reading.
//
// Opening reads the manifest, the source states and the postings file's footer —
// a few kilobytes — and no column payload. Rows and columns are fetched on
// demand.
type Corpus struct {
	manifest Manifest
	sources  []SourceState
	bySource map[jobposting.PostingSource]int
	runs     []RunRecord
	table    *Table
}

// Open reads a corpus's manifest, sources and postings footer.
func Open(ctx context.Context, store Store) (*Corpus, error) {
	raw, err := ReadFile(ctx, store, ManifestFile)
	if err != nil {
		return nil, fmt.Errorf("corpus: read manifest: %w", err)
	}

	var manifest Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, fmt.Errorf("corpus: decode manifest: %w", err)
	}

	if err := manifest.check(); err != nil {
		return nil, err
	}

	sourcesRaw, err := ReadFile(ctx, store, SourcesFile)
	if err != nil {
		return nil, fmt.Errorf("corpus: read %s: %w", SourcesFile, err)
	}

	var sources []SourceState
	if err := json.Unmarshal(sourcesRaw, &sources); err != nil {
		return nil, fmt.Errorf("corpus: decode %s: %w", SourcesFile, err)
	}

	size, err := store.Size(ctx, PostingsFile)
	if err != nil {
		return nil, fmt.Errorf("corpus: stat %s: %w", PostingsFile, err)
	}

	table, err := OpenTable(&storeReaderAt{ctx: ctx, store: store, name: PostingsFile}, size)
	if err != nil {
		return nil, err
	}

	if table.Rows() != manifest.Rows {
		return nil, formatErr("manifest claims %d rows, %s holds %d",
			manifest.Rows, PostingsFile, table.Rows())
	}

	runs, err := readRuns(ctx, store)
	if err != nil {
		return nil, err
	}

	corpus := newCorpus(manifest, sources, table)
	corpus.runs = runs

	return corpus, nil
}

func newCorpus(manifest Manifest, sources []SourceState, table *Table) *Corpus {
	c := &Corpus{
		manifest: manifest,
		sources:  sources,
		bySource: make(map[jobposting.PostingSource]int, len(sources)),
		table:    table,
	}

	for i, state := range sources {
		c.bySource[state.Source] = i
	}

	return c
}

// Empty returns a corpus with no rows and no sources, which is what [Apply]
// folds the first run into.
func Empty() *Corpus {
	return newCorpus(
		Manifest{FormatVersion: FormatVersion, MinReaderVersion: MinReaderVersion, IdentityVersion: IdentityVersion},
		nil,
		emptyTable(),
	)
}

func emptyTable() *Table {
	var buf bytes.Buffer

	if _, err := NewBuilder(0).WriteTo(&buf); err != nil {
		panic("corpus: writing an empty table cannot fail: " + err.Error())
	}

	table, err := OpenTable(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		panic("corpus: reading an empty table cannot fail: " + err.Error())
	}

	return table
}

// Manifest returns the generation's manifest.
func (c *Corpus) Manifest() Manifest { return c.manifest }

// Table returns the postings file, for a caller that wants one column rather
// than every row. This is the streaming read mode.
func (c *Corpus) Table() *Table { return c.table }

// Sources returns the per-integration state, in the corpus's stored order —
// sorted by platform then key.
func (c *Corpus) Sources() []SourceState { return slices.Clone(c.sources) }

// Runs returns the retained run history, oldest first.
func (c *Corpus) Runs() []RunRecord { return slices.Clone(c.runs) }

// Source returns one integration's state.
func (c *Corpus) Source(source jobposting.PostingSource) (SourceState, bool) {
	index, ok := c.bySource[source]
	if !ok {
		return SourceState{Source: source}, false
	}

	return c.sources[index], true
}

// Rows decodes every row.
//
// It is an [iter.Seq2] for the same reason internal.Jobs is: a caller that wants
// ten postings must not be forced to hold 780,489. The decode itself is not
// incremental — a columnar file has to read a whole column to read a row — so
// this bounds what the caller keeps, not what the reader allocates. A caller
// that genuinely wants ten rows should select on a column first.
func (c *Corpus) Rows(context.Context) iter.Seq2[Row, error] {
	return func(yield func(Row, error) bool) {
		rows, err := readRows(c.table)
		if err != nil {
			yield(Row{}, err)

			return
		}

		for _, row := range rows {
			if !yield(row, nil) {
				return
			}
		}
	}
}

// LastSeen resolves a row's last observation against its source's state.
//
// A stored LastSeen wins, then a closure's, then the source's LastQualifying.
// The last case is the common one and the reason an open row's bytes do not
// change between generations: "still open" is derived from one field on the
// source rather than written on every row.
func (c *Corpus) LastSeen(row Row) time.Time {
	if row.Closed != nil && !row.Closed.LastSeen.IsZero() {
		return row.Closed.LastSeen
	}

	if !row.LastSeen.IsZero() {
		return row.LastSeen
	}

	state, _ := c.Source(row.Posting.Source)

	return state.LastQualifying
}

// State computes a row's lifecycle state. It is a pure function of the row, its
// source's state and the clock reading the caller supplies.
func (c *Corpus) State(row Row, now time.Time) State {
	policy := c.manifest.Policy.withDefaults()

	if row.Closed != nil {
		if row.Closed.Reason == ReasonAbsent {
			return StateClosed
		}

		// Lapsed and retired are archived alongside closures and are emphatically
		// not closures: nobody observed an end, so reporting one would be inventing
		// a date.
		return StateLapsed
	}

	state, known := c.Source(row.Posting.Source)
	if !known || state.LastQualifying.IsZero() {
		// A row whose source has never had a qualifying run has never had its
		// absence tested. It cannot be called open.
		return StateLapsed
	}

	since := now.Sub(state.LastQualifying)

	switch {
	case since >= policy.LapseAfter:
		return StateLapsed
	case since >= policy.FreshnessTarget:
		return StateStale
	default:
		return StateOpen
	}
}

// Verify recomputes the content digest and checks it against the manifest.
//
// It reads every column, so it is the expensive check, and it is the one that
// catches a truncated download, a corrupted object store and a range request
// that returned a re-compressed body. Compression is not checked: the digest is
// over uncompressed payloads precisely so a Go release that changes
// compress/flate cannot red this.
func Verify(_ context.Context, c *Corpus) error {
	if err := c.manifest.check(); err != nil {
		return err
	}

	digest, err := c.table.ContentDigest()
	if err != nil {
		return err
	}

	if c.manifest.ContentDigest != "" && digest != c.manifest.ContentDigest {
		return fmt.Errorf("corpus: content digest %s does not match the manifest's %s",
			digest, c.manifest.ContentDigest)
	}

	if got, want := c.table.Rows(), c.manifest.Rows; got != want {
		return formatErr("manifest claims %d rows, %s holds %d", want, PostingsFile, got)
	}

	for i := 1; i < len(c.sources); i++ {
		if compareSources(c.sources[i-1].Source, c.sources[i].Source) >= 0 {
			return fmt.Errorf("corpus: %s is not sorted at entry %d", SourcesFile, i)
		}
	}

	return nil
}

func compareSources(a, b jobposting.PostingSource) int {
	if c := cmp.Compare(a.Platform, b.Platform); c != 0 {
		return c
	}

	return cmp.Compare(a.Key, b.Key)
}

// storeReaderAt adapts a [Store] object to [io.ReaderAt].
//
// The context is captured rather than threaded because io.ReaderAt has no place
// to put one, and every call site is inside a single Open or query whose context
// is the one held here.
type storeReaderAt struct {
	ctx   context.Context
	store Store
	name  string
}

func (s *storeReaderAt) ReadAt(p []byte, off int64) (int, error) {
	return s.store.ReadAt(s.ctx, s.name, p, off)
}
