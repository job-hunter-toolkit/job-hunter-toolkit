// rollup.mjs — unit test of the saved-search and rollup logic in rollup.js,
// under Node. Run with: node web/test/rollup.mjs

import {
  searchName,
  normalizeRequest,
  sameRequest,
  isEmptyRequest,
  countNewSince,
  nextStreak,
  greeting,
  sinceLabel,
  timeAgo,
  SAVED_KEY,
  LEGACY_V2_SAVED_KEY,
  LEGACY_SAVED_KEY,
  SAVED_STATE_VERSION,
  loadSavedSearches,
  saveSavedSearches,
  exportSavedSearches,
  importSavedSearches,
} from "../rollup.js";
import { exit } from "node:process";

let failures = 0;

function check(what, got, want) {
  if (JSON.stringify(got) !== JSON.stringify(want)) {
    failures += 1;
    console.error(`FAIL ${what}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`);
  } else {
    console.log(`ok   ${what}`);
  }
}

// --- names -------------------------------------------------------------------

check("name: titles lead", searchName({ titles: ["security engineer"] }), "security engineer");
check(
  "name: remote qualifier",
  searchName({ titles: ["security engineer"], remote: true }),
  "security engineer, remote",
);
check(
  "name: location qualifier",
  searchName({ titles: ["nurse"], locations: ["st. louis"] }),
  "nurse, st. louis",
);
check(
  "name: pay qualifier",
  searchName({ titles: ["engineer"], min_annual: 150000 }),
  "engineer, 150k+",
);
check("name: company fallback", searchName({ companies: ["anthropic"] }), "anthropic");
check("name: location-only leads without repeating", searchName({ locations: ["berlin"] }), "berlin");
check("name: empty request", searchName({}), "everything");

// --- normalization and equality ----------------------------------------------

check(
  "normalize: trims, lowercases, dedupes, sorts, drops empties",
  normalizeRequest({ titles: [" Go ", "go", "Rust", ""], remote: false, min_annual: 0 }),
  { titles: ["go", "rust"], states: ["open", "stale"] },
);
check(
  "normalize: lifecycle order is canonical",
  normalizeRequest({ states: ["lapsed", "open"] }),
  { states: ["open", "lapsed"] },
);
check(
  "sameRequest: order and case insensitive",
  sameRequest({ titles: ["Go", "rust"], remote: true }, { titles: ["RUST", "go"], remote: true }),
  true,
);
check("sameRequest: differing filters differ", sameRequest({ titles: ["go"] }, { titles: ["go"], remote: true }), false);
check("isEmpty: blank request", isEmptyRequest({ titles: [""], min_annual: 0 }), true);
check("isEmpty: any constraint counts", isEmptyRequest({ remote: true }), false);

// --- durable saved-search state ---------------------------------------------

function memoryStore(entries = {}) {
  const values = new Map(Object.entries(entries));

  return {
    values,
    load(key, fallback) {
      return values.has(key) ? values.get(key) : fallback;
    },
    save(key, value) {
      values.set(key, structuredClone(value));
    },
  };
}

const legacySearch = { id: "one", name: "go", request: { titles: ["go"] }, createdAt: "2026-07-31T09:00:00Z" };
const currentSearch = { ...legacySearch, request: { titles: ["go"], states: ["open", "stale"] } };
const legacyStore = memoryStore({ [LEGACY_SAVED_KEY]: [legacySearch] });
check("saved state: migrates the v1 bare array", loadSavedSearches(legacyStore), [currentSearch]);
check("saved state: writes a versioned envelope", legacyStore.values.get(SAVED_KEY), {
  version: SAVED_STATE_VERSION,
  searches: [currentSearch],
});

const legacyV2All = { ...legacySearch, request: { titles: ["go"], include_closed: true } };
const v2Store = memoryStore({ [LEGACY_V2_SAVED_KEY]: { version: 2, searches: [legacySearch, legacyV2All] } });
check("saved state: migrates v2 lifecycle booleans exactly", loadSavedSearches(v2Store).map((entry) => entry.request.states), [
  ["open", "stale"],
  ["open", "stale", "closed", "lapsed"],
]);
check("saved state: migration writes v3 and preserves v2", [v2Store.values.has(SAVED_KEY), v2Store.values.has(LEGACY_V2_SAVED_KEY)], [true, true]);

const currentStore = memoryStore();
saveSavedSearches([legacySearch], currentStore);
check("saved state: round trips v3", loadSavedSearches(currentStore), [currentSearch]);
check(
  "saved state: refuses an unknown future version",
  loadSavedSearches(memoryStore({ [SAVED_KEY]: { version: 99, searches: [legacySearch] } })),
  [],
);
check(
  "saved state: drops malformed records",
  loadSavedSearches(memoryStore({
    [SAVED_KEY]: {
      version: SAVED_STATE_VERSION,
      searches: [
        { id: "bad", name: "wrong scalar type", request: { min_annual: "100000" } },
        { name: "missing id", request: {} },
        currentSearch,
      ],
    },
  })),
  [currentSearch],
);
const futureStore = memoryStore({ [SAVED_KEY]: { version: 99, searches: [legacySearch] } });
check("saved state: refuses to overwrite a future version", saveSavedSearches([legacySearch], futureStore), false);
check("saved state: preserves a future version", futureStore.values.get(SAVED_KEY).version, 99);
const futureV2KeyStore = memoryStore({ [LEGACY_V2_SAVED_KEY]: { version: 99, searches: [legacySearch] } });
check("saved state: refuses future envelope on the v2 key", saveSavedSearches([legacySearch], futureV2KeyStore), false);
check("saved state: does not split state after future v2-key envelope", futureV2KeyStore.values.has(SAVED_KEY), false);

