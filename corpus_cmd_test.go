package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/corpus"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/jobposting"
	"github.com/spf13/cobra"
)

// corpusRoot builds a root carrying only the corpus commands, for the same
// reason shardRoot exists: these tests must pass whether or not the group is
// wired into the real root yet.
func corpusRoot() *cobra.Command {
	root := &cobra.Command{Use: "job-hunter-toolkit", SilenceUsage: true}
	root.AddCommand(newCorpusCommand())

	return root
}

// runCorpus executes a corpus command, returning stdout, stderr and the error.
func runCorpus(t *testing.T, args ...string) (string, string, error) {
	t.Helper()

	var stdout, stderr bytes.Buffer

	cmd := corpusRoot()
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)

	err := cmd.ExecuteContext(t.Context())

	return stdout.String(), stderr.String(), err
}

// writeCrawlPair writes a postings.ndjson + crawl-manifest.json pair the way
// `corpus crawl` would, from synthetic data, and returns their paths.
func writeCrawlPair(
	t *testing.T,
	dir string,
	finishedAt time.Time,
	sources []corpus.SourceRun,
	postings []jobposting.JobPosting,
) (string, string) {
	t.Helper()

	postingsPath := filepath.Join(dir, "postings.ndjson")
	manifestPath := filepath.Join(dir, "crawl-manifest.json")

	var stream bytes.Buffer

	encoder := json.NewEncoder(&stream)
	for i := range postings {
		if err := encoder.Encode(&postings[i]); err != nil {
			t.Fatalf("encode posting: %v", err)
		}
	}

	if err := os.WriteFile(postingsPath, stream.Bytes(), 0o600); err != nil {
		t.Fatalf("write postings: %v", err)
	}

	manifest := map[string]any{
		"schema_version": 2,
		"started_at":     finishedAt.Add(-time.Minute),
		"finished_at":    finishedAt,
		"status":         "complete",
		"postings":       len(postings),
		"sources":        sources,
	}

	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("encode manifest: %v", err)
	}

	if err := os.WriteFile(manifestPath, raw, 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	return postingsPath, manifestPath
}

func testPosting(company, title, url string, source jobposting.PostingSource) jobposting.JobPosting {
	return jobposting.JobPosting{
		Company:  company,
		Title:    title,
		Location: "Remote",
		URL:      url,
		Source:   source,
	}
}

var (
	testSourceA = jobposting.PostingSource{Platform: "greenhouse", Key: "acme"}
	testSourceB = jobposting.PostingSource{Platform: "lever", Key: "globex"}
)

func testRun(source jobposting.PostingSource, postings int, status string) corpus.SourceRun {
	return corpus.SourceRun{
		Platform: source.Platform,
		Key:      source.Key,
		Company:  source.Key,
		Status:   status,
		Postings: postings,
	}
}

func TestCorpusApplyCreatesAndFoldsGenerations(t *testing.T) {
	dir := t.TempDir()
	corpusDir := filepath.Join(dir, "corpus")

	run1 := time.Date(2026, 7, 28, 9, 0, 0, 0, time.UTC)
	postings1 := []jobposting.JobPosting{
		testPosting("acme", "Engineer", "https://acme.example/1", testSourceA),
		testPosting("acme", "Designer", "https://acme.example/2", testSourceA),
		testPosting("globex", "Analyst", "https://globex.example/1", testSourceB),
	}
	sources1 := []corpus.SourceRun{
		testRun(testSourceA, 2, "complete"),
		testRun(testSourceB, 1, "complete"),
	}

	postingsPath, manifestPath := writeCrawlPair(t, dir, run1, sources1, postings1)

	stdout, stderr, err := runCorpus(t, "corpus", "apply",
		"--corpus", corpusDir,
		"--postings", postingsPath,
		"--manifest", manifestPath,
		"--writer", "test",
	)
	if err != nil {
		t.Fatalf("first apply: %v\nstderr: %s", err, stderr)
	}

	var first corpusApplySummary
	if err := json.Unmarshal([]byte(stdout), &first); err != nil {
		t.Fatalf("decode first summary %q: %v", stdout, err)
	}

	if first.Generation != 1 || first.Rows != 3 || first.Churn.Appeared != 3 {
		t.Fatalf("first apply summary = %+v, want generation 1 with 3 appeared rows", first)
	}

	// Second run: one posting gone from a complete source, one new one. The
	// missing posting must be counted missing, not closed: MissingRuns is 2.
	run2 := run1.Add(24 * time.Hour)
	postings2 := []jobposting.JobPosting{
		testPosting("acme", "Engineer", "https://acme.example/1", testSourceA),
		testPosting("globex", "Analyst", "https://globex.example/1", testSourceB),
		testPosting("globex", "Manager", "https://globex.example/2", testSourceB),
	}
	sources2 := []corpus.SourceRun{
		testRun(testSourceA, 1, "complete"),
		testRun(testSourceB, 2, "complete"),
	}

	dir2 := t.TempDir()
	postingsPath2, manifestPath2 := writeCrawlPair(t, dir2, run2, sources2, postings2)

	stdout, stderr, err = runCorpus(t, "corpus", "apply",
		"--corpus", corpusDir,
		"--postings", postingsPath2,
		"--manifest", manifestPath2,
		"--writer", "test",
	)
	if err != nil {
		t.Fatalf("second apply: %v\nstderr: %s", err, stderr)
	}

	var second corpusApplySummary
	if err := json.Unmarshal([]byte(stdout), &second); err != nil {
		t.Fatalf("decode second summary %q: %v", stdout, err)
	}

	if second.Generation != 2 || second.Rows != 4 {
		t.Fatalf("second apply summary = %+v, want generation 2 with 4 rows", second)
	}

	churn := second.Churn
	if churn.Appeared != 1 || churn.Unchanged != 2 || churn.Missing != 1 || churn.Closed != 0 {
		t.Fatalf("second apply churn = %+v, want 1 appeared, 2 unchanged, 1 missing, 0 closed", churn)
	}

	if !strings.Contains(stderr, "churn:") {
		t.Fatalf("stderr churn report missing: %q", stderr)
	}
}

