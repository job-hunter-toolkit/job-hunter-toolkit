# Surfaces and extensibility

`docs/architecture-roadmap.md` names six surfaces — CLI, snapshots, history, TUI,
MCP, service — and states the invariants they must not break. It does not decide
the three questions that determine whether those surfaces are cheap or expensive
to build:

1. Can a browser reach a job board directly, or does every client-side surface
   need a server in front of it?
2. What is this project's public API, given that today every package is under
   `internal/` and therefore importable by nobody?
3. Where does state live, when the same query has to run against a local file, a
   SQLite database, browser storage, or a remote service?

This document answers those three, and only those three. It is a set of
decisions, not a feature list.

## 1. The browser can crawl 57% of the registry directly

**Measured 2026-07-28**, sending `Origin: https://job-hunter-toolkit.github.io`
against live boards and reading `Access-Control-Allow-Origin` off the response:

| Platform | Sources | Browser-crawlable |
| --- | ---: | --- |
| greenhouse | 647 | `*` |
| ashby | 418 | `*` |
| workable | 67 | `*` |
| smartrecruiters | 54 | `*` |
| recruitee | 35 | reflects the request origin |
| pinpoint | 34 | `*` |
| workday · lever · rippling · jibe · bamboohr · … | 956 | no CORS header |

**1,255 of 2,211 sources — 57% — are reachable from a browser with no proxy.**
Greenhouse and Ashby alone are 48%.

This is the single most consequential fact for the client-side story, and it was
not obvious: the natural assumption is that a public API served for a company's
own careers page would be same-origin only. Most of these are deliberately
CORS-open because customers embed them in their own sites.

Three caveats, all load-bearing:

- **This was measured with `curl`, not with a browser.** The container that
  produced it has no browser egress: Chromium renders a network error page for
  even `https://example.com` while `curl` gets 200 from the same host, so a
  `js/wasm` probe was attempted and told us nothing. A response carrying
  `Access-Control-Allow-Origin` is strong evidence a browser fetch would
  succeed, but it is not the same claim — a preflight on a non-simple request,
  or a header the CDN varies by client, can still fail in a browser that curl
  never exercises. **The first task of any client-side work is to re-run this
  table from an actual browser.** Until then, treat 57% as a hypothesis with
  good evidence, not a measurement.
- **A CORS header is a fact about today, not a contract.** No vendor documents
  it. It can be withdrawn without notice, and the failure mode in a browser is a
  network error indistinguishable from an outage. A client-side surface must
  degrade to snapshots rather than break.
- **`lever` answered `*` to a `HEAD` and nothing to a `GET`.** Treat the table as
  a floor, re-measure it in CI, and never hardcode a platform as browser-safe
  without a live check. `docs/research/` exists because an endpoint assumption
  that is wrong produces an adapter that returns nothing while looking healthy.

### The WASM build itself is not a risk

Verified locally: `GOOS=js GOARCH=wasm go build ./...` and
`GOOS=wasip1 GOARCH=wasm go build ./...` both succeed against the tree as it
stands, with no source changes. The CGO-free invariant that the portability CI
job already enforces is what bought this, and it means the WASM question is
entirely about network reachability and payload size, not about portability of
the code.

### What follows

The client-side surface is **hybrid, not either/or**:

- **Live** for the CORS-open majority. A user searching Greenhouse and Ashby
  gets results from the source, seconds old, with no backend at all.
- **Snapshot** for the rest, and as the fallback for the majority when CORS is
  withdrawn or a board is down. Snapshots are already a planned artifact; this
  gives them a second consumer beyond merge and history.

A hosted crawl proxy stays **optional**, never required. The moment a surface
requires one, the project owns a server, and the GitHub-Pages deployment stops
being free to run.

## 2. The public API is a small vocabulary, not the crawler

Today `go.mod` declares `github.com/job-hunter-toolkit/job-hunter-toolkit` and
every package lives under `internal/`. Nothing outside this repository can
import a single type. There is no SDK, no plugin, and no third-party adapter,
because the language forbids it.

That was the right default while the shape was unsettled. It is now the binding
constraint on every item in the vision.

**Promote a deliberately small surface, and keep the rest internal.** What goes
public is a vocabulary — the nouns every surface already passes around — plus
the two interfaces an extension needs to implement:

