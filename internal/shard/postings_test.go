package shard

import (
	"strings"
	"testing"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// TestDedupeIdentityMatchesInternalDedupe pins the merge's notion of "the same
// posting" to the crawl's.
//
// If the two ever drift, a sharded total and an unsharded total would disagree
// on the same data, and the disagreement would be read as the job market
// moving rather than as a bug.
func TestDedupeIdentityMatchesInternalDedupe(t *testing.T) {
	t.Parallel()

	postings := []*internal.JobPosting{
		{Company: "Acme", URL: "https://jobs.example.com/1", Title: "A", Location: "Remote"},
		// Same URL through a second integration: one posting.
		{Company: "Acme Inc", URL: "https://jobs.example.com/1", Title: "A", Location: "NYC"},
		{Company: "Acme", URL: "https://jobs.example.com/2", Title: "B", Location: "Remote"},
		// No URL: identity falls back to company, title and location.
		{Company: "Globex", Title: "C", Location: "Remote"},
		{Company: "Globex", Title: "C", Location: "Remote"},
		{Company: "Globex", Title: "C", Location: "London"},
	}

	seq := func(yield func(*internal.JobPosting, error) bool) {
		for _, job := range postings {
			if !yield(job, nil) {
				return
			}
		}
	}

	deduped := 0
	for range internal.Dedupe(seq) {
		deduped++
	}

	keys := map[string]struct{}{}
	for _, job := range postings {
		keys[PostingKey(job)] = struct{}{}
	}

	must.Eq(t, deduped, len(keys))
	test.Eq(t, 4, deduped)
}

func TestPostingKeyIsAFixedWidthFingerprint(t *testing.T) {
	t.Parallel()

	key := PostingKey(&internal.JobPosting{URL: "https://jobs.example.com/1"})

	test.Eq(t, PostingKeyBytes*2, len(key))
	test.Eq(t, key, PostingKey(&internal.JobPosting{URL: "https://jobs.example.com/1"}))
	test.NotEq(t, key, PostingKey(&internal.JobPosting{URL: "https://jobs.example.com/2"}))
}

func TestPostingWriterEmitsOneRecordPerLine(t *testing.T) {
	t.Parallel()

	var out strings.Builder

	writer := NewPostingWriter(&out)
	must.NoError(t, writer.Write(&internal.JobPosting{
		Company: "Acme",
		URL:     "https://jobs.example.com/1",
		Source:  internal.PostingSource{Platform: "greenhouse"},
	}))
	// A nil posting is not a record. Adapters yield (nil, err) on failure and
	// the crawl loop must not turn that into a phantom posting.
	must.NoError(t, writer.Write(nil))
	must.NoError(t, writer.Flush())

	test.Eq(t, 1, writer.Written())

	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	must.Len(t, 1, lines)
	test.StrContains(t, lines[0], `"company":"Acme"`)
	test.StrContains(t, lines[0], `"platform":"greenhouse"`)
}

func TestReadPostingsRejectsUnreadableRecords(t *testing.T) {
	t.Parallel()

	// A parser that skipped what it could not read would undercount silently,
	// which is indistinguishable from postings disappearing from the market.
	_, err := readPostings(strings.NewReader("{\"key\":\"\",\"company\":\"Acme\"}\n"), "shard-0.ndjson", func(PostingRecord) error { return nil })
	must.ErrorContains(t, err, "no key")

	_, err = readPostings(strings.NewReader("not json\n"), "shard-0.ndjson", func(PostingRecord) error { return nil })
	must.ErrorContains(t, err, "line 1")
}

func TestDecodePostingKeyRejectsMalformedKeys(t *testing.T) {
	t.Parallel()

	_, err := decodePostingKey("abc")
	must.ErrorContains(t, err, "want 32")

	_, err = decodePostingKey(strings.Repeat("z", PostingKeyBytes*2))
	must.ErrorContains(t, err, "not hexadecimal")

	key, err := decodePostingKey(strings.Repeat("aB", PostingKeyBytes))
	must.NoError(t, err)
	test.Eq(t, byte(0xab), key[0])
}

func TestManifestCompleteRequiresEveryTerminalSource(t *testing.T) {
	t.Parallel()

	complete := manifestOf(run("alpha", "a", "complete", 5), run("alpha", "b", "failed", 5))
	test.True(t, complete.Complete())
	test.SliceEmpty(t, complete.UnfinishedSources())

	stalled := manifestOf(run("alpha", "a", "complete", 5), run("alpha", "b", "running", 0))
	test.False(t, stalled.Complete())
	test.Len(t, 1, stalled.UnfinishedSources())

	partial := manifestOf(run("alpha", "a", "complete", 5))
	partial.Status = StatusPartial
	test.False(t, partial.Complete())
}
