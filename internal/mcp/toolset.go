package mcp

import (
	"fmt"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/jobposting"
)

// employmentTypeNames returns the canonical employment types as schema enum
// values, from the same list the CLI validates against, so the vocabulary an
// agent is shown cannot drift from the one the filter accepts.
func employmentTypeNames() []string {
	values := jobposting.EmploymentTypeValues()
	names := make([]string, 0, len(values))

	for _, v := range values {
		names = append(names, string(v))
	}

	return names
}

// workplaceTypeNames returns the canonical workplace types as schema enum values.
func workplaceTypeNames() []string {
	values := jobposting.WorkplaceTypeValues()
	names := make([]string, 0, len(values))

	for _, v := range values {
		names = append(names, string(v))
	}

	return names
}

// tools returns the tool surface, in a stable order.
func (s *Server) tools() []tool {
	limits := s.Limits.withDefaults()

	return []tool{
		s.searchJobsTool(limits),
		s.listCompaniesTool(),
		s.listPlatformsTool(),
		s.lookupEmployerTool(),
	}
}

func (s *Server) searchJobsTool(limits Limits) tool {
	return tool{
		Name:  "search_jobs",
		Title: "Search job postings at named companies",
		Description: fmt.Sprintf(`Search live job postings by crawling the job boards of named companies on demand.

REQUIRES "companies". This is not a formality. Postings are fetched from each company's ATS at call time; there is no cached corpus to query. Naming companies is what selects which boards get fetched, and it is the only argument that does. A search without it would have to crawl all %d job boards in the registry, which takes roughly 15 minutes — so it is refused immediately instead.

Every other argument filters postings after they are fetched. Adding "titles" or "remote" to a search makes the answer smaller, never faster, and omitting them does not make it slower.

WILL: answer in seconds for a "companies" term selecting a handful of boards. Terms match both the display name and the ATS slug, and match as substrings, so "anthropic" selects 1 board and "tech" selects 121.

WILL NOT: search all companies, search by title alone, or search "any remote Go job". Those need a name to start from. Call list_companies first if you do not have one.

REFUSES, with a message saying what to change, when: "companies" is missing or blank; the terms match no known board; or the terms match more than %d boards.

Results are capped at %d postings by default and %d at most, sorted by company then title, and report whether the crawl finished. A crawl that hit its %s budget returns what it collected with complete=false — treat that as a partial answer, not as evidence that nothing else exists.`,
			len(s.Catalog.Sources()), limits.MaxSources, limits.DefaultLimit, limits.MaxLimit, limits.Timeout),
		Annotations: annotations{
			Title:           "Search job postings",
			ReadOnlyHint:    true,
			DestructiveHint: ptr(false),
			// Not idempotent: job boards change under it, and two identical
			// calls minutes apart legitimately differ.
			OpenWorldHint: ptr(true),
		},
		InputSchema: object(map[string]*schema{
			"companies": stringList(
				`REQUIRED. Company names or ATS slugs to search. Case-insensitive substring match against both the display name and the tenant key, so "anthropic" and "pfizer.wd1.myworkdayjobs.com" both work. Multiple entries are OR-ed. Keep terms specific: short or generic terms ("a", "data", "group") match many boards and may be refused as too broad.`),
			"titles": stringList(
				`Match postings whose title contains any of these terms (case-insensitive substring, OR-ed).`),
			"exclude_titles": stringList(
				`Reject postings whose title contains any of these terms. Applied after "titles", so it can carve exceptions out of a match.`),
			"locations": stringList(
				`Match postings whose location contains any of these terms (OR-ed). Free text as the board wrote it: "London", "CA", "United States".`),
			"departments": stringList(
				`Match postings whose department or team contains any of these terms. Both fields are searched, because platforms disagree about which one holds the word you would type.`),
			"remote": boolArg(
				`Match only postings that appear to be remote. Uses the board's own remote flag where it published one, and otherwise reads the location and title text.`),
			"has_compensation": boolArg(
				`Match only postings that published a pay range. Most postings on most boards publish none, so this excludes the majority.`),
			"min_annual": {
				Type:        "number",
				Description: `Match only postings whose published pay reaches this annual figure. Hourly and monthly ranges are annualized; the top of a range is compared. Implies has_compensation, so postings that disclosed nothing are excluded — a pay floor cannot be applied to an unknown.`,
				Minimum:     ptr(0.0),
			},
			"employment_types": enumList(
				`Match only postings with any of these normalized employment types. Postings whose board published no employment type are excluded, which on most boards is most of them.`,
				employmentTypeNames()),
			"workplace_types": enumList(
				`Match only postings with any of these normalized workplace types. Where a board published no structured value, "remote" and "hybrid" fall back to reading the location and title text; "onsite" does not, because the absence of the word "remote" is not evidence that an employer requires an office.`,
				workplaceTypeNames()),
			"posted_since": {
				Type:        "string",
				Description: `Match only postings published at or after this instant. Accepts "2026-01-31" or RFC 3339. Postings whose board published no date are excluded — most boards publish none, and treating those as recent would quietly fill a "last week" query with postings of unknown age.`,
			},
			"posted_within_days": intArg(
				`Match only postings published within this many days of now. Convenience for posted_since; setting both is an error.`, 1, 3650),
			"limit": intArg(
				fmt.Sprintf(`Maximum postings to return (default %d, maximum %d). The crawl still fetches every selected board; this bounds the reply, not the work.`,
					limits.DefaultLimit, limits.MaxLimit),
				1, float64(limits.MaxLimit)),
		}, "companies"),
	}
}

