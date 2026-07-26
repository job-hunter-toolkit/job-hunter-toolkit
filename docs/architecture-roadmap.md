# Architecture roadmap

This document is a direction, not a promise to build every feature. The project
should remain useful as a small, portable CLI while gaining richer operating
modes through shared packages rather than parallel implementations.

## Product shape

One engine should support several progressively richer surfaces:

1. **CLI:** live job search and source health with no required state.
2. **Snapshots:** reproducible crawl artifacts that can be merged, inspected,
   or stored.
3. **History:** an optional embedded database for job-market analysis.
4. **TUI:** an interactive explorer over the same query APIs.
5. **MCP:** agent-facing tools and resources over live or stored data.
6. **Service:** an optional long-running scheduler and read API.

The CLI remains fully functional without a database, daemon, telemetry backend,
TUI, or MCP client. Optional surfaces must not leak their dependencies into the
crawler's core contracts.

## Non-negotiable invariants

- A partial crawl is never recorded or graphed **as complete**. A deliberately
  retained deadline observation carries an explicit partial status everywhere
  and is excluded from the completed trend line.
- Sharding must not turn totals into sums that bypass global deduplication.
- A failed source cannot make all of its previously seen jobs look removed.
- Source, company, and ATS identity are separate concepts.
- Higher concurrency and additional IPs are not permission to increase pressure
  on a shared service.
- Structured data goes to stdout; progress and diagnostics go to stderr.
- Secrets, proxy credentials, job descriptions, and other high-cardinality or
  sensitive values do not become metric labels.
- The default binary stays portable and has no CGO requirement.

## Build the run model first

The current crawler passes anonymous `JobsFunc` values to its worker pool. That
is enough to fetch postings, but it discards the source identity needed to
answer basic operational questions:

- Which source is running?
- How long did it take?
- How many pages, retries, 429s, and postings did it produce?
- Did it finish, fail, or end because its caller stopped early?

Introduce a source-aware runner before adding distributed execution. A run
should produce:

- `postings.ndjson`: normalized posting records, still suitable for streaming.
- `manifest.json`: the plan, provenance, per-source outcomes, and completion
  proof.

A versioned manifest should contain at least:

```text
schema version
run ID and parent workflow ID
binary version and git commit
start and finish timestamps
requested source IDs and shard identity
completed, failed, and truncated source IDs
per-source platform, company, key, duration, posting count, and error class
per-service request, retry, 429, and maximum Retry-After summaries
whether the artifact is complete and safe to merge
```

The first local step is now present: `total --manifest` writes schema version 1
with per-source lifecycle, duration, raw posting count, error count, and coarse
error class. `--log-format=json --log-level=info` emits matching
`source.start`/`source.finish` events. The next iteration should add stable run
and commit identity plus request/retry/429 aggregates from the shared HTTP
transport.

Use `platform + key` as the stable integration ID. A separately curated company
ID can outlive moves from Greenhouse to Ashby or from a branded Phenom front end
to Workday.

## Native diagnostics

Start with useful diagnostics that require no backend:

- structured `slog` events for run, source, and retry lifecycle;
- `--log-format text|json`;
- per-source elapsed time and posting count;
- a final slowest-sources and failed-sources summary;
- a machine-readable manifest;
- a GitHub Actions step summary built from that manifest.

Suggested event hierarchy:

```text
crawl.start
  source.start
    http.retry
    http.rate_limited
  source.finish
crawl.finish
```

Every wait must remain context-cancelable. Source work stays bounded, and
goroutines are joined before a run returns.

OpenTelemetry should be opt-in and consume the same events, not define the core
API. Traces and metrics are stable in the Go SDK; log export has historically
matured more slowly, so `slog` remains the canonical application logging API.

Low-cardinality metrics may use platform, service, outcome, and HTTP status
class. Source keys, raw URLs, run IDs, job IDs, titles, and locations belong in
logs, spans, manifests, or analytical storage—not metric attributes.

## Integrity-preserving GitHub Actions sharding

GitHub Actions matrix jobs and artifacts are a good fit once manifests exist.
The workflow should have three phases:

1. **Plan:** resolve the exact source set and produce a deterministic shard
   plan.
2. **Crawl matrix:** each job writes postings and a manifest, then uploads both
   even when its shard fails.
3. **Merge:** download every artifact, verify the plan was covered exactly once,
   globally deduplicate postings, and only then produce the total and chart.

The merge job fails closed when a shard is missing, truncated, built from a
different commit, uses another schema version, or omits a planned source.
Adding per-shard totals is not sufficient because the same posting URL can
appear through two integrations.

Use `fail-fast: false` so one failure does not hide the health of the remaining
shards. Use a conservative `max-parallel` rather than launching every shard at
once.

### Shard by service behavior, not equal source counts

Equal-sized shards are unlikely to be balanced: one very large employer can
cost more than hundreds of small boards. Begin with platform affinity:

- Keep each known shared backend in one process so its limiter and 429 cooldown
  remain globally effective for that run.
- Split Workday across a small number of shards because tenant hosts are
  isolated, but validate provider-level behavior before raising that number.
- Keep Phenom in one shard until real timings and request topology justify
  splitting it.
- Pack smaller platforms into mixed shards using measured historical duration.

Once several manifests exist, use a deterministic greedy bin-packing plan based
on a rolling duration estimate. Cap large changes so one anomalous day does not
thrash the plan.

Separate hosted runners often have separate outbound IPs. That improves fault
isolation but must not be treated as rate-limit evasion. A service should not
appear in multiple concurrent shards unless its topology is known to make that
safe.

The workflow summary should show, per shard and platform:

