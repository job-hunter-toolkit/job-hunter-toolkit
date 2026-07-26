# Compensation data

Pay is the field most job seekers want and the one most likely to be silently
wrong, because a plausible wrong number looks exactly like a right one. This is
how the toolkit tries to earn trust in it.

## The rule: prefer structure, label everything

Pay is read from the most trustworthy source available, and every value records
where it came from in `Compensation.Provenance`:

| Provenance | Source | Trust |
| --- | --- | --- |
| `employer` | A dedicated field in the platform's API | Authoritative |
| `structured` | Markup the board renders *from* a real pay field, a container that declares its contents to be the pay range | Strong; structural, not inferred |
| `description` | Read out of description prose | Best-effort; the only source that can be wrong about what a number *means* |

`Compensation.MoreTrustedThan` orders these, and the extractor stops at the first
source that yields a confident answer. Nothing blends sources.

**Absence is not zero.** `Compensation` is nil when pay was not disclosed, which
is the majority of postings on most boards. It never means the job is unpaid.

## Per-platform coverage

Measured 2026-07-25 against live APIs. Re-verify before relying on it: these are
undocumented endpoints that change.

| Platform | Structured pay? | Notes |
| --- | --- | --- |
| Jibe / iCIMS | **Yes** | `salary_min_value`/`salary_max_value` plus a `salary_frequency` enum. Measured 369/400 PetSmart postings populated, the best pay source in the project. |
| Ashby | **Yes** | Requires `?includeCompensation=true`; the key is absent without it. Per-company opt-in: 268/349 Harvey postings, 0 for several other companies. |
| Lever | Yes, sparse | `salaryRange` in the default response, populated on a minority of postings. |
| Greenhouse | No API field | `pay_input_ranges` exists in the schema but was empty on every board sampled. Pay appears only in the description. |
| Workday | No | Free text in the per-job **detail** endpoint only, costing one extra request per posting. |
| SmartRecruiters | No fixed field | Rides in a generic `customField` array with a per-company label. |

## Reading pay out of descriptions

For the platforms with no structured field, `internal.ParseCompensationFromDescription`
extracts pay from the description. It is deliberately hard to satisfy, because a
job description is full of dollar amounts that are not wages.

Two layers:

1. **Structured markup.** Greenhouse renders a `pay-range` container when an
   employer fills in its pay field. That container declares its own meaning, so no
   guessing is needed. The description is parsed as HTML, not scanned with a
   regular expression; so nesting and attribute order do not matter.
