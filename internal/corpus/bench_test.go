package corpus

import (
	"bytes"
	"fmt"
	"math/rand/v2"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/jobposting"
	"github.com/shoenig/test/must"
)

// BenchRows is the corpus size docs/design/storage-engine.md and
// docs/design/corpus-format.md both measure against: the 07/28 crawl's
// deduplicated posting count. Every number this file reports is comparable with
// theirs only because it is the same row count.
const BenchRows = 780_489

// platformShape is the per-platform postings and source counts from
// docs/measurements/2026-07-28-crawl.md's "Cost per platform" table. That table
// covers 647,368 of the run's 780,489 postings across twelve platforms; the
// remainder is synthesized as a thirteenth so the total matches.
//
// The distribution matters because it is what decides dictionary sizes and the
// source-major compression win. A uniform generator would flatter the format.
var platformShape = []struct {
	platform string
	postings int
	sources  int
}{
	{"jibe", 202_926, 125},
	{"workday", 174_929, 210},
	{"oraclecloud", 91_030, 30},
	{"smartrecruiters", 44_414, 54},
	{"greenhouse", 42_010, 646},
	{"phenom", 22_859, 15},
	{"successfactors", 18_375, 30},
	{"ashby", 12_772, 417},
	{"personio", 11_920, 970},
	{"lever", 9_897, 161},
	{"recruitee", 9_830, 492},
	{"pinpoint", 6_406, 118},
	{"other", 133_121, 417},
}

// The generator's vocabularies. Cardinality is called out because
// storage-engine.md §9 flags it as the reason to expect the real corpus to
// exceed the prototype's size: "real location cardinality is far higher, which
// will grow the location dictionary and shrink the compression advantage".
var (
	benchTitles = []string{
		"Software Engineer", "Senior Software Engineer", "Staff Software Engineer",
		"Product Manager", "Data Scientist", "Security Engineer", "Site Reliability Engineer",
		"Account Executive", "Customer Success Manager", "Registered Nurse",
		"Delivery Driver", "Warehouse Associate", "Store Manager", "Financial Analyst",
		"Mechanical Engineer", "Recruiter", "Designer", "Technical Program Manager",
	}

	benchLocations = []string{
		"Remote", "Remote - US", "San Francisco, CA", "New York, NY", "Austin, TX",
		"Seattle, WA", "Memphis, TN", "Chicago, IL", "London, UK", "Berlin, Germany",
		"Dublin, Ireland", "Bengaluru, India", "Toronto, Canada", "Sydney, Australia",
		"Tokyo, Japan", "Paris, France", "Amsterdam, Netherlands", "Warsaw, Poland",
		"São Paulo, Brazil", "Singapore", "Hybrid - Boston, MA", "Multiple Locations",
	}

	benchDepartments = []string{
		"Engineering", "Sales", "Marketing", "Operations", "Finance", "People",
		"Customer Support", "Legal", "Clinical", "Logistics",
	}
)

// benchCorpus builds a corpus shaped like the real crawl.
//
// Deterministic from a fixed seed, so two runs of the benchmark measure the same
// bytes and a size regression is a real one rather than a reshuffle.
func benchCorpus(rows int) ([]Row, []SourceState) {
	random := rand.New(rand.NewPCG(0x6a6874, 0x636f7270))

	out := make([]Row, 0, rows)
	states := make([]SourceState, 0, 4000)

	runAt := time.Date(2026, 7, 28, 3, 0, 0, 0, time.UTC)
	scale := float64(rows) / float64(BenchRows)

	for _, shape := range platformShape {
		sources := max(1, int(float64(shape.sources)*scale))
		budget := int(float64(shape.postings) * scale)

		// The registry is extremely skewed — 50% of all postings come from 40 of
		// 3,685 sources — so a power law rather than an even split. jibe/fedex alone
		// held 103,196 of jibe's 202,926.
		weights := make([]float64, sources)
		total := 0.0

		for i := range weights {
			weights[i] = 1 / float64(i+1)
			total += weights[i]
		}

		for i := range sources {
			key := shape.platform + "-tenant-" + strconv.Itoa(i)
			src := jobposting.PostingSource{Platform: shape.platform, Key: key}
			company := "Company " + strconv.Itoa(len(states))

			count := int(float64(budget) * weights[i] / total)
			if count == 0 {
				count = 1
			}

			states = append(states, SourceState{
				Source:         src,
				Company:        company,
				LastAttempt:    runAt,
				LastQualifying: runAt,
				LastPostings:   count,
				Trailing:       []int{count},
				Open:           count,
			})

			for j := range count {
				if len(out) >= rows {
					break
				}

				out = append(out, benchRow(random, src, company, j, runAt))
			}
		}
	}

	// Top up to the exact row count so the measurement is comparable with the
	// designs' 780,489 rather than approximately it.
	filler := states[0]
	for len(out) < rows {
		out = append(out, benchRow(random, filler.Source, filler.Company, len(out), runAt))
	}

	sortRows(out)

	return out, states
}

