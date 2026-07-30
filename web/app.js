// app.js — the DOM layer, kept deliberately thin.
//
// Everything that decides anything lives in Go (web/engine, compiled to
// engine.wasm); this file fetches, wires events, and renders. All posting data
// reaches the page through textContent, never innerHTML, so corpus content
// cannot inject markup.

import { resolveCorpusBase } from "./config.js";
import { createStore } from "./corpus-store.js";

const $ = (id) => document.getElementById(id);

const els = {
  banner: $("banner"),
  stage: $("stage"),
  form: $("filters"),
  results: $("results"),
  count: $("count"),
  list: $("list"),
  more: $("more"),
  error: $("error"),
};

let store;
let corpusURL = "";
let offset = 0;
let lastRequest = null;

boot();

async function boot() {
  try {
    setStage("Fetching snapshot metadata…");
    corpusURL = await resolveCorpusBase();
    store = await createStore(corpusURL);

    setStage("Loading query engine…");
    await loadEngine();

    const summary = JSON.parse(await jhtEngine.open(store));
    renderBanner(summary);

    const ticker = setInterval(() => {
      setStage(
        `Loading ${summary.rows.toLocaleString()} postings… ` +
          `${(store.stats.bytesFetched / (1024 * 1024)).toFixed(1)} MiB fetched`,
      );
    }, 250);

    let stats;
    try {
      stats = JSON.parse(await jhtEngine.load());
    } finally {
      clearInterval(ticker);
    }

    setStage(
      `Loaded ${stats.rows.toLocaleString()} rows in ` +
        `${(stats.elapsed_ms / 1000).toFixed(1)} s ` +
        `(${(store.stats.bytesFetched / (1024 * 1024)).toFixed(1)} MiB, ` +
        `${store.stats.requests} requests, ${store.stats.mode} mode)`,
    );

    els.form.hidden = false;
    els.results.hidden = false;
    wireForm();
    await search(true);
  } catch (err) {
    showError(err);
  }
}

// loadEngine lazy-loads the wasm: the page paints and the metadata fetch
// starts before the ~1 MiB (gzipped) binary is requested, and the two then
// stream in parallel with a visible stage line instead of a blank tab.
async function loadEngine() {
  await injectScript("wasm_exec.js");

  const go = new Go();
  const ready = new Promise((resolve) => {
    globalThis.jhtEngineReady = resolve;
  });

  const result = await WebAssembly.instantiateStreaming(
    fetch("engine.wasm"),
    go.importObject,
  ).catch(async () => {
    // instantiateStreaming requires Content-Type: application/wasm; fall back
    // for hosts that mislabel it.
    const response = await fetch("engine.wasm");

    return WebAssembly.instantiate(await response.arrayBuffer(), go.importObject);
  });

  go.run(result.instance); // resolves only if the engine exits; not awaited
  await ready;
}

function injectScript(src) {
  return new Promise((resolve, reject) => {
    const script = document.createElement("script");
    script.src = src;
    script.onload = resolve;
    script.onerror = () => reject(new Error(`failed to load ${src}`));
    document.head.append(script);
  });
}

// --- honesty banner ---------------------------------------------------------

// renderBanner is the one non-negotiable piece of UI: the user must see what
// they are querying — a snapshot, from when, complete or not — before they see
// a single posting.
function renderBanner(summary) {
  els.banner.replaceChildren();

  const ageHours = summary.age_hours ?? 0;
  const crawled = summary.run_at
    ? `crawled ${summary.run_at.replace("T", " ").replace(":00Z", " UTC")} — ${formatAge(ageHours)}`
    : "crawl date unknown";

  const freshness =
    ageHours <= 0 || !summary.run_at ? "old" : ageHours <= 36 ? "fresh" : ageHours <= 8 * 24 ? "aging" : "old";
  els.banner.classList.add(freshness);

  addSpan(els.banner, "strong", `Snapshot · generation ${summary.generation}`);
  addSpan(els.banner, "", crawled);
  addSpan(
    els.banner,
    "",
    `${summary.open.toLocaleString()} open postings from ${summary.sources.toLocaleString()} sources`,
  );

  if (summary.partial) {
    els.banner.classList.add("partial");
    addSpan(
      els.banner,
      "warn",
      "PARTIAL CRAWL — the producing crawl did not finish; counts are a floor, not a total",
    );
  }

  if (freshness === "old") {
    addSpan(els.banner, "warn", "This data is not live. Postings may have closed since the crawl.");
  }
}

function formatAge(hours) {
  if (hours < 1) return "under an hour ago";
  if (hours < 48) return `${Math.round(hours)} hours ago`;

  return `${Math.round(hours / 24)} days ago`;
}

// --- search -----------------------------------------------------------------

