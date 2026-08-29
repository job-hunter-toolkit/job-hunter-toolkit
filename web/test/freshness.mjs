import { resultCountText, snapshotStatus } from "../freshness.js";
import { exit } from "node:process";

let failures = 0;
function check(name, got, want) {
  if (got !== want) {
    failures++;
    console.error(`FAIL ${name}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`);
  } else {
    console.log(`ok   ${name}`);
  }
}

const now = new Date("2026-08-29T12:00:00Z");
check("fresh level", snapshotStatus({ run_at: "2026-08-29T03:00:00Z" }, now).level, "fresh");
check("aging boundary", snapshotStatus({ run_at: "2026-08-27T12:00:00Z" }, now).level, "aging");
check("old level", snapshotStatus({ run_at: "2026-08-07T10:30:11Z" }, now).level, "old");
check("old relative", snapshotStatus({ run_at: "2026-08-07T10:30:11Z" }, now).relative, "collected 3 weeks ago");
check("exact UTC", snapshotStatus({ run_at: "2026-08-07T10:30:11Z" }, now).exact.includes("Aug 7, 2026"), true);
check("unknown date", snapshotStatus({ run_at: "broken" }, now).level, "unknown");
check(
  "old counts avoid lifecycle alarm",
  resultCountText({ matched: 1376107, states: { stale: 1376107 } }, 100, "old"),
  "1,376,107 listings in this snapshot, newest first, showing 100. Availability may have changed since collection.",
);
check(
  "fresh counts explain stale semantics",
  resultCountText({ matched: 12, states: { open: 10, stale: 2 } }, 12, "fresh"),
  "12 matches (10 recently checked · 2 not recently checked), newest first, showing 12",
);

if (failures) exit(1);
console.log("freshness test passed");
