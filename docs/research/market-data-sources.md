# recon:market-data

## Summary

The repo is a stateless, single-binary crawler: `internal.Jobs = iter.Seq2[*JobPosting, error]` (internal/all.go:15) with two existing stream decorators (`Filter.Apply` internal/filter.go:159, `Dedupe` internal/filter.go:237) and a lifecycle decorator (`services.Observe` internal/services/observe.go:47). `go run . companies` reports 1,929 distinct companies today. There is no existing enrichment, cache, or embed anywhere in the tree (grep for enrich/edgar/wikidata/onet/naics returns only false positives from tenant slugs like "catonetworks").

Of the candidate sources, five are genuinely free, key-free, canonical and useful here: SEC EDGAR (submissions + XBRL + company_tickers.json + nightly bulk zips), DOL OFLC LCA/PERM disclosure bulk files, BLS OEWS flat files, O*NET's database text files (including the ~55k-row Alternate Titles file), and Wikidata. USAspending, ProPublica Nonprofit Explorer, USCIS H-1B Data Hub and GLEIF are key-free too but narrower. BLS API v2 and O*NET Web Services both require registration and are disqualified; their bulk-file equivalents are not, which is the whole reason to prefer flat files.

The single most important architectural conclusion is that enrichment must NOT be live per-query HTTP. The nightly crawl already fails its 75-minute budget at 473,385 postings / 1,772 companies; adding ~1,900 more third-party lookups to that path makes a known-broken workflow strictly worse, and `data.sec.gov` caps at 10 req/s across all EDGAR hosts, which would serialize into minutes of added wall time. Everything should be a generated, reviewed, `go:embed`-ed table with zero request cost at query time, refreshed by a separate generator that runs in GitHub Actions (which has real network access) — this also satisfies docs/architecture-roadmap.md:11 ("no required state") and :36 ("no CGO requirement").

Two concrete blockers exist in the current code. First, `httpx.DefaultUserAgent` (internal/httpx/httpx.go:28) carries no contact email; SEC's documented policy requires `User-Agent: Sample Company Name AdminContact@<domain>.com` and Wikimedia 403s generic agents, so a separate client via `httpx.WithUserAgent` (httpx.go:93) is mandatory. Second, `servicePolicyFor` (httpx.go:242) has no branch grouping `www.sec.gov`/`data.sec.gov`/`efts.sec.gov` under one key with a pacing interval, so the generic policy (4 concurrent, interval 0) would blow the 10 req/s cap and earn a ~10-minute IP block.

The second blocker is a data-model gap: `internal.JobPosting` (internal/job_posting.go:5-22) carries only a `Company` display string — no platform, no ATS key — while the enrichment join key must be the roadmap's `platform + key` stable integration ID (architecture-roadmap.md:78). So enrichment has to attach at the `services.Source` level, wrapping `JobsFunc` exactly the way `services.Observe` already does, not downstream on the posting stream. The real cost of this whole capability is entity resolution (ATS slug "andurilindustries" → legal entity → CIK / OFLC employer name), which should be done once offline into a reviewed table, not fuzzy-matched at runtime.

Build order: land the mechanism (generator + embedded table + Source-level attach + filter flags) with SEC EDGAR as the first small, easily-verified payload; then land O*NET SOC/title mapping, which is the join key everything else needs; then OFLC employer wage benchmarks, which is the highest user value because docs/compensation.md:74-79 shows only 48.2% pay extraction on Greenhouse and structured pay is rare everywhere else. Wage benchmarks must live in a new field, never in `Compensation`, because docs/compensation.md:20 states "Nothing blends sources".

## Findings

### SEC EDGAR submissions API: key-free legal entity, SIC industry, state, public status
- impact: high | effort: small | confidence: documented
- files: internal/enrich/edgar/edgar.go

GET https://data.sec.gov/submissions/CIK{cik10}.json where {cik10} is the CIK zero-padded to 10 digits. Documented top-level fields include: cik, entityType, sic, sicDescription, name, tickers[], exchanges[], ein, description, website, investorWebsite, category, fiscalYearEnd, stateOfIncorporation, stateOfIncorporationDescription, addresses, phone, formerNames[], and filings.recent (parallel columnar arrays sharing an index). No API key, no auth. Answers: is this employer a public company; what industry (4-digit SIC) is it in; where is it incorporated; what is its legal name and any former names; how recently did it file. Confidence: endpoint shape and field list are documented via secondary sources plus sec.gov's own API page; NOT live-probed (container has no egress).

### SEC EDGAR XBRL: companyfacts / companyconcept / frames give revenue and headcount-adjacent signals
- impact: high | effort: medium | confidence: documented
- files: internal/enrich/edgar/edgar.go

Three endpoints, all key-free: (1) https://data.sec.gov/api/xbrl/companyfacts/CIK{cik10}.json — every XBRL fact the filer ever tagged; (2) https://data.sec.gov/api/xbrl/companyconcept/CIK{cik10}/{taxonomy}/{concept}.json e.g. taxonomy=us-gaap, concept=Revenues or RevenueFromContractWithCustomerExcludingAssessedTax; (3) https://data.sec.gov/api/xbrl/frames/{taxonomy}/{concept}/{unit}/CY{year}{period}.json e.g. .../us-gaap/Revenues/USD/CY2024Q1I.json — one concept across ALL filers for a period, which is the cheapest way to build a cross-company revenue column in a single request instead of 1,929. Note headcount is NOT a us-gaap concept; dei:EntityNumberOfEmployees exists but is sparsely tagged, so employee count from EDGAR should be treated as best-effort, and Wikidata used as the fallback. Frames is the right primitive for a vendored table generator.

