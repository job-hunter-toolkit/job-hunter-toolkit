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

export const SAVED_KEY = "jht.saved.v1";
export const VISIT_KEY = "jht.visit.v1";
export const STREAK_KEY = "jht.streak.v1";

export const MAX_SAVED = 8;

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
  if (request.remote) out.remote = true;
  if (request.has_compensation) out.has_compensation = true;
  if (request.include_closed) out.include_closed = true;
  if (request.min_annual > 0) out.min_annual = request.min_annual;
  if (request.posted_since_days > 0) out.posted_since_days = request.posted_since_days;

  return out;
}

export function sameRequest(a, b) {
  return JSON.stringify(normalizeRequest(a)) === JSON.stringify(normalizeRequest(b));
}

export function isEmptyRequest(request) {
  return Object.keys(normalizeRequest(request)).length === 0;
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
