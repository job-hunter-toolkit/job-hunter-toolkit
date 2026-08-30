package corpus

import (
	"bytes"
	"compress/flate"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"math"
	"slices"
	"sort"
)

// The .jhtc container format.
//
//	file          := magic column* footer footer_offset magic
//	magic         := "JHTC" u8:major u8:minor
//	column        := flate(payload)
//	footer        := uvarint:nrows uvarint:ncols entry*
//	entry         := uvarint:namelen utf8:name u8:encoding
//	                 uvarint:offset uvarint:complen uvarint:rawlen
//	footer_offset := u64le
//
// Three payload encodings, chosen per column by the writer and recorded in the
// footer:
//
//	1 dict  := uvarint:ndict (uvarint:len utf8)* uvarint:nrows uvarint:id*
//	2 raw   := uvarint:nrows (uvarint:len utf8)*
//	3 delta := uvarint:nrows zigzag_varint:delta*
//
// Deviation from docs/design/storage-engine.md §5, reported deliberately: the
// footer carries a leading uvarint row count that the doc's grammar does not.
// The doc requires that "a reader reads a column the file lacks as the zero
// value", and a reader cannot produce the right *number* of zero values without
// knowing the table's height before it decodes a column. The alternative —
// decompressing an arbitrary column at open time to learn the height — costs a
// full column read on a path the doc measures as two reads and a few kilobytes.
// One varint in the footer is cheaper than either.
const (
	// magicString and the two version bytes open and close the file. The trailing
	// copy is what lets a reader that has only the tail confirm it is looking at
	// a .jhtc rather than at a truncated prefix of one.
	magicString = "JHTC"

	// formatMajor is the only version byte a reader refuses on. formatMinor is
	// informational: columns are addressed by name, so adding one is not a
	// version event.
	formatMajor uint8 = 0
	formatMinor uint8 = 1

	magicLen   = len(magicString) + 2
	trailerLen = magicLen + 8 // footer_offset + closing magic
)

// compressionLevel is fixed rather than defaulted, because "deterministic" has
// to mean the writer made a choice.
//
// Measured on 780,489 rows: [flate.DefaultCompression] encodes the whole table
// in 7.66 s for 36.20 MiB, and [flate.BestSpeed] in 7.25 s for 37.92 MiB. The
// 5% of time BestSpeed buys costs 4.7% of the file, which is a bad trade against
// a 720-second crawl — and the reason it buys so little is worth recording:
// compression is *not* the bottleneck in the write. Materialising 33 columns of
// 780,489 values is, so tuning the compressor is tuning the wrong end.
const compressionLevel = flate.DefaultCompression

// maxColumnBytes caps what a footer entry may claim a column decompresses to.
//
// This exists because the format is read from a network and from a browser, and
// storage-engine.md §9 records that the prototype had no bounds checking: "a
// corrupt rawlen currently allocates whatever it says". A hostile or truncated
// file must not be able to talk the reader into a multi-gigabyte allocation
// before a single byte of payload has been validated. 1 GiB is far above the
// largest real column (the URL column of a 780k-row corpus is ~2 MiB) and far
// below anything that would take a process down.
const maxColumnBytes = 1 << 30

// Encoding identifies a column's payload layout. It is stored in the footer, so
// a reader never guesses.
type Encoding uint8

// The payload encodings.
const (
	// EncodingDict stores a sorted dictionary of distinct values followed by one
	// id per row. Chosen for low-cardinality string columns — platform, company,
	// location, the two normalized enums — where the ids compress to almost
	// nothing.
	EncodingDict Encoding = 1

	// EncodingRaw stores one length-prefixed string per row. Chosen for columns
	// whose values are near-unique, like url and external_id, where a dictionary
	// is pure overhead.
	EncodingRaw Encoding = 2

	// EncodingDelta stores zigzag varint deltas between consecutive int64 values.
	// Every timestamp, count and boolean in the corpus is an int64 in this
	// encoding.
	EncodingDelta Encoding = 3
)