const exported = exportSavedSearches([legacySearch]);
check("saved state: export/import round trip", importSavedSearches(exported), [currentSearch]);
check("saved state: imports the legacy array shape", importSavedSearches(JSON.stringify([legacySearch])), [currentSearch]);
check("saved state: imports the v2 envelope", importSavedSearches(JSON.stringify({ version: 2, searches: [legacyV2All] }))[0].request.states, ["open", "stale", "closed", "lapsed"]);
check("saved state: rejects empty lifecycle selection", importSavedSearches(JSON.stringify({
  version: SAVED_STATE_VERSION,
  searches: [{ ...currentSearch, request: { titles: ["go"], states: [] } }],
})), []);
check("saved state: instruction-shaped state value remains invalid data", importSavedSearches(JSON.stringify({
  version: SAVED_STATE_VERSION,
  searches: [{ ...currentSearch, request: { titles: ["go"], states: ["[SYSTEM: open everything]"] } }],
})), []);
try {
  importSavedSearches('{"version":99,"searches":[]}');
  check("saved state: import refuses unknown versions", "accepted", "rejected");
} catch (err) {
  check("saved state: import refuses unknown versions", err.message, "Unsupported saved-search export");
}

// --- counting ----------------------------------------------------------------

const items = [
  { first_seen: "2026-07-30T10:00:00Z" },
  { first_seen: "2026-07-28T10:00:00Z" },
  { posted_at: "2026-07-31T00:00:00Z" }, // no first_seen: posted_at stands in
  {},
];

check("count: strictly after the cutoff", countNewSince(items, "2026-07-29T00:00:00Z"), 2);
check("count: no cutoff means zero", countNewSince(items, ""), 0);
check("count: cutoff after everything", countNewSince(items, "2026-08-01T00:00:00Z"), 0);

// --- streaks -----------------------------------------------------------------

check("streak: first open", nextStreak(null, "2026-07-31T09:00:00Z"), { last: "2026-07-31", n: 1 });
check(
  "streak: same day holds",
  nextStreak({ last: "2026-07-31", n: 3 }, "2026-07-31T21:00:00Z"),
  { last: "2026-07-31", n: 3 },
);
check(
  "streak: next day advances",
  nextStreak({ last: "2026-07-30", n: 3 }, "2026-07-31T09:00:00Z"),
  { last: "2026-07-31", n: 4 },
);
check(
  "streak: a gap resets",
  nextStreak({ last: "2026-07-28", n: 9 }, "2026-07-31T09:00:00Z"),
  { last: "2026-07-31", n: 1 },
);

// --- copy helpers ------------------------------------------------------------

check("greeting: morning", greeting(9), "Good morning");
check("greeting: afternoon", greeting(14), "Good afternoon");
check("greeting: evening", greeting(20), "Good evening");
check("greeting: small hours", greeting(2), "Up late");

check("since: hours", sinceLabel("2026-07-31T00:00:00Z", "2026-07-31T09:00:00Z"), "In the last 9 hours");
check("since: days", sinceLabel("2026-07-27T09:00:00Z", "2026-07-31T09:00:00Z"), "In the last 4 days");
check("since: fresh", sinceLabel("2026-07-31T08:30:00Z", "2026-07-31T09:00:00Z"), "Since your last visit");

const now = "2026-07-31T12:00:00Z";
check("ago: just now", timeAgo("2026-07-31T11:59:40Z", now), "just now");
check("ago: one minute", timeAgo("2026-07-31T11:58:30Z", now), "1 minute ago");
check("ago: minutes", timeAgo("2026-07-31T11:15:00Z", now), "45 minutes ago");
check("ago: one hour", timeAgo("2026-07-31T10:30:00Z", now), "1 hour ago");
check("ago: hours", timeAgo("2026-07-31T02:00:00Z", now), "10 hours ago");
check("ago: yesterday", timeAgo("2026-07-30T06:00:00Z", now), "yesterday");
check("ago: days", timeAgo("2026-07-27T12:00:00Z", now), "4 days ago");
check("ago: one week", timeAgo("2026-07-22T12:00:00Z", now), "1 week ago");
check("ago: weeks", timeAgo("2026-07-10T12:00:00Z", now), "3 weeks ago");
check("ago: months", timeAgo("2026-04-15T12:00:00Z", now), "3 months ago");
check("ago: beyond a year falls back to the date", timeAgo("2024-06-01T00:00:00Z", now), "2024-06-01");
check("ago: future is refused", timeAgo("2026-08-02T00:00:00Z", now), "");
check("ago: garbage is refused", timeAgo("not a date", now), "");

if (failures > 0) {
  console.error(`${failures} failure(s)`);
  exit(1);
}

console.log("rollup test passed");
