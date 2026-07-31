// app.js — the DOM layer, kept deliberately thin.
//
// Everything that decides anything lives in Go (web/engine, compiled to
// engine.wasm); this file fetches, wires events, and renders. All posting data
// reaches the page through textContent, never innerHTML, so corpus content
// cannot inject markup.

import { resolveCorpusBase } from "./config.js";
import { EngineClient } from "./engine-client.js";
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

// The engine lives in a worker so no scan ever blocks this thread: typing
// stays instant no matter what a search costs. Constructing the client first
// thing starts the wasm download before anything else on this page runs.
const engine = new EngineClient();

let corpusURL = "";
let offset = 0;
let lastRequest = null;
let rowsLoaded = false; // search is legal only after the worker loads the rows
let searchSeq = 0; // stale async results must never paint over newer ones

// The service worker makes the shell installable and offline-capable. Its
// failure is never the page's problem.
if ("serviceWorker" in navigator) {
  navigator.serviceWorker.register("sw.js").catch(() => {});
}

boot();

async function boot() {
  try {
    // The scaffold — form, skeleton results, placeholder banner — is static
    // HTML, painted from the first frame; scripts only replace content inside
    // boxes that already exist. Only the saved chips render here, because
    // localStorage is script territory.
    setStage("Fetching snapshot metadata…");
    wireForm();
    renderSavedChips();

    corpusURL = await resolveCorpusBase();

    const summary = await engine.open(corpusURL);
    renderBanner(summary);

    // While rows stream, the stage line names real employers going by: the
    // data is genuinely arriving, so the page shows it arriving. The worker
    // sends the names once sources.json lands; the biggest boards make the
    // best marquee, and registry slugs too mangled to read as names stay out.
    let companies = [];
    engine.onCompanies = (names) => {
      companies = shuffle(
        names
          .filter((s) => s.company.length <= 14 && s.open > 0)
          .sort((a, b) => b.open - a.open)
          .slice(0, 400)
          .map((s) => s.company),
      );
    };

    let tick = 0;
    let bytesFetched = 0;
    engine.onProgress = (stats) => {
      bytesFetched = stats.bytesFetched;
    };

    const ticker = setInterval(() => {
      let line = `Loading ${summary.rows.toLocaleString()} postings (${(bytesFetched / (1024 * 1024)).toFixed(1)} MiB)`;

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
      stats = await engine.load();
    } finally {
      clearInterval(ticker);
    }

    rowsLoaded = true;
    setStage(
      `Loaded ${stats.rows.toLocaleString()} rows in ` +
        `${(stats.elapsed_ms / 1000).toFixed(1)} s ` +
        `(${(stats.bytesFetched / (1024 * 1024)).toFixed(1)} MiB, ` +
        `${stats.requests} requests, ${stats.mode} mode)`,
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
    const response = await engine.search({ ...entry.request, offset: 0, limit: 400 });
    counts.push({ entry, total: response.matched, fresh: countNewSince(response.items, prevVisit) });
  }

  const card = els.rollup.firstElementChild;
  card.replaceChildren();

  const head = document.createElement("p");
  head.className = "rollup-head";
  const streakNote = streak.n >= 2 ? ` Day ${streak.n} in a row.` : "";
  head.textContent = `${greeting(now.getHours())}. ${sinceLabel(prevVisit, nowISO)}:${streakNote}`;
  card.append(head);

  const items = document.createElement("div");
  items.className = "rollup-items";

  for (const { entry, total, fresh } of counts) {
    const item = document.createElement("button");
    item.type = "button";
    item.className = fresh > 0 ? "rollup-item fresh" : "rollup-item";
    // 400 is the sample cap, so a full sample means "at least", and the label
    // says so instead of pretending precision.
    const freshLabel = fresh >= 400 ? "400+ new" : `${fresh.toLocaleString()} new`;
    item.textContent = fresh > 0 ? `${entry.name}: ${freshLabel}` : `${entry.name}: nothing new`;
    item.title = `${total.toLocaleString()} total matches`;
    item.addEventListener("click", () => {
      haptic();
      applyRequest(entry.request);
      runSearch(true);
    });
    items.append(item);
  }

  card.append(items);

  if (counts.every((c) => c.fresh === 0)) {
    const quiet = document.createElement("p");
    quiet.className = "rollup-quiet";
    quiet.textContent = "All quiet. Your searches are up to date.";
    card.append(quiet);
  }

  const dismiss = document.createElement("button");
  dismiss.type = "button";
  dismiss.className = "rollup-dismiss";
  dismiss.textContent = "×";
  dismiss.setAttribute("aria-label", "Dismiss summary");
  dismiss.addEventListener("click", () => {
    els.rollup.classList.remove("open");
    els.rollup.addEventListener("transitionend", () => (els.rollup.hidden = true), { once: true });
  });
  card.append(dismiss);

  // Unhide collapsed, then open on the next frame: the card glides in and the
  // results below move with the transition instead of jumping.
  els.rollup.hidden = false;
  requestAnimationFrame(() => els.rollup.classList.add("open"));
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
    await search(reset);
  } catch (err) {
    showError(err);
  } finally {
    if (button) button.disabled = false;
    els.spin.classList.remove("busy");
    els.list.classList.remove("searching");
  }
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

  const response = await engine.search({ ...lastRequest, offset, limit: 100 });

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

  if (response.matched === 0) {
    els.count.textContent = "No postings match.";
    renderEmptyState();
    return;
  }

  els.count.textContent = `${response.matched.toLocaleString()} matches (${states}), newest first, showing ${Math.min(offset, response.matched).toLocaleString()}`;
}

// renderEmptyState replaces a bare "no results" sentence with the one action
// that helps: start over.
function renderEmptyState() {
  const empty = document.createElement("li");
  empty.className = "empty";

  const message = document.createElement("p");
  message.append("Nothing matches ");
  const emphasis = document.createElement("strong");
  emphasis.textContent = "all";
  message.append(emphasis, " of these filters at once. Loosen one, or start over.");
  empty.append(message);

  const clear = document.createElement("button");
  clear.type = "button";
  clear.className = "secondary";
  clear.textContent = "Clear filters";
  clear.addEventListener("click", () => {
    haptic();
    applyRequest({});
    runSearch(true);
  });
  empty.append(clear);

  els.list.replaceChildren(empty);
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

  // Skeletons promise results; an error must not leave that promise shimmering.
  if (els.list.querySelector(".skeleton")) {
    els.list.replaceChildren();
  }

  els.error.hidden = false;

  // Offline is a state, not a bug; say so before the technical detail.
  const offline = navigator.onLine === false ? "You appear to be offline, and this data is not cached yet. " : "";

  els.error.textContent =
    offline +
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
