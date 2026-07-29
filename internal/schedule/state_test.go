package schedule

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func fixtureStore(t testing.TB) *Store {
	t.Helper()

	store := NewStore()
	store.WrittenAt = time.Date(2026, 7, 28, 9, 12, 0, 0, time.UTC)
	store.Writer = "9c71fc0"

	// Deliberately inserted out of order: Encode sorts, and a test that fed it
	// sorted input would not prove that.
	store.PutSource(SourceState{
		Platform:    "personio",
		Key:         "zeta",
		Company:     "Zeta",
		Group:       "platform:personio",
		LastAttempt: "2026-07-27T09:03:11Z",
		DurationMS:  []int32{2400, 2600},
		Postings:    []int32{12},

		ConsecutiveFailures: 2,
		ErrorClass:          "timeout",
	})
	store.PutSource(SourceState{
		Platform:    "ashby",
		Key:         "0x",
		Company:     "0x",
		LastAttempt: "2026-07-28T09:03:11Z",
		LastSuccess: "2026-07-28T09:03:11Z",
		DurationMS:  []int32{80, 74, 91},
		Postings:    []int32{13, 13, 14},
	})
	store.PutSource(SourceState{
		Platform:  "lever",
		Key:       "gone",
		Retired:   true,
		RetiredAt: "2026-07-01T00:00:00Z",
	})

	store.PutGroup(GroupState{Key: "platform:personio", ParallelismMilli: 3900, ObservedAt: "2026-07-28T09:12:00Z", Samples: 4})
	store.PutGroup(GroupState{Key: "service:boards-api.greenhouse.io", ParallelismMilli: 3820, Samples: 6})

	return store
}

func TestEncodeIsByteDeterministic(t *testing.T) {
	t.Parallel()

	// The whole argument for JSON Lines over the corpus's columnar format is that
	// this file is read whole, written whole, and reviewed by a human in a diff.
	// That only holds if the same state always produces the same bytes.
	var first, second bytes.Buffer

	must.NoError(t, Encode(&first, fixtureStore(t)))

	for range 8 {
		second.Reset()
		must.NoError(t, Encode(&second, fixtureStore(t)))
		test.Eq(t, first.String(), second.String())
	}
}

func TestEncodeOrdersHeaderThenSourcesThenGroups(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	must.NoError(t, Encode(&buf, fixtureStore(t)))

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	must.SliceLen(t, 6, lines)

	test.StrContains(t, lines[0], `"kind":"header"`)
	test.StrContains(t, lines[0], `"schema":"job-hunter-toolkit/scheduler-state"`)

	// Sources sorted by (platform, key), then groups sorted by key.
	test.StrContains(t, lines[1], `"platform":"ashby"`)
	test.StrContains(t, lines[2], `"platform":"lever"`)
	test.StrContains(t, lines[3], `"platform":"personio"`)
	test.StrContains(t, lines[4], `"key":"platform:personio"`)
	test.StrContains(t, lines[5], `"key":"service:boards-api.greenhouse.io"`)
}

func TestDecodeRoundTripsAStore(t *testing.T) {
	t.Parallel()

	original := fixtureStore(t)

	var buf bytes.Buffer
	must.NoError(t, Encode(&buf, original))

	encoded := buf.String()

	decoded, skipped, err := Decode(&buf)
	must.NoError(t, err)
	test.Eq(t, 0, skipped)

	if !reflect.DeepEqual(original, decoded) {
		t.Fatalf("round trip changed the store:\n got %#v\nwant %#v", decoded, original)
	}

	// And the second encode of the decoded store is byte-identical, which is the
	// property that actually keeps a daily diff readable.
	var again bytes.Buffer
	must.NoError(t, Encode(&again, decoded))
	test.Eq(t, encoded, again.String())
}

func TestGoldenStateFile(t *testing.T) {
	t.Parallel()

	// A serialisation change should be a visible diff in a review, not a silent
	// change to a file other surfaces are seeded from.
	var buf bytes.Buffer
	must.NoError(t, Encode(&buf, fixtureStore(t)))

	path := filepath.Join("testdata", "golden-state.jsonl")

	want, err := os.ReadFile(path)
	must.NoError(t, err)

	if buf.String() != string(want) {
		t.Fatalf("golden state file %s is stale:\n got:\n%s\nwant:\n%s", path, buf.String(), want)
	}
}

func TestDecodeIgnoresUnknownFields(t *testing.T) {
	t.Parallel()

	// Additive fields never bump the version, so an older binary must read a file
	// a newer one wrote. DisallowUnknownFields must never be set on this file.
	input := strings.Join([]string{
		`{"kind":"header","schema":"job-hunter-toolkit/scheduler-state","version":1}`,
		`{"kind":"source","platform":"ashby","key":"0x","postings":[13],"churn":[4],"tags":{"a":1}}`,
		"",
	}, "\n")

	store, skipped, err := Decode(strings.NewReader(input))
	must.NoError(t, err)
	test.Eq(t, 0, skipped)

	state, ok := store.Source(SourceID{Platform: "ashby", Key: "0x"})
	must.True(t, ok)
	test.Eq(t, []int32{13}, state.Postings)
}

func TestDecodeSkipsUnknownKinds(t *testing.T) {
	t.Parallel()

	input := strings.Join([]string{
		`{"kind":"header","schema":"job-hunter-toolkit/scheduler-state","version":1}`,
		`{"kind":"corpus","generation":9}`,
		`{"kind":"source","platform":"ashby","key":"0x"}`,
		"",
	}, "\n")

	store, skipped, err := Decode(strings.NewReader(input))
	must.NoError(t, err)
	test.Eq(t, 1, skipped)
	test.Eq(t, 1, store.Len())
}

