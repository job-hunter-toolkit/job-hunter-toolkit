package wikidata_test

import (
	"testing"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/enrich/enrichtest"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/enrich/wikidata"
	"github.com/shoenig/test/must"
)

// bindingsFixture is a trimmed SPARQL JSON result.
//
// It has the shape that makes merging necessary: one item arrives as several
// rows because it has two dated headcounts and two industries, and the row order
// is not the order anyone would want the answer in.
const bindingsFixture = `{
  "head": {"vars": ["item", "itemLabel", "cik", "employees"]},
  "results": {"bindings": [
    {
      "item": {"value": "http://www.wikidata.org/entity/Q1"},
      "itemLabel": {"value": "Cloudflare"},
      "cik": {"value": "0001477333"},
      "employees": {"value": "1200"},
      "employeesAsOf": {"value": "2019-01-01T00:00:00Z"},
      "inception": {"value": "2009-07-01T00:00:00Z"},
      "industryLabel": {"value": "web services"},
      "hqLabel": {"value": "San Francisco"}
    },
    {
      "item": {"value": "http://www.wikidata.org/entity/Q1"},
      "cik": {"value": "0001477333"},
      "employees": {"value": "3200"},
      "employeesAsOf": {"value": "2023-12-31T00:00:00Z"},
      "industryLabel": {"value": "computer security"},
      "parentLabel": {"value": "Example Holding"}
    },
    {
      "item": {"value": "http://www.wikidata.org/entity/Q2"},
      "itemLabel": {"value": "Example Robotics"},
      "cik": {"value": "44"},
      "inception": {"value": "2015-03-04T00:00:00Z"}
    }
  ]}
}`

func TestByCIKMergesBindings(t *testing.T) {
	t.Parallel()

	client, transport := enrichtest.Client(map[string]string{"query.wikidata.org": bindingsFixture})

	companies, err := wikidata.ByCIK(t.Context(), client, []string{"0001477333", "0000000044"})
	must.NoError(t, err)
	must.MapLen(t, 2, companies)

	cloudflare := companies["0001477333"]
	must.NotNil(t, cloudflare)
	must.Eq(t, "Q1", cloudflare.QID)
	must.Eq(t, "Cloudflare", cloudflare.Label)

	// The statement with the latest point-in-time qualifier wins. Wikidata
	// records headcount as a time series, and taking whichever row arrived first
	// would report a company's 2019 size as often as its current one.
	must.Eq(t, 3200, cloudflare.Employees)

	must.Eq(t, 2009, cloudflare.Founded)
	must.Eq(t, "San Francisco", cloudflare.Headquarters)
	must.Eq(t, "Example Holding", cloudflare.Parent)

	// Several industries are joined in sorted order, so a regenerated table does
	// not differ between runs because the service returned rows in another
	// order.
	must.Eq(t, "computer security; web services", cloudflare.Industry)

	// A CIK echoed back in its unpadded spelling is re-padded, or it would key
	// the map under a form nothing else uses.
	must.MapContainsKey(t, companies, "0000000044")

	must.Len(t, 1, transport.Requests())
}

// TestQueryOffersBothCIKSpellings: P5531 values are entered by hand and both
// the padded and unpadded forms appear in the wild. This has never been checked
// against the live service, so the query is deliberately the tolerant one.
func TestQueryOffersBothCIKSpellings(t *testing.T) {
	t.Parallel()

	query := wikidata.Query([]string{"0001477333"})

	must.StrContains(t, query, `"0001477333"`)
	must.StrContains(t, query, `"1477333"`)
	must.StrContains(t, query, "wdt:P5531")
	must.StrContains(t, query, "pq:P585", must.Sprint(
		"the point-in-time qualifier is what makes the newest headcount findable"))
}

// TestByCIKChunksLargeInputs keeps one query from growing an unbounded URL, and
// keeps a refresh of the whole matched set to a handful of queries rather than
// one per company.
func TestByCIKChunksLargeInputs(t *testing.T) {
	t.Parallel()

	client, transport := enrichtest.Client(map[string]string{
		"query.wikidata.org": `{"results": {"bindings": []}}`,
	})

	ciks := make([]string, 0, wikidata.ChunkSize*2+1)
	for i := range cap(ciks) {
		ciks = append(ciks, padded(i+1))
	}

	companies, err := wikidata.ByCIK(t.Context(), client, ciks)
	must.NoError(t, err)
	must.MapEmpty(t, companies, must.Sprint("a CIK Wikidata does not know is absent, not an error"))
	must.Len(t, 3, transport.Requests())
}

// TestByCIKReportsAFailedQuery: a silently empty decoration step would commit a
// table with every headcount blank and no sign anything went wrong.
func TestByCIKReportsAFailedQuery(t *testing.T) {
	t.Parallel()

	client, _ := enrichtest.Client(nil)

	_, err := wikidata.ByCIK(t.Context(), client, []string{"0001477333"})
	must.ErrorContains(t, err, "querying Wikidata")
}

// padded renders an integer as a ten-digit CIK.
func padded(n int) string {
	digits := []byte("0000000000")

	for i := len(digits) - 1; n > 0; i-- {
		digits[i] = byte('0' + n%10)
		n /= 10
	}

	return string(digits)
}

// TestWebsitesKeysByPaddedCIK: P5531 is hand-entered and appears both padded
// and bare, and a corroboration table keyed inconsistently corroborates
// nothing.
func TestWebsitesKeysByPaddedCIK(t *testing.T) {
	t.Parallel()

	client, transport := enrichtest.Client(map[string]string{
		"query.wikidata.org": `{"results": {"bindings": [
      {"cik": {"value": "0000000077"}, "site": {"value": "https://www.cae.com/"}},
      {"cik": {"value": "77"},         "site": {"value": "https://careers.cae.com/"}},
      {"cik": {"value": "77"},         "site": {"value": "https://www.cae.com/"}},
      {"cik": {"value": "1000002"},    "site": {"value": "https://example.test/"}},
      {"cik": {"value": ""},           "site": {"value": "https://orphan.test/"}}
    ]}}`,
	})

	sites, err := wikidata.Websites(t.Context(), client)
	must.NoError(t, err)

	must.MapLen(t, 2, sites)
	must.Eq(t, []string{"https://www.cae.com/", "https://careers.cae.com/"}, sites["0000000077"],
		must.Sprint("both spellings of one CIK are one filer, and a repeated site is not two"))
	must.Eq(t, []string{"https://example.test/"}, sites["0001000002"])

	must.Len(t, 1, transport.Requests(), must.Sprint(
		"the whole P5531 x P856 join is one query; chunking it would be one request per chunk for no gain"))
}

// TestWebsitesReportsAFailedQuery: an empty corroboration table silently turns
// every short-name match into a candidate, so a failure has to be an error
// rather than a map with nothing in it.
func TestWebsitesReportsAFailedQuery(t *testing.T) {
	t.Parallel()

	client, _ := enrichtest.Client(nil)

	_, err := wikidata.Websites(t.Context(), client)
	must.Error(t, err)
	must.StrContains(t, err.Error(), "filer websites")
}
