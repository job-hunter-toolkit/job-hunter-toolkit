package internal

// JobPosting is the basic building block of
// the functionality provided by this toolkit.
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
