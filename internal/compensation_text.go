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

// currencyMarkerPattern matches the currency marker that must sit in front of a
// figure for it to be considered money at all.
//
// The marker is captured rather than discarded because "$" is not a currency:
// it is also CAD, AUD, NZD, SGD, HKD, MXN and BRL. Hardcoding USD for anything
// that matched "$" published "C$95,000 - C$120,000" as a USD range, which is a
// ~28% error in the number a job seeker is shown and is invisible at a glance.
//
// The prefixed forms are deliberately written with no space before the "$":
// allowing one turns the ordinary English "a $95,000 base salary" into an AUD
// figure. Real postings write "C$95,000" and "A$110,000" closed up.
const currencyMarkerPattern = `((?:\b(?:us|ca|au|nz|sg|hk|mx|c|a|s|r)\$)|\$|\b(?:usd|cad|aud|nzd|sgd|hkd|mxn|brl|eur|gbp)\s*)`

// moneyAmountPattern matches the digits of a figure, thousands separators and
// all.
const moneyAmountPattern = `([\d,]+(?:\.\d+)?)`

// magnitudePattern matches an optional magnitude suffix.
//
// The trailing \b is load-bearing. Without it the optional ([km]) group happily
// consumed the first letter of the *next* word and parseMoney multiplied by
// 1e3/1e6: "$12,000 monthly" became 12,000,000 and was then thrown out by the
// plausibility bound, so a valid range reported no pay at all; "$140,000 to
// $180,000 minimum." lost an entire explicit range the same way; and worst,
// "$45 knowing the market" fabricated a $45,000 salary that was never written.
// RE2 has no lookahead, so \b is the available anchor.
//
// "thousand"/"million" are matched explicitly because the old unanchored group
// handled "$1.2 million" correctly by accident (it grabbed the "m"), and the \b
// anchor would otherwise have silently turned that into $1.20.
const magnitudePattern = `(?:\s*(thousand|million|k|m))?\b`

// Submatch pair indexes into a FindAllStringSubmatchIndex entry for the money
// patterns. Named because adding the currency-marker groups shifted every other
// group along, and an off-by-two here reads an amount as a magnitude suffix
// without failing anything loudly.
const (
	lowMarkerGroup     = 2
	lowAmountGroup     = 4
	lowMagnitudeGroup  = 6
	highMarkerGroup    = 8
	highAmountGroup    = 10
	highMagnitudeGroup = 12
)

// moneyRangePattern matches a money range such as "$150,000 - $200,000",
// "$150K–$200K", "C$95,000 - C$120,000", or "$22.50 to $28.00 per hour".
//
// Both ends accept a currency marker. Making the second one only "$ or nothing"
// meant a currency-prefixed second operand such as "C$120,000" failed to match
// as part of the range; the text then fell through to moneySinglePattern and the
// upper bound was silently discarded, so a published range was reported as a
// lone floor.
var moneyRangePattern = regexp.MustCompile(
	`(?i)` + currencyMarkerPattern + moneyAmountPattern + magnitudePattern +
		`\s*(?:-|–|—|to|through|up to)\s*` +
		currencyMarkerPattern + `?` + moneyAmountPattern + magnitudePattern,
)

// moneySinglePattern matches a lone money figure, used when no range is present.
var moneySinglePattern = regexp.MustCompile(
	`(?i)` + currencyMarkerPattern + moneyAmountPattern + magnitudePattern,
)

// trailingCurrencyPattern matches an ISO code written immediately after a
// figure, which is how boards render "$145,700 — $200,300 USD" and
// "$95,000 - $120,000 CAD". It is the last currency signal available when
// neither end of the range carried a prefix.
var trailingCurrencyPattern = regexp.MustCompile(`^\s*([a-z]{3})\b`)

// defaultCurrency is what an unmarked "$" figure is recorded as.
//
// This is an assumption, not an observation, and docs/compensation.md says so:
// every board this toolkit crawls is a US-headquartered ATS serving mostly US
// postings, and the plausibility bounds below are USD-calibrated, so a bare "$"
// figure that survives them has already been judged against USD. An explicit
// marker anywhere in the match always overrides it.
const defaultCurrency = "USD"

// currencyCodes maps every marker the money patterns can capture to an ISO 4217
// code. Written lowercase because matching runs against the lowercased text.
var currencyCodes = map[string]string{
	"us$": "USD", "usd": "USD",
	"c$": "CAD", "ca$": "CAD", "cad": "CAD",
	"a$": "AUD", "au$": "AUD", "aud": "AUD",
	"nz$": "NZD", "nzd": "NZD",
	"s$": "SGD", "sg$": "SGD", "sgd": "SGD",
	"hk$": "HKD", "hkd": "HKD",
	"mx$": "MXN", "mxn": "MXN",
	"r$": "BRL", "brl": "BRL",
	"eur": "EUR", "gbp": "GBP",
}

// hourlyMarkers indicate a figure is an hourly rate rather than an annual one.
var hourlyMarkers = []string{"per hour", "an hour", "/hour", "/hr", "hourly", "per hr"}