- wall time;
- sources planned/completed/failed;
- postings before and after global deduplication;
- retries and 429s;
- slowest sources;
- incomplete or suspiciously empty sources.

## Historical storage

Start with SQLite through a pure-Go driver. It preserves the single-binary,
local-first experience and is enough to validate the data model and real query
patterns before operating an OLAP service.

The first schema should be append-oriented:

```text
crawl_run
company
source_integration
source_run
posting
posting_observation
```

`source_run` records completeness and diagnostics. A successful source run can
advance `last_seen` or mark a posting absent; a failed or truncated source run
cannot.

`posting` holds stable identity. `posting_observation` holds the values seen in a
particular run so title, location, compensation, remote status, and source data
can change without rewriting history. Preserve raw provenance where lawful and
useful, but avoid making full descriptions mandatory for the first storage
version.

SQLite should have one writer during artifact merge. WAL mode can support local
readers, but sharded Actions jobs should not try to share a mutable database.

ClickHouse is a plausible optional backend for a hosted, multi-user explorer
with very large analytical workloads. It is not the starting point. Keep the
analytical model exportable—NDJSON or Parquet—so measured scale and query
latency, rather than anticipation, decide when a columnar backend is warranted.

## MCP surface

Use the official Tier 1 Go SDK,
`github.com/modelcontextprotocol/go-sdk/mcp`, behind the same application
service used by the CLI. Start with stdio transport; a remote HTTP transport,
authentication, and multi-user policy are later service concerns.

Capability tiers should be explicit.

### Live, stateless tools

- `search_jobs`: crawl selected companies/platforms with filters and a strict
  time/result budget.
- `list_sources`: return company, integration, platform, and health metadata.
- `check_sources`: inspect a bounded set of source health results.
- `market_snapshot`: return a complete current snapshot or fail as incomplete.

Tools should return structured results with source provenance and truncation
state. Defaults must be bounded so an agent cannot accidentally launch the
entire crawl for every conversational turn.

### Historical tools and resources

Available only when a store is configured:

- `query_jobs`;
- `compare_snapshots`;
- `job_market_trends`;
- `source_history`;
- resources for run manifests, source catalogs, and saved analyses.

Analytical tools should accept explicit dimensions and limits rather than
arbitrary SQL by default.

### Administration

Administrative actions—starting full crawls, changing source configuration,
managing retention, proxy pools, schedules, or migrations—must be separate from
read-only research tools. They should be disabled by default, visibly
destructive where appropriate, auditable, and protected by authentication and
policy for any non-stdio deployment.

The MCP layer must never shell out to the CLI. Both interfaces call the same Go
service methods, so cancellation, validation, source limits, telemetry, and
security behavior remain identical.

## CLI and TUI

Keep Cobra as the command model and consider Fang for polished help, errors,
version output, man pages, and completions. Configuration should appear only
when there are enough durable settings to justify it, with precedence:
flags, environment, file, defaults.

Potential long-term commands:

```text
postings        live, pipeable search
snapshot        produce or store a complete crawl
runs            inspect crawl history and diagnostics
sources         list integrations and health
explore         interactive TUI over live or stored data
mcp             run the MCP server
serve           optional scheduler and read API
```

Build the TUI after query and storage APIs stabilize. Use the v2 Charm stack at
its canonical module paths:

- `charm.land/bubbletea/v2`;
- `charm.land/lipgloss/v2`;
- `charm.land/bubbles/v2`.

The TUI is a view, never the crawl engine. It should respect terminal
capabilities and `NO_COLOR`; non-interactive commands remain clean in pipes.

Useful initial views are runs, source health, job search, compensation and
location breakdowns, and an individual run's slowest/erroring sources.

## Optional service

A daemon is valuable only after the run and storage models are trustworthy. It
should compose the same runner, store, queries, and telemetry setup:

- scheduled snapshots with overlap prevention;
- graceful cancellation and shutdown;
- retention and database maintenance;
- read-only HTTP and MCP endpoints first;
- authenticated administration later.

Do not make a daemon the only way to get history. `snapshot --store file.db`
should remain sufficient for cron, GitHub Actions, and personal use.

## Delivery sequence

### Phase 1: observable single-process runs

- Preserve source metadata through the runner.
- Add versioned manifests and JSON logs.
- Record per-source duration, result count, retry, and 429 summaries.
- Render a useful Actions job summary.
- Measure the current full crawl before changing parallelism.

### Phase 2: sharded Actions crawl

- Add deterministic plan, shard, and merge commands.
- Upload immutable postings and manifest artifacts.
- Merge with exact source coverage and global deduplication.
- Adopt conservative service-aware shard boundaries.
- Compare wall time, request volume, failures, and 429s with the baseline.

### Phase 3: embedded history

- Add pure-Go SQLite storage and migrations.
- Store runs, integrations, observations, and provenance transactionally.
- Define absence only from complete source runs.
- Add historical CLI queries and portable exports.

### Phase 4: human and agent explorers

- Add the Charm v2 TUI over stable query APIs.
- Add a read-only stdio MCP server using the official Go SDK.
- Keep live tools bounded and historical tools conditional on a configured
  store.

### Phase 5: optional operations

- Add opt-in OTLP traces and metrics.
- Add the scheduler/service mode and remote MCP transport.
- Evaluate ClickHouse or another OLAP backend using measured SQLite query and
  ingestion limits.

## What to do next

The next self-contained unit should be Phase 1 only. It creates the evidence
needed to design shards, storage, TUI, MCP, and daemon behavior without guessing,
and it improves today's single-process CLI even if none of the later phases are
built.