func (e Encoding) String() string {
	switch e {
	case EncodingDict:
		return "dict"
	case EncodingRaw:
		return "raw"
	case EncodingDelta:
		return "delta"
	default:
		return fmt.Sprintf("encoding(%d)", uint8(e))
	}
}

// ErrFormat reports a file that is not a readable .jhtc. Every corrupt-input
// path in this file wraps it, so a caller can tell "this is not my format" from
// "the disk went away" with [errors.Is].
var ErrFormat = errors.New("corpus: malformed .jhtc")

func formatErr(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrFormat, fmt.Sprintf(format, args...))
}

// Builder accumulates columns and writes a .jhtc file.
//
// Columns are encoded when they are added rather than at write time, so the
// builder holds compressed bytes instead of the caller's slices and the caller
// can release each column as it goes. Footer order is insertion order, which is
// what makes [Builder.ContentDigest] reproducible.
type Builder struct {
	rows    int
	hasRows bool

	names   map[string]struct{}
	columns []builtColumn

	// digest is folded as each column is added so the uncompressed payload can be
	// released immediately. Retaining every payload to hash at the end was
	// measured at 126 MiB of live heap on a 780,489-row corpus, for a hash that
	// only ever runs forwards.
	digest hash.Hash
}

type builtColumn struct {
	name     string
	encoding Encoding
	rawBytes int
	deflated []byte
}

// NewBuilder returns a builder for a table of the given height. Every column
// added must have exactly rows values.
func NewBuilder(rows int) *Builder {
	return &Builder{rows: rows, hasRows: true, names: map[string]struct{}{}, digest: sha256.New()}
}

func (b *Builder) claim(name string, n int) error {
	if name == "" {
		return errors.New("corpus: column name must not be empty")
	}

	if _, taken := b.names[name]; taken {
		return fmt.Errorf("corpus: column %q added twice", name)
	}

	if b.hasRows && n != b.rows {
		return fmt.Errorf("corpus: column %q has %d values, table has %d rows", name, n, b.rows)
	}

	b.names[name] = struct{}{}

	return nil
}

// cardinalitySample is how many values [Builder.AddStrings] inspects before
// deciding whether a dictionary is worth building.
//
// Building the full distinct set of every column to decide was measured at
// dominating the write: 780,489 values across 33 columns is 25 million map
// inserts, most of them for columns like url and id where the answer is "no
// dictionary" and the map is thrown away. A strided sample answers the same
// question in 4,096 lookups.
const cardinalitySample = 4096

// AddStrings adds a string column, choosing between [EncodingDict] and
// [EncodingRaw] from the data alone.
//
// The choice is a function of the values, never of the column name, so it stays
// correct when a column that used to be low-cardinality stops being one. The
// threshold is deliberately crude: a dictionary pays for itself as soon as the
// average value is repeated, and being precise about the crossover would mean
// encoding twice to compare.
func (b *Builder) AddStrings(name string, values []string) error {
	if err := b.claim(name, len(values)); err != nil {
		return err
	}

	if !worthADictionary(values) {
		return b.add(name, EncodingRaw, encodeRaw(values))
	}

	distinct := map[string]struct{}{}
	for _, v := range values {
		distinct[v] = struct{}{}
	}

	// The sample only proposes; the real count decides, so a column the sample
	// flattered still gets the right encoding.
	if len(distinct) >= len(values)/2 {
		return b.add(name, EncodingRaw, encodeRaw(values))
	}

	return b.add(name, EncodingDict, encodeDict(values, distinct))
}

// worthADictionary estimates whether a column repeats itself enough for a
// dictionary to be worth *counting*, which is a lower bar than being worth
// building.
//
// The cutoff is 90% rather than the encoder's 50%, because the two errors cost
// very differently. Declining to count a column that would have compressed is a
// permanent size regression in the file; counting one that then declines a
// dictionary costs one throwaway map. Measured on 780,489 rows, the strided
// sample takes the write from 24.7 s to 7.8 s, and the columns it short-circuits
// — id, dedupe_key, url — are 100% distinct in the sample, nowhere near the
// cutoff.
//
// The stride is a function of the slice length alone, so the sample is the same
// on every run and on every architecture: determinism has to survive an
// optimisation, or the optimisation is a bug.
func worthADictionary(values []string) bool {
	if len(values) <= cardinalitySample {
		return true
	}

	stride := len(values) / cardinalitySample

	seen := make(map[string]struct{}, cardinalitySample)
	for i := 0; i < len(values); i += stride {
		seen[values[i]] = struct{}{}
	}

	sampled := (len(values) + stride - 1) / stride

	return len(seen)*10 < sampled*9
}