### SEC company_tickers.json is the seed mapping; nightly bulk zips avoid 1,900 requests entirely
- impact: high | effort: small | confidence: documented
- files: internal/enrich/edgar/edgar.go

https://www.sec.gov/files/company_tickers.json — ~10,000 entries of {cik_str, ticker, title}, CIK without leading zeros. Also https://www.sec.gov/files/company_tickers_exchange.json which adds exchange. Bulk: https://www.sec.gov/Archives/edgar/daily-index/bulkdata/submissions.zip (all filers' submissions JSON) and https://www.sec.gov/Archives/edgar/daily-index/xbrl/companyfacts.zip (~1 GB, all XBRL facts). Both documented as recompiled nightly. For a generator that runs in GitHub Actions, downloading company_tickers.json (small) plus a handful of frames calls is far cheaper than the 1 GB companyfacts.zip; recommend company_tickers.json + per-CIK submissions for the ~300-500 matched public employers, paced at <10 req/s.

### SEC EDGAR full-text search (efts.sec.gov) is key-free but is the wrong tool for this project
- impact: low | effort: small | confidence: documented
- files: 

GET https://efts.sec.gov/LATEST/search-index?q=%22term%22&forms=10-K&dateRange=custom&startdt=YYYY-MM-DD&enddt=YYYY-MM-DD&from=0 — undocumented but public, returns raw Elasticsearch JSON, covers filings from 2001 to present, path casing /LATEST/ matters (/latest/ 404s), no key. It counts against the same 10 req/s EDGAR budget. It answers 'which filings mention X', which is a research question, not a job-search question. Do not build on it in the first pass; it is a poor entity-resolution tool compared with company_tickers.json name matching.

### BLOCKER: httpx.DefaultUserAgent has no contact email, which violates SEC and Wikimedia policy
- impact: critical | effort: small | confidence: verified-in-code
- files: internal/httpx/httpx.go

internal/httpx/httpx.go:28 sets DefaultUserAgent = "job-hunter-toolkit/1.0 (+https://github.com/job-hunter-toolkit/job-hunter-toolkit)". SEC's documented requirement is the form `User-Agent: Sample Company Name AdminContact@<sample company domain>.com`; requests lacking a proper UA are rejected. Wikimedia's UA policy 403s empty or generic agents and asks for an email or full URL. The fix is already available: httpx.WithUserAgent (internal/httpx/httpx.go:93) — build a SEPARATE enrichment client rather than changing the crawler UA, so job-board behavior is untouched. The generator, not the CLI, is what makes these calls, so the contact address can be a repo-owned one.

### BLOCKER: servicePolicyFor has no sec.gov branch, so EDGAR calls would exceed 10 req/s and earn a 10-minute block
- impact: critical | effort: small | confidence: verified-in-code
- files: internal/httpx/httpx.go

internal/httpx/httpx.go:242 servicePolicyFor keys on strings.ToLower(req.URL.Host) and defaults to maxConcurrent=defaultPerHostLimit (4, httpx.go:48) with interval=0 — i.e. unbounded request rate. SEC documents a hard cap of 10 requests/second per IP applied ACROSS all EDGAR domains (www.sec.gov, data.sec.gov, efts.sec.gov), enforced by a 403 and roughly a 10-minute IP block. The existing peopleforce.io branch (httpx.go:263-267) is the exact template: collapse all three SEC hosts to one key (e.g. "sec.gov") with maxConcurrent 1-2 and interval >= 100ms, giving <=10 req/s deterministically. Same pattern needed for query.wikidata.org (documented: 60s query time per minute, 5 concurrent per IP, HTTP 429 then 403 ban for non-backing-off clients).

### DOL OFLC disclosure data is the strongest key-free employer-level wage benchmark that exists
- impact: critical | effort: large | confidence: documented
- files: internal/enrich/oflc/oflc.go

Landing page: https://www.dol.gov/agencies/eta/foreign-labor/performance. Publishes per-fiscal-year, cumulative-per-quarter Excel disclosure files for LCA (H-1B/H-1B1/E-3), PERM, PWD, H-2A, H-2B, CW-1. Each row is one certified application and carries employer name, job title, SOC/O*NET code and title, wage offered (WAGE_RATE_OF_PAY_FROM/TO with WAGE_UNIT_OF_PAY), the DOL PREVAILING_WAGE and wage level (I-IV), worksite city/state, and full-time flag — 75+ columns. Record layouts are published alongside as PDFs; the URL pattern https://www.dol.gov/sites/dolgov/files/ETA/oflc/pdfs/LCA_Record_Layout_FY2025_Q3.pdf and .../LCA_Record_Layout_FY2024_Q4.pdf were both observed as live search-result URLs, which establishes the /ETA/oflc/pdfs/ directory convention. Cadence: quarterly, cumulative within a fiscal year (FY runs Oct 1 - Sep 30); Q1 FY26 (Oct 1 2025 - Dec 31 2025) and Q2 FY26 (through Mar 31 2026) are already out. Public-domain US government work. This is the ONLY source here that gives you actual employer x role x location pay for private companies. CAVEAT I could not verify: the exact .xlsx filename for the data files. The generator should parse the performance page for hrefs rather than hardcode a filename.

