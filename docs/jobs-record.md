# The posting record

`jobs_record.txt` is this project's longest-running artifact: one row per day
since April 2020, recorded by the Track Jobs workflow.

Rows are whitespace-separated, one per day, appended in date order. Every shape
the file can contain:

```
MM/DD/YY  POSTINGS  SOURCES  [STATUS]
07/22/20  ?         537                 <- posting count was lost that day
07/25/26  13467     558                 <- legacy 3-column row: complete
07/26/26  473385    1772      partial   <- deadline snapshot
07/27/26  486210    1890      complete
```

`jobs_record.png` is rendered from it by `jobs_record.gnuplot`.

All 2,046 rows written up to and including 07/25/26 have exactly three fields.
The fourth column is new as of July 26, 2026, so any parser that was written
against this file before then has never seen one. Rows without it are treated as
`complete`, which is what they were.

`STATUS` is `complete` when every planned source had an opportunity to finish,
and `partial` when the overall crawl reached its deadline first. A partial row is
a useful observation but not an equivalent measurement, and nothing in this
repository is allowed to treat it as one:

- The row itself carries the word, so the file is self-describing.
- The chart draws it as an isolated purple diamond and breaks both the postings
  line and the source line around it, so it can never join a trend.
- `crawl-manifest.json` records the same status plus per-source outcomes.

A `complete` crawl for a date **supersedes** an existing `partial` row for that
date: the Track Jobs workflow rewrites the row in place rather than appending a
second one. Before that, the first observation of a day won permanently, so a
deadline day stayed a diamond forever even when a re-run would have finished.

## How to read it, and what not to conclude

The series is **not** a clean measure of the job market. Its level has moved
several times for reasons that have nothing to do with hiring, and reading it as
market data would be wrong. Every one of the following was verified against the
data and against git history.

| Period | What the data does | Why |
| --- | --- | --- |
| 04/23/20 to 07/14/20 | 65,319 falling to ~37,000 | First 82 days of tracking. |
| 07/22/20 to 06/24/21 | **no posting counts** (300 days) | The workflow broke silently. Counts resume the day after `ac49557` "Fix track_jobs.yml". |
| 06/25/21 to 01/26/22 | 80,000 rising to 131,895 | Peak of the series. |
| 01/27/22 to 11/06/22 | **no posting counts** (247 days) | Broke again. Counts resume the day after `4dd3685` "Remove broken stuff". |
| 11/07/22 onward | ~22,000 declining to ~13,500 | Level shift is the source pruning in that same commit, not a market crash. |
| 07/26/26 onward | ~13,500 jumping to the high hundreds of thousands | The crawler was rewritten from ~558 per-company scrapers to per-ATS adapters over ~1,890 sources. **Coverage change, not a market change.** |

So the honest summary is: **the level of this series tracks the crawler's health
and coverage at least as much as it tracks hiring.** Comparing any two points
across one of the boundaries above compares two different measuring instruments.

### The July 2026 step change is roughly 35x

This one deserves its own warning because of its size. The last per-company row
is `07/25/26 13467 558`. The first per-ATS crawl counted **473,385 postings from
1,772 companies** — about 35 times the postings and about 3 times the sources, in
a single day-over-day step.

Two consequences worth knowing before reading the chart:

- The postings panel autoscales from zero, so the entire 2020–2026 history —
  including the 131,895 peak — now occupies roughly the bottom quarter of the
  panel. That is honest rather than pretty; a truncated axis would exaggerate
  every wobble in the new era instead.
- **Do not compute a percentage change across 07/25/26 to 07/26/26.** It is not a
  hiring number. It is the difference between two instruments, exactly like the
  11/07/22 prune, only in the other direction.

The Track Jobs workflow enforces a coverage floor against the previous recorded
row, so a *drop* of this magnitude is rejected rather than recorded. A deliberate
one — the 11/07/22 source prune is the precedent — has to be dispatched manually
with the `allow_level_shift` input, which is the point: a level shift should be
somebody's decision, not a silent nightly write.

## The 550 missing values

550 of 2,046 rows carry `?` in the postings column:

```
07/22/20 ? 537
```

Those days recorded no posting count. The old workflow extracted the count by
grepping test output, and when that produced nothing it wrote the row anyway with
an empty field. The result collapsed to two whitespace-separated columns, so
anything reading column 2 silently got the **source count** instead of the
posting count. The chart drew 537 as though it were the day's posting total.

The `?` marker was added so the unknown reads as unknown. The original rows are
in git history. The source count on those days is genuine and was kept.

Anything parsing this file must handle `?`. In gnuplot, note that
`set datafile missing "?"` is **not** sufficient: it skips the row but still
joins the points either side, drawing a smooth ramp across the void. Substituting
`NaN` is what actually breaks the line.

## Why this cannot happen again

`total` exits non-zero if the crawl does not finish inside its time budget by
default. The Track Jobs workflow makes an explicit exception with
`--allow-partial`: it runs the crawler for up to 330 minutes and records a
deadline snapshot as `partial`. The remaining 30 minutes of GitHub Actions'
six-hour job limit pay for installing gnuplot, rendering the chart, committing,
and retrying the push.

This is intentionally different from the old failure mode. A partial count can
no longer masquerade as a completed observation: its quality is in the row, in
the JSON manifest, in the Actions summary, and in the chart's visual encoding.

### What the workflow refuses to record

`--allow-partial` removed the one thing that used to stop a five-minute network
blip from becoming a permanent point on a six-year series, so the guards had to
replace it. A row is rejected — the run fails and records nothing — when any of
these holds:

| Check | Why |
| --- | --- |
| `total` wrote more or fewer than one line to stdout | A second line would be concatenated into the parsed values, and would also produce a malformed `GITHUB_OUTPUT` write. |
| The date, postings, or sources field is not exactly the expected shape | `[ "$x" -lt 1000 ]` exits 2 on non-numeric input, and inside an `if` condition that does not trip `set -e`. The old guard therefore skipped itself on precisely the garbage it existed to catch. |
| Status is neither `complete` nor `partial` | An unrecognised status means the producer changed and the consumer has not. |
| Postings below 1,000, or sources below 100 | Absolute floors. The series has never legitimately reported fewer than 537 sources. |
| Sources below 60% of the previous recorded row | The coverage floor. Without it a crawl that died after thirty boards is recorded permanently, because it still clears the posting floor. |
| Postings below 50% of the previous recorded row | Same idea, on the headline number. |

The relative floors can be waived with the `allow_level_shift` dispatch input,
for a deliberate change like the 11/07/22 prune. Absolute floors and the format
checks cannot be waived at all.

### How the row survives the trip to master

The row exists only on the runner until the push lands, so everything after the
crawl is arranged to fail towards keeping it:

- Nothing writes to `jobs_record.txt` until the commit step, which first fetches
  master and rebuilds the row on top of the real tip. The checkout is up to six
  hours old by then and master takes commits all day.
- The duplicate check that matters runs *after* that fetch, milliseconds before
  the write. The pre-crawl one is an optimisation only: it tests a date computed
  from the step-start clock, while the row carries the crawl-*end* date, and a
  second run queued in the same concurrency group clears it too.
- The push is retried up to five times, refetching and rebuilding each time,
  because a rejected push used to discard the day entirely.
- Chart rendering and the diagnostics summary cannot cost the row. The chart is
  rendered to a side file and swapped in only on success; the manifest summary is
  defensive and `continue-on-error`. Both used to run between the append and the
  commit, where any error threw the number away.

Truncation is detected from the crawl context, deliberately not from per-source
errors: the HTTP client's own timeout produces errors that wrap
`context.DeadlineExceeded` too, so inspecting individual errors would condemn a
perfectly complete crawl because one board was slow.

Every Track Jobs run uploads `crawl-manifest.json` and `total.txt` for 14 days.
The versioned manifest records the overall result and each planned source's
platform, company, key, status, duration, raw posting count, error count, and a
coarse error class. The Actions run summary shows outcome counts and the 15
slowest sources, making a deadline or platform bottleneck visible without
downloading the artifact.

## The chart

`jobs_record.gnuplot` renders two stacked panels sharing one time axis. Notes on
the choices, since they are deliberate:

- **Not a dual-axis chart.** Postings and source count differ by three orders of
  magnitude. On twin y-scales the source line becomes a meaningless flat streak
  and the two series invite false visual correlation.
- **Both axes start at zero**, because a truncated axis exaggerates change.
- **Outages are shaded and labelled** on the postings panel only. The source
  count kept being recorded throughout, so shading the lower panel would
  contradict it.
- **Most annotations are positioned in graph coordinates**, not data values, so
  they stay put as the y-scale grows after the coverage change. The two that
  point *at* a measurement — the peak label and the source-prune arrowhead — are
  the exception and use data coordinates. With the rewrite's ~473k rows in the
  series, `graph 0.97` resolves to roughly 485,000, which parked the label
  "peak 131,895" three and a half times higher than the peak it names.
- **Each trend line carries a same-colour dot overlay** at pointsize 0.35.
  Breaking the line on `NaN` means a lone valid point with `NaN` on both sides
  draws nothing at all — no line segment, no filled area — while still stretching
  the y-axis to accommodate it. A complete crawl fenced by two partial days is
  exactly that shape, and it is the value a reader most wants to see. Under the
  line the dots are invisible; where the line cannot be drawn, they are the mark.
- Colours come from a colourblind-validated palette: worst-pair ΔE 24.7 under
  protanopia, 33.6 for normal vision.

Regenerate locally with:

```console
$ gnuplot jobs_record.gnuplot
```

Both filenames are overridable, which is how the workflow renders a preview and
how CI renders without clobbering the committed PNG:

```console
$ gnuplot -e "datafile='jobs_record-preview.txt'; outputfile='preview.png'" jobs_record.gnuplot
```

CI renders this script on every pull request, against the committed record and
against a fixture holding every row shape — legacy three-column, `?`, `partial`,
and `complete` — and then asserts the evaluated column expressions really do keep
partial rows out of the completed trend. The check exists because the script was
changed twice without ever being executed, and it runs in the nightly at a point
where a non-zero exit discards the day's row.

## Ideas not yet done

- A second panel or view of **postings per source**, which is far less sensitive
  to coverage changes than the raw total and would be closer to a real market
  signal.
- Recording the crawler version or source count alongside each row, so future
  discontinuities are self-describing rather than needing archaeology.
- Splitting the series at the coverage boundary. This is no longer hypothetical:
  measured against a 473,385 row, the whole 2020–2026 era including the 131,895
  peak compresses into roughly the bottom quarter of the postings panel. A second
  chart scoped to the per-ATS era, or a small-multiples layout, would let both
  eras be read properly without truncating either axis.
- Recording the `partial` source count on the source panel's line rather than
  only as a diamond. It is arguably a genuine measurement of how many sources
  were reached — but drawing it on the trend implies "coverage was this wide",
  which is false for a truncated crawl, so it is deliberately left as a diamond.
