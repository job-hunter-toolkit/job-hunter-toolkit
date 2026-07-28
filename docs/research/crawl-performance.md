# recon:perf

## Summary

The crawl is not slow because of rate limiting — the 75-minute failing run logged only two "failed after retries" warnings, both PeopleForce, both within the first 3 minutes, then 72 minutes of silent work (verified in the run log for job 89785096822, run 30198816666, 10:45:55Z → 12:00:55Z). Wall time is dominated by (a) too few workers, (b) a confirmed semaphore-leak bug that makes every large Workday tenant stall for exactly one Client.Timeout doing nothing, and (c) strictly sequential page-by-page pagination inside single enormous sources. Scheduling today is two uncoordinated limiters: a global worker pool in internal.AllWithConcurrency (internal/all.go:42) sized by `min(32, max(8, NumCPU*4))` (internal/all.go:27) — which on a 4-vCPU ubuntu-latest runner is 16, not 32 — and a per-service semaphore inside httpx.hostLimiter (internal/httpx/httpx.go:341) with a hard cap of 4 (2 for Workable and PeopleForce). 1,953 sources through 16 workers gives each source a mean budget of ~29s to fit in an hour; FedEx on Jibe alone needs 1,380 strictly sequential requests. I confirmed by direct experiment (copy of the repo, stub transport, httpx.NewClient) that internal/services/workday.go:331's `defer resp.Body.Close()` inside the pagination loop never releases the hostLimiter slot — httpx wraps every body in `releaseOnClose` (httpx.go:370,376-390) and releases only on Close — so the 5th page of every Workday tenant blocks until the request context expires. In production that is the 2-minute httpx `defaultTimeout` (httpx.go:35), silently (retryTransport returns early at httpx.go:523 without logging), and yields at most 4×20 = 80 postings per Workday tenant. With 212 distinct Workday hosts that is up to ~7 worker-hours of pure dead time, ~25 minutes of the 75-minute wall clock at 16 workers. The 58-minute Retry-After values from peopleforce.io are already clamped to maxDelay=30s in both `limitState.penalize` (httpx.go:325-338) and `retryTransport.backoff` (httpx.go:629-648), so they are NOT costing 58 minutes — that assumption is wrong. Interleaving (services/builtin.go:119) round-robins platforms, but I measured that the last 400 scheduled sources are exclusively Greenhouse (315) and Ashby (85), two shared-host services capped at 4 concurrent each, so the crawl's tail can make progress on at most 8 requests regardless of free workers. The biggest wins are pressure-neutral: fix the Workday body leak, decouple concurrency from NumCPU, use each service's existing 4-slot budget for bounded parallel pagination instead of leaving it idle behind a sequential loop, and schedule known-largest sources first. Measurement is feasible offline: the fixture harness (services/adapters_test.go:18) bypasses httpx entirely, so a new latency-simulating transport driven through httpx.NewClient plus a synthetic 1,953-source crawl benchmark would make all of this reproducible without network.

## Findings

### CONFIRMED BUG: Workday's `defer resp.Body.Close()` inside the pagination loop leaks hostLimiter slots and stalls every large tenant for a full 2-minute Client.Timeout
- impact: critical | effort: small | confidence: verified-in-code
- files: internal/services/workday.go, internal/httpx/httpx.go, internal/services/adapters_test.go, internal/companies/uber.go

internal/services/workday.go:315-369 paginates in a `for {}` loop and at line 331 does `defer resp.Body.Close()` — deferred to *function* scope, so no page's body is closed until the whole source finishes. httpx wraps every response body in `releaseOnClose` (internal/httpx/httpx.go:370, type at 376-390) whose `release` (the `<-state.sem` at line 370) fires only on Close. The per-service semaphore has capacity `defaultPerHostLimit = 4` (httpx.go:48); Workday tenant hosts are deliberately NOT grouped (httpx.go:47 comment) so each of the 212 distinct hosts gets its own 4-slot limiter. Result: pages 1-4 succeed, page 5 blocks at `case state.sem <- struct{}{}` (httpx.go:345) until `req.Context().Done()`.

I verified this empirically in a scratchpad COPY of the repo (not the repo itself): a program calling httpx.NewClient(WithTransport(stub)) and replicating the deferred-Close pattern printed `page 0..3: ok in 0s` then `page 4: ERROR after 3.002s: context deadline exceeded`, and with `context.Background()` plus defaultTimeout patched to 3s: `page 4: ERROR after 3s: ... (Client.Timeout exceeded while awaiting headers)`. In production the bound is httpx.go:35 `defaultTimeout = 2 * time.Minute`.