### OFLC wage data must be aggregated offline, not shipped raw — and must never be written into Compensation
- impact: critical | effort: medium | confidence: verified-in-code
- files: internal/job_posting.go, docs/compensation.md

Raw LCA is hundreds of thousands of rows per fiscal year in .xlsx; that cannot be embedded in a portable binary. Aggregate offline to (employer_key, soc_code, state) -> {n, p25, p50, p75 of annualized WAGE_RATE_OF_PAY_FROM}, restricted to the ~1,929 employers this repo already crawls. Estimated size: if each matched employer has ~30 distinct SOC x state cells, that is ~50k rows, roughly 2-3 MB TSV, well under 1 MB gzipped. CRITICAL CONSTRAINT: docs/compensation.md:20 states "Nothing blends sources" and job_posting.go:56-78 documents Compensation as what the EMPLOYER PUBLISHED with the posting. A DOL benchmark is a third-party estimate about a different application, so it must land in a new field (e.g. Employer.WageBenchmark), never in JobPosting.Compensation, and must not participate in --min-pay. Adding a fourth Provenance value would be wrong for the same reason.

### BLS: API v1 is key-free but useless at scale; the OEWS flat files are the correct key-free path
- impact: high | effort: medium | confidence: documented
- files: internal/enrich/bls/oews.go

