package corpus

import (
	"strconv"
	"testing"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/shard"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/jobposting"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func source(platform, key string) jobposting.PostingSource {
	return jobposting.PostingSource{Platform: platform, Key: key}
}

func posting(src jobposting.PostingSource, fields ...string) *jobposting.JobPosting {
	// fields is (external, requisition, url, title, location) so a test can say
	// exactly which rungs a board published and leave the rest empty.
	get := func(i int) string {
		if i < len(fields) {
			return fields[i]
		}

		return ""
	}

	return &jobposting.JobPosting{
		Source:        src,
		ExternalID:    get(0),
		RequisitionID: get(1),
		URL:           get(2),
		Title:         get(3),
		Location:      get(4),
		Company:       src.Key,
	}
}

func TestIdentifyPrefersTheMostSpecificKeyThePlatformPublished(t *testing.T) {
	t.Parallel()

	src := source("greenhouse", "anthropic")

	got := Identify(src, []*jobposting.JobPosting{
		posting(src, "ext-1", "REQ-1", "https://example.com/1", "Engineer", "Remote"),
		posting(src, "", "REQ-2", "https://example.com/2", "Designer", "Remote"),
		posting(src, "", "", "https://example.com/3", "Analyst", "Remote"),
		posting(src, "", "", "", "Manager", "Remote"),
	}, false)

	test.Eq(t, []Basis{BasisExternal, BasisRequisition, BasisURL, BasisDescriptor}, got.Bases)
	test.SliceEmpty(t, got.Demoted)
	test.Eq(t, 0, got.Collisions)

	for i, id := range got.IDs {
		test.Eq(t, 32, len(id), test.Sprintf("row %d", i))
	}
}

func TestIdentifyDemotesACollidingRequisitionForTheWholeSource(t *testing.T) {
	t.Parallel()

	// greenhouse/stripe publishes the literal string "See Opening ID" on 531 of
	// 532 postings. The rung is demoted for the source, not skipped for the two
	// rows that happened to collide, because a requisition that is not unique is
	// not an identifier at that source at all.
	src := source("greenhouse", "stripe")

	postings := make([]*jobposting.JobPosting, 5)
	for i := range postings {
		postings[i] = posting(src, "ext-"+strconv.Itoa(i), "See Opening ID",
			"https://example.com/"+strconv.Itoa(i), "Engineer", "SF")
	}

	got := Identify(src, postings, false)

	test.Eq(t, []Basis{BasisRequisition}, got.Demoted)

	for i, basis := range got.Bases {
		test.Eq(t, BasisExternal, basis, test.Sprintf("row %d", i))
	}
}

func TestIdentifyFallsPastAColldingRequisitionToURLWhenThereIsNoExternalID(t *testing.T) {
	t.Parallel()

	src := source("teamtailor", "acme")

	postings := []*jobposting.JobPosting{
		posting(src, "", "SHARED", "https://example.com/1", "Engineer", "Remote"),
		posting(src, "", "SHARED", "https://example.com/2", "Engineer", "Berlin"),
	}

	got := Identify(src, postings, false)

	test.Eq(t, []Basis{BasisRequisition}, got.Demoted)
	test.Eq(t, []Basis{BasisURL, BasisURL}, got.Bases)
}

func TestIdentifyHonoursTheStickyRequisitionUnsafeFlag(t *testing.T) {
	t.Parallel()

	// A lucky day must not re-promote a field that collided yesterday, so the
	// caller's remembered flag demotes the rung even when this run's requisitions
	// happen to be distinct.
	src := source("greenhouse", "databricks")

	postings := []*jobposting.JobPosting{
		posting(src, "", "REQ-1", "https://example.com/1", "Engineer", "Remote"),
		posting(src, "", "REQ-2", "https://example.com/2", "Engineer", "Berlin"),
	}

	safe := Identify(src, postings, false)
	test.Eq(t, []Basis{BasisRequisition, BasisRequisition}, safe.Bases)

	unsafe := Identify(src, postings, true)
	test.Eq(t, []Basis{BasisURL, BasisURL}, unsafe.Bases)
	test.Eq(t, []Basis{BasisRequisition}, unsafe.Demoted)
}

func TestIdentifyDemotesACollidingURL(t *testing.T) {
	t.Parallel()

	// Two Greenhouse slugs for one employer can publish the same absolute URL.
	src := source("greenhouse", "acme")

	postings := []*jobposting.JobPosting{
		posting(src, "", "", "https://example.com/1", "Engineer", "Remote"),
		posting(src, "", "", "https://example.com/1?utm_source=x", "Designer", "Berlin"),
	}

	got := Identify(src, postings, false)

	test.Eq(t, []Basis{BasisURL}, got.Demoted)
	test.Eq(t, []Basis{BasisDescriptor, BasisDescriptor}, got.Bases)
	test.Eq(t, 0, got.Collisions)
}

func TestIdentifyNeverDemotesTheBottomRungWholesale(t *testing.T) {
	t.Parallel()

	// Two rows that are genuinely indistinguishable must not cost a third row its
	// identity. The corpus drops the duplicate and counts it; it does not abandon
	// descriptor identity for the source.
	src := source("recruitee", "acme")

	postings := []*jobposting.JobPosting{
		posting(src, "", "", "", "Engineer", "Remote"),
		posting(src, "", "", "", "Engineer", "Remote"),
		posting(src, "", "", "", "Designer", "Remote"),
	}

	got := Identify(src, postings, false)

	test.Eq(t, 1, got.Collisions)
	test.Eq(t, "", got.IDs[1])
	test.NotEq(t, "", got.IDs[0])
	test.NotEq(t, "", got.IDs[2])
}

func TestIdentityIsScopedToTheIntegration(t *testing.T) {
	t.Parallel()

	// An employer on two ATSs has two rows, which is the truth: two applications,
	// two URLs, two closing dates. It is also what makes per-source closure sound,
	// because a Greenhouse failure cannot reason about an Ashby row.
	greenhouse := ID(source("greenhouse", "acme"), BasisExternal, "1")
	ashby := ID(source("ashby", "acme"), BasisExternal, "1")
	otherKey := ID(source("greenhouse", "acme2"), BasisExternal, "1")

	test.NotEq(t, greenhouse, ashby)
	test.NotEq(t, greenhouse, otherKey)
}

func TestIDIsNotAmbiguousAcrossFieldBoundaries(t *testing.T) {
	t.Parallel()

	// The NUL separators are what stop platform "ab" key "c" from hashing the same
	// as platform "a" key "bc".
	test.NotEq(t,
		ID(source("ab", "c"), BasisExternal, "1"),
		ID(source("a", "bc"), BasisExternal, "1"),
	)

	test.NotEq(t,
		ID(source("a", "b"), BasisExternal, "1"),
		ID(source("a", "b"), BasisURL, "1"),
	)
}

func TestIDIsStableAcrossRuns(t *testing.T) {
	t.Parallel()

	// A golden value, because the entire corpus is renumbered if this changes and
	// IdentityVersion is what a change must bump.
	test.Eq(t, IdentityVersion, 1)
	test.Eq(t,
		ID(source("greenhouse", "anthropic"), BasisExternal, "4012345"),
		ID(source("greenhouse", "anthropic"), BasisExternal, "4012345"),
	)
	test.Eq(t, 32, len(ID(source("greenhouse", "anthropic"), BasisExternal, "4012345")))
}

func TestDedupeKeyMatchesShardPostingKey(t *testing.T) {
	t.Parallel()

	// Continuity of the jobs_record posting column is a hard requirement, and it
	// holds only while these two agree byte for byte. The corpus reimplements the
	// four lines rather than importing internal/shard, so this is the pin.
	cases := []*jobposting.JobPosting{
		{URL: "https://example.com/1", Company: "acme", Title: "Engineer", Location: "Remote"},
		{Company: "acme", Title: "Engineer", Location: "Remote"},
		{},
		{URL: "https://example.com/2"},
	}

	for i, c := range cases {
		var asInternal internal.JobPosting = *c

		test.Eq(t, shard.PostingKey(&asInternal), DedupeKey(c), test.Sprintf("case %d", i))
	}
}

func TestIdentifySkipsNilPostings(t *testing.T) {
	t.Parallel()

	src := source("greenhouse", "acme")

	got := Identify(src, []*jobposting.JobPosting{
		nil,
		posting(src, "ext-1", "", "", "Engineer", "Remote"),
	}, false)

	must.SliceLen(t, 2, got.IDs)
	test.Eq(t, "", got.IDs[0])
	test.NotEq(t, "", got.IDs[1])
}
