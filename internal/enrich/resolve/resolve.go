// Package resolve ties a crawled job board to an external legal entity.
//
// This is the hard part of enrichment and the part that fails quietly when it is
// done badly. The crawler's identity for a company is whatever its ATS uses:
// "andurilindustries", "paloaltonetworks2", "2u", a Workday tenant URL, a Phenom
// hostname. SEC EDGAR knows "ANDURIL INDUSTRIES, INC." and a ten-digit CIK. DOL
// disclosure files know an all-caps employer string typed by a paralegal.
// Nothing joins those on its own.
//
// The rule this package is built around: a match that cannot be shown to be
// unique is not a match. Attributing one company's headcount, industry or wage
// distribution to another produces a number that looks exactly like a correct
// one, and at 473,404 postings nobody will ever spot it. So the output is split
// in two — matches confident enough to commit, and candidates for a human to
// read — and everything ambiguous goes in the second pile. Leaving a company
// unmatched costs a user one absent JSON key. Matching it wrongly costs them a
// decision.
//
// Nothing here runs at query time. It runs in the generator, its output is
// reviewed in a pull request, and the CLI only ever reads the reviewed result.
package resolve

import (
	"slices"
	"strings"
	"unicode"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/enrich"
)

// Source is the crawler's view of one job board: the platform, the tenant key
// the adapter fetches with, and the display name derived from it.
//
// It is a copy of the three fields of services.Source that matter here rather
// than the type itself, so that resolution can be tested without building the
// adapter registry, and so that a change to how a source fetches cannot change
// how it resolves.
type Source struct {
	Platform string
	Key      string
	Company  string
}

// Entity is an external record a source might be describing: a SEC filer, in
// the only implementation today.
type Entity struct {
	// ID is the identifier in the upstream dataset. For EDGAR it is the CIK,
	// zero-padded to ten digits.
	ID string

	// Name is the entity's name as the upstream dataset spells it.
	Name string

	// Ticker is carried through to the table when present.
	Ticker string
}

// Match is a resolution confident enough to commit to the table.
type Match struct {
	Source     Source
	Entity     Entity
	Method     enrich.Method
	Confidence enrich.Confidence
}

// Candidate is a resolution that was considered and not committed. It exists to
// be read by a person.
type Candidate struct {
	Source     Source
	Entity     Entity
	Method     enrich.Method
	Confidence enrich.Confidence

	// Why says, in one line, what stopped this from being accepted. It is the
	// only part of the candidates file a reviewer actually reads, so it names
	// the competing interpretation rather than restating the rule.
	Why string
}

// Result is what one resolution pass produced.
type Result struct {
	// Matches are unique in both directions and safe to write to the table.
	Matches []Match

	// Candidates are everything else that was considered, ordered by source.
	Candidates []Candidate
}

// legalSuffixes are the corporate-form words that carry no identifying
// information.
//
// They are stripped so that "Cloudflare, Inc." and a board slug of "cloudflare"
// reduce to the same string. The list is conservative on purpose: every entry
// removed here is a chance for two genuinely different entities to collapse into
// one, so words that can be part of a real name ("technologies", "systems",
// "labs", "partners") are deliberately absent even though they are common.
var legalSuffixes = map[string]bool{
	"inc": true, "incorporated": true,
	"corp": true, "corporation": true,
	"co": true, "company": true,
	"llc": true, "lc": true, "llp": true, "lp": true,
	"ltd": true, "limited": true,
	"plc": true, "sa": true, "nv": true, "bv": true, "ag": true, "as": true,
	"ab": true, "oyj": true, "oy": true, "aps": true, "gmbh": true, "srl": true,
	"spa": true, "pte": true, "pty": true, "kk": true, "kg": true, "sas": true,
	"holdings": true, "holding": true,
	"group": true, "the": true,
}

// NormalizeName reduces an entity or company name to its identifying words.
//
// Lowercased, ampersands spelled out, punctuation turned into separators, legal
// forms dropped, whitespace collapsed. "Palo Alto Networks, Inc." and "PALO ALTO
// NETWORKS INC" both become "palo alto networks".
//
// Ampersands become " and " rather than being deleted so that "AT&T" and "AT T"
// do not both collapse onto "att", which would merge two different strings on a
// coincidence of punctuation.
func NormalizeName(raw string) string {
	lowered := strings.ToLower(raw)
	lowered = strings.ReplaceAll(lowered, "&", " and ")

	var b strings.Builder

	b.Grow(len(lowered))

	for _, r := range lowered {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		default:
			b.WriteByte(' ')
		}
	}

	words := strings.Fields(b.String())
	kept := make([]string, 0, len(words))

	for _, word := range words {
		if legalSuffixes[word] {
			continue
		}

		kept = append(kept, word)
	}

	// A name made entirely of legal forms ("The Company") would normalize to
	// nothing and then match every other such name. Keep the original words
	// instead; an odd key is better than an empty one that collides.
	if len(kept) == 0 {
		return strings.Join(words, " ")
	}

	return strings.Join(kept, " ")
}

// Squash is [NormalizeName] with the separators removed, which is the form an
// ATS slug arrives in: "cloudflare", "paloaltonetworks", "1password".
func Squash(raw string) string {
	return strings.ReplaceAll(NormalizeName(raw), " ", "")
}

// trimTenantDigits removes the digits an ATS appends when a slug is already
// taken: "paloaltonetworks2", "stripe3".
//
// Only ever applied to the tenant key, and only ever to produce a MEDIUM
// confidence candidate, never a committed match. The transformation is not
// information-preserving — "3m" and "2u" are companies whose names are mostly
// digits — so it is a hint for a reviewer rather than evidence. Leading digits
// are never touched and a result shorter than three characters is discarded.
func trimTenantDigits(squashed string) string {
	trimmed := strings.TrimRightFunc(squashed, unicode.IsDigit)

	if len(trimmed) < 3 || trimmed == squashed {
		return ""
	}

	return trimmed
}