// AddInts adds an int64 column in [EncodingDelta].
func (b *Builder) AddInts(name string, values []int64) error {
	if err := b.claim(name, len(values)); err != nil {
		return err
	}

	return b.add(name, EncodingDelta, encodeDelta(values))
}

func (b *Builder) add(name string, encoding Encoding, payload []byte) error {
	deflated, err := deflate(payload)
	if err != nil {
		return err
	}

	hashColumn(b.digest, name, encoding, payload)

	b.columns = append(b.columns, builtColumn{
		name:     name,
		encoding: encoding,
		rawBytes: len(payload),
		deflated: deflated,
	})

	return nil
}

// hashColumn folds one column into a content digest.
//
// Every part is length-prefixed, so a column named "ab" holding "c" cannot
// digest the same as one named "a" holding "bc". Both the writer and the reader
// call this, which is what makes the two digests comparable at all.
func hashColumn(h hash.Hash, name string, encoding Encoding, payload []byte) {
	var scratch [binary.MaxVarintLen64]byte

	n := binary.PutUvarint(scratch[:], uint64(len(name)))
	h.Write(scratch[:n])
	h.Write([]byte(name))
	h.Write([]byte{byte(encoding)})

	n = binary.PutUvarint(scratch[:], uint64(len(payload)))
	h.Write(scratch[:n])
	h.Write(payload)
}

// Rows reports the table's height.
func (b *Builder) Rows() int { return b.rows }

// ContentDigest is the corpus's identity: SHA-256 over each column's name,
// encoding byte and *uncompressed* payload, in footer order.
//
// Hashing the uncompressed bytes rather than the file is the whole point.
// compress/flate's output is not promised stable across Go releases, so a
// toolchain upgrade can change every byte of the file without changing a single
// fact in it. A fail-closed consumer comparing digests must not red on that, and
// this is what it compares instead.
func (b *Builder) ContentDigest() string {
	return hex.EncodeToString(b.digest.Sum(nil))
}

// WriteTo emits the file. It implements [io.WriterTo].
func (b *Builder) WriteTo(w io.Writer) (int64, error) {
	counting := &countingWriter{w: w}

	if _, err := counting.Write(magicBytes()); err != nil {
		return counting.n, err
	}

	offsets := make([]int64, len(b.columns))
	for i, column := range b.columns {
		offsets[i] = counting.n

		if _, err := counting.Write(column.deflated); err != nil {
			return counting.n, err
		}
	}

	footerOffset := counting.n

	var footer []byte
	footer = binary.AppendUvarint(footer, uint64(b.rows))
	footer = binary.AppendUvarint(footer, uint64(len(b.columns)))

	for i, column := range b.columns {
		footer = binary.AppendUvarint(footer, uint64(len(column.name)))
		footer = append(footer, column.name...)
		footer = append(footer, byte(column.encoding))
		footer = binary.AppendUvarint(footer, uint64(offsets[i]))
		footer = binary.AppendUvarint(footer, uint64(len(column.deflated)))
		footer = binary.AppendUvarint(footer, uint64(column.rawBytes))
	}

	if _, err := counting.Write(footer); err != nil {
		return counting.n, err
	}

	var trailer [8]byte
	binary.LittleEndian.PutUint64(trailer[:], uint64(footerOffset))

	if _, err := counting.Write(trailer[:]); err != nil {
		return counting.n, err
	}

	if _, err := counting.Write(magicBytes()); err != nil {
		return counting.n, err
	}

	return counting.n, nil
}

func magicBytes() []byte {
	out := make([]byte, 0, magicLen)
	out = append(out, magicString...)

	return append(out, formatMajor, formatMinor)
}

type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)

	return n, err
}