Consequences: (1) every Workday tenant with >80 postings yields exactly 80 postings and then an error — coverage loss on 216 of 1,953 sources, including FedEx, Boeing, CVS, HCA, Home Depot, Target, Wells Fargo; (2) each such source burns 120 seconds of a worker doing literally nothing; (3) it is SILENT — httpx.go:521-525 returns `nil, err` without calling logExhausted when the request context is already expired, which is exactly why the 75-minute failure log contains no Workday warnings at all. At 16 workers, ~200 affected tenants × 120s = 24,000 worker-seconds ≈ 25 minutes of the 75-minute wall clock.

Why no test catches it: services/adapters_test.go:60 `fixtureClient` returns a bare `&http.Client{Transport: transport}` — it never goes through httpx.NewClient, so no fixture test ever exercises the limiter. Workday has no fixture test at all (only TestWorkdayCompanyName:365, TestWorkdayCXSURL:396, TestWorkdayCompanyURLsHaveNoRedundantJobsSuffix:454).

Fix: extract a `workdayPage(ctx, client, cxsURL, limit, offset) (*workdayInfo, error)` helper exactly like the existing `leverPage` (lever.go:218, whose doc comment at 215-217 says it exists for precisely this reason), `jibePage` (jibe.go:155), and `phenomPage` (phenom.go:78). internal/companies/uber.go:84 has the identical pattern but that package is dead code — nothing in main.go or internal/ imports it.

### Global worker pool is sized from NumCPU and is 16 on GitHub runners, for 1,953 network-bound sources
- impact: critical | effort: small | confidence: verified-in-code
- files: internal/all.go, main.go, .github/workflows/track_jobs.yml

internal/all.go:27: `var DefaultConcurrency = min(32, max(8, runtime.NumCPU()*4))`. I ran the formula: NumCPU=4 (a standard ubuntu-latest runner, and this container) → 16. NumCPU=8 → 32. The comment at all.go:24-26 says the work is network-bound and set "well above the CPU count", but the value is still *derived* from it, so CI silently runs at half the intended ceiling. .github/workflows/track_jobs.yml never passes `--concurrency`, so the nightly crawl uses 16 workers.

Arithmetic: 1,953 sources / 16 workers × 60 min = a mean budget of 29.5 seconds per source. Any source needing more sequential work than that eats other sources' budget.

Critically, raising this is PRESSURE-NEUTRAL: the actual request rate to every backend is gated by httpx.hostLimiter (httpx.go:341-373), not by the worker count. Extra workers just stop the pool from idling while workers are parked on service semaphores. `internal.AllWithConcurrency` already takes an explicit limit (all.go:42) and main.go:52 already exposes `--concurrency`, so this is a one-line workflow change plus decoupling the default from NumCPU (e.g. a flat 64, or `--concurrency` set explicitly in track_jobs.yml).

### Every adapter paginates strictly sequentially, even when the API returns a total count that makes bounded parallel fetching trivial
- impact: critical | effort: medium | confidence: verified-in-code
- files: internal/services/workday.go, internal/services/jibe.go, internal/services/smart_recruiters.go, internal/services/phenom.go, internal/services/peopleforce.go, internal/services/lever.go

No adapter issues more than one request at a time for a single source, so each source uses 1 of its service's 4 allotted slots. Three adapters already decode a total and throw it away for scheduling purposes:

- Workday: `workdayInfo.Total` (workday.go:278) is used only as a stop condition at workday.go:364. Page size is hardcoded `limit = 20` (workday.go:312) — and 20 is the provider's maximum, so larger pages are NOT available (documented: Workday cxs supports offset/limit with 1-20 per page and returns `total`). FedEx's two Workday sites, Boeing, HCA etc. therefore need hundreds to thousands of sequential 20-row requests.
- Jibe: `jibeJobs.TotalCount` (jibe.go:120) is decoded and never used; the loop stops on a short page (jibe.go:...  `if len(apiResp.Jobs) < jibePageSize`). Page size is 100 (jibe.go:19).
- SmartRecruiters: `smartRecruitersJobs.TotalFound` (smart_recruiters.go:78) is used only as a stop condition. `smartRecruitersPage` (smart_recruiters.go:91-97) sends ONLY `offset` — no `limit`. The documented default is 100, so this is currently fine, but it is implicit.

Offset-addressable and parallelizable: Workday (offset/limit + total), Jibe (page/limit + totalCount), SmartRecruiters (offset + totalFound), Lever (skip/limit, max 100 per page — lever.go:242 already uses 100), Phenom (from/size — phenom.go:20 uses 100, but no total is decoded; phenomSearchResults at phenom.go:58-68 only captures Data.Jobs).
NOT parallelizable without a total: Jobvite (jobvite.go:178, HTML, stops on an end-of-results marker), PeopleForce (peopleforce.go:142, HTML, has a "Displaying X - Y of Z" total extracted at peopleforce.go:196 that COULD drive parallel pages).
Single-request already: Greenhouse (greenhouse.go:697), Ashby (ashbyhq.go:492), Workable (workable.go:104), BambooHR (bamboohr.go:98, with a `// TODO: handle pagination` at line 139), Gem (gem.go:142), Rippling (rippling.go:184).