func TestCorpusApplyIsDeterministic(t *testing.T) {
	dir := t.TempDir()

	runAt := time.Date(2026, 7, 28, 9, 0, 0, 0, time.UTC)
	postings := []jobposting.JobPosting{
		testPosting("acme", "Engineer", "https://acme.example/1", testSourceA),
		testPosting("globex", "Analyst", "https://globex.example/1", testSourceB),
	}
	sources := []corpus.SourceRun{
		testRun(testSourceA, 1, "complete"),
		testRun(testSourceB, 1, "complete"),
	}

	postingsPath, manifestPath := writeCrawlPair(t, dir, runAt, sources, postings)

	digests := func(out string) map[string]string {
		t.Helper()

		if _, _, err := runCorpus(t, "corpus", "apply",
			"--corpus", out,
			"--postings", postingsPath,
			"--manifest", manifestPath,
			"--writer", "test",
		); err != nil {
			t.Fatalf("apply into %s: %v", out, err)
		}

		sums := map[string]string{}

		for _, name := range []string{corpus.PostingsFile, corpus.SourcesFile, corpus.RunsFile, corpus.ManifestFile} {
			body, err := os.ReadFile(filepath.Join(out, name))
			if err != nil {
				t.Fatalf("read %s: %v", name, err)
			}

			sums[name] = string(sha256Bytes(body))
		}

		return sums
	}

	first := digests(filepath.Join(dir, "a"))
	second := digests(filepath.Join(dir, "b"))

	for name, sum := range first {
		if second[name] != sum {
			t.Fatalf("%s differs between two applies of the same inputs", name)
		}
	}
}

func sha256Bytes(body []byte) []byte {
	sum := sha256.Sum256(body)

	return sum[:]
}

func TestCorpusApplyRefusesACorruptBase(t *testing.T) {
	dir := t.TempDir()
	corpusDir := filepath.Join(dir, "corpus")

	if err := os.MkdirAll(corpusDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// A manifest that exists but cannot be read must be an error, never a
	// silent restart from generation zero.
	if err := os.WriteFile(filepath.Join(corpusDir, corpus.ManifestFile), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	runAt := time.Date(2026, 7, 28, 9, 0, 0, 0, time.UTC)
	postingsPath, manifestPath := writeCrawlPair(t, dir, runAt,
		[]corpus.SourceRun{testRun(testSourceA, 0, "complete")}, nil)

	_, _, err := runCorpus(t, "corpus", "apply",
		"--corpus", corpusDir,
		"--postings", postingsPath,
		"--manifest", manifestPath,
	)
	if err == nil {
		t.Fatal("apply over a corrupt base succeeded; it must fail closed")
	}
}

func TestCorpusApplyDryRunWritesNothing(t *testing.T) {
	dir := t.TempDir()
	corpusDir := filepath.Join(dir, "corpus")

	runAt := time.Date(2026, 7, 28, 9, 0, 0, 0, time.UTC)
	postingsPath, manifestPath := writeCrawlPair(t, dir, runAt,
		[]corpus.SourceRun{testRun(testSourceA, 1, "complete")},
		[]jobposting.JobPosting{testPosting("acme", "Engineer", "https://acme.example/1", testSourceA)})

	stdout, _, err := runCorpus(t, "corpus", "apply",
		"--corpus", corpusDir,
		"--postings", postingsPath,
		"--manifest", manifestPath,
		"--dry-run",
	)
	if err != nil {
		t.Fatalf("dry-run apply: %v", err)
	}

	var summary corpusApplySummary
	if err := json.Unmarshal([]byte(stdout), &summary); err != nil {
		t.Fatalf("decode summary: %v", err)
	}

	if !summary.DryRun || summary.Rows != 1 {
		t.Fatalf("summary = %+v, want dry_run with 1 row", summary)
	}

	if _, err := os.Stat(corpusDir); !os.IsNotExist(err) {
		t.Fatalf("dry-run created %s", corpusDir)
	}
}

func TestCorpusInspectAndVerify(t *testing.T) {
	dir := t.TempDir()
	corpusDir := filepath.Join(dir, "corpus")

	runAt := time.Date(2026, 7, 28, 9, 0, 0, 0, time.UTC)
	postingsPath, manifestPath := writeCrawlPair(t, dir, runAt,
		[]corpus.SourceRun{testRun(testSourceA, 1, "complete")},
		[]jobposting.JobPosting{testPosting("acme", "Engineer", "https://acme.example/1", testSourceA)})

	if _, _, err := runCorpus(t, "corpus", "apply",
		"--corpus", corpusDir, "--postings", postingsPath, "--manifest", manifestPath,
	); err != nil {
		t.Fatalf("apply: %v", err)
	}

	stdout, _, err := runCorpus(t, "corpus", "inspect", "--corpus", corpusDir)
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}

	if !strings.Contains(stdout, "generation 1") || !strings.Contains(stdout, "1 rows over 1 sources") {
		t.Fatalf("inspect output unexpected: %q", stdout)
	}

	stdout, _, err = runCorpus(t, "corpus", "verify", "--corpus", corpusDir)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}

	if !strings.Contains(stdout, "ok: generation 1") {
		t.Fatalf("verify output unexpected: %q", stdout)
	}
}