| Package | Contains | Why public |
| --- | --- | --- |
| `jobposting` | the posting record and its identity | Every surface speaks it. It is already the de-facto wire type in NDJSON and JSON output. |
| `source` | `Source`, `JobsFunc`, the registry, `Register` | The extension point. A third-party adapter is a `JobsFunc` and a registry entry. |
| `query` | filter predicates and their parsing | So a TUI, an MCP tool and a PWA share one query language instead of three. |
| `snapshot` | reading and writing postings + manifest | The interchange format between crawl, merge, history and the PWA. |
| `storage` | the `Backend` interface (§3) | So a consumer can supply its own store. |

**Stays internal**: `httpx`, every adapter under `services/`, `shard`, `enrich`'s
generator. These are implementation. Publishing the rate limiter would freeze a
politeness policy that must stay free to change, and publishing 19 adapters
would make every board's response shape a compatibility surface.

Two rules that keep this honest:

- **A public type may not transitively expose an internal one.** If `source`
  needs to hand out an HTTP client, it hands out `*http.Client`, not an
  `httpx.Client`. This is what keeps the limiter free to change.
- **Nothing goes public without a consumer.** Each promotion should be justified
  by a surface that needs it. Speculative API is the expensive kind.

Until v1, the public API carries no compatibility promise, and should say so in
its doc comments.

## 3. Storage is an interface with pagination, and the default is no storage

The CLI works today with no database and must keep working that way — that is an
invariant in the roadmap, not a preference. So `storage` is optional by
construction: the core produces an iterator of postings, and a `Backend` is
something a *surface* attaches, never something the crawler requires.

```
Backend:
    Put(ctx, postings iterator) -> (written, error)
    Query(ctx, query, page) -> (postings, nextPage, error)
    Stats(ctx) -> (counts, freshness, error)
```

Pagination is in the interface from the first commit rather than added later.
A full crawl is ~473k postings; every consumer that is not the CLI's stdout
stream — a TUI scrolling, a PWA rendering a list, an MCP tool answering under a
token budget — needs a bounded read, and retrofitting that into a `[]JobPosting`
signature means changing every implementation at once.

Planned implementations, in the order they earn their place:

| Backend | For | Constraint it must respect |
| --- | --- | --- |
| memory | tests, one-shot CLI queries | none |
| snapshot files (NDJSON) | the artifact that already exists | streaming; must not load 473k rows to answer one query |
| SQLite | history, daemon | **pure-Go driver only** — the default binary stays CGO-free |
| IndexedDB | the PWA, offline | via the WASM build; quota-aware |
| remote HTTP | thin clients against a service | the service is optional, never assumed |

The CGO constraint is not incidental. `docs/architecture-roadmap.md` lists
"the default binary stays portable and has no CGO requirement" as
non-negotiable, and the portability CI job asserts it across four
OS/arch pairs. A `mattn/go-sqlite3` dependency would break that on contact.

## 4. What this rules out, deliberately

- **No plugin system based on dynamic loading.** Go plugins are platform-locked
  and version-brittle, and they cannot work in WASM at all. Extension happens by
  importing the `source` package and registering — which is a compile-time
  dependency and is fine, because the consumer is building a binary anyway.
- **No required backend for any surface.** Every surface must have a
  zero-infrastructure mode. This is what keeps the project free to run.
- **No second source of truth for rate limits.** The sharding work already
  established this pattern: affinity is derived from `httpx`'s own limiter table
  rather than a curated list, because two lists drift and the drift looks like a
  rate-limit problem. Any client-side crawler must derive its pacing the same
  way, not reimplement it.

## Sequencing

Coverage work and surface work compete for the same crawl budget, and the crawl
does not currently finish: the last full run recorded 473,404 postings from
1,772 sources, still unfinished after 350 minutes. Nothing in this document
should land ahead of the sharded crawl earning its schedule.

The order that respects that:

1. **Correctness** — adapters verified against live boards, not documents.
2. **Throughput** — the sharded crawl cut over, so a complete snapshot exists.
3. **Public API** — extract the vocabulary packages, no behaviour change.
4. **Storage** — the interface plus the two backends that already have consumers.
5. **Surfaces** — WASM/PWA and MCP, both on top of 3 and 4.

A snapshot that is complete and trustworthy is the prerequisite for every
surface in the vision. Step 2 is therefore not a detour from the vision; it is
the foundation of it.
