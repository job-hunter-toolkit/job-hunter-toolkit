// card.js renders the engine's bounded CardView. Corpus strings are untrusted:
// every value enters the DOM through textContent, and only HTTP(S) URLs become
// links. This module has no corpus or query semantics.

const STATE_DETAILS = {
  stale: "Visible on the company's board at its last successful check, but that source has not been checked recently.",
  closed: "Gone from the company's board after two later checks agreed it was removed.",
  lapsed: "The company's board has not had a successful check recently enough to know this posting's status.",
};

export function renderCard(document, item, { snapshotLevel, nowISO, timeAgo }) {
  const view = item.view ?? {};
  const li = document.createElement("li");
  li.className = "card";

  const url = safeHTTPURL(item.url);
  const title = document.createElement(url ? "a" : "span");
  title.className = "title";
  title.textContent = view.title || "Untitled posting";
  if (url) {
    title.href = url;
    title.target = "_blank";
    title.rel = "noopener noreferrer";
    title.setAttribute("aria-label", view.accessible_name || `${view.title || "Untitled posting"} (opens in a new tab)`);
  } else {
    li.setAttribute("aria-label", view.accessible_name?.replace(" (opens in a new tab)", "") || view.title || "Untitled posting");
  }
  li.append(title);

  const where = document.createElement("div");
  where.className = "where";
  addText(document, where, "company", view.company || "Unknown employer");
  const location = locationPresentation(item.location || view.location || "");
  if (location.summary) addText(document, where, "location", location.summary);
  li.append(where);

  if (location.disclose) {
    const details = document.createElement("details");
    details.className = "location-details";
    const summary = document.createElement("summary");
    summary.textContent = "View location details";
    const full = document.createElement("p");
    full.textContent = location.full;
    details.append(summary, full);
    if (location.truncated) {
      const note = document.createElement("p");
      note.className = "location-note";
      note.textContent = url
        ? "The browser result is bounded. Open the posting title for the complete source list."
        : "The browser result is bounded; the complete source list is unavailable here.";
      details.append(note);
    }
    li.append(details);
  }

  const facts = document.createElement("div");
  facts.className = "card-facts";

  const status = group(document, "Status and recency", "meta status-meta");
  if (item.state === "stale" && snapshotLevel !== "old") {
    addFact(document, status, "Not recently checked", "state-stale", STATE_DETAILS.stale);
  } else if (item.state === "closed" || item.state === "lapsed") {
    addFact(document, status, item.state === "closed" ? "Closed in snapshot" : "Source status unknown", `state-${item.state}`, STATE_DETAILS[item.state]);
  }
  if (item.date_anomaly === "future") {
    addFact(document, status, "Source date appears in the future", "date-anomaly");
    addDate(document, status, "Source date", item.posted_at);
  } else if (item.posted_at) {
    addDate(document, status, "Posted", item.posted_at, timeAgo(item.posted_at, nowISO));
  } else if (item.first_seen) {
    addDate(document, status, "First seen", item.first_seen, timeAgo(item.first_seen, nowISO));
  } else {
    addFact(document, status, "Posting date unknown", "unknown");
  }
  if (status.children.length) facts.append(status);

  const terms = group(document, "Job details", "meta detail-meta");
  if (item.compensation) addFact(document, terms, item.compensation, "pay", `Compensation: ${item.compensation}`);
  if (view.remote_eligibility) addFact(document, terms, view.remote_eligibility, "eligibility");
  if (view.workplace) addFact(document, terms, view.workplace, "workplace", `Workplace: ${view.workplace}`);
  if (view.employment) addFact(document, terms, view.employment, "employment", `Employment: ${view.employment}`);
  if (view.seniority) addFact(document, terms, view.seniority, "seniority", `Seniority: ${view.seniority}`);
  if (terms.children.length) facts.append(terms);

  if (view.organization) {
    const organization = group(document, "Organization", "meta org-meta");
    addFact(document, organization, `Organization: ${view.organization}`, "organization");
    facts.append(organization);
  }
  li.append(facts);

  if (view.source) {
    const source = document.createElement("div");
    source.className = "card-source";
    source.textContent = view.source;
    li.append(source);
  }

  return li;
}

function group(document, label, className) {
  const node = document.createElement("div");
  node.className = className;
  node.setAttribute("role", "list");
  node.setAttribute("aria-label", label);
  return node;
}

function addFact(document, parent, text, className = "", accessibleText = "") {
  const span = document.createElement("span");
  span.className = `badge ${className}`.trim();
  span.setAttribute("role", "listitem");
  span.textContent = text;
  if (accessibleText) span.setAttribute("aria-label", accessibleText);
  parent.append(span);
  return span;
}

function addDate(document, parent, label, value, relative = "") {
  const fact = addFact(document, parent, "", "date");
  fact.append(`${label} `);
  const time = document.createElement("time");
  time.dateTime = value;
  time.title = value;
  time.textContent = relative || value.slice(0, 10);
  const exact = document.createElement("span");
  exact.className = "sr-only";
  exact.textContent = ` (${value})`;
  fact.append(time, exact);
}

function addText(document, parent, className, text) {
  const span = document.createElement("span");
  span.className = className;
  span.textContent = text;
  parent.append(span);
}

export function safeHTTPURL(raw) {
  if (!raw) return "";
  try {
    const url = new URL(raw);
    return url.protocol === "https:" || url.protocol === "http:" ? url.toString() : "";
  } catch {
    return "";
  }
}

export function locationPresentation(raw) {
  const normalized = String(raw).replace(/\s+/gu, " ").trim();
  if (!normalized) return { summary: "", full: "", disclose: false, truncated: false };

  const sourceTruncated = normalized.endsWith("…");
  const split = normalized.split(/\s*(?:\|+|;+|\n+)\s*/u).map((part) => part.trim()).filter(Boolean);
  const unique = [];
  const seen = new Set();
  for (const part of split.slice(0, 500)) {
    const key = part.toLocaleLowerCase();
    if (!seen.has(key)) {
      seen.add(key);
      unique.push(part);
    }
  }
  const countTruncated = split.length > 500;
  const fullTruncated = [...normalized].length > 4096;
  const truncated = sourceTruncated || countTruncated || fullTruncated;
  const full = fullTruncated ? `${[...normalized].slice(0, 4095).join("")}…` : normalized;

  if (unique.length <= 1) {
    const only = unique[0] || normalized;
    const long = [...only].length > 180;
    return {
      summary: long ? `${[...only].slice(0, 177).join("")}…` : only,
      full,
      disclose: long || truncated,
      truncated: truncated || long,
    };
  }

  const exactCount = !sourceTruncated && !countTruncated;
  const count = exactCount ? String(unique.length) : `${Math.min(unique.length, 500)}+`;
  const shortList = unique.length <= 3 && unique.join(" · ").length <= 120 && !truncated;
  return {
    summary: shortList ? unique.join(" · ") : `${count} locations, including ${bounded(unique[0], 72)}`,
    full,
    disclose: !shortList,
    truncated,
  };
}

function bounded(value, limit) {
  const runes = [...value];
  return runes.length <= limit ? value : `${runes.slice(0, limit - 1).join("")}…`;
}
