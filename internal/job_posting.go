package internal

import (
	"strings"
	"time"
	"unicode"
)

// JobPosting is the basic building block of
// the functionality provided by this toolkit.
//
// The fields fall into two groups with deliberately different contracts.
// Company, URL, Title and Location are always emitted, even when empty: the
// README advertises piping NDJSON into jq, and a key that disappears when a
// board omits a value turns `.location` into null for some rows and a missing
// field for others. Everything after them is enrichment that most boards do not
// publish, so each is omitted entirely when absent. Absence there means "the
// board did not say", never "no" — the same rule Compensation already documents.
//
// Adapters populate the enrichment fields only from data the board already put
// on the wire. Nothing here is worth an extra request per posting.
type JobPosting struct {
	Company  string `json:"company"`
	URL      string `json:"url"`
	Title    string `json:"title"`
	Location string `json:"location"`

	// Compensation is the pay range the employer published, when it published
	// one. It is nil far more often than not: most job boards expose no
	// structured pay field at all, and on the boards that do it is a per-company
	// opt-in. Absence means "not disclosed", never "unpaid".
	Compensation *Compensation `json:"compensation,omitempty"`

	// Remote reports whether the board explicitly marked this posting as remote.
	// It is nil when the board publishes no structured remote field, which is the
	// common case; use [JobPosting.IsRemote] for an answer that falls back to
	// reading the location text.
	Remote *bool `json:"remote,omitempty"`

	// Department is the org unit the board filed the posting under, such as
	// "Engineering" or "Global Sales". Boards disagree about granularity: Lever
	// publishes categories.department alongside categories.team, Ashby has both
	// department and team, and BambooHR has only departmentLabel. The coarser
	// value belongs here.
	Department string `json:"department,omitempty"`

	// Team is the finer unit inside Department when the board publishes both,
	// such as "Platform" within "Engineering". [Filter.Departments] searches this
	// field too, because which of the two carries the word a job seeker actually
	// typed varies by platform and nobody should have to know that.
	Team string `json:"team,omitempty"`

	// EmploymentType is the normalized full-time/part-time/contract distinction.
	//
	// Adapters must map their platform's spelling through
	// [NormalizeEmploymentType] instead of storing it raw. The same concept
	// arrives as "FullTime" (Ashby employmentType), "Fulltime" (Lever
	// categories.commitment), "Full-Time" (BambooHR employmentStatusLabel),
	// "Full Time Position" (PeopleForce) and "FULL_TIME" (schema.org, via Jibe).
	// A filter that had to know all five would grow a new special case with every
	// platform added, in the one file where per-platform knowledge cannot be
	// unit-tested per adapter.
	EmploymentType EmploymentType `json:"employment_type,omitempty"`

	// WorkplaceType is the normalized remote/hybrid/onsite distinction exactly as
	// the board published it, which is far stronger evidence than
	// [JobPosting.IsRemote] guessing from location text. Rippling already
	// downloads this field on every posting and spends it appending the literal
	// word "Remote" to a location string; this is where it belongs instead.
	WorkplaceType WorkplaceType `json:"workplace_type,omitempty"`

	// Seniority is the board's own level label: "Senior", "Staff", "L5",
	// "Mid-Senior". Deliberately a free string rather than an enum, unlike the
	// two fields above. Levelling is a per-employer ladder rather than a small
	// shared vocabulary, so any canonical mapping would be this project's opinion
	// about another company's job architecture.
	Seniority string `json:"seniority,omitempty"`

	// PostedAt is when the board says the posting was first published, and
	// UpdatedAt when it last changed.
	//
	// [time.Time] rather than an epoch number or a preformatted string: it
	// round-trips through encoding/json as RFC 3339 without a custom marshaler,
	// it is directly comparable for [Filter.PostedSince] with no reparsing per
	// posting, and its zero value is unambiguous. An int64 epoch would make
	// "the board published no date" indistinguishable from 1970, and this project
	// has to assume every field is missing on most postings.
	//
	// Boards publish these as ISO-8601 (Ashby publishedAt, Greenhouse
	// updated_at), epoch milliseconds (Lever createdAt), epoch seconds (Gem
	// firstPublishedTsSec) and even relative English prose (Workday's "Posted 5
	// Days Ago"). Each adapter converts its own format and stores UTC, so that
	// comparing two postings from different platforms is a comparison of instants
	// rather than of formats.
	PostedAt  time.Time `json:"posted_at,omitzero"`
	UpdatedAt time.Time `json:"updated_at,omitzero"`

	// RequisitionID is the employer's own requisition number, like "JR0012345".
	// It is the identifier a referral form, an internal HR system, or a recruiter
	// conversation is keyed by, and it is not the same thing as ExternalID.
	RequisitionID string `json:"requisition_id,omitempty"`

	// ExternalID is the ATS's identifier for the posting within the tenant: a
	// Greenhouse job id, an Ashby posting id, a Gem extId. Together with Source
	// it identifies a posting even after the URL changes, which URL-keyed
	// [Dedupe] cannot do.
	ExternalID string `json:"external_id,omitempty"`

	// Source records which integration produced this posting.
	Source PostingSource `json:"source,omitzero"`
}

