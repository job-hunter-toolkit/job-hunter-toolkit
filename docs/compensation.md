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
     numbers are two unrelated figures rather than a range.

Rejected outright, each drawn from language that really appears in postings:
401(k) match, tuition reimbursement, signing and referral bonuses, stipends,
equity value, funding raised, ARR, insurance deductibles and premiums.

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

When a board states no period, one is inferred from magnitude: a top figure of
250 or less is read as hourly, otherwise annual. An explicitly stated period
always wins over the inference.
