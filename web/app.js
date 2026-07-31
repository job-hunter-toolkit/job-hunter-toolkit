// app.js — the DOM layer, kept deliberately thin.
//
// Everything that decides anything lives in Go (web/engine, compiled to
// engine.wasm); this file fetches, wires events, and renders. All posting data
// reaches the page through textContent, never innerHTML, so corpus content
// cannot inject markup.

import { resolveCorpusBase } from "./config.js";
import { createStore } from "./corpus-store.js";
import {
  SAVED_KEY,
  VISIT_KEY,
  STREAK_KEY,
  MAX_SAVED,
  searchName,
  sameRequest,
  isEmptyRequest,
  countNewSince,
  nextStreak,
  greeting,
  sinceLabel,
  timeAgo,
  storage,
} from "./rollup.js";

const $ = (id) => document.getElementById(id);

const els = {
  banner: $("banner"),
  stage: $("stage"),
  loading: $("loading"),
  rollup: $("rollup"),
  saved: $("saved"),
  form: $("filters"),
  go: $("go"),
  save: $("save"),
  spin: $("spin"),
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
let rowsLoaded = false; // search is legal only after jhtEngine.load()
let searchSeq = 0; // stale async results must never paint over newer ones

// The service worker makes the shell installable and offline-capable. Its
// failure is never the page's problem.
if ("serviceWorker" in navigator) {
  navigator.serviceWorker.register("sw.js").catch(() => {});
}

boot();

async function boot() {
  try {
    // The wasm download and compile overlap the corpus metadata fetch: they
    // share no state until open(), and serializing them was pure wasted wait.
    const engineReady = loadEngine();

    setStage("Fetching snapshot metadata…");
    corpusURL = await resolveCorpusBase();
    store = await createStore(corpusURL);

    setStage("Loading query engine…");
    await engineReady;

    const summary = JSON.parse(await jhtEngine.open(store));
    renderBanner(summary);

    // The form is live from here: typing during the load queues one search
    // that fires the moment the rows land. Skeleton cards hold the space so
    // the page reads as "results are coming", not "blank".
    els.form.hidden = false;
    els.results.hidden = false;
    wireForm();
    renderSavedChips();
    renderSkeletons();

    // While rows stream, the stage line names real employers going by: the
    // data is genuinely arriving, so the page shows it arriving. Fetched
    // through the store so the bytes count and the SW caches it; a failure
    // just means a plainer loading line.
    let companies = [];
    store
      .whole("sources.json")
      .then((bytes) => {
        const parsed = JSON.parse(new TextDecoder().decode(bytes));
        // The biggest boards make the best marquee: recognizable names, and
        // registry slugs too mangled to read as names stay out of it.
        companies = shuffle(
          parsed
            .filter((s) => s.company && s.company.length <= 14 && (s.open ?? 0) > 0)
            .sort((a, b) => (b.open ?? 0) - (a.open ?? 0))
            .slice(0, 400)
            .map((s) => s.company),
        );
      })
      .catch(() => {});

    let tick = 0;
    const ticker = setInterval(() => {
      const mib = `${(store.stats.bytesFetched / (1024 * 1024)).toFixed(1)} MiB`;
      let line = `Loading ${summary.rows.toLocaleString()} postings (${mib})`;

      if (companies.length) {
        const a = companies[(tick * 3) % companies.length];
        const b = companies[(tick * 3 + 1) % companies.length];
        const c = companies[(tick * 3 + 2) % companies.length];
        line += ` · reading ${displayCompany(a)}, ${displayCompany(b)}, ${displayCompany(c)}…`;
      }

      setStage(line);
      tick += 1;
    }, 600);

    let stats;
    try {
      stats = JSON.parse(await jhtEngine.load());
    } finally {
      clearInterval(ticker);
    }

    rowsLoaded = true;
    setStage(
      `Loaded ${stats.rows.toLocaleString()} rows in ` +
        `${(stats.elapsed_ms / 1000).toFixed(1)} s ` +
        `(${(store.stats.bytesFetched / (1024 * 1024)).toFixed(1)} MiB, ` +
        `${store.stats.requests} requests, ${store.stats.mode} mode)`,
    );
    els.loading.hidden = true;

    await search(true);

    // The rollup runs after the first results paint: it re-queries the corpus
    // once per saved search, and nothing about it should delay first light.
    renderRollup().catch(() => {});
  } catch (err) {
    showError(err);
  }
}

// renderSkeletons fills the list with placeholder cards while rows stream in.
function renderSkeletons() {
  els.count.textContent = "";
  els.list.replaceChildren();

  for (let i = 0; i < 5; i++) {
    const li = document.createElement("li");
    li.className = "card skeleton";
    li.style.setProperty("--stagger", `${i * 60}ms`);
    const bar1 = document.createElement("div");
    bar1.className = "bone title-bone";
    const bar2 = document.createElement("div");
    bar2.className = "bone where-bone";
    const bar3 = document.createElement("div");
    bar3.className = "bone meta-bone";
    li.append(bar1, bar2, bar3);
    els.list.append(li);
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
    ? `crawled ${summary.run_at.replace("T", " ").replace(/(:\d{2})?Z$/, " UTC")} (${formatAge(ageHours)})`
    : "crawl date unknown";

  const freshness =
    ageHours <= 0 || !summary.run_at ? "old" : ageHours <= 36 ? "fresh" : ageHours <= 8 * 24 ? "aging" : "old";
  els.banner.classList.add(freshness);

  addSpan(els.banner, "strong", `Snapshot, generation ${summary.generation}`);
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
      "Partial crawl: the producing crawl did not finish, so counts are a floor, not a total",
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
    haptic();
    runSearch(true, { fromButton: true });
  });

  els.more.addEventListener("click", () => {
    haptic();
    runSearch(false, { fromButton: true });
  });

  els.save.addEventListener("click", () => {
    haptic();
    saveCurrentSearch();
  });

  // Search-as-you-type. Text and number inputs debounce so the engine sees
  // the pause, not every keystroke; selects and checkboxes are single
  // deliberate acts and search immediately.
  const debounced = debounce(() => runSearch(true), 160);

  for (const input of els.form.querySelectorAll('input[type="text"], input[type="number"]')) {
    input.addEventListener("input", debounced);
  }

  for (const control of els.form.querySelectorAll("select, input[type='checkbox']")) {
    control.addEventListener("change", () => runSearch(true));
  }

  // "/" focuses search from anywhere; Escape clears it. The muscle memory
  // every fast search product shares.
  document.addEventListener("keydown", (event) => {
    const inField = /^(INPUT|SELECT|TEXTAREA)$/.test(document.activeElement?.tagName ?? "");

    if (event.key === "/" && !inField) {
      event.preventDefault();
      $("f-title").focus();
    }

    if (event.key === "Escape" && document.activeElement === $("f-title") && $("f-title").value) {
      $("f-title").value = "";
      runSearch(true);
    }
  });
}