// PostingSource identifies the integration a posting came from: the ATS platform
// and the tenant key on that platform.
//
// docs/architecture-roadmap.md settles on platform + key as the stable
// integration ID, and until now that identity died inside the crawler. A posting
// carried only a short company name, so "which integration produced this?" and
// "did this company's postings move from Greenhouse to Ashby?" could not be
// answered from the output at all. It is deliberately not the company name:
// several tenants can map to one employer, one Workday tenant hosts several
// brands, and the crawl already had a bug where a source key (a tenant URL) was
// compared against the derived company name and silently discarded every
// posting.
//
// This is also the join key the market-data work needs, so it must be stable
// across runs rather than derived from anything a board can restyle.
type PostingSource struct {
	// Platform is the ATS family, matching services.Source.Platform:
	// "greenhouse", "lever", "workday", and so on.
	Platform string `json:"platform,omitempty"`

	// Key is the tenant identifier the adapter fetched with, matching
	// services.Source.Key: a board slug on most platforms, a full tenant URL on
	// Workday, a hostname on Phenom.
	Key string `json:"key,omitempty"`
}

// IsZero reports whether no source identity was recorded. It exists so the
// `omitzero` tag on [JobPosting.Source] drops the object entirely rather than
// emitting an empty one on every posting an unmigrated adapter produces.
func (s PostingSource) IsZero() bool {
	return s.Platform == "" && s.Key == ""
}

// EmploymentType is the normalized shape of an engagement: full-time,
// part-time, contract, and so on.
//
// The canonical set is closed and small on purpose. Every adapter maps its
// platform's spelling onto it at the adapter boundary with
// [NormalizeEmploymentType], exactly as ashbyPeriod and leverIntervals already
// do for pay periods, so that filtering never has to know how any one board
// spells "full time".
type EmploymentType string

// The canonical employment types. The zero value is [EmploymentTypeUnknown],
// which is what makes `omitempty` drop the field for the boards (most of them)
// that publish nothing.
const (
	EmploymentTypeUnknown    EmploymentType = ""
	EmploymentTypeFullTime   EmploymentType = "full_time"
	EmploymentTypePartTime   EmploymentType = "part_time"
	EmploymentTypeContract   EmploymentType = "contract"
	EmploymentTypeInternship EmploymentType = "internship"
	EmploymentTypeTemporary  EmploymentType = "temporary"
	EmploymentTypeVolunteer  EmploymentType = "volunteer"
)

// WorkplaceType is the normalized shape of where the work happens.
type WorkplaceType string

// The canonical workplace types. The zero value is [WorkplaceTypeUnknown]; note
// that unknown is not onsite. Lever publishes the literal value "unspecified",
// and treating that as an office requirement would invent a fact the employer
// declined to state.
const (
	WorkplaceTypeUnknown WorkplaceType = ""
	WorkplaceTypeRemote  WorkplaceType = "remote"
	WorkplaceTypeHybrid  WorkplaceType = "hybrid"
	WorkplaceTypeOnsite  WorkplaceType = "onsite"
)

// EmploymentTypeValues returns the canonical employment types in a stable order.
// It backs the CLI's flag validation and help text, so that the list a user is
// shown cannot drift from the list the filter accepts.
func EmploymentTypeValues() []EmploymentType {
	return []EmploymentType{
		EmploymentTypeFullTime,
		EmploymentTypePartTime,
		EmploymentTypeContract,
		EmploymentTypeInternship,
		EmploymentTypeTemporary,
		EmploymentTypeVolunteer,
	}
}

// WorkplaceTypeValues returns the canonical workplace types in a stable order.
func WorkplaceTypeValues() []WorkplaceType {
	return []WorkplaceType{
		WorkplaceTypeRemote,
		WorkplaceTypeHybrid,
		WorkplaceTypeOnsite,
	}
}

