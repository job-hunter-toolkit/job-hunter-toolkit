package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/enrich"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/services"
	"github.com/spf13/cobra"
)

// newCompanyCommand builds the `company` command.
//
// It is the cheapest possible surface for the enrichment table: no crawl, no
// requests, no deadline. `companies` already lists which boards this binary can
// reach; this answers what is known about the employer behind one of them, in
// milliseconds, from data compiled into the binary.
//
// Sources are selected with services.SourcesMatching, the same function
// `postings --company` uses, so `company pfizer` and
// `company pfizer.wd1.myworkdayjobs.com` select the same thing here as they do
// there. A lookup command that resolved names its own way would answer a
// different question than the one the user is about to ask the crawler.
func newCompanyCommand() *cobra.Command {
	var (
		asJSON      bool
		showUnknown bool
	)

	cmd := &cobra.Command{
		Use:     "company [terms...]",
		Aliases: []string{"employer", "employers"},
		Short:   "Show what is known about the companies behind the job boards",
		Long: "Show what is known about the companies behind the job boards.\n\n" +
			"Reads a reviewed table compiled into this binary: legal name, SEC CIK,\n" +
			"ticker, SIC industry, headcount, founding year, headquarters and parent.\n" +
			"It makes no requests and needs no network.\n\n" +
			"Coverage is deliberately partial and is reported on stderr. Most companies\n" +
			"here are private startups that SEC EDGAR has never heard of, so \"nothing\n" +
			"known\" is the ordinary answer rather than a failure. Nothing is guessed: a\n" +
			"company is only in the table if a human reviewed the match that put it\n" +
			"there.",
		Example: "  # What is known about one company\n" +
			"  job-hunter-toolkit company cloudflare\n\n" +
			"  # Every company with a reviewed record, as JSON\n" +
			"  job-hunter-toolkit company --json\n\n" +
			"  # Which of these boards have no record yet\n" +
			"  job-hunter-toolkit company --unknown stripe cloudflare",
		RunE: func(cmd *cobra.Command, args []string) error {
			table, err := enrich.Default()
			if err != nil {
				return err
			}

			sources := services.SourcesMatching(args)
			if len(sources) == 0 {
				return fmt.Errorf("no known companies match %s", strings.Join(args, ", "))
			}

			identities := make([]internal.PostingSource, 0, len(sources))
			for _, source := range sources {
				identities = append(identities, internal.PostingSource{
					Platform: source.Platform,
					Key:      source.Key,
				})
			}

			var (
				found   []*enrich.Employer
				unknown []services.Source
			)

			for i, identity := range identities {
				if employer, ok := table.For(identity); ok {
					found = append(found, employer)

					continue
				}

				unknown = append(unknown, sources[i])
			}

			out := cmd.OutOrStdout()

			switch {
			case showUnknown:
				if err := writeUnknown(out, unknown, asJSON); err != nil {
					return err
				}
			case asJSON:
				if err := writeEmployersJSON(out, found); err != nil {
					return err
				}
			default:
				if err := writeEmployersText(out, found); err != nil {
					return err
				}
			}

			// Coverage is a diagnostic, not data, so it goes to stderr where it
			// cannot corrupt a JSON stream being piped into jq. It is always
			// printed because the interesting case is the empty one: without it,
			// "no output" is indistinguishable between "this company is private"
			// and "the table is empty".
			matched, total := table.Coverage(identities)

			fmt.Fprintf(cmd.ErrOrStderr(), "%d of %d matching sources have a reviewed record (%d in the table overall)\n",
				matched, total, table.Len())

			return nil
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "output newline-delimited JSON")
	cmd.Flags().BoolVar(&showUnknown, "unknown", false,
		"list the matching sources that have no record instead of the ones that do")

	return cmd
}

// writeEmployersText prints one record per company in a readable block.
func writeEmployersText(w io.Writer, employers []*enrich.Employer) error {
	for i, employer := range employers {
		if i > 0 {
			if _, err := fmt.Fprintln(w); err != nil {
				return err
			}
		}

		for _, field := range employerFields(employer) {
			if field.value == "" {
				continue
			}

			if _, err := fmt.Fprintf(w, "%-14s %s\n", field.name+":", field.value); err != nil {
				return err
			}
		}
	}

	return nil
}

// employerField is one labelled line of the text output.
type employerField struct {
	name  string
	value string
}

// employerFields renders an employer for humans, in a fixed order, omitting
// whatever is unknown.
//
// The match line is never omitted. A reader deciding whether to trust a
// headcount needs to know it came from a normalized-name match rather than from
// an identifier, and burying that behind --json would make the untrustworthy
// case the quiet one.
func employerFields(employer *enrich.Employer) []employerField {
	fields := []employerField{
		{"company", employer.Company},
		{"source", employer.Source.Platform + "/" + employer.Source.Key},
		{"legal name", employer.LegalName},
		{"industry", strings.TrimSpace(employer.SIC + " " + employer.Industry)},
		{"public", publicText(employer.Public)},
		{"ticker", strings.TrimSpace(employer.Ticker + " " + employer.Exchange)},
		{"cik", employer.CIK},
		{"employees", positiveText(employer.Employees)},
		{"founded", positiveText(employer.Founded)},
		{"headquarters", employer.Headquarters},
		{"parent", employer.Parent},
		{"wikidata", employer.WikidataID},
		{"match", matchText(employer.Match)},
	}

	for _, benchmark := range employer.WageBenchmarks {
		fields = append(fields, employerField{
			name: "benchmark",
			value: fmt.Sprintf("%s %s in %s: p25 %.0f p50 %.0f p75 %.0f (n=%d, %s %s; NOT this employer's published pay)",
				benchmark.SOC, benchmark.Occupation, benchmark.Area,
				benchmark.P25, benchmark.P50, benchmark.P75,
				benchmark.N, benchmark.Source, benchmark.AsOf),
		})
	}

	return fields
}

// publicText renders the tri-state public flag, keeping "unknown" distinct from
// "no" the way the field itself does.
func publicText(public *bool) string {
	if public == nil {
		return ""
	}

	if *public {
		return "yes"
	}

	return "no"
}

// positiveText renders a count, leaving it out entirely when it was never
// established. Zero employees is not a fact anybody recorded.
func positiveText(value int) string {
	if value <= 0 {
		return ""
	}

	return strconv.Itoa(value)
}

// matchText renders the provenance line.
func matchText(match enrich.Match) string {
	parts := []string{string(match.Method), string(match.Confidence) + " confidence"}

	if len(match.DataSources) > 0 {
		parts = append(parts, "via "+strings.Join(match.DataSources, ", "))
	}

	if match.RetrievedAt != "" {
		parts = append(parts, "retrieved "+match.RetrievedAt)
	}

	return strings.Join(parts, ", ")
}

// writeEmployersJSON writes one record per line, matching the NDJSON shape
// `postings --json` already produces so both can be piped into the same jq.
func writeEmployersJSON(w io.Writer, employers []*enrich.Employer) error {
	enc := json.NewEncoder(w)

	for _, employer := range employers {
		if err := enc.Encode(employer); err != nil {
			return fmt.Errorf("writing JSON: %w", err)
		}
	}

	return nil
}

// writeUnknown lists the sources with no record.
//
// It is a first-class output rather than an afterthought because the absence of
// a record is the common case, and "which of my shortlist do I have no context
// for" is a question a job seeker actually asks.
func writeUnknown(w io.Writer, sources []services.Source, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(w)

		for _, source := range sources {
			if err := enc.Encode(internal.PostingSource{Platform: source.Platform, Key: source.Key}); err != nil {
				return fmt.Errorf("writing JSON: %w", err)
			}
		}

		return nil
	}

	for _, source := range sources {
		if _, err := fmt.Fprintf(w, "%s\t%s/%s\n", source.Company, source.Platform, source.Key); err != nil {
			return err
		}
	}

	return nil
}

