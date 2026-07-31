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
  { titles: ["go", "rust"] },
);
check(
  "sameRequest: order and case insensitive",
  sameRequest({ titles: ["Go", "rust"], remote: true }, { titles: ["RUST", "go"], remote: true }),
  true,
);
check("sameRequest: differing filters differ", sameRequest({ titles: ["go"] }, { titles: ["go"], remote: true }), false);
check("isEmpty: blank request", isEmptyRequest({ titles: [""], min_annual: 0 }), true);
check("isEmpty: any constraint counts", isEmptyRequest({ remote: true }), false);

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

if (failures > 0) {
  console.error(`${failures} failure(s)`);
  exit(1);
}

console.log("rollup test passed");