func benchRow(random *rand.Rand, src jobposting.PostingSource, company string, n int, runAt time.Time) Row {
	id := strconv.Itoa(1_000_000 + n)

	posting := jobposting.JobPosting{
		Source:     src,
		Company:    company,
		URL:        "https://" + src.Platform + ".example.com/" + src.Key + "/jobs/" + id,
		Title:      benchTitles[random.IntN(len(benchTitles))],
		Location:   benchLocations[random.IntN(len(benchLocations))],
		Department: benchDepartments[random.IntN(len(benchDepartments))],
		ExternalID: id,
		PostedAt:   runAt.Add(-time.Duration(random.IntN(120)) * 24 * time.Hour),
	}

	if random.IntN(4) == 0 {
		posting.UpdatedAt = posting.PostedAt.Add(time.Duration(random.IntN(48)) * time.Hour)
	}

	if random.IntN(10) == 0 {
		posting.RequisitionID = "REQ-" + id
	}

	// Most boards publish no structured pay field at all, so most rows must have
	// none: making every row carry compensation would measure a corpus that does
	// not exist.
	if random.IntN(8) == 0 {
		low := float64(60_000 + random.IntN(120_000))
		posting.Compensation = &jobposting.Compensation{
			Min: low, Max: low + float64(20_000+random.IntN(60_000)),
			Currency: "USD", Period: jobposting.PeriodYear,
		}
	}

	firstSeen := runAt.Add(-time.Duration(random.IntN(200)) * 24 * time.Hour)

	return Row{
		ID:        ID(src, BasisExternal, id),
		Basis:     BasisExternal,
		DedupeKey: DedupeKey(&posting),
		FirstSeen: firstSeen.Truncate(time.Second),
		Posting:   posting,
	}
}

func benchSize(tb testing.TB, rows []Row) []byte {
	tb.Helper()

	builder, err := buildTable(rows)
	must.NoError(tb, err)

	var buf bytes.Buffer

	_, err = builder.WriteTo(&buf)
	must.NoError(tb, err)

	return buf.Bytes()
}

