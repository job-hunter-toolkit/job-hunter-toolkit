// app.js — the DOM layer, kept deliberately thin.
//
// Everything that decides anything lives in Go (web/engine, compiled to
// engine.wasm); this file fetches, wires events, and renders. All posting data
// reaches the page through textContent, never innerHTML, so corpus content
// cannot inject markup.

import { resolveCorpusBase } from "./config.js";
import { EngineClient } from "./engine-client.js";
import { resultCountText, snapshotStatus } from "./freshness.js";
import {
  VISIT_KEY,
  STREAK_KEY,
  searchName,
  sameRequest,
  isEmptyRequest,
  countNewSince,
  nextStreak,
  greeting,
  sinceLabel,
  timeAgo,
  loadSavedSearches,
  saveSavedSearches,
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
  refine: $("refine"),
  filterSummary: $("filter-summary"),
  clear: $("clear"),
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
let searchController = null; // a superseded scan should stop, not merely lose its paint race
let summary = null;
let freshnessTimer = null;

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

    summary = await engine.open(corpusURL);
    renderBanner(summary);
    els.count.textContent = "Preparing the snapshot for search…";
    els.list.setAttribute("aria-busy", "true");

    let bytesFetched = 0;
    engine.onProgress = (stats) => {
      bytesFetched = stats.bytesFetched;
    };

    const ticker = setInterval(() => {
      const transferred = bytesFetched > 0
        ? ` · ${(bytesFetched / (1024 * 1024)).toFixed(1)} MiB transferred`
        : "";
      setStage(`Preparing ${summary.rows.toLocaleString()} listings for fast local search${transferred}`);
    }, 600);

    let stats;
    try {
      stats = await engine.load();
    } finally {
      clearInterval(ticker);
    }

    rowsLoaded = true;
    els.go.disabled = false;
    els.list.setAttribute("aria-busy", "false");
    const elapsed = stats.elapsed_ms < 100 ? "under 0.1 s" : `${(stats.elapsed_ms / 1000).toFixed(1)} s`;
    setStage(`Ready · ${stats.rows.toLocaleString()} listings indexed locally in ${elapsed}`);
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
  els.banner.className = "banner";
  const status = snapshotStatus(summary, new Date());
  els.banner.classList.add(status.level);

  const line = document.createElement("div");
  line.className = "snapshot-line";
  addSpan(line, "strong", status.label);
  const time = document.createElement("time");
  time.dateTime = summary.run_at || "";
  time.title = status.exact;
  time.textContent = status.relative;
  line.append(time);
  addSpan(line, "snapshot-counts", `${summary.open.toLocaleString()} believed open · ${summary.sources.toLocaleString()} sources`);
  els.banner.append(line);

  const details = document.createElement("details");
  details.className = "snapshot-details";
  const disclosure = document.createElement("summary");
  disclosure.textContent = "About this snapshot";
  const exact = document.createElement("p");
  exact.textContent = `Collected ${status.exact}. `;
  const statusLink = document.createElement("a");
  statusLink.href = "https://github.com/job-hunter-toolkit/job-hunter-toolkit/actions/workflows/corpus.yml";
  statusLink.target = "_blank";
  statusLink.rel = "noopener noreferrer";
  statusLink.textContent = "Publication status";
  exact.append(statusLink);
  const explanation = document.createElement("p");
  explanation.textContent = status.explanation;
  details.append(disclosure, exact, explanation);

  if (summary.partial) {
    els.banner.classList.add("partial");
    addSpan(
      details,
      "caution",
      "This snapshot came from a partial crawl, so its counts are a floor rather than a complete total.",
    );
  }
  els.banner.append(details);

  clearTimeout(freshnessTimer);
  freshnessTimer = setTimeout(() => renderBanner(summary), 60_000);
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

  els.clear.addEventListener("click", () => {
    haptic();
    applyRequest({ titles: terms("f-title") });
    runSearch(true);
  });

  // Search-as-you-type. Text and number inputs debounce so the engine sees
  // the pause, not every keystroke; selects and checkboxes are single
  // deliberate acts and search immediately.
  const debounced = debounce(() => runSearch(true), 160);

  for (const input of els.form.querySelectorAll('input[type="text"], input[type="number"]')) {
    input.addEventListener("input", () => {
      updateFilterSummary();
      debounced();
    });
  }

  for (const control of els.form.querySelectorAll("select, input[type='checkbox']")) {
    control.addEventListener("change", () => {
      updateFilterSummary();
      runSearch(true);
    });
  }

  updateFilterSummary();

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
  return loadSavedSearches();
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
  if (!saveSavedSearches(saved)) {
    flashButton(els.save, "Update the app to save");
    return;
  }
  renderSavedChips();
  flashButton(els.save, "Saved");
}