BLS Public Data API v1 (https://api.bls.gov/publicAPI/v1/timeseries/data/) requires no registration but is documented as hitting its daily cap in ~10 requests. API v2 gives 500 requests/day and 50 series/request but REQUIRES a free registration key — disqualified under the user's constraint. The flat files are not: OEWS ZIPs live under the OEWS special-requests directory, e.g. https://www.bls.gov/oes/special-requests/oesm23ma.zip and https://www.bls.gov/oes/special.requests/oesm24all.zip were both observed as live search-result URLs (note BOTH a hyphen and a dot form of the directory appear in the wild — resolve the real href from https://www.bls.gov/oes/tables.htm rather than hardcoding). Content: employment and wage estimates (mean, and 10/25/50/75/90th percentiles) by 6-digit SOC, at national, state, and metro/nonmetro levels; ~830 detailed occupations. Cadence: annual, released each spring for the prior May reference period (May 2024 estimates current; May 2025 pages already exist). Answers: 'is this posting's pay above or below market for this occupation in this metro'.

### BLS OEWS embedding budget: national+state fits the binary, metro does not
- impact: medium | effort: medium | confidence: inferred
- files: docs/architecture-roadmap.md

National cross-industry is ~830 SOC rows. State adds ~51x that (~42k rows) — a few hundred KB gzipped, fine to embed. Metro/nonmetro is ~400 areas x 830 SOC = up to ~330k rows, which is tens of MB raw and several MB compressed; that is too much for a portable single binary that also must stay CGO-free per docs/architecture-roadmap.md:36. Recommendation: embed national + state by default, and put metro behind an optional on-disk cache the user opts into (e.g. --enrich-cache ~/.cache/job-hunter-toolkit), preserving the 'no required state' invariant.

### O*NET: the DATABASE download is CC BY 4.0 and key-free; Web Services is NOT covered and requires registration
- impact: high | effort: medium | confidence: documented
- files: internal/enrich/onet/onet.go

Two distinct products with different terms. (1) O*NET Database text files: current production release is 30.3, distributed as tab-delimited text (also Excel and MySQL/MSSQL/Oracle SQL) from https://www.onetcenter.org/database.html; archive at https://www.onetcenter.org/db_releases.html. The database CONTENT is licensed CC BY 4.0 (https://www.onetcenter.org/license_db.html) — attribution required, redistribution allowed. (2) O*NET Web Services (services.onetcenter.org) requires a registered account, and its own license page states the CC license does NOT cover data returned through the APIs. So: use the bulk download, do not use the API. Cadence: roughly quarterly point releases (30.x), annual major. Path pattern observed live: https://www.onetcenter.org/dl_files/database/db_27_2_text/Alternate%20Titles.txt, implying https://www.onetcenter.org/dl_files/database/db_30_3_text.zip for the full archive.

### O*NET Alternate Titles is the join key that makes every wage benchmark usable
- impact: critical | effort: medium | confidence: documented
- files: internal/enrich/onet/titles.go

File: 'Alternate Titles.txt', 4 tab-delimited columns — O*NET-SOC Code, Alternate Title, Short Title, Source(s). Row counts by release: 55,094 (28.1), 55,024 (28.3), 60,234 (22.3). This is the lay-title-to-SOC dictionary the DOL built specifically to power keyword search in O*NET OnLine and Code Connector. Without it you cannot turn 'Senior Application Security Engineer' into 15-1212.00 and therefore cannot look up either OEWS or OFLC. Size estimate: ~55k rows x ~45 bytes ≈ 2.5 MB raw, ~600 KB gzipped — embeddable. Also worth pulling from the same archive: Occupation Data.txt (SOC code + title + description), Job Zones (education/experience level 1-5), Skills.txt / Knowledge.txt / Tasks.txt for qualification matching. Matching strategy: normalize (lowercase, strip seniority prefixes like senior/staff/principal/lead/sr., strip roman numerals and Roman/Arabic level suffixes, strip parenthetical location), then exact match, then longest-token-overlap fallback, and record a confidence so a bad match is visible rather than silent.

### Wikidata covers the private companies EDGAR cannot: employee count, founding, HQ, parent, industry
- impact: high | effort: medium | confidence: documented
- files: internal/enrich/wikidata/wikidata.go

Two key-free access paths. (1) SPARQL: POST/GET https://query.wikidata.org/sparql?query=...&format=json — documented limits are 60s wall-clock per query (503 on exceed), 60s of query time per minute per IP+UA (burst 120s), 5 concurrent queries per IP, 30 errors/min (burst 60); non-compliant UAs are blocked outright and clients ignoring 429 get escalated to a 403 ban. (2) Entity JSON: https://www.wikidata.org/wiki/Special:EntityData/Q95.json for a single item. Relevant properties: P452 industry, P571 inception, P159 headquarters location, P1128 employees, P749 parent organization, P414 stock exchange, P414/P249 ticker, P5531 CIK, P946/P1278 identifiers. License is CC0 — the most permissive of any source here. Best used as a SINGLE bulk SPARQL query in the generator (fetch all companies with a CIK or a ticker, plus employees/inception/industry) rather than 1,929 item fetches. Wikimedia-wide guidance for unauthenticated clients: keep concurrency to 3 and stay under ~5 req/s.

### USCIS H-1B Employer Data Hub: per-employer approval/denial counts, key-free CSV
- impact: medium | effort: small | confidence: documented
- files: 

https://www.uscis.gov/tools/reports-and-studies/h-1b-employer-data-hub with bulk per-fiscal-year CSV/Excel at https://www.uscis.gov/archive/h-1b-employer-data-hub-files. Coverage FY2009 through FY2026 Q3. Fields include employer name, city, state, ZIP, NAICS code, initial/continuing approvals and denials. Cadence: quarterly. Complements OFLC: OFLC tells you the wage an employer certified, USCIS tells you whether that employer actually gets petitions approved. Answers 'does this employer sponsor, and how reliably'. Cheap to add once the OFLC employer-name normalizer exists, because it needs the same normalization.

### USAspending.gov: key-free federal award totals per recipient
- impact: medium | effort: small | confidence: documented
- files: 

POST https://api.usaspending.gov/api/v2/search/spending_by_award/ with a JSON body of {subawards, limit, page, filters:{recipient_search_text:["Lockheed Martin"], award_type_codes:[...], time_period:[{start_date,end_date}]}}. Also /api/v2/search/spending_by_award_count/ and recipient endpoints. Explicitly public, no API key. Full OpenAPI-style contracts are in the fedspendingtransparency/usaspending-api GitHub repo under usaspending_api/api_contracts/contracts/v2/. Rate limits are not publicly documented, which argues for conservative pacing in the generator. Answers 'is this employer a federal contractor / how dependent are they on government revenue' — genuinely useful for defense, aerospace and health-system employers already in this repo's coverage, but narrow overall. Low priority.

### IRS Form 990 via ProPublica Nonprofit Explorer: key-free, and more relevant than it looks
- impact: medium | effort: small | confidence: documented
- files: 

Base https://projects.propublica.org/nonprofits/api/v2, GET /search.json?q={name}&state[id]={XX} and GET /organizations/{ein}.json. No API key, no auth, JSONP supported via callback. Returns EIN, NTEE code, revenue/expenses/assets by filing year, and links to the underlying 990s. The README states coverage 'spans hospital systems, universities' — those are overwhelmingly 501(c)(3)s with no SEC filings at all, so 990 data is the ONLY financial signal available for a meaningful slice of this repo's employers. Caveat on canonicality: ProPublica is a news organization redistributing IRS data, not the primary source; the canonical alternatives are IRS Exempt Organizations Business Master File extracts and the IRS 990 e-file XML index (both key-free but far more work to parse). Recommend ProPublica for the first pass, with the IRS source named in a comment as the fallback of record.

### GLEIF LEI: key-free entity resolution and corporate hierarchy, useful as a matcher not as a feature
- impact: low | effort: small | confidence: documented
- files: 

https://api.gleif.org/api/v1/lei-records with filters; documented at 60 requests/minute per user, no authentication, no charge, up to 200 records per request. Returns legal name, registered and HQ addresses, entity status/category, legal form, jurisdiction, BIC codes, and parent/child corporate hierarchy. Its value here is not user-facing — it is a way to resolve 'Anduril Industries, Inc.' vs 'Anduril Industries Inc' vs 'ANDURIL INDUSTRIES INC' across EDGAR, OFLC and USCIS, which all spell employer names differently. Consider it a tool for the offline entity-resolution step, not a data source to embed.

### ESCO (EU): key-free occupation/skill taxonomy, but wrong hemisphere for this repo's first pass
- impact: low | effort: medium | confidence: documented
- files: 

https://ec.europa.eu/esco/api — no authentication key required; versions 1.0.9 (default), 1.1.2, 1.2.0; bulk download also available from https://esco.ec.europa.eu/en/use-esco/download. Every ESCO occupation maps to exactly one ISCO-08 code, and the skills pillar is a full knowledge/skill/competence taxonomy. It is the right taxonomy for European postings and the right bridge if this project ever needs ISCO. But OFLC and OEWS — the two wage sources that actually matter — are both keyed on SOC, not ISCO, so O*NET/SOC must come first. Add ESCO only when non-US coverage becomes a goal.

### Explicitly DISQUALIFIED under the no-key constraint
- impact: medium | effort: small | confidence: documented
- files: 

BLS Public Data API v2 (free registration key required; v1 is keyless but caps out in ~10 requests/day). O*NET Web Services (registered account required, and its returned data is explicitly outside the CC BY 4.0 database license). SAM.gov entity API (key required). OpenCorporates (key required, and now commercially gated). Companies House UK (key required). Census Bureau API (works keyless for low volume but formally asks for a free key above 500 queries/day — treat as borderline, and prefer the Census bulk County Business Patterns flat files if industry/size data is ever needed). Crunchbase, PitchBook, LinkedIn, Glassdoor, Levels.fyi: all key-gated and/or ToS-hostile to redistribution — out of scope entirely.

### BLOCKER: JobPosting carries no source identity, so enrichment cannot join on it downstream
- impact: critical | effort: medium | confidence: verified-in-code
- files: internal/job_posting.go, internal/services/builtin.go, docs/architecture-roadmap.md

internal/job_posting.go:5-22 defines JobPosting with only Company, URL, Title, Location, Compensation, Remote. The stable enrichment key must be the roadmap's `platform + key` integration ID (docs/architecture-roadmap.md:78: "Use `platform + key` as the stable integration ID. A separately curated company ID can outlive moves from Greenhouse to Ashby"). That identity exists ONLY on services.Source (internal/services/builtin.go:14-37) and is discarded by services.JobsFuncs (builtin.go:57) before internal.All ever sees it. Joining on JobPosting.Company instead is unsafe: Phenom and Workday derive short display names from hostnames/tenant URLs (multiJobsFuncNamed, builtin.go:180) and services.Companies() dedupes case-insensitively (builtin.go:157-163), so distinct employers can collide on the display string. Therefore enrichment must attach at the Source level.

### The right attach point is a Source-level decorator modeled exactly on services.Observe
- impact: critical | effort: medium | confidence: verified-in-code
- files: internal/services/observe.go, internal/services/builtin.go

internal/services/observe.go:47 has signature `func Observe(sources []Source, logger *slog.Logger) ([]internal.JobsFunc, func() []SourceRun)` — it wraps each Source's Jobs func, closes over per-source identity, and returns plain []internal.JobsFunc for internal.AllWithConcurrency. That is precisely the shape enrichment needs: `func Attach(sources []Source, table *enrich.Table) []internal.JobsFunc`, where the closure looks up table.For(source.Platform, source.Key) ONCE per source (not per posting) and stamps the resulting *enrich.Employer pointer onto every posting it yields. Zero HTTP, one map lookup per source, a shared immutable pointer per posting. It composes with Observe by wrapping in either order. Note the existing wrapper in builtin.go:180-200 also demonstrates the ctx.Err() check that must be preserved.

### Enrichment must be a vendored generated table, not live fetch — the crawl budget forbids anything else
- impact: critical | effort: medium | confidence: inferred
- files: docs/architecture-roadmap.md, internal/enrich/gen/main.go

The nightly Track Jobs workflow already fails: 473,385 postings from 1,772 companies did not finish inside 1h15m. Live enrichment would add ~1,929 lookups to that path, and SEC's 10 req/s ceiling alone makes that >3 minutes of pure serialization even before Wikidata's 5-concurrent limit. Live enrichment also breaks docs/architecture-roadmap.md:11 ("CLI: live job search and source health with no required state") by introducing a network dependency the crawl does not have today. Design: a `//go:build ignore` generator (or a separate cmd/ target) that runs in GitHub Actions, writes internal/enrich/data/*.tsv.gz, and opens a PR; the CLI reads those via go:embed with zero requests. Refresh cadence can be a monthly scheduled workflow — SEC submissions change daily but SIC/state/public-status essentially never do, OFLC is quarterly, OEWS is annual, O*NET is quarterly.

### Proposed internal/enrich package shape, matching existing idioms
- impact: high | effort: medium | confidence: inferred
- files: internal/enrich/enrich.go, internal/enrich/table.go, internal/filter.go

internal/enrich/enrich.go: `type Employer struct { LegalName, CIK, Ticker, Exchange string; SIC, SICDescription string; StateOfIncorporation string; Public bool; Employees int; FoundedYear int; ParentOrg string; LastFilingDate string; FederalAwardsUSD float64; WageBenchmark *WageBenchmark }` and `type WageBenchmark struct { SOCCode, SOCTitle string; Source string /* "oflc"|"oews" */; Area string; N int; P25, P50, P75 float64; AsOf string }`. internal/enrich/table.go: `//go:embed data/employers.tsv.gz` + a sync.OnceValue loader parsing into map[string]*Employer keyed by platform+"\x00"+key (mirroring the \x00 join already used at internal/filter.go:257). internal/enrich/onet: title->SOC matcher over the embedded Alternate Titles table. internal/enrich/{edgar,oflc,bls,wikidata}: generator-only clients. Sub-packages keep generator-only code out of the CLI's import graph, so the shipped binary embeds tables but not HTTP clients it never calls.

