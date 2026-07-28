// Package wikidata reads the company facts EDGAR cannot supply.
//
// EDGAR covers filers. Most companies this project crawls are private startups
// on Greenhouse, Ashby and Lever, and for them EDGAR has nothing at all —
// no headcount, no founding date, no parent, no headquarters. Wikidata has all
// four, is key-free, and is CC0, which is the most permissive licence of any
// source considered for this feature: the derived table can be shipped in the
// binary with no attribution obligation (though the package comment is one
// anyway).
//
// # It decorates, it does not resolve
//
// Items are fetched by CIK (property P5531), never by name. That is the whole
// safety argument for using Wikidata here: an item joined by identifier to a
// row EDGAR already identified cannot attach the wrong company's headcount,
// while a label search for "Atlas" would have several thousand ways to do so.
// The consequence is that Wikidata coverage is a subset of EDGAR coverage today.
// Widening it to private companies means resolving those companies first, by
// hand, into manual.tsv — which is exactly the reviewed-artifact workflow the
// rest of this feature is built around.
//
// # Access policy
//
// The query service documents 60 seconds of wall clock per query, 60 seconds of
// query time per minute per client, five concurrent queries per IP, and an
// escalation path from 429 to an outright ban for clients that do not back off.
// Wikimedia's user-agent policy rejects generic agents. The generator therefore
// runs one query at a time through [fetch.Client], paced, with a contact in the
// User-Agent, and asks for a few hundred companies per query rather than one
// query per company.
package wikidata

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/enrich/fetch"
)

// Endpoint is the SPARQL query service.
const Endpoint = "https://query.wikidata.org/sparql"

// Interval is the minimum spacing between queries. Each one asks about a few
// hundred companies, so a monthly refresh of this project's entire matched set
// is a handful of queries and a few seconds of the service's time.
const Interval = 2 * time.Second

// ChunkSize is how many CIKs one query asks about.
//
// Bounded by URL length rather than by the service's patience: each CIK is sent
// in two spellings (see [Query]), so 150 companies is roughly 5 KB of query
// string, comfortably inside what any proxy will forward as a GET.
const ChunkSize = 150

// Company is what Wikidata knows about one filer.
type Company struct {
	// CIK is the identifier the row was joined on, ten digits.
	CIK string

	// QID is the Wikidata item, such as "Q312".
	QID string

	// Label is the item's English label, which is the company's common name
	// rather than its legal name — "Apple" rather than "Apple Inc.". EDGAR is
	// the better source for the legal name; this is the better source for the
	// name a person would say out loud.
	Label string

	// Employees is the headcount from the statement with the most recent
	// point-in-time qualifier.
	Employees int

	// Founded is the year of inception (P571).
	Founded int

	// Industry joins every industry statement (P452) with "; ", sorted, because
	// a company can be in several and picking one arbitrarily would make the
	// generated table differ between runs for no reason.
	Industry string

	// Headquarters is the headquarters location label (P159).
	Headquarters string

	// Parent is the parent organization label (P749).
	Parent string
}

// Query returns the SPARQL that fetches the given CIKs.
//
// Each CIK is offered in two spellings. Wikidata's P5531 values are entered by
// hand, and both the ten-digit zero-padded form and the bare integer form appear
// in the wild; asking for both costs nothing and is the difference between
// matching an item and silently not matching it. This has not been verified
// against the live service — this project's build environment has no egress —
// so it is deliberately the tolerant version.
//
// Exported so a reviewer can paste it into query.wikidata.org and see for
// themselves what the generator asked for.
func Query(ciks []string) string {
	values := make([]string, 0, len(ciks)*2)
	seen := make(map[string]bool, len(ciks)*2)

	for _, cik := range ciks {
		for _, form := range []string{cik, strings.TrimLeft(cik, "0")} {
			if form == "" || seen[form] {
				continue
			}

			seen[form] = true

			values = append(values, `"`+form+`"`)
		}
	}

	return `SELECT ?item ?itemLabel ?cik ?employees ?employeesAsOf ?inception ?industryLabel ?hqLabel ?parentLabel WHERE {
  VALUES ?cik { ` + strings.Join(values, " ") + ` }
  ?item wdt:P5531 ?cik.
  OPTIONAL {
    ?item p:P1128 ?employeesStatement.
    ?employeesStatement ps:P1128 ?employees.
    OPTIONAL { ?employeesStatement pq:P585 ?employeesAsOf. }
  }
  OPTIONAL { ?item wdt:P571 ?inception. }
  OPTIONAL { ?item wdt:P452 ?industry. }
  OPTIONAL { ?item wdt:P159 ?hq. }
  OPTIONAL { ?item wdt:P749 ?parent. }
  SERVICE wikibase:label { bd:serviceParam wikibase:language "en". }
}`
}