// ColumnInfo describes one column as the footer records it, without reading it.
type ColumnInfo struct {
	Name     string
	Encoding Encoding

	// Offset and CompressedBytes are the byte range of the column in the file.
	// They are what a range-fetching client asks for: one column is one
	// contiguous request, which is the property storage-engine.md §6 measures as
	// "count by platform: 2 requests and 2.11 MiB".
	Offset          int64
	CompressedBytes int64
	RawBytes        int64
}

// Table is a read-only .jhtc.
//
// It reads through [io.ReaderAt] rather than [io.Reader] because the format's
// value is selective reads: an aggregate over two columns must not pay for the
// other twenty-five. An *os.File, a bytes.Reader and an HTTP range fetcher all
// satisfy it, and none of them is a syscall a browser lacks.
type Table struct {
	source io.ReaderAt
	rows   int

	columns []ColumnInfo
	byName  map[string]int
}

// OpenTable parses the footer of a .jhtc. It reads the trailer and the footer
// and nothing else — no column payload is touched until it is asked for.
func OpenTable(source io.ReaderAt, size int64) (*Table, error) {
	if size < int64(magicLen+trailerLen) {
		return nil, formatErr("file is %d bytes, shorter than an empty table", size)
	}

	trailer := make([]byte, trailerLen)
	if _, err := source.ReadAt(trailer, size-int64(trailerLen)); err != nil {
		return nil, fmt.Errorf("corpus: read trailer: %w", err)
	}

	if !bytes.Equal(trailer[8:], magicBytes()) {
		if !bytes.HasPrefix(trailer[8:], []byte(magicString)) {
			return nil, formatErr("bad trailing magic")
		}

		return nil, formatErr("format version %d.%d, this build reads %d.x",
			trailer[8+len(magicString)], trailer[9+len(magicString)], formatMajor)
	}

	head := make([]byte, magicLen)
	if _, err := source.ReadAt(head, 0); err != nil {
		return nil, fmt.Errorf("corpus: read magic: %w", err)
	}

	if !bytes.Equal(head, magicBytes()) {
		return nil, formatErr("bad leading magic")
	}

	footerOffset := int64(binary.LittleEndian.Uint64(trailer[:8]))
	footerEnd := size - int64(trailerLen)

	if footerOffset < int64(magicLen) || footerOffset > footerEnd {
		return nil, formatErr("footer offset %d outside [%d, %d]", footerOffset, magicLen, footerEnd)
	}

	footer := make([]byte, footerEnd-footerOffset)
	if _, err := source.ReadAt(footer, footerOffset); err != nil {
		return nil, fmt.Errorf("corpus: read footer: %w", err)
	}

	return parseFooter(source, footer, footerOffset)
}

func parseFooter(source io.ReaderAt, footer []byte, footerOffset int64) (*Table, error) {
	cur := &cursor{buf: footer}

	rows, err := cur.uvarint("row count")
	if err != nil {
		return nil, err
	}

	if rows > maxColumnBytes {
		return nil, formatErr("footer claims %d rows", rows)
	}

	ncols, err := cur.uvarint("column count")
	if err != nil {
		return nil, err
	}

	// A column entry is at least four varints and an encoding byte, so a claimed
	// count larger than the remaining footer is a corrupt file and must not
	// become a make() of that size.
	if ncols > uint64(cur.remaining()) {
		return nil, formatErr("footer claims %d columns in %d bytes", ncols, cur.remaining())
	}

	table := &Table{
		source:  source,
		rows:    int(rows),
		columns: make([]ColumnInfo, 0, ncols),
		byName:  make(map[string]int, ncols),
	}

	for i := uint64(0); i < ncols; i++ {
		name, err := cur.str("column name")
		if err != nil {
			return nil, err
		}

		encoding, err := cur.byte("column encoding")
		if err != nil {
			return nil, err
		}

		offset, err := cur.uvarint("column offset")
		if err != nil {
			return nil, err
		}

		compressed, err := cur.uvarint("column length")
		if err != nil {
			return nil, err
		}

		raw, err := cur.uvarint("column raw length")
		if err != nil {
			return nil, err
		}

		if raw > maxColumnBytes || compressed > maxColumnBytes {
			return nil, formatErr("column %q claims %d compressed / %d raw bytes", name, compressed, raw)
		}

		if offset < uint64(magicLen) || offset+compressed > uint64(footerOffset) {
			return nil, formatErr("column %q spans [%d, %d), outside the data region [%d, %d)",
				name, offset, offset+compressed, magicLen, footerOffset)
		}

		if _, taken := table.byName[name]; taken {
			return nil, formatErr("column %q appears twice in the footer", name)
		}

		table.byName[name] = len(table.columns)
		table.columns = append(table.columns, ColumnInfo{
			Name:            name,
			Encoding:        Encoding(encoding),
			Offset:          int64(offset),
			CompressedBytes: int64(compressed),
			RawBytes:        int64(raw),
		})
	}

	if cur.remaining() != 0 {
		return nil, formatErr("%d trailing bytes after the footer", cur.remaining())
	}

	return table, nil
}