function debounce(fn, ms) {
  let timer;

  return () => {
    clearTimeout(timer);
    timer = setTimeout(fn, ms);
  };
}

// --- saved searches ---------------------------------------------------------

function savedSearches() {
  const saved = storage.load(SAVED_KEY, []);

  return Array.isArray(saved) ? saved : [];
}

function saveCurrentSearch() {
  const request = buildRequest();

  if (isEmptyRequest(request)) {
    flashButton(els.save, "Add a filter first");
    return;
  }

  const saved = savedSearches();

  if (saved.some((s) => sameRequest(s.request, request))) {
    flashButton(els.save, "Already saved");
    return;
  }

  saved.unshift({ id: crypto.randomUUID(), name: searchName(request), request, createdAt: new Date().toISOString() });
  storage.save(SAVED_KEY, saved.slice(0, MAX_SAVED));
  renderSavedChips();
  flashButton(els.save, "Saved");
}

function removeSavedSearch(id) {
  storage.save(
    SAVED_KEY,
    savedSearches().filter((s) => s.id !== id),
  );
  renderSavedChips();
}

function renderSavedChips() {
  const saved = savedSearches();
  els.saved.replaceChildren();
  els.saved.hidden = saved.length === 0;

  for (const entry of saved) {
    const chip = document.createElement("span");
    chip.className = "chip";

    const run = document.createElement("button");
    run.type = "button";
    run.className = "chip-run";
    run.textContent = entry.name;
    run.title = "Run this saved search";
    run.addEventListener("click", () => {
      haptic();
      applyRequest(entry.request);
      runSearch(true);
    });

    const remove = document.createElement("button");
    remove.type = "button";
    remove.className = "chip-x";
    remove.textContent = "×";
    remove.setAttribute("aria-label", `Remove saved search ${entry.name}`);
    remove.addEventListener("click", () => removeSavedSearch(entry.id));

    chip.append(run, remove);
    els.saved.append(chip);
  }
}