// employmentTypeWords maps a distinctive word to the type it implies, in
// priority order: the first entry whose word appears in the squashed input wins.
//
// Order is load-bearing. "internship" precedes "intern" so the longer word is
// not consumed by the shorter, and "temporary" precedes nothing that contains
// it. Every word here is long enough to be safe as a substring, which is what
// lets it match padded platform values such as PeopleForce's "Full Time
// Position" or Ashby's "Intern (Summer 2026)".
var employmentTypeWords = []struct {
	word string
	typ  EmploymentType
}{
	{"fulltime", EmploymentTypeFullTime},
	{"parttime", EmploymentTypePartTime},
	{"internship", EmploymentTypeInternship},
	{"contractor", EmploymentTypeContract},
	{"contract", EmploymentTypeContract},
	{"freelance", EmploymentTypeContract},
	{"temporary", EmploymentTypeTemporary},
	{"seasonal", EmploymentTypeTemporary},
	{"volunteer", EmploymentTypeVolunteer},
	{"intern", EmploymentTypeInternship},
}

// employmentTypeExact holds spellings that are only safe to match in full.
// "ft", "pt" and "temp" are real board values, and all three appear inside
// ordinary English words, so matching them as substrings would misfile postings
// rather than leave them unlabelled.
var employmentTypeExact = map[string]EmploymentType{
	"ft":   EmploymentTypeFullTime,
	"pt":   EmploymentTypePartTime,
	"temp": EmploymentTypeTemporary,
}

// workplaceTypeWords maps a distinctive word to the workplace type it implies,
// in priority order.
//
// "hybrid" is checked before "remote" because boards write "Hybrid Remote" and
// "Remote/Hybrid" to mean hybrid; the office requirement is the constraining
// half of that pair, so it wins. "office" is last so that "Office - Remote
// optional" resolves as remote rather than onsite.
var workplaceTypeWords = []struct {
	word string
	typ  WorkplaceType
}{
	{"hybrid", WorkplaceTypeHybrid},
	{"remote", WorkplaceTypeRemote},
	{"workfromhome", WorkplaceTypeRemote},
	{"telecommute", WorkplaceTypeRemote},
	{"distributed", WorkplaceTypeRemote},
	{"anywhere", WorkplaceTypeRemote},
	{"onsite", WorkplaceTypeOnsite},
	{"onlocation", WorkplaceTypeOnsite},
	{"inperson", WorkplaceTypeOnsite},
	{"inoffice", WorkplaceTypeOnsite},
	{"office", WorkplaceTypeOnsite},
}

// workplaceTypeExact holds workplace spellings unsafe to match as substrings.
var workplaceTypeExact = map[string]WorkplaceType{
	"wfh": WorkplaceTypeRemote,
}

// NormalizeEmploymentType maps one board's spelling of an engagement onto the
// canonical [EmploymentType], reporting false when the value is not recognised.
//
// Adapters call this at the point of decoding and store the result; a false
// result means leave the field empty, never guess. An unrecognised value that
// became full_time would be worse than an absent one, because a filter cannot
// tell a wrong answer from a right one, while an absent field is visibly absent.
//
// Values deliberately left unrecognised: Lever's "unspecified", schema.org's
// "OTHER" and "PER_DIEM", and Workday's "Regular" and "Permanent". The last two
// describe tenure rather than hours — a permanent part-time role is ordinary —
// and mapping them to full-time would be a guess dressed as data.
func NormalizeEmploymentType(raw string) (EmploymentType, bool) {
	key := squashVocabulary(raw)
	if key == "" {
		return EmploymentTypeUnknown, false
	}

	if typ, ok := employmentTypeExact[key]; ok {
		return typ, true
	}

	// The canonical values themselves must round-trip, so a posting that has
	// already been normalized once survives a second pass unchanged.
	for _, candidate := range EmploymentTypeValues() {
		if key == squashVocabulary(string(candidate)) {
			return candidate, true
		}
	}

	for _, entry := range employmentTypeWords {
		if strings.Contains(key, entry.word) {
			return entry.typ, true
		}
	}

	return EmploymentTypeUnknown, false
}

// NormalizeWorkplaceType maps one board's spelling of where the work happens
// onto the canonical [WorkplaceType], reporting false when the value is not
// recognised.
//
// Feed this the board's structured workplace field, not its location string. A
// location reading "Remote, OR" is about Oregon often enough that
// [JobPosting.IsRemote]'s heuristic exists precisely to be kept separate from
// this function's structured answer.
func NormalizeWorkplaceType(raw string) (WorkplaceType, bool) {
	key := squashVocabulary(raw)
	if key == "" {
		return WorkplaceTypeUnknown, false
	}

	if typ, ok := workplaceTypeExact[key]; ok {
		return typ, true
	}

	for _, candidate := range WorkplaceTypeValues() {
		if key == squashVocabulary(string(candidate)) {
			return candidate, true
		}
	}

	for _, entry := range workplaceTypeWords {
		if strings.Contains(key, entry.word) {
			return entry.typ, true
		}
	}

	return WorkplaceTypeUnknown, false
}

