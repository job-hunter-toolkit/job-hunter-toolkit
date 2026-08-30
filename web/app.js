// app.js — the DOM layer, kept deliberately thin.
//
// Everything that decides anything lives in Go (web/engine, compiled to
// engine.wasm); this file fetches, wires events, and renders. All posting data
// reaches the page through textContent, never innerHTML, so corpus content
// cannot inject markup.

import { resolveCorpusBases } from "./config.js";
import { openSnapshot } from "./snapshot.js";
import { EngineClient } from "./engine-client.js";
import { advanceProgress, failureState, sameVerifiedSnapshot } from "./readiness.js";
import { renderCard } from "./card.js";
import { resultCountText, snapshotStatus } from "./freshness.js";
import { parseQuery, queryForRequest } from "./query-state.js";
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
  formReadiness: $("form-readiness"),
  spin: $("spin"),
  results: $("results"),
  count: $("count"),
  list: $("list"),
  pagination: $("pagination"),
  previous: $("previous"),
  next: $("next"),
  pageJump: $("page-jump"),
  pageNumber: $("page-number"),
  pageTotal: $("page-total"),
  error: $("error"),
  urlNote: $("url-note"),
};

// The engine lives in a worker so no scan ever blocks this thread: typing
// stays instant no matter what a search costs. Constructing the client first
// thing starts the wasm download before anything else on this page runs.
let engine = new EngineClient();

// WebMCP is a progressive enhancement. Unsupported browsers do not even fetch
// its module; supported browsers reuse this exact client and resident engine.
// Saved searches are intentionally absent because the current draft has no
// per-call user-approval primitive for private localStorage data.
let webMCPState = { phase: "metadata", summary: null };
if (typeof document.modelContext?.registerTool === "function") {
  import("./webmcp.js")
    .then(({ installWebMCP }) => installWebMCP(document.modelContext, {
      getState: () => webMCPState,
      search: (request, options) => engine.search(request, options),
      detail: (url, options) => engine.detail(url, options),
    }))
    .catch(() => {});
}

let corpusURL = "";
let offset = 0;
const PAGE_SIZE = 100;
let currentPage = 1;
let lastRequest = null;
let rowsLoaded = false; // search is legal only after the worker loads the rows
let searchSeq = 0; // stale async results must never paint over newer ones
let searchController = null; // a superseded scan should stop, not merely lose its paint race
let summary = null;
let freshnessTimer = null;
let preparing = false;
let progress = null;

// The service worker makes the shell installable and offline-capable. Its
// failure is never the page's problem.
if ("serviceWorker" in navigator) {
  navigator.serviceWorker.register("sw.js", { updateViaCache: "none" }).catch(() => {});
}

hydrateFromURL();
wireForm();
boot();

