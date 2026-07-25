package internal

import (
	"html"
	"regexp"
	"strconv"
	"strings"
)

// Provenance records where a piece of posting data came from, because data an
// employer published in a structured field and data inferred from prose do not
// deserve equal trust.
type Provenance string

// Provenance values, in descending order of trustworthiness.
//
// The distinction exists so a consumer can decide how much to trust a figure
// rather than being handed one number with no idea how it was obtained. A wrong
// salary is indistinguishable from a right one at a glance, so the only defence
// is knowing where it came from.
const (
	// ProvenanceEmployer means the platform published the value in a dedicated
	// API field. Authoritative.
	ProvenanceEmployer Provenance = "employer"

	// ProvenanceStructured means the value came from markup the board renders
	// from a real pay field, a container that declares its contents to be the
	// pay range. Not an API field, but not a guess either.
	ProvenanceStructured Provenance = "structured"

	// ProvenanceDescription means the value was read out of description prose.
	// Best-effort, and the only source that can plausibly be wrong about what a
	// number means.
	ProvenanceDescription Provenance = "description"
)

// trustRank orders provenances so the best available source can be chosen.
var trustRank = map[Provenance]int{
	ProvenanceEmployer:    3,
	ProvenanceStructured:  2,
	ProvenanceDescription: 1,
}

// MoreTrustedThan reports whether this compensation came from a better source
// than other. A nil or empty-provenance value is the least trusted.
func (c *Compensation) MoreTrustedThan(other *Compensation) bool {
	if c == nil {
		return false
	}

	if other == nil {
		return true
	}

	return trustRank[c.Provenance] > trustRank[other.Provenance]
}

// Plausibility bounds for an extracted pay figure, once annualized.
//
// These exist to reject the many large numbers in a job description that are not
// salaries: funding rounds, revenue, customer counts, addressable market. A
// figure outside this range is far more likely to be one of those than a wage.
const (
	minPlausibleAnnual = 12_000
	maxPlausibleAnnual = 3_000_000
)

// maxRangeRatio rejects "ranges" whose ends are implausibly far apart, which is
// the signature of two unrelated numbers being paired by accident.
const maxRangeRatio = 8

// payCueWindow is how many characters before a money figure are searched for a
// phrase indicating the figure is pay. Long enough to span a sentence opening,
// short enough that a cue from an unrelated sentence does not leak in.
const payCueWindow = 140

// payCues are phrases that mark a nearby figure as compensation. A money figure
// with none of these near it is not treated as pay at all: requiring a cue is
// what separates a salary from every other dollar amount in a job description.
var payCues = []string{
	"salary",
	"base pay",
	"base compensation",
	"pay range",
	"pay rate",
	"payrange",
	"compensation range",
	"compensation for this",
	"total compensation",
	"annual compensation",
	"hourly rate",
	"hourly pay",
	"per hour",
	"an hour",
	"/hour",
	"/hr",
	"expected pay",
	"target pay",
	"pay for this",
	"pay scale",
	"wage",
	"earn",
	"paid",
	"pays",
	"position pays",
	"role pays",
	"starting at",
	"budgeted",
	"pay transparency",
}

// payDisqualifiers are phrases that mean a nearby figure is money but not the
// wage for this job. Each of these was chosen because it realistically appears
// next to a dollar amount in a job description.
var payDisqualifiers = []string{
	"401",
	"retirement",
	"tuition",
	"reimburs",
	"stipend",
	"signing bonus",
	"sign-on",
	"referral bonus",
	"equity",
	"stock option",
	"raised",
	"funding",
	"valuation",
	"revenue",
	"arr",
	"aum",
	"deductible",
	"premium",
	"copay",
	"discount",
	"donat",
	"grant",
	"scholarship",
	"budget of",
	"portfolio",
	"savings",
	"credit",
	"fine",
	"penalt",
}

