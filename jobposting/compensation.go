package jobposting

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