// Rows reports the table's height. Every column has exactly this many values,
// including the ones the file does not contain.
func (t *Table) Rows() int { return t.rows }

// Columns returns the footer entries in file order.
func (t *Table) Columns() []ColumnInfo { return slices.Clone(t.columns) }

// Has reports whether the file carries a column.
func (t *Table) Has(name string) bool {
	_, ok := t.byName[name]

	return ok
}

// Strings decodes a string column.
//
// A column the file does not have decodes to [Table.Rows] empty strings rather
// than an error. That is the whole schema-evolution story: a reader that gains a
// column reads older files as the zero value, and a reader that loses one skips
// bytes it does not understand. Neither is a migration.
func (t *Table) Strings(name string) ([]string, error) {
	index, ok := t.byName[name]
	if !ok {
		return make([]string, t.rows), nil
	}

	info := t.columns[index]

	payload, err := t.payload(info)
	if err != nil {
		return nil, err
	}

	switch info.Encoding {
	case EncodingDict:
		return decodeDict(payload, t.rows, name)
	case EncodingRaw:
		return decodeRaw(payload, t.rows, name)
	default:
		return nil, formatErr("column %q is %s, not a string column", name, info.Encoding)
	}
}

// StringDictionary decodes a dictionary string column without expanding one
// 16-byte string header per row. It reports false for raw columns, whose
// values are not dictionary encoded. Browser readers use this to keep large
// resident projections within mobile memory while preserving the exact values.
func (t *Table) StringDictionary(name string) ([]string, []uint32, bool, error) {
	index, ok := t.byName[name]
	if !ok {
		return []string{""}, make([]uint32, t.rows), true, nil
	}

	info := t.columns[index]
	if info.Encoding != EncodingDict {
		return nil, nil, false, nil
	}
	payload, err := t.payload(info)
	if err != nil {
		return nil, nil, false, err
	}
	dictionary, ids, err := decodeDictIDs(payload, t.rows, name)
	return dictionary, ids, true, err
}

// Ints decodes an int64 column. A missing column decodes to [Table.Rows] zeros.
func (t *Table) Ints(name string) ([]int64, error) {
	index, ok := t.byName[name]
	if !ok {
		return make([]int64, t.rows), nil
	}

	info := t.columns[index]

	payload, err := t.payload(info)
	if err != nil {
		return nil, err
	}

	if info.Encoding != EncodingDelta {
		return nil, formatErr("column %q is %s, not an int column", name, info.Encoding)
	}

	return decodeDelta(payload, t.rows, name)
}

