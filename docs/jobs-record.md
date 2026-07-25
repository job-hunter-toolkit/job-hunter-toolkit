# The posting record

`jobs_record.txt` is this project's longest-running artifact: one row per day
since April 2020, recorded by the Track Jobs workflow.

```
MM/DD/YY  POSTINGS  SOURCES
07/25/26  13467     558
```

`jobs_record.png` is rendered from it by `jobs_record.gnuplot`.

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
| 07/26/26 onward | expected to jump several-fold | The crawler was rewritten from ~558 per-company scrapers to per-ATS adapters covering far more companies. **Coverage change, not a market change.** |

So the honest summary is: **the level of this series tracks the crawler's health
and coverage at least as much as it tracks hiring.** Comparing any two points
across one of the boundaries above compares two different measuring instruments.

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

`total` now **exits non-zero if the crawl did not finish** inside its time
budget, and the workflow discards the row rather than recording it. It also
refuses to record an implausibly low count. A partial or empty count can no
longer enter the record silently, which is the failure mode that produced both
outages and the 550 empty rows.

Truncation is detected from the crawl context, deliberately not from per-source
errors: the HTTP client's own timeout produces errors that wrap
`context.DeadlineExceeded` too, so inspecting individual errors would condemn a
perfectly complete crawl because one board was slow.

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
- **Annotations are positioned in graph coordinates**, not data values, so they
  stay put as the y-scale grows after the coverage change.
- Colours come from a colourblind-validated palette: worst-pair ΔE 24.7 under
  protanopia, 33.6 for normal vision.

Regenerate locally with:

```console
$ gnuplot jobs_record.gnuplot
```

## Ideas not yet done

- A second panel or view of **postings per source**, which is far less sensitive
  to coverage changes than the raw total and would be closer to a real market
  signal.
- Recording the crawler version or source count alongside each row, so future
  discontinuities are self-describing rather than needing archaeology.
- Splitting the series at the coverage boundary if the older era becomes
  unreadably compressed once the new level settles.