// dailyMarkers indicate a day rate.
//
// These are checked before the hourly markers because without a day branch at
// all, "$200 per day" fell through to PeriodUnknown, the magnitude heuristic in
// effectivePeriod called anything at or under 250 hourly, and the posting was
// published at $416,000/yr instead of $52,000/yr — an 8x overstatement carrying
// the same provenance as a correct figure. Larger day rates escaped only by
// accident: $600/day cleared the hourly ceiling, was read as annual, and was
// then rejected as implausibly low, so those postings reported no pay at all.
var dailyMarkers = []string{"per day", "/day", "a day", "daily", "per diem", "day rate"}

// annualMarkers indicate a figure is an annual salary. Recording the period the
// posting actually stated is more precise than leaving it to be inferred from the
// size of the number, even when both reach the same conclusion.
var annualMarkers = []string{"/yr", "/year", "per year", "per annum", "annually", "a year", "annual salary"}

// payBound says which end of a range a lone figure represents.
type payBound int

// Pay bounds for an open-ended figure.
const (
	// boundFloor means the figure is the bottom of the range, the default.
	boundFloor payBound = iota

	// boundCeiling means the wording made the figure a maximum.
	boundCeiling
)

// boundWindow is how many characters before a lone figure are searched for an
// open-ended cue. Short, because "up to" three sentences earlier says nothing
// about this figure.
const boundWindow = 32

// ceilingCues mean the figure that follows is the top of the range.
//
// Without these, "Salary up to $200,000" was recorded as Min=200,000 with Max
// unset, which inverts the meaning of the sentence: the CSV writer emitted
// pay_min=200000 with an empty pay_max, and a stated ceiling was published as a
// floor to every consumer reading the JSON "min" field.
var ceilingCues = []string{
	"up to",
	"as much as",
	"maximum of",
	"max of",
	"no more than",
	"not to exceed",
}

// floorCues mean the figure that follows is the bottom of the range. This is
// already the default; the list exists so a floor cue sitting nearer the figure
// than a ceiling cue wins, rather than the first cue found deciding.
var floorCues = []string{
	"starting at",
	"starting from",
	"beginning at",
	"at least",
	"minimum of",
	"no less than",
	"from",
}

// trailingBoundWindow is how many characters after a lone figure are searched
// for a trailing bound word. Deliberately tight: it needs to reach "maximum" in
// "$95,000 maximum." and nothing further, so that "The salary is $50,000 and our
// maximum PTO is unlimited" does not flip the figure to a ceiling.
const trailingBoundWindow = 12

// trailingCeilingCues and trailingFloorCues are the same distinction written
// after the figure, which is how postings phrase "$95,000 maximum." A leading
// cue outranks these; they only decide when there is no leading cue at all.
var (
	trailingCeilingCues = []string{"maximum", "at most", "or less"}
	trailingFloorCues   = []string{"minimum", "or more", "and up"}
)

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
//   - for a range, the two ends are within a sane ratio of each other;
//   - for a range, the two ends do not name different currencies.
//
// What is accepted is then reported as literally as the wording allows: an
// explicit currency marker sets [Compensation.Currency] rather than USD being
// assumed, "up to $X" sets Max rather than Min, and a stated day rate is a day
// rate rather than an hourly one. Each of those was a real defect that published
// a confidently wrong number.
//
// The result is always marked [ProvenanceDescription] so callers can tell it
// apart from an employer-published figure, and should never be blended with one.
func ParseCompensationFromText(text string) *Compensation {
	// Everything below both matches against and slices this one string.
	//
	// The patterns carry (?i), so matching the lowercased form finds exactly the
	// same figures; but strings.ToLower is not length-preserving, and match
	// offsets taken from the original string used to be applied to the lowercased
	// one. U+0130 (İ, Turkish dotted capital I) shrinks from 2 bytes to 1 and
	// U+212A (KELVIN SIGN) from 3 to 1, so the cue and period windows drifted.
	// Measured: 61 İ before the pay line slid the period window off "per year"
	// and a $150,000-$200,000 annual range was published with no period at all;
	// at 70 the window slice panicked with "slice bounds out of range". Keeping
	// one coordinate space is the whole fix.
	lowered := strings.ToLower(normalizeText(text))
	if lowered == "" {
		return nil
	}

	// Ranges are tried first: an explicit range is much stronger evidence than a
	// lone figure.
	ranges := moneyRangePattern.FindAllStringSubmatchIndex(lowered, -1)

	for _, match := range ranges {
		if !isPayContext(lowered, match[0]) {
			continue
		}

		currency, agreed := currencyForMatch(lowered, match)
		if !agreed {
			continue
		}

		low := parseMoney(group(lowered, match, lowAmountGroup), group(lowered, match, lowMagnitudeGroup))
		high := parseMoney(group(lowered, match, highAmountGroup), group(lowered, match, highMagnitudeGroup))

		comp := buildCompensation(low, high, currency, lowered, match[1])
		if comp != nil {
			return comp
		}
	}

	for _, match := range moneySinglePattern.FindAllStringSubmatchIndex(lowered, -1) {
		// A figure inside a range that was already rejected must not be salvaged
		// as a lone figure: in "$30,000 - $9,000,000" the range was thrown out as
		// two unrelated numbers, so neither end is a credible salary on its own.
		// The same applies to a range whose two ends named different currencies.
		if withinAny(ranges, match[0]) {
			continue
		}

		if !isPayContext(lowered, match[0]) {
			continue
		}

		currency, agreed := currencyForMatch(lowered, match)
		if !agreed {
			continue
		}

		value := parseMoney(group(lowered, match, lowAmountGroup), group(lowered, match, lowMagnitudeGroup))

		low, high := openEndedPair(lowered, match[0], match[1], value)

		comp := buildCompensation(low, high, currency, lowered, match[1])
		if comp != nil {
			return comp
		}
	}

	return nil
}

