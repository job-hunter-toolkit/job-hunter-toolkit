# Research notes

Working notes from the survey that produced [#29](https://github.com/job-hunter-toolkit/job-hunter-toolkit/pull/29).
They are kept because the expensive part of adding a source is not writing the
adapter, it is establishing which endpoint a platform actually serves, whether
it needs a key, and what its response contains.

**Read the confidence marker on every finding before acting on it.** Each one is
labelled `verified-in-code`, `documented`, `inferred`, or `speculative`, and the
difference matters:

- `verified-in-code` means someone read the code path in this repository. Treat
  it as fact about this codebase.
- `documented` means it came from vendor documentation or from working
  open-source implementations. Treat it as a strong starting point, not as
  proof: nothing here was confirmed against a live job board, because the
  environment that produced these notes had no network access to one.
- `inferred` and `speculative` mean exactly what they say.

An endpoint template with a wrong field name yields an adapter that returns
nothing while looking healthy, which is the failure mode this project is most
often bitten by. Capture one real response before modelling a platform, per
[adding-a-source.md](../adding-a-source.md).

## Still forward-looking

| File | What it is for |
| --- | --- |
| [ats-platform-survey.md](ats-platform-survey.md) | ~19 candidate ATS platforms with endpoint templates, key requirements, response fields, pagination, employer coverage estimates, and postings-per-request rankings. The backlog for coverage work, ordered by value per HTTP request. |
| [market-data-sources.md](market-data-sources.md) | Key-free canonical data sources — SEC EDGAR, DOL OFLC wage disclosures, BLS OEWS, O\*NET, Wikidata, USAspending — with access details, rate limits, licensing posture, and how each maps to a user question. The backlog for `internal/enrich`, whose tables ship empty. |

## Historical

These describe problems #29 fixed. They are kept for provenance: each explains
why a piece of the crawler is shaped the way it is, in more detail than a commit
message carries.

| File | What it found |
| --- | --- |
| [correctness-audit.md](correctness-audit.md) | The Workday semaphore leak, the PeopleForce 429 livelock, unbounded pagination in eight adapters, the Gem posting-URL bug, and four pay-parsing defects. |
| [crawl-performance.md](crawl-performance.md) | Where a full crawl's wall time goes, and which optimisations reduce it without increasing pressure on any single backend. |
| [posting-field-audit.md](posting-field-audit.md) | Per-platform inventory of fields already downloaded and discarded, which is where the extra posting fields came from at no request cost. |
| [ci-pipeline-audit.md](ci-pipeline-audit.md) | Every way the nightly could lose a day's data, including the chart step that ran between appending the row and committing it. |

## Provenance

Produced 2026-07-26 to 2026-07-28 by parallel research agents reading this
repository, vendor documentation, and public open-source implementations. Tenant
seed lists recovered by the same pass are staged, unregistered, in
[`internal/services/testdata/candidates/`](../../internal/services/testdata/candidates/).