// TestCorpusAtFullScale is the round trip the assignment asks for: build the
// 07/28 crawl's row count, write it, read it back and compare every row.
//
// It also prints the size table this package's report quotes. Skipped under
// -short because it is ~30 seconds and a gigabyte of transient heap.
func TestCorpusAtFullScale(t *testing.T) {
	if testing.Short() {
		t.Skip("full-scale round trip is slow; run without -short")
	}

	t.Parallel()

	rows, _ := benchCorpus(BenchRows)
	must.SliceLen(t, BenchRows, rows)

	var before, after, resident runtime.MemStats

	runtime.GC()
	runtime.ReadMemStats(&before)

	start := time.Now()
	builder, err := buildTable(rows)
	must.NoError(t, err)
	encode := time.Since(start)

	runtime.ReadMemStats(&after)

	// Two different numbers, because conflating them is how a memory claim becomes
	// wrong. Churn is everything the encode allocated, most of which is one
	// column's value slice handed straight to the garbage collector; live is what
	// the rows and the finished builder actually hold after a collection, and it
	// is the one that decides whether this fits in a browser tab.
	runtime.GC()
	runtime.ReadMemStats(&resident)

	var buf bytes.Buffer

	start = time.Now()
	_, err = builder.WriteTo(&buf)
	must.NoError(t, err)
	write := time.Since(start)

	start = time.Now()
	table, err := OpenTable(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	must.NoError(t, err)

	got, err := readRows(table)
	must.NoError(t, err)
	decode := time.Since(start)

	must.SliceLen(t, len(rows), got)

	for i := range rows {
		if rows[i].ID != got[i].ID || rows[i].Posting != got[i].Posting ||
			!rows[i].FirstSeen.Equal(got[i].FirstSeen) {
			// Compared by hand rather than with a deep-equal over 780k rows so a
			// failure names the row instead of printing the corpus.
			if rows[i].Posting.Compensation != nil && got[i].Posting.Compensation != nil &&
				*rows[i].Posting.Compensation == *got[i].Posting.Compensation &&
				rows[i].ID == got[i].ID && rows[i].FirstSeen.Equal(got[i].FirstSeen) {
				continue
			}

			t.Fatalf("row %d did not round trip:\n have %+v\n want %+v", i, got[i], rows[i])
		}
	}

	var raw int64
	for _, info := range table.Columns() {
		raw += info.RawBytes
	}

	t.Logf("rows=%d file=%s raw=%s bytes/row=%.1f encode=%s write=%s decode=%s encode-churn=%s live-after-encode=%s",
		len(rows),
		mib(int64(buf.Len())), mib(raw),
		float64(buf.Len())/float64(len(rows)),
		encode.Round(time.Millisecond), write.Round(time.Millisecond), decode.Round(time.Millisecond),
		mib(int64(after.TotalAlloc-before.TotalAlloc)), mib(int64(resident.HeapAlloc)))

	for _, info := range table.Columns() {
		t.Logf("  %-24s %-5s %8s gz  %9s raw", info.Name, info.Encoding,
			mib(info.CompressedBytes), mib(info.RawBytes))
	}
}

func mib(n int64) string { return fmt.Sprintf("%.2f MiB", float64(n)/(1<<20)) }

func BenchmarkBuildTable(b *testing.B) {
	rows, _ := benchCorpus(BenchRows)

	b.ResetTimer()
	b.ReportMetric(float64(len(rows)), "rows")

	for b.Loop() {
		builder, err := buildTable(rows)
		must.NoError(b, err)

		b.SetBytes(int64(builder.Rows()))
	}
}

func BenchmarkReadRows(b *testing.B) {
	rows, _ := benchCorpus(BenchRows)
	body := benchSize(b, rows)

	b.ResetTimer()

	for b.Loop() {
		table, err := OpenTable(bytes.NewReader(body), int64(len(body)))
		must.NoError(b, err)

		got, err := readRows(table)
		must.NoError(b, err)

		if len(got) != len(rows) {
			b.Fatal("short read")
		}
	}
}

// BenchmarkCountByPlatform is the streaming read mode: decode the one column the
// query needs and discard it. It is the aggregate storage-engine.md §3 measures
// at 56 ms cold on native and 202 ms on js/wasm.
func BenchmarkCountByPlatform(b *testing.B) {
	rows, _ := benchCorpus(BenchRows)
	body := benchSize(b, rows)

	b.ResetTimer()

	for b.Loop() {
		table, err := OpenTable(bytes.NewReader(body), int64(len(body)))
		must.NoError(b, err)

		platforms, err := table.Strings(colPlatform)
		must.NoError(b, err)

		counts := map[string]int{}
		for _, platform := range platforms {
			counts[platform]++
		}

		if len(counts) != len(platformShape) {
			b.Fatalf("counted %d platforms, want %d", len(counts), len(platformShape))
		}
	}
}

// BenchmarkOpenTable is the cost of learning what a corpus contains: the trailer
// and the footer, and no column payload. It is the "~30 KB, 2 reads" number the
// browser story rests on.
func BenchmarkOpenTable(b *testing.B) {
	rows, _ := benchCorpus(BenchRows)
	body := benchSize(b, rows)

	b.ResetTimer()

	for b.Loop() {
		table, err := OpenTable(bytes.NewReader(body), int64(len(body)))
		must.NoError(b, err)

		if table.Rows() != len(rows) {
			b.Fatal("wrong height")
		}
	}
}

// BenchmarkApplyFullRewrite is the whole point of the rewrite-every-run model:
// read the corpus, fold a run into it, sort and write a new one.
// storage-engine.md §5 measures the equivalent at 5.96 s against a 720-second
// crawl.
func BenchmarkApplyFullRewrite(b *testing.B) {
	rows, states := benchCorpus(BenchRows)
	body := benchSize(b, rows)

	runs := make([]SourceRun, 0, len(states))
	for _, state := range states {
		runs = append(runs, SourceRun{
			Platform: state.Source.Platform, Key: state.Source.Key,
			Company: state.Company, Status: StatusComplete, Postings: state.LastPostings,
		})
	}

	postings := make([]*jobposting.JobPosting, len(rows))
	for i := range rows {
		postings[i] = &rows[i].Posting
	}

	table, err := OpenTable(bytes.NewReader(body), int64(len(body)))
	must.NoError(b, err)

	base := newCorpus(Manifest{Rows: len(rows), Policy: DefaultPolicy()}, states, table)

	b.ResetTimer()

	for b.Loop() {
		generation, err := Apply(b.Context(), base, RunInput{
			RunAt:    time.Date(2026, 7, 29, 3, 0, 0, 0, time.UTC),
			Sources:  runs,
			Postings: seq(postings...),
		}, Policy{})
		must.NoError(b, err)

		builder, err := buildTable(generation.Rows)
		must.NoError(b, err)

		var buf bytes.Buffer

		_, err = builder.WriteTo(&buf)
		must.NoError(b, err)
	}
}