async function boot({ retry = false } = {}) {
  if (preparing) return;
  preparing = true;
  rowsLoaded = false;
  els.error.hidden = true;
  els.loading.hidden = false;
  els.list.setAttribute("aria-busy", "true");
  setSearchAvailable(false);

  if (retry) {
    engine.terminate();
    engine = new EngineClient();
  }

  try {
    // The scaffold — form, skeleton results, placeholder banner — is static
    // HTML, painted from the first frame; scripts only replace content inside
    // boxes that already exist. Only the saved chips render here, because
    // localStorage is script territory.
    setStage("Fetching snapshot metadata…");

    if (retry && summary && corpusURL) {
      const reopened = await timedEngineCall((signal) => engine.open(corpusURL, { signal }), 30_000);
      if (!sameVerifiedSnapshot(summary, reopened)) {
        const err = new Error("The verified snapshot changed before recovery could start");
        err.phase = "metadata";
        err.retryable = false;
        throw err;
      }
    } else {
      const candidates = await resolveCorpusBases();
      const opened = await openSnapshot(
        candidates,
        (candidate) => timedEngineCall((signal) => engine.open(candidate, { signal }), 30_000),
        () => setStage("The pinned snapshot moved before it opened. Checking the published snapshot…"),
      );
      corpusURL = opened.base;
      summary = opened.summary;
    }

    progress = advanceProgress(null, {
      phase: "network", label: "Downloading verified snapshot columns", completed: 0, total: 30,
    });
    webMCPState = { phase: "indexing", summary, progress };
    renderBanner(summary);
    els.count.textContent = "Preparing the snapshot for search…";
    els.list.setAttribute("aria-busy", "true");

    let bytesFetched = 0;
    engine.onProgress = (stats) => {
      bytesFetched = stats.bytesFetched;
      progress = advanceProgress(progress, { ...stats, label: progressLabel(stats) });
      webMCPState = { phase: "indexing", summary, progress };
      reportProgress();
    };

    const reportProgress = () => {
      const transferred = bytesFetched > 0
        ? ` · ${(bytesFetched / (1024 * 1024)).toFixed(1)} MiB transferred`
        : "";
      const percent = progress?.total > 0
        ? ` · ${Math.floor((progress.completed / progress.total) * 100)}%`
        : "";
      const label = progress?.label || "Downloading verified snapshot columns";
      setStage(`${label}${percent}${transferred}`);
      if (progress?.total > 0) {
        els.loading.setAttribute("aria-valuenow", String(progress.completed));
        els.loading.setAttribute("aria-valuemax", String(progress.total));
      }
    };
    reportProgress();
    const ticker = setInterval(reportProgress, 600);

    let stats;
    try {
      stats = await timedEngineCall((signal) => engine.load({ signal }), 75_000);
    } finally {
      clearInterval(ticker);
    }

    rowsLoaded = true;
    const elapsed = stats.elapsed_ms < 100 ? "under 0.1 s" : `${(stats.elapsed_ms / 1000).toFixed(1)} s`;
    progress = advanceProgress(progress, { phase: "query", label: "Running the first complete search", completed: 30, total: 32 });
    webMCPState = { phase: "indexing", summary, progress };
    els.loading.setAttribute("aria-valuenow", "30");
    els.loading.setAttribute("aria-valuemax", "32");
    setStage(progress.label);

    try {
      await search(true);
    } catch (err) {
      err.phase ??= "query";
      err.retryable = true;
      throw err;
    }
    progress = advanceProgress(progress, { phase: "paint", label: "Painting verified results", completed: 31, total: 32 });
    els.loading.setAttribute("aria-valuenow", "31");
    progress = advanceProgress(progress, { phase: "ready", label: "Search ready", completed: 32, total: 32 });
    els.loading.setAttribute("aria-valuenow", "32");
    els.list.setAttribute("aria-busy", "false");
    webMCPState = { phase: "ready", summary, progress };
    setSearchAvailable(true);
    setStage(`Ready · ${stats.rows.toLocaleString()} listings indexed locally in ${elapsed}`);
    els.loading.hidden = true;
    renderSavedChips();

    // The rollup runs after the first results paint: it re-queries the corpus
    // once per saved search, and nothing about it should delay first light.
    renderRollup().catch(() => {});
  } catch (err) {
    showError(err);
  } finally {
    preparing = false;
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
    runSearch(true, { fromButton: true, historyMode: "push" });
  });

  els.previous.addEventListener("click", () => {
    haptic();
    navigatePage(currentPage - 1);
  });

  els.next.addEventListener("click", () => {
    haptic();
    navigatePage(currentPage + 1);
  });

  els.pageJump.addEventListener("submit", (event) => {
    event.preventDefault();
    navigatePage(Number(els.pageNumber.value));
  });

  els.save.addEventListener("click", () => {
    haptic();
    saveCurrentSearch();
  });

  els.clear.addEventListener("click", () => {
    haptic();
    applyRequest({ titles: terms("f-title") });
    runSearch(true, { historyMode: "push" });
    $("f-title").focus();
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
      if (control.dataset.lifecycleState && selectedStates().length === 0) {
        control.checked = true;
        return;
      }
      updateFilterSummary();
      runSearch(true);
    });
  }

  updateFilterSummary();

  globalThis.addEventListener("popstate", () => {
    const state = parseQuery(globalThis.location.search);
    applyRequest(state.valid ? state.request : {});
    currentPage = state.valid ? state.page : 1;
    runSearch(true, { historyMode: null, preservePage: true, focusResults: true });
  });

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
      updateFilterSummary();
      runSearch(true, { historyMode: "replace" });
    }

    if (!inField && (event.key === "[" || event.key === "]")) {
      event.preventDefault();
      navigatePage(currentPage + (event.key === "]" ? 1 : -1));
    }
  });
}

