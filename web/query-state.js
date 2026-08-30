// query-state.js owns the shareable search URL. It contains query definitions
// only: corpus selection is preserved independently, while saved searches,
// visit history, result rows and other local state never enter the URL.

export const QUERY_VERSION = "1";
export const MAX_QUERY_LENGTH = 8192;

const LISTS = {
  title: "titles",
  exclude: "exclude_titles",
  location: "locations",
  company: "companies",
  department: "departments",
};
const ENUMS = {
  employment: ["full_time", "part_time", "contract", "internship", "temporary", "volunteer"],
  workplace: ["remote", "hybrid", "onsite"],
  since: ["1", "7", "30", "90"],
};
const STATES = ["open", "stale", "closed", "lapsed"];
const KNOWN = new Set(["qv", "corpus", "page", "state", ...Object.keys(LISTS), ...Object.keys(ENUMS), "min_annual", "remote", "has_pay"]);

export function parseQuery(search) {
  if (typeof search !== "string" || search.length > MAX_QUERY_LENGTH) {
    return { request: {}, page: 1, valid: false, reason: "too-long", unknown: [] };
  }

  let params;
  try {
    params = new URLSearchParams(search);
  } catch {
    return { request: {}, page: 1, valid: false, reason: "invalid-encoding", unknown: [] };
  }

  const unknown = [...new Set([...params.keys()].filter((key) => !KNOWN.has(key)))];
  const version = params.get("qv");
  if (version === null) return { request: {}, page: 1, valid: true, reason: "absent", unknown };
  if (version !== QUERY_VERSION) return { request: {}, page: 1, valid: false, reason: "unsupported-version", unknown };
  if ([...params].length > 80) return { request: {}, page: 1, valid: false, reason: "too-many-parameters", unknown };

  const request = {};
  for (const [parameter, field] of Object.entries(LISTS)) {
    const values = params.getAll(parameter).flatMap(splitTerms);
    if (values.length > 20 || values.some((value) => value.length > 160)) {
      return { request: {}, page: 1, valid: false, reason: "invalid-terms", unknown };
    }
    if (values.length) request[field] = values;
  }

  for (const [parameter, allowed] of Object.entries(ENUMS)) {
    const value = params.get(parameter) || "";
    if (value && !allowed.includes(value)) {
      return { request: {}, page: 1, valid: false, reason: `invalid-${parameter}`, unknown };
    }
    if (parameter === "employment" && value) request.employment_types = [value];
    if (parameter === "workplace" && value) request.workplace_types = [value];
    if (parameter === "since" && value) request.posted_since_days = Number(value);
  }

  const pay = params.get("min_annual") || "";
  if (pay && (!/^\d+$/.test(pay) || Number(pay) <= 0 || Number(pay) > 10_000_000)) {
    return { request: {}, page: 1, valid: false, reason: "invalid-min-annual", unknown };
  }
  if (pay) request.min_annual = Number(pay);
  if (flag(params, "remote")) request.remote = true;
  if (flag(params, "has_pay")) request.has_compensation = true;
  const states = params.getAll("state");
  if (states.length && (new Set(states).size !== states.length || states.some((state) => !STATES.includes(state)))) {
    return { request: {}, page: 1, valid: false, reason: "invalid-state", unknown };
  }
  request.states = states.length ? STATES.filter((state) => states.includes(state)) : ["open", "stale"];

  const pageValue = params.get("page") || "1";
  if (!/^\d+$/.test(pageValue) || Number(pageValue) < 1 || Number(pageValue) > 100_000) {
    return { request: {}, page: 1, valid: false, reason: "invalid-page", unknown };
  }
  return { request, page: Number(pageValue), valid: true, reason: "ok", unknown };
}

export function queryForRequest(request, currentSearch = "", page = 1) {
  const params = new URLSearchParams();
  params.set("qv", QUERY_VERSION);
  appendList(params, "title", request.titles);
  appendList(params, "exclude", request.exclude_titles);
  appendList(params, "location", request.locations);
  appendList(params, "company", request.companies);
  appendList(params, "department", request.departments);
  if (request.min_annual > 0) params.set("min_annual", String(request.min_annual));
  if (request.employment_types?.[0]) params.set("employment", request.employment_types[0]);
  if (request.workplace_types?.[0]) params.set("workplace", request.workplace_types[0]);
  if (request.posted_since_days > 0) params.set("since", String(request.posted_since_days));
  if (request.remote) params.set("remote", "1");
  if (request.has_compensation) params.set("has_pay", "1");
  const states = request.states ?? (request.include_closed ? STATES : ["open", "stale"]);
  if (states.join(",") !== "open,stale") {
    for (const state of STATES) if (states.includes(state)) params.append("state", state);
  }
  if (page > 1) params.set("page", String(page));

  const corpus = new URLSearchParams(currentSearch).get("corpus");
  if (corpus) params.set("corpus", corpus);
  const encoded = params.toString();
  if (encoded.length > MAX_QUERY_LENGTH) throw new RangeError("Search URL exceeds 8 KiB");
  return `?${encoded}`;
}

function appendList(params, name, values = []) {
  for (const value of values) {
    const trimmed = String(value).trim();
    if (trimmed) params.append(name, trimmed);
  }
}

function splitTerms(value) {
  return value.split(",").map((term) => term.trim()).filter(Boolean);
}

function flag(params, name) {
  const value = params.get(name);
  return value === "1" || value === "true" || value === "on";
}