### JSON contract: an additive omitempty field is safe, and there is direct precedent
- impact: high | effort: small | confidence: verified-in-code
- files: internal/job_posting.go

Add `Employer *Employer \`json:"employer,omitempty"\`` to internal/job_posting.go. Both Compensation (job_posting.go:15) and Remote (job_posting.go:21) are already pointer+omitempty fields added the same way, so `postings --json` NDJSON consumers that read company/url/title/location are unaffected, and the key is simply absent when a company is not in the table (which will be the common case initially). Do NOT flatten enrichment fields to the top level — a nested object keeps the posting's own facts distinguishable from third-party facts about its employer, which is the same trust boundary docs/compensation.md:9-20 draws for pay provenance.

### CSV contract: append columns, never insert — the codebase states this rule explicitly
- impact: high | effort: small | confidence: verified-in-code
- files: main.go

main.go:286-316 writes a fixed 8-field record with no header: `[]string{j.Company, j.Title, j.Location, j.URL, payMin, payMax, currency, period}` (main.go:305). The comment at main.go:288-289 states the rule: "Pay columns are appended rather than inserted, so anything reading the original four fields keeps working." Follow it: append enrichment columns at the end (suggest ticker, sic, public, soc_code, benchmark_p50), ALWAYS emitted and empty when unknown, so the column count does not vary with whether --enrich was passed. A variable-width CSV would be worse than a wider one. Also note the --csv help text at main.go:229 enumerates the column list and must be updated in lockstep.