function hydrateFromURL() {
  const state = parseQuery(globalThis.location.search);
  if (state.valid) {
    currentPage = state.page;
    applyRequest(state.request);
    return;
  }

  applyRequest({});
  els.urlNote.textContent = "This link uses unsupported or invalid search parameters, so no filters were applied. The original URL was left unchanged.";
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
      runSearch(true, { historyMode: "push" });
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
  const states = request.states ?? (request.include_closed
    ? ["open", "stale", "closed", "lapsed"]
    : ["open", "stale"]);
  for (const control of els.form.querySelectorAll("[data-lifecycle-state]")) {
    control.checked = states.includes(control.dataset.lifecycleState);
  }
  $("f-employment").value = request.employment_types?.[0] ?? "";
  $("f-workplace").value = request.workplace_types?.[0] ?? "";
  $("f-since").value = request.posted_since_days > 0 ? String(request.posted_since_days) : "";
  updateFilterSummary();
}

function updateFilterSummary() {
  const request = buildRequest();
  const active = Object.entries(request).reduce((count, [key, value]) => {
    if (key === "titles") return count;
    if (key === "states" && value.join(",") === "open,stale") return count;
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
async function runSearch(reset, { fromButton = false, historyMode = "replace", preservePage = false, focusResults = false } = {}) {
  if (reset && !preservePage) currentPage = 1;
  if (reset && historyMode) syncURL(historyMode);

  if (!rowsLoaded) {
    // Typing during the load is fine: boot's first search reads the form as
    // it stands the moment the rows land, so nothing typed is lost.
    return;
  }

  const button = fromButton ? els.go : null;
  if (button) button.disabled = true;
  els.spin.classList.add("busy");
  els.list.classList.add("searching");

  try {
    await search(reset);
    if (focusResults) focusResultCount();
  } catch (err) {
    if (err?.name !== "AbortError") showError(err);
  } finally {
    if (button) button.disabled = false;
    els.spin.classList.remove("busy");
    els.list.classList.remove("searching");
  }
}

function navigatePage(page) {
  const max = Number(els.pageNumber.max) || 1;
  const nextPage = Math.min(max, Math.max(1, Math.trunc(page) || 1));
  if (nextPage === currentPage) return;
  currentPage = nextPage;
  runSearch(true, { historyMode: "push", preservePage: true, focusResults: true });
}

function focusResultCount() {
  els.count.focus({ preventScroll: true });
  els.results.scrollIntoView({
    block: "start",
    behavior: globalThis.matchMedia("(prefers-reduced-motion: reduce)").matches ? "auto" : "smooth",
  });
}

function syncURL(mode) {
  try {
    const search = queryForRequest(buildRequest(), globalThis.location.search, currentPage);
    globalThis.history[`${mode}State`](null, "", `${globalThis.location.pathname}${search}${globalThis.location.hash}`);
  } catch {
    // Search remains useful when history is unavailable or the query exceeds
    // the bounded shareable format. Never turn a browser feature into a gate.
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

function selectedStates() {
  return [...els.form.querySelectorAll("[data-lifecycle-state]:checked")]
    .map((control) => control.dataset.lifecycleState);
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
    states: selectedStates(),
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
    lastRequest = buildRequest();
  }

  const controller = new AbortController();
  searchController = controller;
  let response;
  try {
    offset = (currentPage - 1) * PAGE_SIZE;
    response = await engine.search({ ...lastRequest, offset, limit: PAGE_SIZE }, { signal: controller.signal });
  } finally {
    if (searchController === controller) searchController = null;
  }

  // A newer search finished the race while this one was in flight; painting
  // these rows now would show stale results under fresh filters.
  if (seq !== searchSeq) {
    return;
  }

  try {
    // Entrance stagger belongs to fresh arrivals, not to every keystroke: the
    // first real paint of the list gets rhythm, retyping gets immediacy.
    const firstPaint = els.list.querySelector(".card:not(.skeleton)") === null;

    els.list.replaceChildren();

    els.list.classList.toggle("instant", !firstPaint);

    response.items.forEach((item, i) => {
      const card = renderCard(document, item, {
        snapshotLevel: snapshotStatus(summary, new Date()).level,
        nowISO: new Date().toISOString(),
        timeAgo,
      });
      card.style.setProperty("--stagger", `${Math.min(i, 12) * 25}ms`);
      els.list.append(card);
    });

    const totalPages = Math.max(1, Math.ceil(response.matched / PAGE_SIZE));
    if (currentPage > totalPages) {
      currentPage = totalPages;
      syncURL("replace");
      return search(true);
    }
    renderCount(response);
    renderPagination(response.matched, totalPages);
  } catch (err) {
    err.phase = "paint";
    err.retryable = true;
    throw err;
  }
}

function renderCount(response) {
  if (response.matched === 0) {
    els.count.textContent = "No postings match.";
    renderEmptyState();
    return;
  }

  const level = snapshotStatus(summary, new Date()).level;
  els.count.textContent = resultCountText(response, offset + response.items.length, level);
}

function renderPagination(matched, totalPages) {
  els.pagination.hidden = matched <= PAGE_SIZE;
  els.previous.disabled = currentPage === 1;
  els.next.disabled = currentPage === totalPages;
  els.pageNumber.value = String(currentPage);
  els.pageNumber.max = String(totalPages);
  els.pageTotal.textContent = `of ${totalPages.toLocaleString()}`;
  els.pagination.setAttribute("aria-label", `Result pages, page ${currentPage} of ${totalPages}`);
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
    runSearch(true, { historyMode: "push" });
    $("f-title").focus();
  });
  empty.append(clear);

  els.list.replaceChildren(empty);
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

function progressLabel(update) {
  if (update.phase === "decode") {
    if (update.completed >= 12) return "Decoding compensation";
    if (update.completed >= 11) return "Decoding dates and remote state";
    if (update.completed >= 10) return "Decoding listing links";
    return "Decoding search columns";
  }
  return {
    network: "Downloading verified snapshot columns",
    fold: "Folding text for case-insensitive search",
    state: update.completed >= 23 ? "Computing lifecycle state" : "Computing date and remote state",
    sort: update.completed >= 30 ? "Search index built" : "Building newest-first order",
  }[update.phase] ?? "Preparing complete local search";
}

function setSearchAvailable(available) {
  els.go.hidden = !available;
  els.save.hidden = !available;
  els.go.disabled = !available;
  els.save.disabled = !available;
  els.formReadiness.hidden = available;
  if (!available) {
    els.saved.hidden = true;
    els.formReadiness.textContent = "You can set filters now. Search becomes available after the complete snapshot is indexed locally.";
  }
}

async function timedEngineCall(call, timeoutMS) {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMS);
  try {
    return await call(controller.signal);
  } finally {
    clearTimeout(timer);
  }
}

function showError(err) {
  console.error(err);
  els.loading.hidden = true;

  const failure = failureState(err, progress?.phase ?? "metadata");
  webMCPState = { phase: "error", summary, progress, error: failure };

  // Skeletons promise results; an error must not leave that promise shimmering.
  if (els.list.querySelector(".skeleton")) {
    els.list.replaceChildren();
  }

  els.error.hidden = false;

  // Offline is a state, not a bug; say so before the technical detail.
  const offline = navigator.onLine === false ? "You appear to be offline, and this data is not cached yet. " : "";

  els.error.replaceChildren();
  const message = document.createElement("p");
  const phaseLabel = {
    metadata: "snapshot metadata verification",
    network: "snapshot download",
    decode: "column decoding",
    fold: "search text folding",
    state: "listing state calculation",
    sort: "result ordering",
    query: "initial complete query",
    paint: "initial result paint",
    worker: "search worker",
  }[failure.phase] ?? "snapshot preparation";
  let detail = `The ${phaseLabel} phase failed. ${failure.action}.`;
  if (err?.name === "TimeoutError") {
    detail = `The ${phaseLabel} phase exceeded its time limit and the worker was stopped. ${failure.action} on a stable connection.`;
  } else if (/HTTP 404/.test(String(err?.message ?? err))) {
    detail = `The ${phaseLabel} phase found that the published snapshot changed while loading. ${failure.action}.`;
  }
  message.textContent = offline + detail;
  const retry = document.createElement("button");
  retry.type = "button";
  retry.className = "secondary";
  retry.textContent = failure.retryable ? "Retry in this tab" : "Reload page";
  retry.addEventListener("click", () => {
    retry.disabled = true;
    if (failure.retryable) void boot({ retry: true });
    else globalThis.location.reload();
  });
  els.error.append(message, retry);
  els.error.tabIndex = -1;
  els.error.focus();
  els.go.disabled = true;
  els.list.setAttribute("aria-busy", "false");
  els.formReadiness.hidden = false;
  els.formReadiness.textContent = "Your filters are unchanged. Search will return after recovery succeeds.";
  const retained = summary ? "Verified metadata and filters were retained." : "Your filters were retained.";
  setStage(`Stopped during ${phaseLabel}. ${retained}`);
}

function addSpan(parent, className, text) {
  const span = document.createElement("span");
  if (className) span.className = className;
  span.textContent = text;
  parent.append(span);
}
