// rollup.js — saved searches, visit state, and the open-time rollup.
//
// Everything here is deliberately pure or storage-thin so web/test/rollup.mjs
// can exercise it under Node. The DOM work stays in app.js. Persistence is
// localStorage behind guarded helpers: privacy modes that refuse storage
// degrade to a page that simply never remembers, never to an error.
//
// The rollup is pull-only by design. Nothing is ever sent anywhere; the
// summary is computed from the corpus already in memory, at the moment the
// person opens the page. Spam is structurally impossible.

export const SAVED_KEY = "jht.saved.v3";
export const LEGACY_V2_SAVED_KEY = "jht.saved.v2";
export const LEGACY_SAVED_KEY = "jht.saved.v1";
export const VISIT_KEY = "jht.visit.v1";
export const STREAK_KEY = "jht.streak.v1";

export const MAX_SAVED = 8;
export const SAVED_STATE_VERSION = 3;

const LIFECYCLE_STATES = ["open", "stale", "closed", "lapsed"];
const DEFAULT_STATES = ["open", "stale"];

// searchName derives a short human name from a search request: the strongest
// constraint leads, one qualifier follows. "security engineer, remote" beats
// a JSON dump.
export function searchName(request) {
  const first =
    request.titles?.join(", ") ||
    request.companies?.join(", ") ||
    request.departments?.join(", ") ||
    request.locations?.join(", ") ||
    "";

  const qualifiers = [];
  if (request.remote) qualifiers.push("remote");
  else if (request.workplace_types?.length) qualifiers.push(request.workplace_types.join(", "));
  if (request.locations?.length && first !== request.locations.join(", ")) {
    qualifiers.push(request.locations.join(", "));
  }
  if (request.min_annual > 0) qualifiers.push(`${Math.round(request.min_annual / 1000)}k+`);

  const name = [first, qualifiers.join(", ")].filter(Boolean).join(", ");

  return name || "everything";
}

// normalizeRequest strips paging and empties so two ways of writing the same
// search compare equal.
export function normalizeRequest(request) {
  const out = {};
  const lists = ["titles", "exclude_titles", "locations", "companies", "departments", "employment_types", "workplace_types"];

  for (const key of lists) {
    const values = [...new Set((request[key] ?? []).map((v) => v.trim().toLowerCase()).filter(Boolean))].sort();
    if (values.length) out[key] = values;
  }
  const selected = request.states ?? (request.include_closed ? LIFECYCLE_STATES : DEFAULT_STATES);
  out.states = LIFECYCLE_STATES.filter((state) => selected.includes(state));
  if (request.remote) out.remote = true;
  if (request.has_compensation) out.has_compensation = true;
  if (request.min_annual > 0) out.min_annual = request.min_annual;
  if (request.posted_since_days > 0) out.posted_since_days = request.posted_since_days;

  return out;
}

export function sameRequest(a, b) {
  return JSON.stringify(normalizeRequest(a)) === JSON.stringify(normalizeRequest(b));
}

export function isEmptyRequest(request) {
  const normalized = normalizeRequest(request);
  return Object.keys(normalized).length === 1 && normalized.states.join(",") === DEFAULT_STATES.join(",");
}

// savedState is the durable boundary for saved searches. Version 3 records an
// explicit lifecycle-state selection. Earlier booleans migrate without
// changing meaning: false or absent becomes open+stale, true becomes all four.
function savedState(value) {
  if (!value || value.version !== SAVED_STATE_VERSION || !Array.isArray(value.searches)) {
    return null;
  }

  return { version: SAVED_STATE_VERSION, searches: validSearches(value.searches) };
}

function validSearches(searches, { legacy = false } = {}) {
  const valid = [];
  for (const entry of searches) {
    if (
      !entry ||
      typeof entry.id !== "string" ||
      entry.id.length === 0 ||
      entry.id.length > 128 ||
      typeof entry.name !== "string" ||
      entry.name.length === 0 ||
      entry.name.length > 160 ||
      !validRequestShape(entry.request, { legacy })
    ) continue;

    let request;
    try {
      request = normalizeRequest(entry.request);
    } catch {
      continue;
    }
    if (isEmptyRequest(request)) continue;

    valid.push({
      id: entry.id,
      name: entry.name,
      request,
      ...(typeof entry.createdAt === "string" ? { createdAt: entry.createdAt } : {}),
    });
    if (valid.length === MAX_SAVED) break;
  }

  return valid;
}

function validRequestShape(request, { legacy = false } = {}) {
  if (!request || typeof request !== "object" || Array.isArray(request)) return false;

  const lists = ["titles", "exclude_titles", "locations", "companies", "departments", "employment_types", "workplace_types"];
  for (const key of lists) {
    if (request[key] !== undefined && (!Array.isArray(request[key]) || request[key].some((value) => typeof value !== "string"))) {
      return false;
    }
  }
  for (const key of ["remote", "has_compensation"]) {
    if (request[key] !== undefined && typeof request[key] !== "boolean") return false;
  }
  if (request.include_closed !== undefined && (!legacy || typeof request.include_closed !== "boolean")) return false;
  if (request.include_closed !== undefined && request.states !== undefined) return false;
  if (request.states !== undefined && (
    !Array.isArray(request.states) ||
    request.states.length === 0 ||
    request.states.length > LIFECYCLE_STATES.length ||
    new Set(request.states).size !== request.states.length ||
    request.states.some((state) => !LIFECYCLE_STATES.includes(state))
  )) return false;
  if (!legacy && request.include_closed !== undefined) return false;
  for (const key of ["min_annual", "posted_since_days"]) {
    if (request[key] !== undefined && (typeof request[key] !== "number" || !Number.isFinite(request[key]))) return false;
  }

  return true;
}