func (s *Server) listCompaniesTool() tool {
	return tool{
		Name:  "list_companies",
		Title: "List companies whose job boards are covered",
		Description: fmt.Sprintf(`List the companies whose job boards this server can search, and which ATS platform each is on.

Reads a registry compiled into the binary. Makes no network requests and returns immediately.

Use this to turn a vague ask into a "companies" argument for search_jobs. "Find me an AI safety job" is not searchable; "search anthropic" is.

Coverage is %d job boards. Note that this is a fixed registry, not the whole job market: a company absent here is one nobody has added, not one that is not hiring.`,
			len(s.Catalog.Sources())),
		Annotations: annotations{
			Title:           "List covered companies",
			ReadOnlyHint:    true,
			IdempotentHint:  true,
			DestructiveHint: ptr(false),
			OpenWorldHint:   ptr(false),
		},
		InputSchema: object(map[string]*schema{
			"contains": stringList(
				`Return only companies whose name or ATS slug contains any of these terms (case-insensitive, OR-ed). Omit to list from the beginning of the alphabet.`),
			"platforms": enumList(
				`Return only companies on these ATS platforms. Call list_platforms for the available names.`, nil),
			"limit": intArg(
				fmt.Sprintf(`Maximum companies to return (default %d, maximum %d).`, defaultCompanyLimit, maxCompanyLimit),
				1, maxCompanyLimit),
			"offset": intArg(
				`Skip this many companies before returning any, for paging through the list.`, 0, 1_000_000),
		}),
	}
}

func (s *Server) listPlatformsTool() tool {
	return tool{
		Name:  "list_platforms",
		Title: "List covered ATS platforms",
		Description: `List the applicant tracking system platforms this server covers, with the number of job boards and distinct companies registered on each.

Reads a registry compiled into the binary. Makes no network requests, takes no arguments, and returns immediately.

Useful for reporting coverage, and for the "platforms" argument of list_companies. Not useful for finding a job: postings are not searchable by platform, because which ATS a company bought says nothing about the work.`,
		Annotations: annotations{
			Title:           "List ATS platforms",
			ReadOnlyHint:    true,
			IdempotentHint:  true,
			DestructiveHint: ptr(false),
			OpenWorldHint:   ptr(false),
		},
		InputSchema: object(map[string]*schema{}),
	}
}

func (s *Server) lookupEmployerTool() tool {
	return tool{
		Name:  "lookup_employer",
		Title: "Look up what is known about an employer",
		Description: fmt.Sprintf(`Look up reviewed facts about the employers behind covered job boards: legal name, SEC CIK, ticker and exchange, industry and SIC code, headcount, founding year, headquarters, and parent company.

Reads a table compiled into the binary. Makes no network requests and returns immediately. It does not fetch postings — pair it with search_jobs when you want both.

Every row was matched offline and carries the method, confidence and retrieval date that produced it, so you can tell a row joined by SEC identifier from one joined because two names happened to be equal.

A company with no row is a company nobody has resolved yet. That is the default answer and it is not an error: the table holds %d rows against %d job boards. Absence means unknown, never "private" or "does not exist".

Headcount and industry are snapshots that age. Read retrieved_at before quoting a number.`,
			s.employerCount(), len(s.Catalog.Sources())),
		Annotations: annotations{
			Title:           "Look up employer facts",
			ReadOnlyHint:    true,
			IdempotentHint:  true,
			DestructiveHint: ptr(false),
			OpenWorldHint:   ptr(false),
		},
		InputSchema: object(map[string]*schema{
			"companies": stringList(
				`REQUIRED. Company names or ATS slugs to look up, matched the same way as in search_jobs.`),
			"limit": intArg(
				fmt.Sprintf(`Maximum results to return (default %d, maximum %d).`, defaultEmployerLimit, maxEmployerLimit),
				1, maxEmployerLimit),
		}, "companies"),
	}
}

// employerCount reports how many reviewed rows are loaded, or zero when no table
// is configured.
func (s *Server) employerCount() int {
	if s.Employers == nil {
		return 0
	}

	return s.Employers.Len()
}