// applyRequest writes a saved request back into the form, so a chip click and
// a hand-filled form are the same code path from here on.
function applyRequest(request) {
  $("f-title").value = (request.titles ?? []).join(", ");
  $("f-exclude").value = (request.exclude_titles ?? []).join(", ");
  $("f-location").value = (request.locations ?? []).join(", ");
  $("f-company").value = (request.companies ?? []).join(", ");
  $("f-department").value = (request.departments ?? []).join(", ");
  $("f-minpay").value = request.min_annual > 0 ? request.min_annual : "";
  $("f-remote").checked = Boolean(request.remote);
  $("f-haspay").checked = Boolean(request.has_compensation);
  $("f-closed").checked = Boolean(request.include_closed);
  $("f-employment").value = request.employment_types?.[0] ?? "";
  $("f-workplace").value = request.workplace_types?.[0] ?? "";
  $("f-since").value = request.posted_since_days > 0 ? String(request.posted_since_days) : "";
}

function flashButton(button, text) {
  const original = button.textContent;
  button.textContent = text;
  button.disabled = true;
  setTimeout(() => {
    button.textContent = original;
    button.disabled = false;
  }, 1300);
}

// --- the rollup -------------------------------------------------------------

// renderRollup computes "what changed since you were last here" for each saved
// search and says it in one card. Pull-only: rendered at open, sent nowhere.
async function renderRollup() {
  const now = new Date();
  const nowISO = now.toISOString();
  const prevVisit = storage.load(VISIT_KEY, "");
  const streak = nextStreak(storage.load(STREAK_KEY, null), nowISO);

  storage.save(VISIT_KEY, nowISO);
  storage.save(STREAK_KEY, streak);

  const saved = savedSearches();

  if (saved.length === 0 || !prevVisit) {
    return; // nothing to summarize on a first or searchless visit
  }

  const counts = [];
  for (const entry of saved) {
    const request = { ...entry.request, offset: 0, limit: 400 };
    const response = JSON.parse(await jhtEngine.search(JSON.stringify(request)));
    counts.push({ entry, total: response.matched, fresh: countNewSince(response.items, prevVisit) });
  }

  els.rollup.replaceChildren();

  const head = document.createElement("p");
  head.className = "rollup-head";
  const streakNote = streak.n >= 2 ? ` Day ${streak.n} in a row.` : "";
  head.textContent = `${greeting(now.getHours())}. ${sinceLabel(prevVisit, nowISO)}:${streakNote}`;
  els.rollup.append(head);

  const items = document.createElement("div");
  items.className = "rollup-items";

  for (const { entry, total, fresh } of counts) {
    const item = document.createElement("button");
    item.type = "button";
    item.className = fresh > 0 ? "rollup-item fresh" : "rollup-item";
    item.textContent =
      fresh > 0 ? `${entry.name}: ${fresh.toLocaleString()} new` : `${entry.name}: nothing new`;
    item.title = `${total.toLocaleString()} total matches`;
    item.addEventListener("click", () => {
      haptic();
      applyRequest(entry.request);
      runSearch(true);
    });
    items.append(item);
  }

  els.rollup.append(items);

  if (counts.every((c) => c.fresh === 0)) {
    const quiet = document.createElement("p");
    quiet.className = "rollup-quiet";
    quiet.textContent = "All quiet. Your searches are up to date.";
    els.rollup.append(quiet);
  }

  const dismiss = document.createElement("button");
  dismiss.type = "button";
  dismiss.className = "rollup-dismiss";
  dismiss.textContent = "×";
  dismiss.setAttribute("aria-label", "Dismiss summary");
  dismiss.addEventListener("click", () => {
    els.rollup.hidden = true;
  });
  els.rollup.append(dismiss);

  els.rollup.hidden = false;
}