2. **Prose.** A money figure is only accepted when *all* of these hold:
   - a compensation cue appears shortly before it ("salary range", "base pay",
     "hourly rate", …);
   - no disqualifying phrase sits nearer than that cue; this is what keeps
     "401(k) match up to $5,000" from becoming a salary;
   - the annualized value falls within plausible wage bounds, which rejects
     funding rounds, revenue, and market-size figures;
   - for a range, the two ends are within a sane ratio, since wildly separated
     numbers are two unrelated figures rather than a range;
   - for a range, the two ends do not name different currencies (see
     [Currency](#currency)).

Rejected outright, each drawn from language that really appears in postings:
401(k) match, tuition reimbursement, signing and referral bonuses, stipends,
equity value, funding raised, ARR, insurance deductibles and premiums.

### Magnitude suffixes

`$120K`, `$1.2M` and `$1.2 million` are expanded. The suffix has to *be* a
suffix: it is anchored on a word boundary, so the `m` of "monthly" and the `k`
of "knowing" are not magnitudes.

That anchor is not cosmetic. Without it the parser read `$12,000 monthly` as
$12,000,000 and then dropped the posting for being implausible, and
`$140,000 to $180,000 minimum.` lost an entire explicit range the same way — the
"broken source becomes silently empty" failure this project has a history of.
Worse, `The salary is $45 knowing the market.` produced $45,000, a number that
appears nowhere in the posting. Fabricating a plausible salary is the single
worst thing this code can do, so both shapes are covered by table tests.

### Which end of the range a lone figure is

Open-ended wording is read, not ignored:

| Wording | Recorded as |
| --- | --- |
| "up to $200,000", "a maximum of $180,000", "as much as $175,000" | `Max` |
| "$95,000 maximum" (trailing, within a few characters) | `Max` |
| "starting at $135,000", "from $120,000", "at least $115,000" | `Min` |
| anything else | `Min` |

A ceiling recorded as `Min` inverts the sentence: the CSV writer emits
`pay_min=200000` with an empty `pay_max`, and anything reading the JSON `min`
field is told a stated maximum is a floor. `AnnualMax`/`AnnualMin` both fall back
to whichever end is populated, so `--min-pay` behaves either way — this is about
what the published record *says*.

### Currency

`$` is not a currency. It is also CAD, AUD, NZD, SGD, HKD, MXN and BRL, and
labelling all of them USD understated or overstated real ranges by tens of
percent in a field nothing downstream converts.

The rules, in order:

1. An explicit marker in front of a figure sets the code: `C$`/`CA$`/`CAD` →
   CAD, `A$`/`AU$`/`AUD` → AUD, and likewise `NZ$`, `S$`/`SG$`, `HK$`, `MX$`,
   `R$`, `US$`/`USD`, plus the word forms `EUR` and `GBP`. Both ends of a range
   accept a marker; a marker on either end applies to the whole range.
2. Failing that, an ISO code written immediately *after* the figures is used, the
   form boards render as `$145,700 — $200,300 USD` or `$95,000 - $120,000 CAD`.
3. Failing both, a bare `$` is recorded as **USD**.

Rule 3 is an assumption, not an observation, and it is stated here so it can be
argued with. Every board this toolkit crawls is a US-headquartered ATS serving
mostly US postings, and the plausibility bounds above are USD-calibrated, so a
bare `$` figure that survives them has already been judged against USD. Recording
an empty currency instead would make no consumer more correct — nothing here
converts between currencies — while discarding a label that is right the large
majority of the time. `Provenance` is `description` on all of it, which is the
signal that says the figure was inferred.

**A range whose two ends name different currencies is refused outright.**
`C$95,000 - A$120,000` is not a range; it is two figures with no meaningful span
between them and no exchange rate available to make one. Neither end is salvaged
as a lone figure either, the same rule already applied to a range rejected as two
unrelated numbers. Recording nothing beats asserting a currency the posting does
not state.

**`--min-pay` is not currency-aware.** It compares `AnnualMax()` as a bare
number, so a CAD or AUD range is compared against a USD threshold without
conversion. Correctly labelling the currency makes that visible rather than
fixing it; a consumer that cares must filter on `Compensation.Currency` itself.

### Non-ASCII descriptions

Cue and period windows are sliced out of the same lowercased string the money
patterns are matched against. This has to hold because `strings.ToLower` is not
length-preserving: U+0130 (İ, Turkish dotted capital I) shrinks from two bytes to
one and U+212A (KELVIN SIGN) from three to one. Offsets taken from one string and
applied to the other drifted — measured on one range, 61 İ before the pay line
published a `$150,000-$200,000` annual range with no period at all, and 70 made
the window slice panic.

### Entity-encoded markup

Some boards publish descriptions whose markup is itself entity-encoded
(`&lt;span&gt;$145,700&lt;/span&gt;&mdash;…`). Entities are therefore decoded
*before* tags are stripped, and again afterwards. Getting this wrong made a real
range read as a lone lower bound; fixing it took Robinhood from 22 to 101
extractions out of 119.

### Measured accuracy

Across 2,529 live Greenhouse postings from 8 companies:

- 1,220 extracted (48.2%)
- **765 from structured markup**, 455 from prose
- **0 implausible values**
- 8 of 8 prose extractions verified correct by hand against their source text

**This measurement predates the extraction fixes below it and has not been
re-run** — there is no network access from the environment those fixes were made
in, and re-measuring needs live boards. Treat the extraction count as a floor
rather than a current figure: five defects were corrected afterwards, four of
which changed which figures come out and what they mean.

- The magnitude-suffix anchor turns postings that reported *no* pay into
  postings that report some, so extraction rate goes **up**.
- Day rates drop by ~8x (2080 → 260), so those postings get **much smaller**.
- Non-USD ranges gain their upper bound and a correct currency code.
- "Up to $X" moves from `min` to `max`, so a consumer reading `min` sees fewer
  populated values.

Any saved `--min-pay` query or stored comparison will therefore return different
results than it did before. `Provenance` already exists to communicate that these
are inferred values; a crawl manifest schema bump is the right way to mark the
point where parser semantics changed.

## Not yet wired into the crawl

The extractor is complete and tested, but the CLI does not yet run it, because
descriptions are not free: requesting them from Greenhouse (`?content=true`)
inflates the response **13.7x** (Databricks 0.7 MB → 9.4 MB; Stripe 0.3 MB →
4.0 MB). Across ~500 Greenhouse companies that is the difference between a ~50 MB
and a ~700 MB crawl.

So it belongs behind an opt-in flag rather than on by default, which needs the
adapter signature to carry per-crawl options. That is the next step; it was left
undone rather than bolted on as hidden global state.

## Annualizing

`AnnualMax`/`AnnualMin` convert to yearly figures (2080 hours, 260 days, 52
weeks, 12 months) so `--min-pay` works across hourly and salaried roles alike:
$29.95/hour correctly clears a $60,000 floor. Those working-time figures are
conventional assumptions, not facts about a specific job.

The conversion is computed on demand from `Min`/`Max` and never written back, so
the multiplier cannot compound: `Min` and `Max` always stay in the period the
posting stated, and 2080 lives in exactly one place (`periodsPerYear`).

Prose states its period with any of these, and a **day rate is checked first**:

| Wording | Period | Multiplier |
| --- | --- | --- |
| "per day", "/day", "a day", "daily", "per diem", "day rate" | `day` | 260 |
| "per hour", "an hour", "/hour", "/hr", "hourly", "per hr" | `hour` | 2080 |
| "per week", "weekly" | `week` | 52 |
| "per month", "monthly" | `month` | 12 |
| "per year", "per annum", "annually", "a year", "/yr" | `year` | 1 |

Day rates were previously unreachable from prose even though `PeriodDay` and its
260 multiplier existed for the Lever and Jibe API fields. A $200/day contract
fell through to the magnitude heuristic below, was called hourly, and was
published at $416,000/yr instead of $52,000 — 8x wrong, and carrying the same
`description` provenance as a correct figure. Larger day rates failed the other
way: $600/day cleared the hourly ceiling, was read as annual, and was then
rejected as implausibly low, so the posting reported no pay at all.

When a board states no period, one is inferred from magnitude: a top figure of
250 or less is read as hourly, otherwise annual. An explicitly stated period
always wins over the inference.