Politeness classification: for tenant-isolated backends (Workday's 212 distinct hosts, Phenom's 15 hosts) fetching 4 offsets at once stays strictly inside the 4-slot budget the project has ALREADY declared safe for that host — no increase over policy. For shared-key backends (jibeapply.com, api.smartrecruiters.com, api.lever.co) the 4 slots are shared across all tenants, so parallel pagination redistributes the same budget rather than increasing it — it changes fairness (a huge tenant would monopolise the platform's slots), not total pressure. That argues for a per-source parallelism cap of 2-3 on shared keys and 4 on isolated ones.

### Head-of-line blocking: FedEx on Jibe is ~1,380 strictly sequential requests through a limiter shared with 70 other tenants
- impact: critical | effort: medium | confidence: inferred
- files: internal/services/jibe.go, internal/services/phenom.go, internal/httpx/httpx.go

`fedex` is in JibeCompanies (jibe.go:44). httpx.go:273-277 groups every `*.jibeapply.com` tenant under one key `jibeapply.com` with maxConcurrent=4, interval=25ms. That single 4-slot bucket serves 71 tenants including costco, marriott, ascension, commonspirit, dollargeneral, heb, mercy, generalmills, footlocker — all large employers.

main.go:538-540 states FedEx alone publishes over 138,000 postings ("more than a thousand sequential paginated requests"). At jibePageSize=100 that is 1,380 requests issued one after another inside a single `Jibe` iterator, each contending with 70 sibling tenants for 4 slots. I measured that fedex lands at index 295 of 1,953 in the interleaved order, so it starts roughly a minute into the crawl and then runs unbounded. INFERRED: at a realistic 1-3s per request under 4-way contention, FedEx alone is 23-69 minutes — plausibly the entire crawl tail on its own. This is not measured; the 07-26 run predates the manifest, and its artifact list is empty.

The same shape applies to Phenom: 15 hostnames (careers.humana.com, careers.united.com, talent.lowes.com, careers.southwestair.com …), each a sequential from=0,100,200… walk where every page is a full HTML document buffered whole into a strings.Builder (phenom.go:100-104) and string-searched for `"eagerLoadRefineSearch":`.

### The scheduler's tail is 100% Greenhouse + Ashby, two shared hosts capped at 4 concurrent each — at most 8 useful requests no matter how many workers are free
- impact: high | effort: medium | confidence: verified-in-code
- files: internal/services/builtin.go, internal/httpx/httpx.go, internal/all.go

`interleaveSources` (services/builtin.go:119-142) round-robins platforms by index, which is correct for the crawl's opening but degenerates at the end: once the small platforms are exhausted only the two largest groups remain. I measured the real interleaved order: the FIRST 200 sources are evenly spread over all 13 platforms (15-16 each), but the LAST 400 are `map[ashby:85 greenhouse:315]`.

Greenhouse is 647 of 1,953 sources (33%) all on `boards-api.greenhouse.io`, given maxConcurrent=4, interval=25ms (httpx.go:255-262). Ashby is 418 sources all on `api.ashbyhq.com`, which is NOT in servicePolicyFor's switch at all — it falls through to the generic exact-host policy (httpx.go:244-248): maxConcurrent=4, interval=0, cooldown=5s. Same for `jobs.jobvite.com` (33 tenants, jobvite.go:59). Their grouping is correct only by accident (all tenants share one host).

Because the worker pool and the service semaphore are independent, a worker holding a Greenhouse source that is blocked at httpx.go:345 is unavailable for any other work. During the tail, 16 workers can be occupied while only 8 requests are in flight.

Measured platform distribution of the 1,953 builtin sources: greenhouse 647, ashby 418, workday 216, lever 161, rippling 128, jibe 71, workable 67, bamboohr 55, smartrecruiters 54, gem 51, peopleforce 37, jobvite 33, phenom 15.

### The 58-minute peopleforce.io Retry-After values are already clamped to 30s — they are not the cost; four 30s retries inside a shared 2-slot limiter are
- impact: medium | effort: small | confidence: verified-in-code
- files: internal/httpx/httpx.go, internal/services/peopleforce.go

Both clamps exist and work. `limitState.penalize` (httpx.go:325-338): `delay := min(s.cooldown, maxDelay)` then `delay = max(delay, min(retryDelay, maxDelay))` → with cooldown=15s (httpx.go:267) and maxDelay=30s (httpx.go:34), a Retry-After of 3510s produces exactly 30s. `retryTransport.backoff` (httpx.go:629-632): `return min(d, t.maxDelay)` → also 30s. The doc comment at httpx.go:323-324 says this explicitly.

So the real cost of a 429'd PeopleForce request is: 4 attempts (defaultMaxAttempts=4, httpx.go:32) × up to 30s of backoff each, all inside a 2-minute Client.Timeout, against a SHARED limiter key `peopleforce.io` (httpx.go:263-268) with maxConcurrent=2 and interval=100ms covering all 37 tenants. `penalize` pushes `s.next` forward for the whole service, so one tenant's 429 delays the other 36 by 30s.

But the magnitude is bounded and small: the entire 75-minute failing run logged exactly TWO `HTTP request failed after retries` lines (10:48:28Z vyriy.peopleforce.io?page=11, 10:48:58Z youscan.peopleforce.io), both in the first 3 minutes, and nothing for the remaining 72 minutes. PeopleForce is 37 of 1,953 sources. Do not spend effort here expecting a large win.

The worthwhile change is a shedding policy, which REDUCES pressure: when a service returns a Retry-After longer than the crawl's remaining budget, mark that service key dead for the run and fail its remaining sources fast instead of retrying 4× at 30s. That needs a new field on limitState and a check in hostLimiter.RoundTrip.

### Transport tuning: gzip and HTTP/2 are already on; MaxIdleConnsPerHost=8 is adequate; there is no CLI flag to tune the per-service limit
- impact: medium | effort: small | confidence: verified-in-code
- files: internal/httpx/httpx.go, main.go

Verified, so that this is not re-litigated: httpx.NewClient (httpx.go:153) clones http.DefaultTransport, which carries ForceAttemptHTTP2=true, so h2 is negotiated. No adapter sets `Accept-Encoding` anywhere in the tree (grep across all .go files returns nothing), and DisableCompression is never set, so net/http's transparent gzip is active on every request. httpx.go:159-160 sets MaxIdleConns=200 and MaxIdleConnsPerHost=8, which exceeds the per-service cap of 4 (2 for Workable/PeopleForce), so connection churn is not a factor today; under h2 a single connection multiplexes anyway. IdleConnTimeout stays at the 90s default, fine for hot shared hosts.

Gap: `httpx.WithPerHostLimit` (httpx.go:140) exists but main.go never calls it — globalFlags.client (main.go:82-100) only wires the logger and proxies. There is no way to change per-service concurrency without a code change, which makes A/B experiments in CI impossible. Add a hidden/advanced flag or an env var.

Also note: `defaultTimeout = 2 * time.Minute` (httpx.go:35) is an http.Client.Timeout, i.e. it bounds the ENTIRE retryTransport loop including all four attempts and their backoff sleeps, because setRequestCancel puts the deadline on the request context and every sleep is ctx-aware (httpx.go:730-744). One 429-chained logical request can therefore consume ~110s of a worker.

### Rippling parses a full Next.js HTML document with x/net/html just to read one <script id="__NEXT_DATA__">; Phenom buffers whole pages into memory
- impact: medium | effort: small | confidence: inferred
- files: internal/services/rippling.go, internal/services/phenom.go, internal/services/json.go

rippling.go:209 calls `html.Parse(resp.Body)` on every one of the 128 Rippling tenant pages, then `extractScriptContent` (rippling.go:298) walks the tree to find a single script node. Next.js `__NEXT_DATA__` pages are frequently multi-megabyte; x/net/html builds a full node tree and allocates heavily. Phenom already demonstrates the cheap alternative in the same package: phenomPage (phenom.go:96-110) does `strings.Index(body, marker)` then points a json.Decoder at the offset, with a comment explaining that the decoder stops at the end of the first complete JSON value. Applying that pattern to Rippling is pure CPU/allocation savings with ZERO change in request pressure.

Phenom's own remaining cost is that phenom.go:100-104 `io.Copy`s the entire response into a strings.Builder before scanning. A streaming scan for the marker would cut peak memory across 15 concurrently-paginating tenants.

Same html.Parse cost applies to peopleforce.go:85 and jobvite.go (via fetchHTML, json.go:112), but those are 37 and 33 sources respectively.

Not measured — I could not fetch a real Rippling page from this container. Marked inferred.

### internal.Dedupe holds ~473k URL strings in a map on the single consumer goroutine; the results channel is unbuffered but is NOT the bottleneck
- impact: medium | effort: small | confidence: inferred
- files: internal/filter.go, internal/all.go, main.go

internal/filter.go:237-271: `seen := make(map[string]struct{})` keyed on the full posting URL (filter.go:253). At the observed 473,385 postings with URLs averaging ~80 bytes, that is roughly 70-80 MB retained for the whole crawl (INFERRED: ~48B map overhead + 16B string header + key bytes per entry). On a 16 GB runner that is survivable but grows linearly with coverage, and fixing Workday will push posting counts well past 600k.

Mitigation with no behaviour change: key on a 128-bit hash of the URL (`map[[16]byte]struct{}`), which drops per-entry cost to ~64B and removes the string retention. Collision risk at 10^6 keys with 128 bits is negligible.

I want to be explicit about what is NOT a problem, so effort is not misdirected: `results = make(chan result)` (internal/all.go:59) is unbuffered, so every posting is a goroutine handoff to a single consumer that does a map insert (Dedupe) then a filter check then `perCompany[...]++` (main.go:442). At Go's ~100-300ns per unbuffered handoff plus ~100ns per map insert, 473k postings total well under a second of aggregate CPU. Buffering the channel (e.g. 1024) is a cheap latency-jitter improvement, not a throughput fix. Do not sell it as one.

### Fixture tests bypass httpx entirely, so nothing hermetic covers the limiter, pacing, retry or slot-release behaviour of a real crawl
- impact: high | effort: medium | confidence: verified-in-code
- files: internal/services/adapters_test.go, internal/httpx/httpx_test.go, internal/services/helpers_test.go

services/adapters_test.go:60 `fixtureClient` builds `&http.Client{Transport: transport}` directly. Every adapter fixture test therefore runs with no limiter, no retry, no pacing and no releaseOnClose wrapper. The only tests that use httpx.NewClient are live-network ones gated behind tests.RequireNetwork: services/helpers_test.go:39, companies/oxide_test.go:17, companies/uber_test.go:15. internal/httpx/httpx_test.go (928 lines) does test the limiter in isolation — TestPerHostLimitBoundsConcurrency:560, TestPerHostLimitIsPerHostNotGlobal:623, TestPerHostLimitReleasesSlotOnError:679, TestServiceLimiterPacesAndCapsLongCooldown:750 — but every one of those closes its bodies, which is exactly why the Workday leak survives a green `go test ./...`.

There are no Benchmarks anywhere in the repo and no pprof wiring (grep for `func Benchmark` and `pprof` returns nothing).

### The per-source instrumentation needed to prove an improvement already exists and shipped one commit ago — it has simply never produced data
- impact: high | effort: small | confidence: verified-in-code
- files: internal/services/observe.go, crawl_report.go, .github/workflows/track_jobs.yml, docs/architecture-roadmap.md

services/Observe (services/observe.go:48) wraps every source and records SourceRun{Platform, Key, Company, Status, StartedAt, FinishedAt, DurationMS, Postings, Errors, ErrorClass} (observe.go:31-42), emitting `source.start` / `source.finish` slog events (observe.go:99, 159). crawl_report.go:15-54 serialises that into a schema-v1 manifest, and track_jobs.yml already renders a "Slowest sources" table of the top 15 by duration_ms and uploads crawl-manifest.json as an artifact with 14-day retention.

But: the last completed scheduled run (30198816666, head_sha 6549410) predates commit 5e4a615 "Record bounded crawl snapshots with diagnostics", so `list_workflow_run_artifacts` returns total_count 0 and the log contains no source.finish lines (it ran at the default `--log-level=warn`). The current master workflow passes `--log-level=info --log-format=json --manifest=crawl-manifest.json`, so the NEXT run will produce the full per-source duration table. Run 30210637964 (workflow_dispatch on 5e4a615) was in_progress at the time of this analysis and should be the first to carry it.

What the manifest still lacks for this specific investigation: requests issued, retries, 429s, bytes read, time spent blocked on the service semaphore, and time spent in interval pacing — all four of which are exactly what docs/architecture-roadmap.md:96-99 already specifies as the `http.retry` / `http.rate_limited` events.

### servicePolicyFor omits two known shared multi-tenant backends and its min() caps are inert at the current default
- impact: medium | effort: small | confidence: verified-in-code
- files: internal/httpx/httpx.go, internal/services/ashbyhq.go, internal/services/jobvite.go

internal/httpx/httpx.go:242-281. The switch names apply.workable.com, ats.rippling.com, jobs.gem.com, api.smartrecruiters.com, api.lever.co, boards-api.greenhouse.io, *.peopleforce.io, *.bamboohr.com, *.jibeapply.com. It does NOT name `api.ashbyhq.com` (418 tenants — the second-largest platform) or `jobs.jobvite.com` (33 tenants). Both still get one shared bucket because every tenant is on one host, but with the generic policy: maxConcurrent=4, interval=0 (no pacing at all), cooldown=5s — differing from every named sibling, silently and unintentionally.

Separately, every `policy.maxConcurrent = min(defaultLimit, 4)` branch is a no-op while defaultPerHostLimit is 4 (httpx.go:48); only the `min(defaultLimit, 2)` branches for Workable (line 252) and PeopleForce (line 265) actually bind. If per-service caps are ever tuned, these `min` expressions become the thing that silently prevents any increase.

Also relevant to the roadmap's sharding plan: httpx.go:47 documents that Workday tenant hosts are deliberately left independent, which matches docs/architecture-roadmap.md:139-141 ("Split Workday across a small number of shards because tenant hosts are isolated").

### BambooHR silently drops postings beyond page one and Greenhouse omits descriptions — both are correctness gaps that interact with any perf work
- impact: medium | effort: small | confidence: verified-in-code
- files: internal/services/bamboohr.go, internal/services/greenhouse.go, internal/compensation_text.go

bamboohr.go:139 carries a bare `// TODO: handle pagination if the API supports it`; the adapter issues one GET to `https://{company}.bamboohr.com/careers/list` and yields `doc.Result` (bamboohr.go:118). `bambooInfo.Meta.TotalCount` is decoded (bamboohr.go:74-76) and used only as a zero-check at line 113 — if totalCount exceeds len(Result), those postings are lost. 55 sources affected. This is cheap to fix and will INCREASE request count, so it must be budgeted alongside the speed work rather than discovered later.

Similarly greenhouse.go:699 notes `?content=true` would add descriptions in the same request — relevant to the "more data per posting" task, and it would materially increase response sizes on the single busiest host (647 sources on boards-api.greenhouse.io). Flagging it here because it will make the tail measured in this analysis worse.

Neither ParseCompensationFromText (compensation_text.go:192) nor ParseCompensationFromDescription (compensation_markup.go:37) has a non-test caller today, so description parsing contributes zero CPU to the current crawl — that will change if descriptions start being fetched.

## Recommended plan

1. Fix the Workday slot leak. Extract `workdayPage(ctx, httpClient, cxsURL, rawURL string, limit, offset int) (*workdayInfo, error)` in internal/services/workday.go that builds the POST, does the request, checks status, decodes, and closes the body before returning — mirroring leverPage (lever.go:218) and jibePage (jibe.go:155). Replace the body of the `for {}` loop at workday.go:315-369 with a call to it, deleting the `defer resp.Body.Close()` at line 331.
   rationale: Verified by direct experiment that the current code blocks on the 5th page of every Workday tenant until Client.Timeout (2 min) and yields at most 80 postings. This is simultaneously the single largest silent wall-time sink (~25 min of the 75-min budget at 16 workers across ~200 large tenants) and a coverage bug on 216 sources. Smallest possible diff, largest single win.
   files: internal/services/workday.go

2. Add a hermetic regression test that would have caught it: in internal/httpx/httpx_test.go, drive `httpx.NewClient(WithTransport(stub))` through more than `defaultPerHostLimit` sequential requests to one host WITHOUT closing the bodies, and assert the call fails fast or that a companion test with proper Close completes. Separately, add a Workday fixture test in internal/services/adapters_test.go covering >4 pages, and change (or add alongside) `fixtureClient` so at least the paginating adapters run through `httpx.NewClient(httpx.WithTransport(transport))` rather than a bare http.Client.
   rationale: adapters_test.go:60 currently bypasses httpx entirely, and every existing httpx limiter test closes its bodies, which is precisely why `go build`, `go vet` and `go test ./...` all pass while the crawl is broken. Without this the same class of bug recurs in the next adapter.
   files: internal/httpx/httpx_test.go, internal/services/adapters_test.go

3. Decouple worker count from CPU count. Change internal/all.go:27 `DefaultConcurrency` to a flat network-appropriate value (start at 64) or read it from an env var, and pass an explicit `--concurrency` in .github/workflows/track_jobs.yml. Do not raise any per-service cap in this step.
   rationale: Verified: the formula yields 16 on a 4-vCPU ubuntu-latest runner, so CI runs at half the documented ceiling for 1,953 sources. This is PRESSURE-NEUTRAL by construction — httpx.hostLimiter (httpx.go:341) gates the actual per-backend request rate, and extra workers only stop the pool from idling while workers are parked on service semaphores. It satisfies docs/architecture-roadmap.md:31 exactly: more concurrency, no more pressure on any service.
   files: internal/all.go, .github/workflows/track_jobs.yml

4. Build the offline crawl benchmark before touching pagination. Add internal/services (or a new internal/crawlbench) `latencyTransport` implementing http.RoundTripper: it matches a URL pattern, sleeps a configured duration, and returns a synthetic page whose size and total-count field are parameterised (e.g. fedex=138,000 postings at 100/page). Wire it through `httpx.NewClient(httpx.WithTransport(...))` so the real limiter, pacing, retry and releaseOnClose code paths execute. Then add `BenchmarkFullCrawl` that runs `internal.AllWithConcurrency(ctx, client, N, services.JobsFuncs(services.SourcesMatching(nil))...)` against it and reports wall time, plus a synthetic-profile variant with realistic per-platform source counts (greenhouse 647, ashby 418, workday 216, lever 161, rippling 128, jibe 71, workable 67, bamboohr 55, smartrecruiters 54, gem 51, peopleforce 37, jobvite 33, phenom 15).
   rationale: There is no live network in this environment and no benchmark anywhere in the repo. Deterministic simulated latency makes scheduling changes measurable and repeatable, and it is the only way to compare 'sequential pagination' against 'bounded parallel pagination' before spending a 350-minute CI slot on it. The existing fixture transport is not sufficient because it bypasses httpx.
   files: internal/services/adapters_test.go, internal/httpx/httpx_test.go, internal/all_test.go

5. Implement bounded parallel pagination behind a shared helper, starting with Workday and Jibe. Fetch page 0, read the total (`workdayInfo.Total` at workday.go:278, `jibeJobs.TotalCount` at jibe.go:120, `smartRecruitersJobs.TotalFound` at smart_recruiters.go:78), compute the remaining offsets, and fetch them with a per-source worker limit: 4 for tenant-isolated hosts (Workday, Phenom), 2 for shared-key hosts (jibeapply.com, api.smartrecruiters.com, api.lever.co). Preserve the iterator contract: postings must still stream out and `yield` returning false must cancel the in-flight page fetches. Do NOT raise any value in servicePolicyFor.
   rationale: Workday's page size is capped at 20 by the provider, so parallel offsets are the only available speedup there; FedEx-on-Jibe needs ~1,380 strictly sequential requests today. Crucially this uses parallelism the project has ALREADY declared safe: a single source currently occupies 1 of its service's 4 slots, so filling that existing budget does not exceed policy on isolated hosts. On shared-key hosts it redistributes rather than increases the budget — hence the lower per-source cap of 2, so one huge tenant cannot starve its 70 siblings.
   files: internal/services/workday.go, internal/services/jibe.go, internal/services/smart_recruiters.go, internal/services/phenom.go, internal/services/lever.go

6. Make scheduling longest-job-first instead of round-robin-only. Extend `interleaveSources` (services/builtin.go:119) to accept an optional per-source cost estimate, sourced from a committed rolling median of `duration_ms` from crawl-manifest.json, and sort each platform group by descending estimate before interleaving. Keep the platform round-robin as the outer loop so the opening wave still spreads across backends. Fall back to today's registry order when no history exists.
   rationale: Purely a reordering — zero change in total requests or per-backend rate, so it is unambiguously pressure-neutral. It directly attacks the tail: I measured that the last 400 scheduled sources today are exclusively greenhouse (315) and ashby (85), while fedex-on-jibe (the likely longest single source) starts at index 295 by luck rather than design. docs/architecture-roadmap.md:133-150 already calls for exactly this cost model for its sharding plan, so the estimate is reusable.
   files: internal/services/builtin.go, crawl_report.go

7. Extend the manifest with the counters needed to attribute wall time. Add to services.SourceRun (observe.go:31): Requests, Retries, RateLimited, BytesRead, SemaphoreWaitMS, PacingWaitMS. Expose them from httpx via a per-request callback or a context-attached collector — httpx.hostLimiter.RoundTrip (httpx.go:341) already knows the semaphore wait and the pacing wait, and retryTransport (httpx.go:495) already knows retries and 429s. Add a per-platform aggregate section to crawlManifest (crawl_report.go:15) and extend the track_jobs.yml step summary to print per-platform wall time, request counts, and the set of sources still `running` at the deadline.
   rationale: The 07-26 failure log is nearly silent — only two warnings in 75 minutes — so today there is no way to attribute the missing 72 minutes. docs/architecture-roadmap.md:96-99 already specifies http.retry and http.rate_limited events, and observe.go plus crawl_report.go already carry per-source timing; this closes the last gap. 'Sources still running at the deadline' is the single most diagnostic field and costs one loop over manifest.Sources.
   files: internal/services/observe.go, internal/httpx/httpx.go, crawl_report.go, .github/workflows/track_jobs.yml

8. Add a service-shedding policy to httpx. Give limitState a `deadUntil time.Time` (or a per-run `shed bool`) set when a 429's Retry-After exceeds a configurable threshold relative to the crawl deadline; hostLimiter.RoundTrip (httpx.go:341) then returns a typed error immediately for that service key instead of queueing. Also plumb `httpx.WithPerHostLimit` (httpx.go:140) through globalFlags.client (main.go:82) as an advanced flag so per-service caps become A/B-testable in CI.
   rationale: This REDUCES request pressure — it is the polite option, not the aggressive one. Note the correction to the working assumption: the 3510s and 3480s Retry-After values from peopleforce.io are already clamped to 30s by limitState.penalize (httpx.go:325-338) and retryTransport.backoff (httpx.go:629-648), so they are not costing 58 minutes; what they cost is four 30s retry rounds inside a 2-slot limiter shared by 37 tenants. Shedding turns that into an immediate, honest failure. Sizing it below the other items because PeopleForce is only 37 of 1,953 sources.
   files: internal/httpx/httpx.go, main.go

9. Cheap allocation and memory cleanups, batched into one change: replace html.Parse in rippling.go:209 with the strings.Index + json.Decoder pattern already used in phenom.go:96-110; stream the marker scan in phenomPage instead of io.Copy-ing the whole page into a strings.Builder (phenom.go:100-104); key internal.Dedupe's `seen` map (filter.go:239) on a 128-bit hash of the URL rather than the URL string; and give internal/all.go:59's `results` channel a small buffer.
   rationale: All four are zero-request-pressure changes. The Rippling one is 128 full HTML tree builds of multi-megabyte Next.js pages replaced by a substring search. The Dedupe change drops ~70-80 MB at 473k postings and matters more once Workday coverage is restored and counts exceed 600k. Be honest in the commit message that the channel buffer is jitter, not throughput: at ~100-300ns per handoff, 473k postings is well under a second in aggregate.
   files: internal/services/rippling.go, internal/services/phenom.go, internal/filter.go, internal/all.go

10. Fix BambooHR pagination (bamboohr.go:139 TODO) using the already-decoded Meta.TotalCount (bamboohr.go:74-76), and give api.ashbyhq.com and jobs.jobvite.com explicit entries in servicePolicyFor (httpx.go:242-281) with deliberate interval and cooldown values rather than the accidental generic policy.
   rationale: BambooHR silently truncates 55 sources today and will add requests once fixed, so it should land while the timing baseline is being re-measured rather than after. Ashby is the second-largest platform (418 sources) and currently runs with interval=0 — no pacing at all — purely because it was never named in the switch; making that explicit is a prerequisite for any informed decision about its concurrency.
   files: internal/services/bamboohr.go, internal/httpx/httpx.go

## Risks

- Fixing the Workday slot leak will sharply INCREASE the crawl's request volume: 216 Workday sources currently stop after 4 pages, and Workday's provider-imposed page size is 20. Restoring full coverage on large tenants (FedEx, Boeing, HCA, CVS, Home Depot, Target) could add tens of thousands of requests and make Workday the new tail. It must ship together with the parallel-pagination work and a re-measured baseline, or the crawl will get slower before it gets faster.
- Posting counts will jump materially once Workday is fixed. jobs_record.txt already faces a ~35x coverage step change from the old scraper era (13,467 → 473,385); this adds a second discontinuity on top of it. The trend line needs an explicit coverage-epoch marker or the graph becomes misleading — docs/architecture-roadmap.md:25-28 already treats this class of corruption as a non-negotiable invariant.
- Parallel offset pagination against a list that mutates between requests can skip postings, not just duplicate them. Dedupe (filter.go:237) covers duplicates but nothing detects a miss. Mitigate by reading the total from page 0, re-reading it on the final page, and refetching if it changed — and accept that a boards' ordering may not be stable at all, in which case parallel offsets are unsafe for that platform.
- Raising the global worker count is pressure-neutral only as long as every hot backend actually has a servicePolicyFor entry. api.ashbyhq.com (418 sources) and jobs.jobvite.com (33) currently fall through to the generic policy with interval=0, so more workers WOULD increase their request rate. Add explicit policies for both before raising concurrency, or the change silently violates docs/architecture-roadmap.md:31.
- Every wall-time attribution in this analysis beyond the Workday stall is arithmetic, not measurement. The 07-26 failing run predates the manifest (head_sha 6549410 vs 5e4a615), its artifact list is empty, and it ran at --log-level=warn so it emitted no source.finish events. The FedEx 23-69 minute estimate in particular is inferred from 138,000 postings / 100 per page × an assumed 1-3s per request. Get one real manifest before committing to a plan ordering.
- Bounded parallel pagination on a shared limiter key (jibeapply.com, api.lever.co, api.smartrecruiters.com) does not increase total pressure but does let one huge tenant monopolise the platform's 4 slots, starving its siblings and potentially making the platform's own tail worse. Per-source parallelism on shared keys must be capped below the service cap (suggest 2 of 4).
- Higher concurrency raises peak memory and file-descriptor use on a 16 GB / default-ulimit runner, and Dedupe's map already retains ~70-80 MB. Combined with the Greenhouse ?content=true work from the separate 'more data per posting' task, memory could become the next failure mode rather than time.
- Nothing in this analysis was verified against a live job board — outbound HTTP to third parties is blocked from this container. The Workday limit<=20 cap, the SmartRecruiters default limit of 100, and the Lever max limit of 100 come from documentation and web search, not from a probe. Confirm each in GitHub Actions before relying on it.