export function loadSavedSearches(store = storage) {
  const current = store.load(SAVED_KEY, null);
  if (current !== null) {
    return savedState(current)?.searches ?? [];
  }

  const v2 = store.load(LEGACY_V2_SAVED_KEY, null);
  if (v2 !== null) {
    if (!v2 || v2.version !== 2 || !Array.isArray(v2.searches)) return [];
    const migrated = { version: SAVED_STATE_VERSION, searches: validSearches(v2.searches, { legacy: true }) };
    store.save(SAVED_KEY, migrated);
    return migrated.searches;
  }

  const legacy = store.load(LEGACY_SAVED_KEY, null);
  if (!Array.isArray(legacy)) return [];

  const migrated = { version: SAVED_STATE_VERSION, searches: validSearches(legacy, { legacy: true }) };
  store.save(SAVED_KEY, migrated);

  return migrated.searches;
}

export function saveSavedSearches(searches, store = storage) {
  const current = store.load(SAVED_KEY, null);
  if (current?.version > SAVED_STATE_VERSION) return false;
  const legacyV2 = store.load(LEGACY_V2_SAVED_KEY, null);
  if (current === null && legacyV2?.version > SAVED_STATE_VERSION) return false;

  store.save(SAVED_KEY, {
    version: SAVED_STATE_VERSION,
    searches: Array.isArray(searches) ? validSearches(searches) : [],
  });

  return true;
}

// Export and import are data-only on purpose. A later UI can wire file or
// clipboard controls without inventing another format or gaining access to
// anything beyond the searches the user explicitly saved.
export function exportSavedSearches(searches) {
  return `${JSON.stringify({
    version: SAVED_STATE_VERSION,
    searches: Array.isArray(searches) ? validSearches(searches) : [],
  }, null, 2)}\n`;
}

export function importSavedSearches(text) {
  const decoded = JSON.parse(text);
  let state;
  if (Array.isArray(decoded)) {
    state = { version: SAVED_STATE_VERSION, searches: validSearches(decoded, { legacy: true }) };
  } else if (decoded?.version === 2 && Array.isArray(decoded.searches)) {
    state = { version: SAVED_STATE_VERSION, searches: validSearches(decoded.searches, { legacy: true }) };
  } else {
    state = savedState(decoded);
  }

  if (!state) throw new Error("Unsupported saved-search export");

  return state.searches;
}

// countNewSince counts postings first observed after `sinceISO`. first_seen is
// the corpus's own "when did this appear" fact; posted_at is the board's
// claim, used only when first_seen is absent.
export function countNewSince(items, sinceISO) {
  if (!sinceISO) return 0;

  let count = 0;
  for (const item of items) {
    const seen = item.first_seen || item.posted_at || "";
    if (seen > sinceISO) count += 1;
  }

  return count;
}

// nextStreak advances a consecutive-days counter. Opening twice in one day
// holds; opening on the next calendar day advances; a gap resets to 1.
export function nextStreak(prev, todayISO) {
  const today = todayISO.slice(0, 10);

  if (!prev || !prev.last) return { last: today, n: 1 };
  if (prev.last === today) return prev;

  const dayMS = 24 * 60 * 60 * 1000;
  const gap = Math.round((Date.parse(today) - Date.parse(prev.last)) / dayMS);

  return { last: today, n: gap === 1 ? prev.n + 1 : 1 };
}

export function greeting(hour) {
  if (hour < 5) return "Up late";
  if (hour < 12) return "Good morning";
  if (hour < 18) return "Good afternoon";

  return "Good evening";
}

// sinceLabel renders the gap since the previous visit as a phrase that reads
// naturally after a greeting: "Since your last visit" / "In the last 9 hours"
// / "In the last 211 days".
export function sinceLabel(sinceISO, nowISO) {
  const ms = Date.parse(nowISO) - Date.parse(sinceISO);
  const hours = ms / (60 * 60 * 1000);

  if (hours < 1.5) return "Since your last visit";
  if (hours < 36) return `In the last ${Math.round(hours)} hours`;

  return `In the last ${Math.round(hours / 24)} days`;
}

// timeAgo renders a timestamp the way a person says it. The exact date
// belongs in a tooltip; the card says "3 hours ago". Falls back to the bare
// date beyond a year, where relative time stops meaning anything.
export function timeAgo(iso, nowISO) {
  const then = Date.parse(iso);
  const now = Date.parse(nowISO);

  if (!Number.isFinite(then) || !Number.isFinite(now) || then > now) return "";

  const minutes = Math.floor((now - then) / 60000);
  if (minutes < 1) return "just now";
  if (minutes < 60) return minutes === 1 ? "1 minute ago" : `${minutes} minutes ago`;

  const hours = Math.floor(minutes / 60);
  if (hours < 24) return hours === 1 ? "1 hour ago" : `${hours} hours ago`;

  const days = Math.floor(hours / 24);
  if (days === 1) return "yesterday";
  if (days < 7) return `${days} days ago`;

  const weeks = Math.floor(days / 7);
  if (weeks < 5) return weeks === 1 ? "1 week ago" : `${weeks} weeks ago`;

  const months = Math.floor(days / 30);
  if (months < 12) return months === 1 ? "1 month ago" : `${months} months ago`;

  return iso.slice(0, 10);
}

// storage wraps localStorage so a refusal (private mode, disabled storage)
// reads as "nothing saved" and writes are silently dropped.
export const storage = {
  load(key, fallback) {
    try {
      const raw = globalThis.localStorage?.getItem(key);

      return raw ? JSON.parse(raw) : fallback;
    } catch {
      return fallback;
    }
  },

  save(key, value) {
    try {
      globalThis.localStorage?.setItem(key, JSON.stringify(value));
    } catch {
      /* storage refused; the page just will not remember */
    }
  },
};