// squashVocabulary reduces a board's spelling to comparable letters and digits:
// lowercased, with punctuation, spaces and separators removed.
//
// That is what collapses "FullTime", "Full-Time", "full_time", "FULL_TIME" and
// "Full Time" into one string to compare, which is the entire difference between
// a normalizer that needs one entry per concept and one that needs an entry per
// platform per concept.
func squashVocabulary(raw string) string {
	var b strings.Builder

	b.Grow(len(raw))

	for _, r := range strings.ToLower(raw) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}

	return b.String()
}

// Period is the unit a pay figure is quoted in.
type Period string

// Pay periods.
const (
	PeriodUnknown Period = ""
	PeriodHour    Period = "hour"
	PeriodDay     Period = "day"
	PeriodWeek    Period = "week"
	PeriodMonth   Period = "month"
	PeriodYear    Period = "year"
)

// periodsPerYear converts a pay period into an annual multiplier.
//
// The working-time assumptions (2080 hours, 260 days) are the conventional US
// full-time figures. They make ranges from different boards comparable, which is
// the point, but they are assumptions rather than facts about any given job.
var periodsPerYear = map[Period]float64{
	PeriodHour:  2080,
	PeriodDay:   260,
	PeriodWeek:  52,
	PeriodMonth: 12,
	PeriodYear:  1,
}

// hourlyMagnitudeCeiling is the value below which an unlabelled pay figure is
// assumed to be hourly. Boards that omit the period are overwhelmingly quoting
// hourly rates for frontline roles, and no realistic annual salary is this low.
const hourlyMagnitudeCeiling = 250

// Compensation is a pay range published with a job posting.
type Compensation struct {
	// Min and Max bound the range, in Currency, per Period. Either may be zero
	// if the employer published only one end of the range.
	Min float64 `json:"min,omitempty"`
	Max float64 `json:"max,omitempty"`

	// Currency is an ISO 4217 code when the board supplies one. Several boards
	// publish amounts with no currency at all, so this is often empty.
	Currency string `json:"currency,omitempty"`

	// Period is the unit Min and Max are quoted in.
	Period Period `json:"period,omitempty"`

	// Summary is the board's own human-readable rendering, such as
	// "$160K – $185K • Offers Equity". Kept because it can carry detail the
	// numeric range cannot, like equity or commission.
	Summary string `json:"summary,omitempty"`

	// Provenance says whether the employer published this in a structured field
	// or it was read out of description prose. Consumers should not treat the two
	// as equally trustworthy, and must not blend them.
	Provenance Provenance `json:"provenance,omitempty"`
}

// IsZero reports whether the compensation carries no usable information.
func (c *Compensation) IsZero() bool {
	return c == nil || (c.Min == 0 && c.Max == 0 && c.Summary == "")
}

// effectivePeriod returns the pay period, inferring one from the magnitude of
// the figures when the board did not say.
func (c *Compensation) effectivePeriod() Period {
	if c.Period != PeriodUnknown {
		return c.Period
	}

	// Infer from magnitude. Documented as a heuristic precisely because it is
	// one: a board that publishes "22.50" without a period means per hour, and
	// one that publishes "185000" means per year.
	top := max(c.Min, c.Max)

	if top > 0 && top <= hourlyMagnitudeCeiling {
		return PeriodHour
	}

	return PeriodYear
}

// AnnualMax returns the top of the range as an annual figure, reporting false if
// there is nothing to convert.
//
// Annualizing is what makes an hourly retail range comparable with a salaried
// one, which any pay filter spanning both needs.
func (c *Compensation) AnnualMax() (float64, bool) {
	if c == nil {
		return 0, false
	}

	top := max(c.Min, c.Max)
	if top <= 0 {
		return 0, false
	}

	return top * periodsPerYear[c.effectivePeriod()], true
}

// AnnualMin returns the bottom of the range as an annual figure, reporting false
// if there is nothing to convert.
func (c *Compensation) AnnualMin() (float64, bool) {
	if c == nil {
		return 0, false
	}

	bottom := c.Min
	if bottom <= 0 {
		bottom = c.Max
	}

	if bottom <= 0 {
		return 0, false
	}

	return bottom * periodsPerYear[c.effectivePeriod()], true
}