### CLI surface: new filter flags on postings plus a standalone lookup subcommand
- impact: high | effort: medium | confidence: verified-in-code
- files: main.go, internal/filter.go, internal/filter_test.go

On `postings` (main.go:132-251): --enrich (opt-in, default off so the default binary behavior is byte-identical), --industry (substring match on SIC description), --public / --private, --min-benchmark (annual, filters on the OFLC/OEWS benchmark for the posting's matched SOC — deliberately a SEPARATE flag from --min-pay at main.go:245, because --min-pay filters on what the employer published and conflating the two would violate docs/compensation.md:20), --soc (filter by matched O*NET-SOC code), --min-employees. These become new fields on internal.Filter (internal/filter.go:18-46) and require updating both Filter.Match (filter.go:105) and Filter.IsZero (filter.go:147) — the 566-line internal/filter_test.go will need matching cases. Also add a new top-level `company` (or `employer`) subcommand alongside companies/total/health (registered at main.go:121-126) that prints the enrichment record for one or more companies with no crawl at all — instant, offline, and the cheapest way to make the table's value visible.

### Entity resolution is the real cost of this project and must be a reviewed artifact, not runtime fuzzing
- impact: critical | effort: large | confidence: inferred
- files: internal/enrich/gen/main.go, docs/compensation.md

The crawler's keys are ATS slugs ('andurilindustries', '2u', 'aha', 'catonetworks', 'onetrust') and Workday tenant URLs; every external source keys on a legal name ('Anduril Industries, Inc.'), a CIK, or an all-caps DOL employer string. Fuzzy-matching that at runtime would silently attach the wrong company's revenue to a posting — a plausible wrong number that looks exactly like a right one, which is the failure mode docs/compensation.md:3-5 was written to prevent. Recommendation: the generator emits candidate matches with a score, a human reviews the low-confidence tail, and the committed table is the reviewed output. Store an explicit match_confidence and match_method column so a bad row is auditable. Expect roughly 15-25% EDGAR coverage of the 1,929 companies (most are private startups on Greenhouse/Ashby/Lever); OFLC will cover materially more because it covers any H-1B-sponsoring private employer.

### Hermetic testing: the existing fixtureTransport pattern covers enrich unchanged
- impact: medium | effort: small | confidence: verified-in-code
- files: internal/services/adapters_test.go, internal/tests/network.go

internal/services/adapters_test.go:18 defines fixtureTransport (a RoundTripper serving canned bodies matched by URL substring, recording request URLs) with fixtureClient at :59, and internal/tests/network.go:11 gates live tests behind JHT_NETWORK_TESTS. Generator clients (edgar, wikidata, oflc parsers) should be tested through exactly this transport with small trimmed fixtures. The embedded-table path needs no transport at all: test enrich.Attach against a hand-built in-memory Table, and add one golden test asserting the committed .tsv.gz parses and has a plausible row count, so a corrupted regeneration fails CI rather than silently shipping an empty table.

### Minor: .gitattributes `* text=auto` can corrupt committed data files
- impact: low | effort: small | confidence: verified-in-code
- files: .gitattributes

.gitattributes line 2 is `* text=auto`. Git's auto-detection normally leaves detected-binary files alone, so a .gz blob is likely safe, but a committed plain .tsv would be LF-normalized on checkout, and any file git mis-detects could be mangled on a Windows clone — which would break a go:embed'd table in a way that only reproduces on one platform. Add explicit `internal/enrich/data/** -text` (or `binary`) when the data directory is created. Cheap insurance.

### Licensing and attribution posture, per source
- impact: high | effort: small | confidence: documented
- files: README.md, docs/compensation.md

Public domain / no restriction (US Government works): SEC EDGAR, DOL OFLC, BLS OEWS, USCIS Data Hub, USAspending, IRS 990 bulk. Attribution required: O*NET database (CC BY 4.0 — must credit O*NET and note any modifications; the license page is https://www.onetcenter.org/license_db.html). Public domain dedication: Wikidata (CC0, no attribution legally required though courteous). Redistributor, not primary source: ProPublica Nonprofit Explorer — check their API terms before redistributing derived tables, or derive from IRS bulk instead. GLEIF: free of charge, but confirm redistribution terms if the LEI data itself is committed rather than used only as a matching aid. Practical consequence: shipping O*NET-derived tables in the binary requires an attribution line in the README and/or `--version` output. Add a docs/enrichment.md that records each embedded table's source, license, retrieval date, and generator command — the same discipline docs/compensation.md already applies to pay.

## Recommended plan

1. Add a sec.gov service policy branch to httpx.servicePolicyFor and build a dedicated enrichment HTTP client with a contact-bearing User-Agent. Group www.sec.gov, data.sec.gov and efts.sec.gov under one limiter key with maxConcurrent 1-2 and interval >= 100ms (mirroring the peopleforce.io branch at internal/httpx/httpx.go:263-267). Do NOT change httpx.DefaultUserAgent; use httpx.WithUserAgent for the enrichment client only.
   rationale: SEC enforces 10 req/s across all EDGAR domains with a ~10-minute IP block, and rejects requests without a contact-form User-Agent. The generic policy today is 4 concurrent with interval 0. Getting this wrong bans the GitHub Actions runner, not a developer laptop.
   files: internal/httpx/httpx.go

2. Define the data model and the embedded-table mechanism before any real data: internal/enrich with Employer + WageBenchmark types, a gzip-TSV Table loaded via go:embed behind sync.OnceValue and keyed by platform+\x00+key, and enrich.Attach(sources []services.Source, t *Table) []internal.JobsFunc modeled on services.Observe (internal/services/observe.go:47). Ship it with a tiny hand-written 5-row table and a golden parse test.
   rationale: The mechanism is the risky, opinion-heavy part; the data is swappable. Proving the join key, the attach point, the zero-HTTP query path and the test pattern with five rows costs a day and de-risks every later payload.
   files: internal/enrich/enrich.go, internal/enrich/table.go, internal/services/observe.go

3. Wire the contract end to end with the toy table: add Employer *Employer `json:"employer,omitempty"` to internal.JobPosting, append the enrichment CSV columns after the existing 8 (following the stated rule at main.go:288-289 and updating the --csv help string at main.go:229), insert enrich.Attach between Dedupe and the filter in newPostingsCommand (main.go:182-186), and add an --enrich flag defaulting to off.
   rationale: Locks the output contract early, while it is cheap to change. Default-off means the existing NDJSON/CSV byte output is provably unchanged, which matters because jobs_record.txt and the nightly workflow consume this binary.
   files: internal/job_posting.go, main.go

4. Build the SEC EDGAR payload as the first real generator: fetch https://www.sec.gov/files/company_tickers.json, match its ~10k names against services.Builtin's 1,929 companies offline, then fetch https://data.sec.gov/submissions/CIK{cik10}.json for each match to pull sic, sicDescription, stateOfIncorporation, tickers, exchanges, ein, formerNames and the latest filing date. Emit the table WITH match_confidence and match_method columns, and require human review of the low-confidence tail before the PR merges.
   rationale: Smallest verifiable payload that exercises the whole pipeline against a real, key-free, canonical API. It also produces the CIK column that Wikidata (P5531) and everything downstream can join on. Expect only ~15-25% coverage — that is fine and expected, and the omitempty JSON field makes partial coverage a non-event.
   files: internal/enrich/gen/main.go, internal/enrich/edgar/edgar.go, internal/enrich/data/employers.tsv.gz

5. Add the O*NET payload: download the current O*NET database text archive (30.3) from onetcenter.org, embed a compacted Alternate Titles table (~55k rows, ~600 KB gzipped) plus Occupation Data and Job Zones, and implement enrich/onet title->SOC normalization (lowercase; strip seniority prefixes, level suffixes and parentheticals; exact match, then longest-token-overlap fallback; always record a confidence). Add the CC BY 4.0 attribution to the README.
   rationale: Nothing else works without it. Both OEWS and OFLC are keyed on SOC, so this is a hard dependency of every wage benchmark, and it is a one-file, license-clean, offline addition with no rate limits.
   files: internal/enrich/onet/onet.go, internal/enrich/onet/titles.go, README.md

6. Add the OFLC employer wage benchmark: scrape hrefs off https://www.dol.gov/agencies/eta/foreign-labor/performance (do not hardcode the .xlsx filename — I could not verify it), parse the LCA disclosure file against the published record layout, restrict to employers already matched in step 4, annualize WAGE_RATE_OF_PAY_FROM by WAGE_UNIT_OF_PAY reusing the periodsPerYear semantics from internal/job_posting.go:42-48, and aggregate to (employer, soc_code, state) -> {n, p25, p50, p75}. Populate Employer.WageBenchmark only — never JobPosting.Compensation.
   rationale: This is the highest user value in the whole assignment: docs/compensation.md:74-79 measures only 48.2% pay extraction on Greenhouse and most platforms publish no pay at all, so an employer-specific, role-specific, key-free benchmark fills the project's biggest documented gap — and unlike EDGAR it covers private companies. It is last in this sequence only because it is the largest parsing job and depends on steps 2, 4 and 5.
   files: internal/enrich/oflc/oflc.go, internal/enrich/gen/main.go, docs/compensation.md

7. Add --industry, --public/--private, --soc, --min-benchmark and --min-employees to internal.Filter, updating both Filter.Match (internal/filter.go:105) and Filter.IsZero (internal/filter.go:147) with matching cases in internal/filter_test.go. Keep --min-benchmark strictly separate from --min-pay (main.go:245).
   rationale: Filter.IsZero is a correctness trap: it short-circuits Apply entirely (filter.go:160-162), so a new field that IsZero does not know about silently does nothing. The --min-pay/--min-benchmark separation enforces docs/compensation.md:20 ("Nothing blends sources") at the CLI surface.
   files: internal/filter.go, internal/filter_test.go, main.go

8. Add a standalone `company` subcommand (registered alongside companies/total/health at main.go:121-126) that prints the enrichment record for named companies with no crawl, plus text/JSON output. Add docs/enrichment.md recording every embedded table's source URL, license, retrieval date, row count and regeneration command.
   rationale: Makes the table's value visible in milliseconds instead of behind a 75-minute crawl, and gives the project the same provenance discipline for enrichment that docs/compensation.md already established for pay.
   files: main.go, docs/enrichment.md

9. Add a monthly scheduled GitHub Actions workflow that runs the generator and opens a PR with the regenerated tables, plus `internal/enrich/data/** -text` in .gitattributes. Keep it entirely separate from track_jobs.yml.
   rationale: GitHub Actions is the only environment with real network access, and refresh cadences are slow (OFLC quarterly, OEWS annual, O*NET quarterly). Keeping it off the nightly crawl workflow means enrichment refresh can never contribute to the Track Jobs timeout that is already failing.
   files: .github/workflows/, ..gitattributes

10. Only after the above ships and is measured, consider the narrow sources: Wikidata SPARQL (one bulk query for employees/inception/industry/parent on the CIK-matched set), USCIS H-1B Data Hub (reuses the OFLC employer normalizer), ProPublica 990 (matters for the hospital/university employers the README claims), and USAspending (defense/aerospace).
   rationale: Each is cheap once entity resolution exists, and worthless before it. Deferring them keeps the first delivery small enough to actually land, and lets real coverage numbers from step 4 decide which gap is worth filling next.
   files: internal/enrich/wikidata/wikidata.go

## Risks

- NO LIVE VERIFICATION WAS POSSIBLE. This container has no egress to third-party hosts, so every URL, field name, rate limit and file size below is from WebSearch of official and secondary documentation, not from a probe. The exact OFLC .xlsx data-file filename in particular was NOT confirmed — only the /sites/dolgov/files/ETA/oflc/pdfs/ directory convention (via live LCA_Record_Layout_FY2025_Q3.pdf and LCA_Record_Layout_FY2024_Q4.pdf result URLs) and the landing page. The generator must discover file URLs by parsing the performance page. Similarly, the BLS OEWS directory appears in the wild as BOTH /oes/special-requests/ and /oes/special.requests/; resolve from https://www.bls.gov/oes/tables.htm rather than hardcoding. Everything needs a first live run in GitHub Actions.
- Entity resolution is where this silently goes wrong. Attaching Lockheed Martin's revenue to a same-named-but-different employer produces a plausible wrong number that looks exactly like a right one — the precise failure mode docs/compensation.md:3-5 was written to prevent. Mitigate with a reviewed table plus a persisted match_confidence, and prefer emitting nothing over emitting a guess.
- EDGAR coverage of this repo's 1,929 companies will be low — most are private startups on Greenhouse/Ashby/Lever. If the first payload lands and only ~300 companies get an employer object, that can read as failure. Set the expectation up front and lead with the coverage number in the PR.
- Embedding data grows the binary and the repo. National+state OEWS, O*NET alternate titles and a company table are each well under 1 MB gzipped, but metro-level OEWS (~330k rows) and raw OFLC (hundreds of thousands of rows per FY) are not. Committing regenerated multi-MB blobs monthly also inflates git history permanently. Cap what is embedded and put the large tiers behind an optional cache.
- Wage benchmarks are not the same thing as pay, and users will conflate them. OFLC wages are what an employer certified for an H-1B application — a specific, self-selected, often-senior slice of hiring, not the median offer. OEWS is a survey estimate with its own suppression rules. If Employer.WageBenchmark ever leaks into JobPosting.Compensation, --min-pay, or the CSV pay columns, the project loses the trust docs/compensation.md was written to earn.
- Adding fields to internal.Filter without updating Filter.IsZero (internal/filter.go:147) is a live trap: IsZero short-circuits Apply entirely at filter.go:160-162, so the new flag would parse, be accepted, and silently filter nothing.
- Scope creep. There are ten plausible sources here and only two that move the needle for a job hunter (OFLC wages, and O*NET as its join key). Shipping all ten shallowly is worse than shipping two well, and this repo already has four other competing priorities including a nightly workflow that has been red since 2026-07-04.