// moneyRangePattern matches a money range such as "$150,000 - $200,000",
// "$150K–$200K", or "$22.50 to $28.00 per hour". Both ends must carry a currency
// marker or magnitude suffix, which keeps it from pairing arbitrary integers.
var moneyRangePattern = regexp.MustCompile(
	`(?i)(?:\$|usd\s*)([\d,]+(?:\.\d+)?)\s*([km])?\s*(?:-|–|—|to|through|up to)\s*(?:\$|usd\s*)?([\d,]+(?:\.\d+)?)\s*([km])?`,
)

// moneySinglePattern matches a lone money figure, used when no range is present.
var moneySinglePattern = regexp.MustCompile(
	`(?i)(?:\$|usd\s*)([\d,]+(?:\.\d+)?)\s*([km])?`,
)

// hourlyMarkers indicate a figure is an hourly rate rather than an annual one.
var hourlyMarkers = []string{"per hour", "an hour", "/hour", "/hr", "hourly", "per hr"}

// annualMarkers indicate a figure is an annual salary. Recording the period the
// posting actually stated is more precise than leaving it to be inferred from the
// size of the number, even when both reach the same conclusion.
var annualMarkers = []string{"/yr", "/year", "per year", "per annum", "annually", "a year", "annual salary"}

// tagPattern strips HTML tags, since most boards publish descriptions as HTML.
var tagPattern = regexp.MustCompile(`<[^>]*>`)

// whitespacePattern collapses runs of whitespace.
var whitespacePattern = regexp.MustCompile(`\s+`)

// ParseCompensationFromText attempts to read a pay range out of job description
// prose, returning nil when it cannot do so confidently.
//
// This is deliberately conservative. Most platforms publish no structured pay
// field, so prose is the only signal available for them; but a job description
// is full of dollar amounts that are not wages: 401(k) match, tuition
// reimbursement, signing bonuses, funding raised, revenue, insurance deductibles.
// Reporting one of those as a salary is far worse than reporting nothing, because
// it silently corrupts every pay filter and comparison downstream.
//
// So a figure is only accepted when all of these hold:
//
//   - a compensation cue appears shortly before it;
//   - no disqualifying phrase appears nearer than that cue;
//   - the annualized value is within plausible wage bounds;
//   - for a range, the two ends are within a sane ratio of each other.
//
// The result is always marked [ProvenanceDescription] so callers can tell it
// apart from an employer-published figure, and should never be blended with one.
func ParseCompensationFromText(text string) *Compensation {
	normalized := normalizeText(text)
	if normalized == "" {
		return nil
	}

	lowered := strings.ToLower(normalized)

	// Ranges are tried first: an explicit range is much stronger evidence than a
	// lone figure.
	ranges := moneyRangePattern.FindAllStringSubmatchIndex(normalized, -1)

	for _, match := range ranges {
		if !isPayContext(lowered, match[0]) {
			continue
		}

		low := parseMoney(normalized[match[2]:match[3]], group(normalized, match, 4))
		high := parseMoney(normalized[match[6]:match[7]], group(normalized, match, 8))

		comp := buildCompensation(low, high, lowered, match[1])
		if comp != nil {
			return comp
		}
	}

	for _, match := range moneySinglePattern.FindAllStringSubmatchIndex(normalized, -1) {
		// A figure inside a range that was already rejected must not be salvaged
		// as a lone figure: in "$30,000 - $9,000,000" the range was thrown out as
		// two unrelated numbers, so neither end is a credible salary on its own.
		if withinAny(ranges, match[0]) {
			continue
		}

		if !isPayContext(lowered, match[0]) {
			continue
		}

		value := parseMoney(normalized[match[2]:match[3]], group(normalized, match, 4))

		comp := buildCompensation(value, 0, lowered, match[1])
		if comp != nil {
			return comp
		}
	}

	return nil
}

// withinAny reports whether position falls inside any of the given match spans.
func withinAny(spans [][]int, position int) bool {
	for _, span := range spans {
		if position >= span[0] && position < span[1] {
			return true
		}
	}

	return false
}

