// Freshness copy and thresholds live here so time-dependent UI remains
// deterministic under Node tests. A snapshot age and a posting lifecycle state
// are different facts: snapshot age says when this corpus was assembled;
// "stale" says the source had not been successfully rechecked recently at the
// time the query was answered.

const HOUR = 60 * 60 * 1000;
const DAY = 24 * HOUR;

export function snapshotStatus(summary, now = new Date()) {
  const generation = Number.isSafeInteger(summary?.generation) && summary.generation > 0
    ? ` · generation ${summary.generation}`
    : "";
  const collected = new Date(summary?.run_at ?? "");
  if (!Number.isFinite(collected.getTime())) {
    return {
      level: "unknown",
      label: `Snapshot date unavailable${generation}`,
      relative: "collection time unknown",
      exact: "Collection time unavailable",
      explanation: "Search results are available, but their collection time could not be verified.",
    };
  }

  const age = Math.max(0, now.getTime() - collected.getTime());
  const relative = relativeAge(collected, now);
  const exact = new Intl.DateTimeFormat("en", {
    dateStyle: "medium",
    timeStyle: "short",
    timeZone: "UTC",
  }).format(collected) + " UTC";

  if (age <= 36 * HOUR) {
    return {
      level: "fresh",
      label: `Current snapshot${generation}`,
      relative,
      exact,
      explanation: "A historical snapshot, not a live check. Listings were visible at their boards’ latest successful checks.",
    };
  }

  if (age <= 8 * DAY) {
    return {
      level: "aging",
      label: `Snapshot update delayed${generation}`,
      relative,
      exact,
      explanation: "Updates are normally published daily. You can still search this snapshot, but availability may have changed since collection.",
    };
  }

  return {
    level: "old",
    label: `Older snapshot${generation}`,
    relative,
    exact,
    explanation: "Publication is delayed. You can still search this historical snapshot, but availability may have changed since collection.",
  };
}

export function relativeAge(then, now = new Date()) {
  const ms = Math.max(0, now.getTime() - then.getTime());
  const minutes = Math.floor(ms / 60000);
  if (minutes < 1) return "collected just now";
  if (minutes < 60) return `collected ${minutes} ${minutes === 1 ? "minute" : "minutes"} ago`;

  const hours = Math.floor(ms / HOUR);
  if (hours < 24) return `collected ${hours} ${hours === 1 ? "hour" : "hours"} ago`;

  const days = Math.floor(ms / DAY);
  if (days < 14) return `collected ${days} ${days === 1 ? "day" : "days"} ago`;

  const weeks = Math.floor(days / 7);
  return `collected ${weeks} ${weeks === 1 ? "week" : "weeks"} ago`;
}

export function resultCountText(response, shown, snapshotLevel) {
  if (response.matched === 0) return "No postings match.";

  const total = response.matched.toLocaleString();
  const window = Math.min(shown, response.matched).toLocaleString();
  if (snapshotLevel === "old" || snapshotLevel === "unknown") {
    return `${total} listings in this snapshot, newest first, showing ${window}. Availability may have changed since collection.`;
  }

  const states = response.states ?? {};
  const parts = [];
  if (states.open) parts.push(`${states.open.toLocaleString()} recently checked`);
  if (states.stale) parts.push(`${states.stale.toLocaleString()} not recently checked`);
  if (states.closed) parts.push(`${states.closed.toLocaleString()} closed in snapshot`);
  if (states.lapsed) parts.push(`${states.lapsed.toLocaleString()} source status unknown`);

  return `${total} matches${parts.length ? ` (${parts.join(" · ")})` : ""}, newest first, showing ${window}`;
}
