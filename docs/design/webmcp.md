# Browser-local tools through WebMCP

Status: first read-only slice implemented. Roadmap owner: [issue #48](https://github.com/job-hunter-toolkit/job-hunter-toolkit/issues/48).

## Decision

Expose the already-loaded browser corpus through three read-only tools:

| Tool | Capability | Bound |
| --- | --- | --- |
| `get_snapshot_status` | Readiness, generation, digest, freshness, partial status, and count semantics | No corpus scan |
| `search_jobs` | The visible UI's filters and deterministic newest-first paging, with optional fixed-cardinality facets | 8 terms per text field, 120 characters per term, 50 returned rows, offset at most 2,500,000 |
| `get_job_detail` | Exact HTTP(S) URL lookup within the current generation | One 2,048-character locator, one returned row |

The tools call the page's existing `EngineClient`, worker, Wasm bridge, and Go
`web/engine`. They do not instantiate another worker or retain another index.
Search therefore has the same filtering, lifecycle, ordering, and facet
semantics as the human interface. Detail scans the existing compact URL column
instead of adding a URL map.

Saved-search export and import are not exposed. Saved searches are private
localStorage state. The current WebMCP API has no standardized per-invocation
user approval primitive, so exposing them would create ambient access. A
future write-capable slice requires a visible, contextual approval design
before registration.

## Standards and browser status, 30 August 2026

The authoritative [WebMCP Community Group report](https://webmachinelearning.github.io/webmcp/)
is dated 26 August 2026. It is explicitly not a W3C Standard and is not on the
W3C Standards Track. The imperative API is:

```js
await document.modelContext.registerTool({
  name,
  title,
  description,
  inputSchema,
  annotations: { readOnlyHint, untrustedContentHint },
  execute: async (input, { signal }) => result,
});
```

This supersedes older examples using `navigator.modelContext`. The draft
standardizes JSON Schema input descriptions, cancellation through
`AbortSignal`, origin isolation, a `tools` Permissions Policy, and read-only and
untrusted-content hints. It does not currently define `outputSchema`, a
website-callable permission prompt, or browser-agent discovery outside a
visited page. Declarative form behavior remains a specification TODO.

The toolkit keeps explicit output schemas beside each tool and tests them, but
browsers cannot discover those schemas through the current standardized
dictionary. Tool responses use a stable `{ok, snapshot, data|error}` envelope
to compensate for the draft's still-generic rejected-execution errors.

[Chrome's documentation](https://developer.chrome.com/docs/ai/webmcp) calls
WebMCP a proposed standard and progressive enhancement. Chrome currently makes
it available behind a local development flag. Its origin trial starts with
Chrome 149. Firefox and Safari have no documented implementation. Production
therefore uses exact feature detection and a standards-faithful Node harness;
unsupported browsers do not fetch `webmcp.js` or change startup behavior.

A secure Portal run in this orb exercised registration, discovery, and all
three tools against the complete generation 11 corpus in Chrome for Testing
152. That build returned discovered `inputSchema` as a serialized JSON string
and required serialized arguments in `executeTool`, while the 26 August draft
defines objects at that in-page boundary. Registration callbacks received the
specified object shape. This implementation difference is confined to the
test invoker and does not change the site's tools. It is also not evidence of
general stable availability: Chrome's published support remains experimental.

## Trust and privacy boundaries

- Every operation is local to the open tab. There is no backend, account,
  telemetry call, saved-search read, corpus export, or arbitrary query/code
  execution.
- Inputs are manually validated in addition to their schemas. Unknown fields,
  unsupported enum values, non-finite numbers, non-HTTP detail locators, and
  over-limit pages are stable `invalid_input` responses.
- Calls before load completes return `not_ready`, with the current phase and
  snapshot provenance when metadata is available. A failed load is not
  described as retryable inside the broken tab.
- Browser cancellation reaches the worker and Go scan. A newer search or detail
  call cancels the older call of the same kind.
- Search and detail are annotated `untrustedContentHint: true`. Job titles,
  companies, locations, URLs, and every other corpus string are data, never
  instructions. They are returned as structured JSON without evaluation or
  markup interpretation.
- Every relevant response names generation, run time, observation time,
  freshness, partial status, digest, schema versions, and count semantics.
  Search, facet, state, detail-match, and corpus totals count rows. Only
  `believed_open_deduplicated` is the generation-wide deduplicated manifest
  count.

## Resource evidence

Generation 11 has 2,005,791 rows. PR #57 measured the compact projection at
528 MiB retained with a 739 MiB allocator high-water mark, under the 576 MiB
resident and 768 MiB Wasm budgets modeled in `web/engine/memory_test.go`.

This integration adds no field to `Engine`, no projected corpus column, and no
worker. A structural engine-size test guards against adding a second index,
and the adapter test rejects worker/store ownership and caps the progressive
module at 24 KiB. Exact detail retrieval is an in-memory scan, so its time is
linear in rows and cancellation yields at the same 32,768-row cadence as
search. The existing production-sized memory model and Wasm smoke test run in
CI.

## Deliberate limitations

- Tool discovery requires visiting the page and an experimental supporting
  browser.
- Output schemas are project contracts, not yet browser-discoverable WebMCP
  metadata.
- No saved-search access or mutation is registered.
- Detail URLs are stable only within the named immutable generation. Duplicate
  historical rows may share a URL, so `matches` retains row semantics and the
  returned item is the deterministic newest row.
- High-cardinality company, location, department, and source facets remain out
  of scope pending the measured index in issue #50.
