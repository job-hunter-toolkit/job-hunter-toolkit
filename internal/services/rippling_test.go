package services

import (
	"slices"
	"testing"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestRippling(t *testing.T) {
	testSingle(t, "chess", Rippling)
}

func TestRippling_all(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	testMultipleParallel(t, slices.Values(RipplingCompanies), Rippling)
}

// ripplingBoardPage renders the careers page the way Rippling serves one: a
// Next.js document whose __NEXT_DATA__ script holds the dehydrated query this
// adapter reads. items is inserted verbatim so a test can shape its own
// postings.
func ripplingBoardPage(company, items string) string {
	return `<html><body><script id="__NEXT_DATA__" type="application/json">{
		"props": {"pageProps": {"dehydratedState": {"queries": [
			{"queryKey": ["board", "` + company + `", "job-posts"],
			 "state": {"data": {"items": [` + items + `]}}}
		]}}}
	}</script></body></html>`
}

// TestRipplingReadsTheFieldsItAlreadyDecoded is a regression test.
//
// department.name has been decoded and never referenced since this adapter was
// written, and the per-site workplaceType was consulted at exactly one place —
// to append the literal word "Remote" to a location string, and only in the
// branch where the site had no name of its own. For every other posting the
// board's structured workplace answer was decoded and thrown away, leaving
// `--remote` to rediscover it by looking for that word in free text.
func TestRipplingReadsTheFieldsItAlreadyDecoded(t *testing.T) {
	t.Parallel()

	client, _ := fixtureClient(map[string]string{
		"ats.rippling.com": ripplingBoardPage("acme", `
			{
				"id": "post-1",
				"name": "Security Engineer",
				"url": "https://ats.rippling.com/acme/jobs/post-1",
				"department": {"name": "Engineering"},
				"locations": [{"name": "San Francisco", "city": "San Francisco",
				               "state": "California", "stateCode": "CA",
				               "country": "United States", "countryCode": "US",
				               "workplaceType": "HYBRID"}],
				"language": "en"
			},
			{
				"id": "post-2",
				"name": "Support Engineer",
				"url": "https://ats.rippling.com/acme/jobs/post-2",
				"department": {"name": "Customer Success"},
				"locations": [
					{"name": "Remote - US", "workplaceType": "REMOTE"},
					{"name": "Remote - EU", "workplaceType": "REMOTE"}
				]
			},
			{
				"id": "post-3",
				"name": "Field Technician",
				"url": "https://ats.rippling.com/acme/jobs/post-3",
				"department": {"name": "Operations"},
				"locations": [
					{"name": "Remote - US", "workplaceType": "REMOTE"},
					{"name": "Dallas", "workplaceType": "ON_SITE"}
				]
			}
		`),
	})

	postings, errs := drain(Rippling(t.Context(), client, "acme"))

	must.SliceEmpty(t, errs)
	must.Len(t, 3, postings)

	test.Eq(t, "Engineering", postings[0].Department)
	test.Eq(t, internal.WorkplaceTypeHybrid, postings[0].WorkplaceType)
	test.Eq(t, "post-1", postings[0].ExternalID)
	test.Eq(t, internal.PostingSource{Platform: ripplingPlatform, Key: "acme"}, postings[0].Source)

	// Hybrid is not remote, and it is not "not remote" either: the board did not
	// answer that question, so the location-text heuristic keeps its say.
	test.Nil(t, postings[0].Remote)

	// Every site agrees, so the posting has one workplace answer.
	test.Eq(t, internal.WorkplaceTypeRemote, postings[1].WorkplaceType)
	must.NotNil(t, postings[1].Remote)
	test.True(t, *postings[1].Remote)

	// The sites disagree, so there is no single true answer. Picking the first
	// would make the value depend on the order the board serialised its
	// locations in, and nobody filtering on it could see that it was a coin flip.
	test.Eq(t, internal.WorkplaceTypeUnknown, postings[2].WorkplaceType)
	test.Nil(t, postings[2].Remote)

	// The location string itself is untouched by any of this.
	test.Eq(t, "Remote - US; Dallas", postings[2].Location)
}

func TestRipplingWorkplaceType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   []ripplingLocation
		want internal.WorkplaceType
	}{
		{name: "no locations"},
		{
			name: "unrecognised vocabulary",
			in:   []ripplingLocation{{WorkplaceType: "FLEXIBLE"}},
		},
		{
			name: "remote",
			in:   []ripplingLocation{{WorkplaceType: "REMOTE"}},
			want: internal.WorkplaceTypeRemote,
		},
		{
			name: "onsite spelled with an underscore",
			in:   []ripplingLocation{{WorkplaceType: "ON_SITE"}},
			want: internal.WorkplaceTypeOnsite,
		},
		{
			// An unreadable site must not veto a readable one, or a board that
			// invented one new spelling would blank the field for every
			// multi-site posting at once.
			name: "one site unreadable",
			in:   []ripplingLocation{{WorkplaceType: "REMOTE"}, {WorkplaceType: ""}},
			want: internal.WorkplaceTypeRemote,
		},
		{
			name: "sites disagree",
			in:   []ripplingLocation{{WorkplaceType: "REMOTE"}, {WorkplaceType: "HYBRID"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			test.Eq(t, tt.want, ripplingWorkplaceType(tt.in))
		})
	}
}