function removeSavedSearch(id) {
  saveSavedSearches(savedSearches().filter((s) => s.id !== id));
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
  updateFilterSummary();
}

function updateFilterSummary() {
  const request = buildRequest();
  const active = Object.entries(request).reduce((count, [key, value]) => {
    if (key === "titles") return count;
    if (Array.isArray(value)) return count + (value.length > 0 ? 1 : 0);
    return count + (value ? 1 : 0);
  }, 0);

  els.filterSummary.textContent = active === 0 ? "Optional" : `${active} active`;
  els.clear.disabled = active === 0;
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
    if (err?.name !== "AbortError") showError(err);
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
    searchController?.abort();
    offset = 0;
    lastRequest = buildRequest();
  }

  const controller = new AbortController();
  searchController = controller;
  let response;
  try {
    response = await engine.search({ ...lastRequest, offset, limit: 100 }, { signal: controller.signal });
  } finally {
    if (searchController === controller) searchController = null;
  }

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
  if (response.matched === 0) {
    els.count.textContent = "No postings match.";
    renderEmptyState();
    return;
  }

  const level = snapshotStatus(summary, new Date()).level;
  els.count.textContent = resultCountText(response, offset, level);
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
    title.setAttribute("aria-label", `${item.title || "Untitled posting"} (opens in a new tab)`);
  }

  li.append(title);

  const where = document.createElement("div");
  where.className = "where";
  if (item.company) addSpan(where, "company", item.company);
  if (item.location) addSpan(where, "location", item.location);
  li.append(where);

  const meta = document.createElement("div");
  meta.className = "meta";

  const level = snapshotStatus(summary, new Date()).level;
  if (item.state === "stale" && level !== "old") {
    const stateBadge = addBadge(meta, "not recently checked", "state-stale");
    stateBadge.title = STATE_TITLES.stale;
  } else if (item.state === "closed" || item.state === "lapsed") {
    const label = item.state === "closed" ? "closed in snapshot" : "source status unknown";
    const stateBadge = addBadge(meta, label, `state-${item.state}`);
    stateBadge.title = STATE_TITLES[item.state];
  }
  if (item.compensation) addBadge(meta, item.compensation, "pay");
  if (item.remote) addBadge(meta, "remote");
  if (item.workplace_type && item.workplace_type !== "remote") addBadge(meta, item.workplace_type);
  if (item.employment_type) addBadge(meta, item.employment_type.replace("_", " "));
  if (item.seniority) addBadge(meta, item.seniority);
  if (item.department || item.team) {
    addBadge(meta, [item.department, item.team].filter(Boolean).join(" / "));
  }
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
  els.loading.setAttribute("aria-valuetext", text);
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

  els.error.replaceChildren();
  const message = document.createElement("p");
  message.textContent = offline + `The snapshot could not be prepared. ${err.message ?? err}`;
  const retry = document.createElement("button");
  retry.type = "button";
  retry.className = "secondary";
  retry.textContent = "Retry";
  retry.addEventListener("click", () => globalThis.location.reload());
  els.error.append(message, retry);
  els.error.tabIndex = -1;
  els.error.focus();
  els.go.disabled = true;
  els.list.setAttribute("aria-busy", "false");
  setStage("Your filters are unchanged. Retry when the connection is available.");
}

function addSpan(parent, className, text) {
  const span = document.createElement("span");
  if (className) span.className = className;
  span.textContent = text;
  parent.append(span);
}

// Lifecycle states in plain words. "Stale" says how old our evidence is,
// not how old the posting is: the nightly crawl refreshes a budgeted slice
// of ~10,000 boards, so a board can go a day or more between checks.
const STATE_TITLES = {
  stale:
    "Visible on the company's board at its last successful check, but that source has not been checked recently. This does not mean the posting is known to be closed.",
  closed: "Gone from the company's board: two later checks agreed it was removed.",
  lapsed:
    "The company's board has not had a successful check for so long that this posting's status is unknown.",
};

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