// registerEnrichmentFlags adds the employer filters to a postings-style command
// and returns the decorator they configure.
//
// It lives here, rather than in newPostingsCommand, so that wiring enrichment
// into the crawl is two lines in a file several agents edit at once:
//
//	enrichJobs := registerEnrichmentFlags(cmd)   // beside flags.register(cmd)
//	...
//	if jobs, err = enrichJobs(jobs); err != nil { // after the posting filter
//		return err
//	}
//
// The returned decorator is the identity function when no employer filter was
// given, so the default `postings` run is byte-identical to what it produces
// today: same stream type, same JSON, same frozen eight CSV columns. Enrichment
// filters postings; it does not change their shape. Adding an `employer` object
// to the posting output needs a field on internal.JobPosting and a column in
// package main's writer, and both belong to whoever owns those files.
func registerEnrichmentFlags(cmd *cobra.Command) func(internal.Jobs) (internal.Jobs, error) {
	var filter enrich.Filter

	cmd.Flags().BoolVar(&filter.Known, "known-employer", false,
		"only postings from companies with a reviewed employer record")
	cmd.Flags().StringSliceVar(&filter.Industries, "industry", nil,
		"only postings from employers whose SIC code or industry contains any of these terms")
	cmd.Flags().BoolVar(&filter.Public, "public", false,
		"only postings from employers established to file with the SEC")
	cmd.Flags().BoolVar(&filter.Private, "private", false,
		"only postings from employers established not to be public; an employer with no record is unknown, not private, and is excluded")
	cmd.Flags().IntVar(&filter.MinEmployees, "min-employees", 0,
		"only postings from employers with at least this many employees; employers whose headcount is unknown are excluded")

	// Both together is a contradiction that would match nothing while looking
	// like a filter that worked.
	cmd.MarkFlagsMutuallyExclusive("public", "private")

	return func(jobs internal.Jobs) (internal.Jobs, error) {
		if filter.IsZero() {
			return jobs, nil
		}

		table, err := enrich.Default()
		if err != nil {
			return nil, err
		}

		// An empty table plus an employer filter means every posting is about
		// to be discarded. Returning zero postings would read as "nothing is
		// hiring"; this says what actually happened. It is the same reasoning
		// internal/filter.go applies to a blank --title.
		if table.Len() == 0 {
			return nil, fmt.Errorf("no employer records are compiled into this binary, so an employer filter would discard every posting; regenerate internal/enrich/data with `go run ./tools/enrichgen`")
		}

		return enrich.Flatten(filter.Apply(table.Attach(jobs))), nil
	}
}
