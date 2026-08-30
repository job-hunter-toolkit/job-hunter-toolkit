import { MAX_QUERY_LENGTH, parseQuery, queryForRequest } from "../query-state.js";

let failures = 0;
function check(label, got, want) {
  if (JSON.stringify(got) !== JSON.stringify(want)) {
    failures++;
    console.error(`FAIL ${label}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`);
  } else console.log(`ok   ${label}`);
}

const complete = {
  titles: ["Security Engineer", "développeur"],
  exclude_titles: ["manager"],
  locations: ["東京"],
  companies: ["A & B"],
  departments: ["R&D"],
  min_annual: 150000,
  employment_types: ["full_time"],
  workplace_types: ["hybrid"],
  posted_since_days: 30,
  remote: true,
  has_compensation: true,
  states: ["open", "stale", "closed", "lapsed"],
};
const encoded = queryForRequest(complete, "?corpus=https%3A%2F%2Fexample.com%2Fsnapshot%2F");
check("every filter round trips with Unicode and order", queryForRequest(parseQuery(encoded).request), queryForRequest(complete));
check("corpus override coexists but is not query state", new URLSearchParams(encoded).get("corpus"), "https://example.com/snapshot/");
check("repeated and comma-separated terms parse", parseQuery("?qv=1&title=a&title=b%2C+c").request.titles, ["a", "b", "c"]);
check("defaults stay absent", parseQuery(queryForRequest({})).request, {});
check("unknown parameters do not become request data", parseQuery("?qv=1&future=x&title=engineer"), {
  request: { titles: ["engineer"] }, page: 1, valid: true, reason: "ok", unknown: ["future"],
});
check("future version fails closed", parseQuery("?qv=999&title=engineer").valid, false);
check("missing version leaves old links inert", parseQuery("?title=engineer").request, {});
check("invalid enums fail closed", parseQuery("?qv=1&workplace=moon").reason, "invalid-workplace");
check("prototype-shaped names remain ordinary unknown parameters", parseQuery("?qv=1&__proto__=x").unknown, ["__proto__"]);
check("oversize query is bounded", parseQuery(`?qv=1&title=${"x".repeat(MAX_QUERY_LENGTH)}`).reason, "too-long");
check("invalid compensation is rejected", parseQuery("?qv=1&min_annual=Infinity").reason, "invalid-min-annual");
check("page position round trips independently", parseQuery(queryForRequest(complete, "", 42)).page, 42);
check("invalid page fails closed", parseQuery("?qv=1&page=0").reason, "invalid-page");
check("default lifecycle states are explicit", parseQuery("?qv=1").request.states, ["open", "stale"]);
check("duplicate lifecycle states fail closed", parseQuery("?qv=1&state=open&state=open").reason, "invalid-state");

if (failures) process.exit(1);
console.log("Query URL state tests passed");