// buildCompensation validates a candidate pair of figures and turns them into a
// Compensation, or returns nil if they do not look like pay.
func buildCompensation(low, high float64, lowered string, end int) *Compensation {
	if high > 0 && low > high {
		low, high = high, low
	}

	// A range whose ends are wildly apart is two unrelated numbers, not a range.
	if low > 0 && high > 0 && high/low > maxRangeRatio {
		return nil
	}

	comp := &Compensation{
		Min:        low,
		Max:        high,
		Currency:   "USD",
		Period:     periodFromContext(lowered, end),
		Provenance: ProvenanceDescription,
	}

	annual, ok := comp.AnnualMax()
	if !ok || annual < minPlausibleAnnual || annual > maxPlausibleAnnual {
		return nil
	}

	if bottom, ok := comp.AnnualMin(); ok && bottom < minPlausibleAnnual {
		return nil
	}

	return comp
}

// isPayContext reports whether the text shortly before position start marks the
// following figure as compensation.
func isPayContext(lowered string, start int) bool {
	from := max(0, start-payCueWindow)

	window := lowered[from:min(start+8, len(lowered))]

	cueAt := -1

	for _, cue := range payCues {
		if at := strings.LastIndex(window, cue); at > cueAt {
			cueAt = at
		}
	}

	if cueAt < 0 {
		return false
	}

	// A disqualifier closer to the figure than the cue wins: in
	// "salary is competitive; 401(k) match up to $5,000" the $5,000 belongs to
	// the 401(k), not the salary.
	for _, bad := range payDisqualifiers {
		if at := strings.LastIndex(window, bad); at > cueAt {
			return false
		}
	}

	return true
}

// periodFromContext infers the pay period from wording near the figure.
func periodFromContext(lowered string, end int) Period {
	window := lowered[max(0, end-60):min(end+40, len(lowered))]

	for _, marker := range hourlyMarkers {
		if strings.Contains(window, marker) {
			return PeriodHour
		}
	}

	if strings.Contains(window, "per week") || strings.Contains(window, "weekly") {
		return PeriodWeek
	}

	if strings.Contains(window, "per month") || strings.Contains(window, "monthly") {
		return PeriodMonth
	}

	for _, marker := range annualMarkers {
		if strings.Contains(window, marker) {
			return PeriodYear
		}
	}

	// Left unknown rather than assumed annual, so the magnitude heuristic in
	// effectivePeriod decides; it handles an unlabelled "$24.00" correctly.
	return PeriodUnknown
}

// parseMoney converts a matched amount and optional magnitude suffix to a number.
func parseMoney(amount, suffix string) float64 {
	value, err := strconv.ParseFloat(strings.ReplaceAll(amount, ",", ""), 64)
	if err != nil {
		return 0
	}

	switch strings.ToLower(suffix) {
	case "k":
		value *= 1_000
	case "m":
		value *= 1_000_000
	}

	return value
}

// group returns submatch n from a FindAllStringSubmatchIndex entry, or "".
func group(s string, match []int, n int) string {
	if len(match) <= n+1 || match[n] < 0 {
		return ""
	}

	return s[match[n]:match[n+1]]
}

// normalizeText decodes HTML entities, strips markup, and collapses whitespace.
//
// Entities are decoded *before* tags are stripped, and again afterwards. Some
// boards publish descriptions whose markup is itself entity-encoded, Databricks
// sends `&lt;span&gt;$145,700&lt;/span&gt;&mdash;&lt;span&gt;$200,300&lt;/span&gt;`
// ; so a stripper that only understands literal angle brackets leaves a wall of
// escaped markup between the two halves of the range. That made a real range read
// as a lone lower bound.
func normalizeText(text string) string {
	if text == "" {
		return ""
	}

	// First pass: turn encoded markup back into markup so it can be stripped.
	decoded := html.UnescapeString(text)

	stripped := tagPattern.ReplaceAllString(decoded, " ")

	// Second pass: content may have been doubly encoded, and this also resolves
	// the entities that carry meaning inside the text, such as an em dash between
	// the ends of a range.
	stripped = tagPattern.ReplaceAllString(html.UnescapeString(stripped), " ")

	// Unicode dashes are normalized so the range pattern needs only one form.
	stripped = strings.NewReplacer("–", "-", "—", "-", "−", "-", " ", " ").Replace(stripped)

	return strings.TrimSpace(whitespacePattern.ReplaceAllString(stripped, " "))
}