// runSearch wraps search with feedback proportional to how it was driven: a
// button press gets a busy state, a keystroke gets only the input spinner.
// Before the rows finish loading it queues exactly one follow-up search.
async function runSearch(reset, { fromButton = false } = {}) {
  if (!rowsLoaded) {
    // Typing during the load is fine: boot's first search reads the form as
    // it stands the moment the rows land, so nothing typed is lost.
    return;
  }

  const button = fromButton ? (reset ? els.go : els.more) : null;
  if (button) button.disabled = true;
  els.spin.classList.add("busy");
  els.list.classList.add("searching");

  try {
    // The wasm scan runs on the main thread, so without an explicit yield the
    // busy affordances above would never reach the screen: the browser would
    // sit frozen on the old frame for the whole search. Two frames guarantee
    // a paint first.
    await nextPaint();
    await search(reset);
  } catch (err) {
    showError(err);
  } finally {
    if (button) button.disabled = false;
    els.spin.classList.remove("busy");
    els.list.classList.remove("searching");
  }
}

function nextPaint() {
  return new Promise((resolve) => {
    requestAnimationFrame(() => requestAnimationFrame(resolve));
  });
}

// haptic gives a single soft tick on devices that support it. Guarded: vibrate
// is absent on desktop and iOS Safari, and must never throw.
function haptic() {
  try {
    navigator.vibrate?.(5);
  } catch {
    /* not supported */
  }
}

// displayCompany turns a registry slug into something a person reads:
// "mayo-clinic" becomes "Mayo Clinic". Imperfect for brand casing, honest
// enough for a loading line.
function displayCompany(slug) {
  return slug
    .split(/[-_ ]+/)
    .map((word) => (word.length > 1 ? word[0].toUpperCase() + word.slice(1) : word.toUpperCase()))
    .join(" ");
}

function shuffle(list) {
  for (let i = list.length - 1; i > 0; i--) {
    const j = Math.floor(Math.random() * (i + 1));
    [list[i], list[j]] = [list[j], list[i]];
  }

  return list;
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
  const seq = ++searchSeq;

  if (reset) {
    offset = 0;
    lastRequest = buildRequest();
  }

  const request = { ...lastRequest, offset, limit: 100 };
  const response = JSON.parse(await jhtEngine.search(JSON.stringify(request)));

  // A newer search finished the race while this one was in flight; painting
  // these rows now would show stale results under fresh filters.
  if (seq !== searchSeq) {
    return;
  }

  // Entrance stagger belongs to fresh arrivals, not to every keystroke: the
  // first real paint of the list gets rhythm, retyping gets immediacy.
  const firstPaint = els.list.querySelector(".card:not(.skeleton)") === null;

  if (reset) {
    els.list.replaceChildren();
  }

  els.list.classList.toggle("instant", !firstPaint);

  response.items.forEach((item, i) => {
    const card = renderItem(item);
    card.style.setProperty("--stagger", `${Math.min(i, 12) * 25}ms`);
    els.list.append(card);
  });

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
      ? "No postings match. Loosen a filter and try again."
      : `${response.matched.toLocaleString()} matches (${states}), newest first, showing ${Math.min(offset, response.matched).toLocaleString()}`;
}

function renderItem(item) {
  const li = document.createElement("li");
  li.className = "card";

  const url = safeHTTPURL(item.url);
  const title = document.createElement(url ? "a" : "span");
  title.className = "title";
  title.textContent = item.title || "(untitled posting)";

  if (url) {
    title.href = url;
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

  // Dates read the way a person says them; the exact day sits in the tooltip.
  const nowISO = new Date().toISOString();
  if (item.posted_at) {
    const badge = addBadge(meta, `posted ${timeAgo(item.posted_at, nowISO) || item.posted_at.slice(0, 10)}`);
    badge.title = item.posted_at.slice(0, 10);
  } else if (item.first_seen) {
    const badge = addBadge(meta, `first seen ${timeAgo(item.first_seen, nowISO) || item.first_seen.slice(0, 10)}`);
    badge.title = item.first_seen.slice(0, 10);
  }

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
  els.loading.hidden = true;
  els.error.hidden = false;
  els.error.textContent =
    `${err.message ?? err}. ` +
    `Corpus URL: ${corpusURL || "(unresolved)"}. ` +
    "If no corpus is published there yet, pass ?corpus=<url> to point elsewhere.";
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

  return span;
}

// safeHTTPURL admits only http(s) targets for posting links. Corpus rows are
// data, not trusted markup, and with ?corpus= anyone can put rows in front of
// a visitor; a javascript: href must render as plain text, not a link.
function safeHTTPURL(raw) {
  if (!raw) return "";
  try {
    const url = new URL(raw);

    return url.protocol === "https:" || url.protocol === "http:" ? url.toString() : "";
  } catch {
    return "";
  }
}
