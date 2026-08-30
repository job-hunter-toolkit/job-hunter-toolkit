package corpus

import (
	"bytes"
	"errors"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// build encodes a table and reopens it, which is the shape of almost every test
// in this file.
func build(t *testing.T, rows int, add func(*Builder)) (*Table, []byte) {
	t.Helper()

	builder := NewBuilder(rows)
	add(builder)

	var buf bytes.Buffer

	n, err := builder.WriteTo(&buf)
	must.NoError(t, err)
	test.Eq(t, int64(buf.Len()), n)

	table, err := OpenTable(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	must.NoError(t, err)

	return table, buf.Bytes()
}

func TestTableRoundTripsEveryEncoding(t *testing.T) {
	t.Parallel()

	// low cardinality -> dict, near-unique -> raw, ints -> delta. The point is
	// that the caller does not choose and does not have to know.
	platforms := []string{"ashby", "greenhouse", "greenhouse", "workday", "ashby", "workday"}
	urls := []string{"a", "b", "c", "d", "e", "f"}
	posted := []int64{1700000000000, 1700000001000, 0, -5, math.MaxInt64, math.MinInt64}

	table, _ := build(t, 6, func(b *Builder) {
		must.NoError(t, b.AddStrings("platform", platforms))
		must.NoError(t, b.AddStrings("url", urls))
		must.NoError(t, b.AddInts("posted", posted))
	})

	test.Eq(t, 6, table.Rows())

	gotPlatforms, err := table.Strings("platform")
	must.NoError(t, err)
	test.Eq(t, platforms, gotPlatforms)

	gotURLs, err := table.Strings("url")
	must.NoError(t, err)
	test.Eq(t, urls, gotURLs)
	_, _, indexed, err := table.StringDictionary("url")
	must.NoError(t, err)
	test.False(t, indexed)

	gotPosted, err := table.Ints("posted")
	must.NoError(t, err)
	test.Eq(t, posted, gotPosted)
}

func TestTableChoosesDictForRepeatedValuesAndRawForUniqueOnes(t *testing.T) {
	t.Parallel()

	repeated := make([]string, 100)
	unique := make([]string, 100)

	for i := range repeated {
		repeated[i] = "greenhouse"
		unique[i] = strconv.Itoa(i)
	}

	table, _ := build(t, 100, func(b *Builder) {
		must.NoError(t, b.AddStrings("repeated", repeated))
		must.NoError(t, b.AddStrings("unique", unique))
	})

	columns := table.Columns()
	must.SliceLen(t, 2, columns)
	test.Eq(t, EncodingDict, columns[0].Encoding)
	test.Eq(t, EncodingRaw, columns[1].Encoding)
	dictionary, ids, indexed, err := table.StringDictionary("repeated")
	must.NoError(t, err)
	test.True(t, indexed)
	indexedRepeated := make([]string, len(ids))
	for i, id := range ids {
		indexedRepeated[i] = dictionary[id]
	}
	test.Eq(t, repeated, indexedRepeated)
}

func TestTableReadsAMissingColumnAsTheZeroValue(t *testing.T) {
	t.Parallel()

	// This is the entire schema-evolution story: a reader that gains a column
	// reads older files without a migration, and it needs the table's height to
	// do it — which is why the footer carries one.
	table, _ := build(t, 3, func(b *Builder) {
		must.NoError(t, b.AddStrings("present", []string{"a", "b", "c"}))
	})

	test.False(t, table.Has("added_later"))

	strings, err := table.Strings("added_later")
	must.NoError(t, err)
	test.Eq(t, []string{"", "", ""}, strings)

	ints, err := table.Ints("added_later")
	must.NoError(t, err)
	test.Eq(t, []int64{0, 0, 0}, ints)
}

func TestTableRejectsReadingAColumnAsTheWrongType(t *testing.T) {
	t.Parallel()

	table, _ := build(t, 2, func(b *Builder) {
		must.NoError(t, b.AddInts("n", []int64{1, 2}))
		must.NoError(t, b.AddStrings("s", []string{"x", "y"}))
	})

	_, err := table.Strings("n")
	must.ErrorIs(t, err, ErrFormat)

	_, err = table.Ints("s")
	must.ErrorIs(t, err, ErrFormat)
}

func TestBuilderRejectsRaggedAndDuplicateColumns(t *testing.T) {
	t.Parallel()

	builder := NewBuilder(3)
	must.NoError(t, builder.AddStrings("a", []string{"1", "2", "3"}))

	test.Error(t, builder.AddStrings("a", []string{"4", "5", "6"}))
	test.Error(t, builder.AddStrings("b", []string{"1", "2"}))
	test.Error(t, builder.AddStrings("", []string{"1", "2", "3"}))
}

func TestTableIsByteIdenticalAcrossWrites(t *testing.T) {
	t.Parallel()

	// Determinism is a hard constraint, and the two places it could leak here are
	// the dictionary's order (a map) and gzip metadata. Two independent builds of
	// the same data must be the same bytes.
	values := make([]string, 5000)
	numbers := make([]int64, 5000)

	for i := range values {
		values[i] = []string{"alpha", "beta", "gamma"}[i%3]
		numbers[i] = int64(i * 7 % 1013)
	}

	var first, second bytes.Buffer

	for _, buf := range []*bytes.Buffer{&first, &second} {
		builder := NewBuilder(len(values))
		must.NoError(t, builder.AddStrings("v", values))
		must.NoError(t, builder.AddInts("n", numbers))

		_, err := builder.WriteTo(buf)
		must.NoError(t, err)
	}

	test.Eq(t, first.Bytes(), second.Bytes())
}

func TestContentDigestIgnoresCompressionAndTracksContent(t *testing.T) {
	t.Parallel()

	// The digest is over uncompressed payloads precisely so that a Go release
	// changing compress/flate cannot red a fail-closed consumer. Here we can at
	// least assert the two halves: the writer and the reader agree, and a changed
	// value changes the digest.
	first := NewBuilder(3)
	must.NoError(t, first.AddStrings("v", []string{"a", "b", "c"}))

	var buf bytes.Buffer
	_, err := first.WriteTo(&buf)
	must.NoError(t, err)

	table, err := OpenTable(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	must.NoError(t, err)

	fromFile, err := table.ContentDigest()
	must.NoError(t, err)
	test.Eq(t, first.ContentDigest(), fromFile)

	second := NewBuilder(3)
	must.NoError(t, second.AddStrings("v", []string{"a", "b", "d"}))
	test.NotEq(t, first.ContentDigest(), second.ContentDigest())
}

func TestContentDigestSeparatesColumnNameFromValue(t *testing.T) {
	t.Parallel()

	// Without length prefixes in the hash, a column named "ab" holding "c" and one
	// named "a" holding "bc" would digest identically. Length-prefixing every part
	// is what stops a rename from being invisible.
	first := NewBuilder(1)
	must.NoError(t, first.AddStrings("ab", []string{"c"}))

	second := NewBuilder(1)
	must.NoError(t, second.AddStrings("a", []string{"bc"}))

	test.NotEq(t, first.ContentDigest(), second.ContentDigest())
}

func TestOpenTableRejectsAHostileFile(t *testing.T) {
	t.Parallel()

	_, good := build(t, 4, func(b *Builder) {
		must.NoError(t, b.AddStrings("v", []string{"a", "b", "c", "d"}))
		must.NoError(t, b.AddInts("n", []int64{1, 2, 3, 4}))
	})

	corruptions := []struct {
		name    string
		corrupt func([]byte) []byte
	}{
		{"empty", func([]byte) []byte { return nil }},
		{"too short", func(b []byte) []byte { return b[:8] }},
		{"truncated tail", func(b []byte) []byte { return b[:len(b)-1] }},
		{"bad leading magic", func(b []byte) []byte {
			out := bytes.Clone(b)
			out[0] = 'X'

			return out
		}},
		{"bad trailing magic", func(b []byte) []byte {
			out := bytes.Clone(b)
			out[len(out)-1] ^= 0xFF

			return out
		}},
		{"footer offset past the end", func(b []byte) []byte {
			out := bytes.Clone(b)
			for i := len(out) - magicLen - 8; i < len(out)-magicLen; i++ {
				out[i] = 0xFF
			}

			return out
		}},
		{"footer offset inside the magic", func(b []byte) []byte {
			out := bytes.Clone(b)
			for i := len(out) - magicLen - 8; i < len(out)-magicLen; i++ {
				out[i] = 0
			}

			return out
		}},
	}

	for _, corruption := range corruptions {
		t.Run(corruption.name, func(t *testing.T) {
			t.Parallel()

			body := corruption.corrupt(good)

			table, err := OpenTable(bytes.NewReader(body), int64(len(body)))
			if err != nil {
				return
			}

			// A corruption that still parses a footer must not survive reading the
			// columns it claims.
			_, first := table.Strings("v")
			_, second := table.Ints("n")

			test.True(t, first != nil || second != nil,
				test.Sprintf("corruption %q produced a readable table", corruption.name))
		})
	}
}

func TestOpenTableSurvivesEverySingleByteFlip(t *testing.T) {
	t.Parallel()

	// The property that matters for a file fetched over the network is not that
	// every corruption is detected — flate has no checksum, so some flips decode
	// to different-but-valid bytes — but that none of them panics, hangs or
	// allocates unboundedly. That is what a corrupt rawlen reaching make() would
	// do, and it is the one gap storage-engine.md §9 named.
	_, good := build(t, 8, func(b *Builder) {
		must.NoError(t, b.AddStrings("v", []string{"a", "b", "c", "d", "e", "f", "g", "h"}))
		must.NoError(t, b.AddInts("n", []int64{1, 2, 3, 4, 5, 6, 7, 8}))
	})

	for i := range good {
		for _, mask := range []byte{0x01, 0x80, 0xFF} {
			body := bytes.Clone(good)
			body[i] ^= mask

			table, err := OpenTable(bytes.NewReader(body), int64(len(body)))
			if err != nil {
				must.ErrorIs(t, err, ErrFormat)

				continue
			}

			_, _ = table.Strings("v")
			_, _ = table.Ints("n")
			_, _ = table.ContentDigest()
		}
	}
}

func TestTableRejectsAnOverstatedRawLength(t *testing.T) {
	t.Parallel()

	// A footer claiming a column inflates to more than it does is the exact
	// allocation attack the bounds checks exist for: the reader must refuse rather
	// than hand back a half-filled slice.
	table, body := build(t, 2, func(b *Builder) {
		must.NoError(t, b.AddStrings("v", []string{"hello", "world"}))
	})

	info := table.Columns()[0]

	// Rewrite the rawlen varint in place. It is the last varint of the only entry,
	// so it is the last byte before the trailer for these small values.
	tampered := bytes.Clone(body)
	rawLenAt := len(tampered) - trailerLen - 1
	must.Eq(t, int(info.RawBytes), int(tampered[rawLenAt]))
	tampered[rawLenAt] = byte(info.RawBytes + 1)

	reopened, err := OpenTable(bytes.NewReader(tampered), int64(len(tampered)))
	must.NoError(t, err)

	_, err = reopened.Strings("v")
	must.ErrorIs(t, err, ErrFormat)
}

func TestTableRejectsAnUnderstatedRawLength(t *testing.T) {
	t.Parallel()

	table, body := build(t, 2, func(b *Builder) {
		must.NoError(t, b.AddStrings("v", []string{"hello", "world"}))
	})

	info := table.Columns()[0]

	tampered := bytes.Clone(body)
	rawLenAt := len(tampered) - trailerLen - 1
	must.Eq(t, int(info.RawBytes), int(tampered[rawLenAt]))
	tampered[rawLenAt] = byte(info.RawBytes - 1)

	reopened, err := OpenTable(bytes.NewReader(tampered), int64(len(tampered)))
	must.NoError(t, err)

	_, err = reopened.Strings("v")
	must.ErrorIs(t, err, ErrFormat)
}

func TestTableHandlesEmptyAndLargeValues(t *testing.T) {
	t.Parallel()

	big := strings.Repeat("x", 1<<20)

	table, _ := build(t, 3, func(b *Builder) {
		must.NoError(t, b.AddStrings("v", []string{"", big, ""}))
	})

	got, err := table.Strings("v")
	must.NoError(t, err)
	test.Eq(t, []string{"", big, ""}, got)
}

func TestEmptyTableRoundTrips(t *testing.T) {
	t.Parallel()

	table, _ := build(t, 0, func(*Builder) {})

	test.Eq(t, 0, table.Rows())
	test.SliceEmpty(t, table.Columns())

	got, err := table.Strings("anything")
	must.NoError(t, err)
	test.SliceEmpty(t, got)
}

func TestFormatErrorsAreDistinguishable(t *testing.T) {
	t.Parallel()

	_, err := OpenTable(bytes.NewReader(nil), 0)
	must.ErrorIs(t, err, ErrFormat)
	test.False(t, errors.Is(err, errors.ErrUnsupported))
}