function wireForm() {
  els.form.addEventListener("submit", (event) => {
    event.preventDefault();
    search(true).catch(showError);
  });

  els.more.addEventListener("click", () => {
    search(false).catch(showError);
  });
}

function terms(id) {
  return $(id)
    .value.split(",")
    .map((t) => t.trim())
    .filter(Boolean);
}

function buildRequest() {
  const request = {
    titles: terms("f-title"),
    exclude_titles: terms("f-exclude"),
    locations: terms("f-location"),
    companies: terms("f-company"),
    departments: terms("f-department"),
    remote: $("f-remote").checked,
    has_compensation: $("f-haspay").checked,
    include_closed: $("f-closed").checked,
    min_annual: Number($("f-minpay").value) || 0,
    posted_since_days: Number($("f-since").value) || 0,
  };

  if ($("f-employment").value) request.employment_types = [$("f-employment").value];
  if ($("f-workplace").value) request.workplace_types = [$("f-workplace").value];

  return request;
}

async function search(reset) {
  if (reset) {
    offset = 0;
    lastRequest = buildRequest();
    els.list.replaceChildren();
  }

  const request = { ...lastRequest, offset, limit: 100 };
  const response = JSON.parse(await jhtEngine.search(JSON.stringify(request)));

  for (const item of response.items) {
    els.list.append(renderItem(item));
  }

  offset += response.items.length;
  renderCount(response);
  els.more.hidden = offset >= response.matched;
}

function renderCount(response) {
  const states = Object.entries(response.states ?? {})
    .sort()
    .map(([state, n]) => `${n.toLocaleString()} ${state}`)
    .join(" · ");

  els.count.textContent =
    response.matched === 0
      ? "No postings match."
      : `${response.matched.toLocaleString()} matches (${states}) — showing ${Math.min(offset, response.matched).toLocaleString()}`;
}

function renderItem(item) {
  const li = document.createElement("li");
  li.className = "card";

  const title = document.createElement(item.url ? "a" : "span");
  title.className = "title";
  title.textContent = item.title || "(untitled posting)";

  if (item.url) {
    title.href = item.url;
    title.target = "_blank";
    title.rel = "noopener noreferrer";
  }

  li.append(title);

  const where = document.createElement("div");
  where.className = "where";
  where.textContent = [item.company, item.location].filter(Boolean).join(" · ");
  li.append(where);

  const meta = document.createElement("div");
  meta.className = "meta";

  addBadge(meta, item.state, `state-${item.state}`);
  if (item.remote) addBadge(meta, "remote");
  if (item.workplace_type && item.workplace_type !== "remote") addBadge(meta, item.workplace_type);
  if (item.employment_type) addBadge(meta, item.employment_type.replace("_", " "));
  if (item.seniority) addBadge(meta, item.seniority);
  if (item.department || item.team) {
    addBadge(meta, [item.department, item.team].filter(Boolean).join(" / "));
  }
  if (item.compensation) addBadge(meta, item.compensation, "pay");
  if (item.platform) addBadge(meta, item.platform, "platform");
  if (item.posted_at) addBadge(meta, `posted ${item.posted_at.slice(0, 10)}`);
  else if (item.first_seen) addBadge(meta, `first seen ${item.first_seen.slice(0, 10)}`);

  li.append(meta);

  return li;
}

// ---------------------------------------------------------------------------
// LIVE CRAWL SEAM — deliberately not implemented in this pass.
//
// docs/surfaces-and-extensibility.md measured (with curl, NOT a browser —
// the distinction is load-bearing) that ~57% of registry sources answer with
// CORS headers that suggest a browser could query them directly. Until that
// table has been re-measured from an actual browser, live mode stays out.
//
// When it lands, it slots in here: a `fetchLive(request)` that queries the
// CORS-open boards for the matched companies, merges over the snapshot, and
// labels every live result as live — never silently mixing seconds-old and
// days-old data. The snapshot path above remains the fallback for everything.
// ---------------------------------------------------------------------------

// --- plumbing ---------------------------------------------------------------

function setStage(text) {
  els.stage.textContent = text;
}

function showError(err) {
  console.error(err);
  els.error.hidden = false;
  els.error.textContent =
    `${err.message ?? err}` +
    ` — corpus URL: ${corpusURL || "(unresolved)"}` +
    " (no corpus published there yet? pass ?corpus=<url> to point elsewhere)";
  setStage("");
}

function addSpan(parent, className, text) {
  const span = document.createElement("span");
  if (className) span.className = className;
  span.textContent = text;
  parent.append(span);
}

function addBadge(parent, text, className = "") {
  const span = document.createElement("span");
  span.className = `badge ${className}`.trim();
  span.textContent = text;
  parent.append(span);
}