func TestDecodeRejectsAFutureVersion(t *testing.T) {
	t.Parallel()

	input := `{"kind":"header","schema":"job-hunter-toolkit/scheduler-state","version":99}` + "\n"

	_, _, err := Decode(strings.NewReader(input))
	must.ErrorIs(t, err, ErrFutureVersion)
}

func TestLoadDegradesToColdStart(t *testing.T) {
	t.Parallel()

	// State is advisory. Losing it is never an error: a missing, truncated or
	// future-versioned file means this run does not get to prioritise, which is
	// exactly what every run before this package did. Refusing to crawl because
	// yesterday's artifact expired is the failure mode to design against.
	for name, input := range map[string]string{
		"empty":          "",
		"garbage":        "not json at all\n",
		"truncated line": `{"kind":"source","platform":"ashby",`,
		"future version": `{"kind":"header","schema":"job-hunter-toolkit/scheduler-state","version":99}`,
		"foreign schema": `{"kind":"header","schema":"something/else","version":1}`,
		"headerless":     `{"kind":"source","platform":"ashby","key":"0x"}`,
	} {
		t.Run(name, func(t *testing.T) {
			store, origin, err := Load(strings.NewReader(input))

			must.NotNil(t, store)

			if name == "empty" {
				// An empty reader is a well-formed empty file, not a failure.
				test.NoError(t, err)
				test.Eq(t, OriginState, origin)

				return
			}

			test.Error(t, err)
			test.Eq(t, OriginCold, origin)
			test.Eq(t, 0, store.Len())
		})
	}
}

func TestReadFileTreatsAMissingFileAsAFirstRun(t *testing.T) {
	t.Parallel()

	store, origin, err := ReadFile(filepath.Join(t.TempDir(), "absent.jsonl"))
	must.NoError(t, err)
	test.Eq(t, OriginCold, origin)
	test.Eq(t, 0, store.Len())
}

func TestWriteFileThenReadFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "scheduler-state.jsonl")

	must.NoError(t, WriteFile(path, fixtureStore(t)))

	store, origin, err := ReadFile(path)
	must.NoError(t, err)
	test.Eq(t, OriginState, origin)
	test.Eq(t, 3, store.Len())
	test.Eq(t, 2, store.GroupLen())

	// The temp file is renamed into place, so nothing is left beside it.
	entries, err := os.ReadDir(filepath.Dir(path))
	must.NoError(t, err)
	test.SliceLen(t, 1, entries)
}

func TestStoreAccessorsDoNotAliasTheirCaller(t *testing.T) {
	t.Parallel()

	// Fold returns a new store so a test can compare before with after. That is
	// only true if the sample slices are not shared.
	store := NewStore()
	store.PutSource(SourceState{Platform: "ashby", Key: "0x", DurationMS: []int32{10}})

	state, ok := store.Source(SourceID{Platform: "ashby", Key: "0x"})
	must.True(t, ok)

	state.DurationMS[0] = 999

	again, _ := store.Source(SourceID{Platform: "ashby", Key: "0x"})
	test.Eq(t, []int32{10}, again.DurationMS)
}

func TestNilStoreIsUsable(t *testing.T) {
	t.Parallel()

	// Cold start must never be a nil dereference, because it is the path taken
	// whenever the state artifact is missing.
	var store *Store

	test.Eq(t, 0, store.Len())
	test.Eq(t, 0, store.GroupLen())
	test.SliceEmpty(t, store.Sources())
	test.SliceEmpty(t, store.Groups())

	_, ok := store.Source(SourceID{Platform: "a", Key: "b"})
	test.False(t, ok)

	var buf bytes.Buffer
	test.NoError(t, Encode(&buf, store))
}

func TestReadFileReportsARealIOError(t *testing.T) {
	t.Parallel()

	// A directory where a file should be is a broken deployment, not a cold
	// start, and the difference is worth a log line.
	dir := t.TempDir()

	_, origin, err := ReadFile(dir)
	test.Error(t, err)
	test.Eq(t, OriginCold, origin)
	test.False(t, errors.Is(err, os.ErrNotExist))
}

func FuzzDecode(f *testing.F) {
	// A state file arrives as a downloaded artifact, so it is untrusted input —
	// the same standing shard.Plan.Validate gives a plan.
	var buf bytes.Buffer
	_ = Encode(&buf, fixtureStore(f))

	f.Add(buf.String())
	f.Add(`{"kind":"header","schema":"job-hunter-toolkit/scheduler-state","version":1}`)
	f.Add(`{"kind":"source","platform":"a","key":"b","duration_ms":[1,2,3]}`)
	f.Add(`{"kind":"group","key":"platform:x","parallelism_milli":4000}`)
	f.Add("\n\n\n")
	f.Add(`{"kind":`)

	f.Fuzz(func(t *testing.T, input string) {
		store, _, err := Decode(strings.NewReader(input))
		if err != nil {
			return
		}

		// Anything that decoded must re-encode and decode back to itself, or the
		// file is not the round-trippable record the rest of the design assumes.
		var out bytes.Buffer
		if err := Encode(&out, store); err != nil {
			t.Fatalf("encode a decoded store: %v", err)
		}

		again, _, err := Decode(&out)
		if err != nil {
			t.Fatalf("decode an encoded store: %v", err)
		}

		if !reflect.DeepEqual(store, again) {
			t.Fatalf("round trip is not stable for %q", input)
		}
	})
}