// results is the SPARQL JSON results shape.
type results struct {
	Results struct {
		Bindings []map[string]struct {
			Value string `json:"value"`
		} `json:"bindings"`
	} `json:"results"`
}

// ByCIK fetches what Wikidata knows about the given CIKs, in chunks.
//
// A CIK with no item is simply absent from the result, which is the ordinary
// case and not an error: Wikidata's coverage of small public companies is
// patchy, and the table records what is known rather than what was asked for.
func ByCIK(ctx context.Context, client *http.Client, ciks []string) (map[string]*Company, error) {
	companies := make(map[string]*Company, len(ciks))

	for chunk := range slices.Chunk(slices.Sorted(slices.Values(ciks)), ChunkSize) {
		if err := queryChunk(ctx, client, chunk, companies); err != nil {
			return nil, err
		}
	}

	return companies, nil
}

// queryChunk runs one query and merges its bindings into companies.
func queryChunk(ctx context.Context, client *http.Client, ciks []string, companies map[string]*Company) error {
	endpoint := Endpoint + "?" + url.Values{
		"query":  {Query(ciks)},
		"format": {"json"},
	}.Encode()

	var payload results

	// The results media type is asked for explicitly: the service content-
	// negotiates, and a client that does not say what it wants can be answered
	// with HTML.
	if err := fetch.JSON(ctx, client, endpoint, "application/sparql-results+json", &payload); err != nil {
		return fmt.Errorf("querying Wikidata for %d CIKs: %w", len(ciks), err)
	}

	// A binding is one row of a join, so an item with three industries and two
	// dated headcounts arrives as six rows. Merging is therefore not optional,
	// and it has to be order-independent or the generated table would depend on
	// the service's row order.
	industries := make(map[string]map[string]bool)
	asOf := make(map[string]string)

	for _, binding := range payload.Results.Bindings {
		value := func(name string) string { return strings.TrimSpace(binding[name].Value) }

		cik := value("cik")
		if cik == "" {
			continue
		}

		// The result echoes whichever spelling matched, so it is re-padded to
		// the canonical ten digits before being used as a key.
		cik = padCIK(cik)

		company, ok := companies[cik]
		if !ok {
			company = &Company{CIK: cik}
			companies[cik] = company
		}

		// Initialized independently of the company: a duplicated CIK in the
		// caller's input can put one company in two chunks, and a nil map here
		// would be a panic rather than a missing industry.
		if industries[cik] == nil {
			industries[cik] = make(map[string]bool)
		}

		if company.QID == "" {
			company.QID = strings.TrimPrefix(value("item"), "http://www.wikidata.org/entity/")
		}

		if company.Label == "" {
			company.Label = value("itemLabel")
		}

		if company.Headquarters == "" {
			company.Headquarters = value("hqLabel")
		}

		if company.Parent == "" {
			company.Parent = value("parentLabel")
		}

		if company.Founded == 0 {
			company.Founded = year(value("inception"))
		}

		if industry := value("industryLabel"); industry != "" {
			industries[cik][industry] = true
		}

		if employees := value("employees"); employees != "" {
			// The statement carrying the latest point-in-time qualifier wins.
			// Wikidata records headcount as a time series, and taking whichever
			// row arrived first would report a company's 2014 size as often as
			// its current one.
			when := value("employeesAsOf")

			if count, err := strconv.Atoi(employees); err == nil && count > 0 {
				if company.Employees == 0 || when > asOf[cik] {
					company.Employees = count
					asOf[cik] = when
				}
			}
		}
	}

	for cik, set := range industries {
		if company, ok := companies[cik]; ok && company.Industry == "" {
			names := slices.Sorted(maps.Keys(set))
			company.Industry = strings.Join(names, "; ")
		}
	}

	return nil
}

// year extracts the year from a Wikidata date literal such as
// "1976-04-01T00:00:00Z". Wikidata also publishes dates before year 1 with a
// leading "-", which is not a company founding date this project will ever see,
// and which would parse as a negative year rather than silently as a positive
// one.
func year(literal string) int {
	if len(literal) < 4 {
		return 0
	}

	value, err := strconv.Atoi(literal[:4])
	if err != nil || value <= 0 {
		return 0
	}

	return value
}

// padCIK renders a CIK as ten digits, tolerating either spelling coming back
// from the query service.
func padCIK(raw string) string {
	trimmed := strings.TrimLeft(strings.TrimSpace(raw), "0")

	value, err := strconv.Atoi(trimmed)
	if err != nil {
		return raw
	}

	return fmt.Sprintf("%010d", value)
}