// proposal is one source's reading of one entity, before uniqueness is checked.
type proposal struct {
	entity     int
	method     enrich.Method
	confidence enrich.Confidence
}

// Sources resolves crawled sources against external entities.
//
// The acceptance rule has two halves, and both are needed:
//
//  1. The source must propose exactly one entity. A slug that normalizes to a
//     name three SEC filers share is not evidence about any of them.
//  2. Every other source proposing that entity must be the same company. Two
//     sources legitimately point at one employer — a company on Greenhouse and
//     on Workday is still one company — so sharing an entity is only a conflict
//     when the display names disagree. That is the roadmap's "source, company
//     and ATS identity are separate concepts" applied to matching.
//
// Everything else becomes a candidate carrying the reason, so the reviewer sees
// the ambiguity rather than the guess.
func Sources(sources []Source, entities []Entity) Result {
	byName := make(map[string][]int, len(entities))
	bySquash := make(map[string][]int, len(entities))

	for i, entity := range entities {
		name := NormalizeName(entity.Name)
		if name == "" {
			continue
		}

		byName[name] = append(byName[name], i)

		if squashed := Squash(entity.Name); squashed != "" {
			bySquash[squashed] = append(bySquash[squashed], i)
		}
	}

	proposals := make([][]proposal, len(sources))
	for i, source := range sources {
		proposals[i] = proposalsFor(source, byName, bySquash)
	}

	// Which sources propose each entity, so the second half of the rule can be
	// checked without rescanning.
	claimants := make(map[int][]int, len(sources))
	for i, list := range proposals {
		for _, p := range list {
			claimants[p.entity] = append(claimants[p.entity], i)
		}
	}

	var result Result

	for i, source := range sources {
		list := proposals[i]

		switch {
		case len(list) == 0:
			// The overwhelmingly common outcome, and not worth a candidate row:
			// most companies this project crawls are private and EDGAR has never
			// heard of them.
			continue

		case len(list) > 1:
			for _, p := range list {
				result.Candidates = append(result.Candidates, Candidate{
					Source:     source,
					Entity:     entities[p.entity],
					Method:     p.method,
					Confidence: enrich.ConfidenceMedium,
					Why:        "ambiguous: this source also matches " + otherNames(entities, list, p.entity),
				})
			}

		default:
			p := list[0]

			if rival, ok := conflictingClaimant(sources, claimants[p.entity], i); ok {
				result.Candidates = append(result.Candidates, Candidate{
					Source:     source,
					Entity:     entities[p.entity],
					Method:     p.method,
					Confidence: enrich.ConfidenceMedium,
					Why:        "contested: " + rival + " also matches this entity under a different name",
				})

				continue
			}

			if p.confidence != enrich.ConfidenceHigh {
				result.Candidates = append(result.Candidates, Candidate{
					Source:     source,
					Entity:     entities[p.entity],
					Method:     p.method,
					Confidence: p.confidence,
					Why:        "matched only after trimming trailing digits from the tenant key; confirm by hand",
				})

				continue
			}

			result.Matches = append(result.Matches, Match{
				Source:     source,
				Entity:     entities[p.entity],
				Method:     p.method,
				Confidence: enrich.ConfidenceHigh,
			})
		}
	}

	return result
}

// proposalsFor collects one source's readings, strongest first, at most one per
// entity.
func proposalsFor(source Source, byName, bySquash map[string][]int) []proposal {
	var (
		list = make([]proposal, 0, 2)
		seen = make(map[int]bool, 2)
	)

	add := func(entities []int, method enrich.Method, confidence enrich.Confidence) {
		for _, entity := range entities {
			if seen[entity] {
				continue
			}

			seen[entity] = true

			list = append(list, proposal{entity: entity, method: method, confidence: confidence})
		}
	}

	// The display name first: it is the string a person recognises, and the one
	// most likely to be spelled the way the entity spells it.
	if name := NormalizeName(source.Company); name != "" {
		add(byName[name], enrich.MethodEDGARExactName, enrich.ConfidenceHigh)
	}

	// Then the tenant key, which catches the boards whose slug is the legal name
	// run together and whose display name is a URL.
	if squashed := Squash(source.Key); squashed != "" {
		add(bySquash[squashed], enrich.MethodEDGARExactKey, enrich.ConfidenceHigh)

		if trimmed := trimTenantDigits(squashed); trimmed != "" {
			add(bySquash[trimmed], enrich.MethodEDGARExactKey, enrich.ConfidenceMedium)
		}
	}

	return list
}

// conflictingClaimant reports another source that matched the same entity under
// a different company name, which is what turns a match into a question.
func conflictingClaimant(sources []Source, claimants []int, self int) (string, bool) {
	mine := NormalizeName(sources[self].Company)

	for _, other := range claimants {
		if other == self {
			continue
		}

		if NormalizeName(sources[other].Company) != mine {
			return sources[other].Platform + "/" + sources[other].Key, true
		}
	}

	return "", false
}

// otherNames lists the competing entity names for a candidate's Why line.
func otherNames(entities []Entity, list []proposal, exclude int) string {
	names := make([]string, 0, len(list))

	for _, p := range list {
		if p.entity == exclude {
			continue
		}

		names = append(names, entities[p.entity].Name)
	}

	slices.Sort(names)

	return strings.Join(names, "; ")
}