// openEndedPair places a lone figure at the end of the range its wording says it
// belongs to: "up to $200,000" is a maximum, "starting at $135,000" a minimum.
func openEndedPair(lowered string, start, end int, value float64) (low, high float64) {
	if boundFromContext(lowered, start, end) == boundCeiling {
		return 0, value
	}

	return value, 0
}

// boundFromContext reads the wording around a lone figure to decide whether it
// states a ceiling or a floor, defaulting to a floor when nothing says.
func boundFromContext(lowered string, start, end int) payBound {
	if bound, found := nearestCue(lowered[max(0, start-boundWindow):start], ceilingCues, floorCues); found {
		return bound
	}

	trailing := lowered[end:min(end+trailingBoundWindow, len(lowered))]

	if bound, found := nearestCue(trailing, trailingCeilingCues, trailingFloorCues); found {
		return bound
	}

	return boundFloor
}

// nearestCue reports which kind of cue sits closest to the end of window, so a
// cue nearer the figure wins rather than whichever list is searched first.
func nearestCue(window string, ceiling, floor []string) (payBound, bool) {
	ceilingAt, floorAt := -1, -1

	for _, cue := range ceiling {
		if at := strings.LastIndex(window, cue); at > ceilingAt {
			ceilingAt = at
		}
	}

	for _, cue := range floor {
		if at := strings.LastIndex(window, cue); at > floorAt {
			floorAt = at
		}
	}

	switch {
	case ceilingAt < 0 && floorAt < 0:
		return boundFloor, false
	case ceilingAt > floorAt:
		return boundCeiling, true
	default:
		return boundFloor, true
	}
}

// currencyForMatch resolves the currency for a matched figure or range,
// reporting false when the two ends of a range name different currencies.
//
// Refusing a mismatched pair is deliberate: "C$95,000 - A$120,000" is not a
// range, it is two figures in two currencies with no meaningful span between
// them, and no exchange rate is available anywhere in this toolkit to make one.
// Reporting nothing is the honest answer.
func currencyForMatch(lowered string, match []int) (string, bool) {
	lowCode, lowExplicit := currencyFromMarker(group(lowered, match, lowMarkerGroup))
	highCode, highExplicit := currencyFromMarker(group(lowered, match, highMarkerGroup))

	switch {
	case lowExplicit && highExplicit:
		if lowCode != highCode {
			return "", false
		}

		return lowCode, true
	case lowExplicit:
		return lowCode, true
	case highExplicit:
		return highCode, true
	}

	// Neither end named a currency, so a code written after the figure is the
	// last signal there is.
	if code := trailingCurrency(lowered[match[1]:]); code != "" {
		return code, true
	}

	return defaultCurrency, true
}

// currencyFromMarker maps a captured currency marker to an ISO 4217 code,
// reporting whether the marker actually named a currency. A bare "$" does not:
// it reports the default with explicit false, so an explicit marker at the other
// end of the range wins over it.
func currencyFromMarker(marker string) (string, bool) {
	trimmed := strings.TrimSpace(marker)
	if trimmed == "" || trimmed == "$" {
		return defaultCurrency, false
	}

	code, ok := currencyCodes[trimmed]
	if !ok {
		return defaultCurrency, false
	}

	return code, true
}

// trailingCurrency reads an ISO code written just after a figure, or "" if the
// following word is not a currency code.
func trailingCurrency(after string) string {
	match := trailingCurrencyPattern.FindStringSubmatch(after)
	if match == nil {
		return ""
	}

	return currencyCodes[match[1]]
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
//
// currency is passed in rather than assumed: hardcoding USD here is what
// published Canadian and Australian ranges as US dollars.
func buildCompensation(low, high float64, currency, lowered string, end int) *Compensation {
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
		Currency:   currency,
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
//
// lowered must be the same string end was measured against; see the note in
// [ParseCompensationFromText] about why that used not to hold.
func periodFromContext(lowered string, end int) Period {
	window := lowered[max(0, end-60):min(end+40, len(lowered))]

	// Day rates are checked first. A day rate that reaches the hourly branch is
	// annualized at 2080 rather than 260 and overstates the job by 8x.
	for _, marker := range dailyMarkers {
		if strings.Contains(window, marker) {
			return PeriodDay
		}
	}

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
	case "k", "thousand":
		value *= 1_000
	case "m", "million":
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