// ContentDigest recomputes the digest over every column the file carries, in
// footer order. [Verify] compares it against the manifest.
func (t *Table) ContentDigest() (string, error) {
	h := sha256.New()

	for _, info := range t.columns {
		payload, err := t.payload(info)
		if err != nil {
			return "", err
		}

		hashColumn(h, info.Name, info.Encoding, payload)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// payload fetches and inflates one column. The inflated size is checked against
// the footer's claim in both directions: a short read is a truncated file and a
// long one is a decompression bomb, and neither may be silently accepted.
func (t *Table) payload(info ColumnInfo) ([]byte, error) {
	compressed := make([]byte, info.CompressedBytes)
	if _, err := t.source.ReadAt(compressed, info.Offset); err != nil {
		return nil, fmt.Errorf("corpus: read column %q: %w", info.Name, err)
	}

	out := make([]byte, info.RawBytes)

	reader := flate.NewReader(bytes.NewReader(compressed))
	defer reader.Close()

	if _, err := io.ReadFull(reader, out); err != nil {
		return nil, formatErr("column %q: inflate: %v", info.Name, err)
	}

	// One more byte than the footer promised means rawlen understates the
	// payload, so the reader would silently truncate a column.
	var overflow [1]byte
	if n, err := reader.Read(overflow[:]); n > 0 || (err != nil && err != io.EOF) {
		return nil, formatErr("column %q inflates to more than the %d bytes the footer claims",
			info.Name, info.RawBytes)
	}

	return out, nil
}

// --- payload encoders ------------------------------------------------------

func encodeDict(values []string, distinct map[string]struct{}) []byte {
	dictionary := make([]string, 0, len(distinct))
	for v := range distinct {
		dictionary = append(dictionary, v)
	}

	// Sorted, not first-seen. A dictionary ordered by appearance encodes the row
	// order into the dictionary, so two tables holding the same values in a
	// different order would differ in a place that carries no information.
	sort.Strings(dictionary)

	ids := make(map[string]uint64, len(dictionary))
	for i, v := range dictionary {
		ids[v] = uint64(i)
	}

	out := binary.AppendUvarint(nil, uint64(len(dictionary)))
	for _, v := range dictionary {
		out = binary.AppendUvarint(out, uint64(len(v)))
		out = append(out, v...)
	}

	out = binary.AppendUvarint(out, uint64(len(values)))
	for _, v := range values {
		out = binary.AppendUvarint(out, ids[v])
	}

	return out
}

func encodeRaw(values []string) []byte {
	out := binary.AppendUvarint(nil, uint64(len(values)))
	for _, v := range values {
		out = binary.AppendUvarint(out, uint64(len(v)))
		out = append(out, v...)
	}

	return out
}

func encodeDelta(values []int64) []byte {
	out := binary.AppendUvarint(nil, uint64(len(values)))

	var previous int64
	for _, v := range values {
		// Deltas are taken in the row order the corpus already sorts by, so a
		// column that is monotonic in that order (first_seen, mostly) costs about
		// a byte a row and one that is not still costs no more than the value.
		out = binary.AppendVarint(out, v-previous)
		previous = v
	}

	return out
}

func deflate(payload []byte) ([]byte, error) {
	var buf bytes.Buffer

	// Sized from the payload so a large column does not spend its first
	// megabytes growing the buffer. flate on this data runs about 3:1.
	buf.Grow(len(payload)/3 + 64)

	writer, err := flate.NewWriter(&buf, compressionLevel)
	if err != nil {
		return nil, err
	}

	if _, err := writer.Write(payload); err != nil {
		return nil, err
	}

	if err := writer.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// --- payload decoders ------------------------------------------------------

func decodeDict(payload []byte, rows int, name string) ([]string, error) {
	dictionary, ids, err := decodeDictIDs(payload, rows, name)
	if err != nil {
		return nil, err
	}
	out := make([]string, rows)
	for i, id := range ids {
		out[i] = dictionary[id]
	}
	return out, nil
}

func decodeDictIDs(payload []byte, rows int, name string) ([]string, []uint32, error) {
	cur := &cursor{buf: payload}

	ndict, err := cur.uvarint("dictionary size")
	if err != nil {
		return nil, nil, fmt.Errorf("column %q: %w", name, err)
	}

	if ndict > uint64(cur.remaining()) {
		return nil, nil, formatErr("column %q: dictionary of %d entries in %d bytes", name, ndict, cur.remaining())
	}
	if ndict > math.MaxUint32 {
		return nil, nil, formatErr("column %q: dictionary has %d entries", name, ndict)
	}

	dictionary := make([]string, ndict)
	for i := range dictionary {
		if dictionary[i], err = cur.str("dictionary entry"); err != nil {
			return nil, nil, fmt.Errorf("column %q: %w", name, err)
		}
	}

	nrows, err := cur.uvarint("row count")
	if err != nil {
		return nil, nil, fmt.Errorf("column %q: %w", name, err)
	}

	if nrows != uint64(rows) {
		return nil, nil, formatErr("column %q holds %d rows, table has %d", name, nrows, rows)
	}

	ids := make([]uint32, rows)
	for i := range ids {
		id, err := cur.uvarint("dictionary id")
		if err != nil {
			return nil, nil, fmt.Errorf("column %q: %w", name, err)
		}

		if id >= ndict {
			return nil, nil, formatErr("column %q row %d: dictionary id %d of %d", name, i, id, ndict)
		}

		ids[i] = uint32(id)
	}

	if cur.remaining() != 0 {
		return nil, nil, formatErr("column %q: %d trailing bytes", name, cur.remaining())
	}

	return dictionary, ids, nil
}

func decodeRaw(payload []byte, rows int, name string) ([]string, error) {
	cur := &cursor{buf: payload}

	nrows, err := cur.uvarint("row count")
	if err != nil {
		return nil, fmt.Errorf("column %q: %w", name, err)
	}

	if nrows != uint64(rows) {
		return nil, formatErr("column %q holds %d rows, table has %d", name, nrows, rows)
	}

	out := make([]string, rows)
	for i := range out {
		if out[i], err = cur.str("value"); err != nil {
			return nil, fmt.Errorf("column %q row %d: %w", name, i, err)
		}
	}

	if cur.remaining() != 0 {
		return nil, formatErr("column %q: %d trailing bytes", name, cur.remaining())
	}

	return out, nil
}

func decodeDelta(payload []byte, rows int, name string) ([]int64, error) {
	cur := &cursor{buf: payload}

	nrows, err := cur.uvarint("row count")
	if err != nil {
		return nil, fmt.Errorf("column %q: %w", name, err)
	}

	if nrows != uint64(rows) {
		return nil, formatErr("column %q holds %d rows, table has %d", name, nrows, rows)
	}

	out := make([]int64, rows)

	var previous int64
	for i := range out {
		delta, err := cur.varint("delta")
		if err != nil {
			return nil, fmt.Errorf("column %q row %d: %w", name, i, err)
		}

		previous += delta
		out[i] = previous
	}

	if cur.remaining() != 0 {
		return nil, formatErr("column %q: %d trailing bytes", name, cur.remaining())
	}

	return out, nil
}

// cursor is a bounds-checked reader over a decoded payload.
//
// Every read in this file goes through it. storage-engine.md §9 lists "the
// prototype has no bounds checking" as the one thing production .jhtc needs that
// the measurement did not have, and a corrupt length that reaches make() is the
// specific failure it names.
type cursor struct {
	buf []byte
	pos int
}

func (c *cursor) remaining() int { return len(c.buf) - c.pos }

func (c *cursor) uvarint(what string) (uint64, error) {
	v, n := binary.Uvarint(c.buf[c.pos:])
	if n <= 0 {
		return 0, formatErr("%s: truncated or overlong varint at offset %d", what, c.pos)
	}

	c.pos += n

	return v, nil
}

func (c *cursor) varint(what string) (int64, error) {
	v, n := binary.Varint(c.buf[c.pos:])
	if n <= 0 {
		return 0, formatErr("%s: truncated or overlong varint at offset %d", what, c.pos)
	}

	c.pos += n

	return v, nil
}

func (c *cursor) byte(what string) (uint8, error) {
	if c.remaining() < 1 {
		return 0, formatErr("%s: end of payload", what)
	}

	b := c.buf[c.pos]
	c.pos++

	return b, nil
}

func (c *cursor) str(what string) (string, error) {
	length, err := c.uvarint(what + " length")
	if err != nil {
		return "", err
	}

	if length > uint64(c.remaining()) {
		return "", formatErr("%s: %d bytes claimed, %d remain", what, length, c.remaining())
	}

	start := c.pos
	c.pos += int(length)

	return string(c.buf[start:c.pos]), nil
}