func TestCorpusQueryFiltersAndPaginates(t *testing.T) {
	dir := t.TempDir()
	corpusDir := filepath.Join(dir, "corpus")

	runAt := time.Date(2026, 7, 28, 9, 0, 0, 0, time.UTC)
	postings := []jobposting.JobPosting{
		testPosting("acme", "Software Engineer", "https://acme.example/1", testSourceA),
		testPosting("acme", "Staff Engineer", "https://acme.example/2", testSourceA),
		testPosting("acme", "Recruiter", "https://acme.example/3", testSourceA),
	}

	postingsPath, manifestPath := writeCrawlPair(t, dir, runAt,
		[]corpus.SourceRun{testRun(testSourceA, 3, "complete")}, postings)

	if _, _, err := runCorpus(t, "corpus", "apply",
		"--corpus", corpusDir, "--postings", postingsPath, "--manifest", manifestPath,
	); err != nil {
		t.Fatalf("apply: %v", err)
	}

	// Evaluated at the run instant, so the rows read open rather than stale no
	// matter when the test runs.
	asOf := runAt.Format(time.RFC3339)

	stdout, stderr, err := runCorpus(t, "corpus", "query",
		"--corpus", corpusDir,
		"--title", "engineer",
		"--limit", "1",
		"--as-of", asOf,
		"--json",
	)
	if err != nil {
		t.Fatalf("query: %v", err)
	}

	lines := strings.Count(strings.TrimSpace(stdout), "\n") + 1
	if lines != 1 {
		t.Fatalf("query --limit 1 emitted %d lines: %q", lines, stdout)
	}

	if !strings.Contains(stderr, "2 match(es); showing 1 after offset 0") {
		t.Fatalf("pagination summary unexpected: %q", stderr)
	}

	var row corpusQueryRow
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &row); err != nil {
		t.Fatalf("decode row: %v", err)
	}

	if row.State != "open" || !strings.Contains(row.Posting.Title, "Engineer") {
		t.Fatalf("row = %+v, want an open engineer row", row)
	}

	// The second page holds the other engineer.
	stdout, _, err = runCorpus(t, "corpus", "query",
		"--corpus", corpusDir,
		"--title", "engineer",
		"--limit", "1",
		"--offset", "1",
		"--as-of", asOf,
		"--json",
	)
	if err != nil {
		t.Fatalf("query page 2: %v", err)
	}

	var second corpusQueryRow
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &second); err != nil {
		t.Fatalf("decode second row: %v", err)
	}

	if second.ID == row.ID {
		t.Fatalf("offset 1 returned the same row %s", second.ID)
	}

	// An unknown state is refused rather than silently matching nothing.
	if _, _, err := runCorpus(t, "corpus", "query",
		"--corpus", corpusDir, "--state", "bogus",
	); err == nil {
		t.Fatal("query with an invalid --state succeeded")
	}
}

func TestCorpusApplyRequiresAClock(t *testing.T) {
	dir := t.TempDir()

	// A manifest with no finished_at and no --run-at must be refused: the corpus
	// takes exactly one clock reading per run and it must come from the inputs.
	manifestPath := filepath.Join(dir, "crawl-manifest.json")
	if err := os.WriteFile(manifestPath, []byte(`{"status":"complete","sources":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	postingsPath := filepath.Join(dir, "postings.ndjson")
	if err := os.WriteFile(postingsPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	_, _, err := runCorpus(t, "corpus", "apply",
		"--corpus", filepath.Join(dir, "corpus"),
		"--postings", postingsPath,
		"--manifest", manifestPath,
	)
	if err == nil || !strings.Contains(err.Error(), "finished_at") {
		t.Fatalf("err = %v, want a finished_at refusal", err)
	}
}
