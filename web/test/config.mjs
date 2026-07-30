// config.mjs — unit test of the corpus-location resolution in config.js,
// under Node with a stubbed fetch. Run with: node web/test/config.mjs

import { resolveCorpusBase, CORPUS_SOURCE } from "../config.js";
import { exit } from "node:process";

let failures = 0;

function check(what, got, want) {
  if (got !== want) {
    failures += 1;
    console.error(`FAIL ${what}: got ${got}, want ${want}`);
  } else {
    console.log(`ok   ${what}`);
  }
}

const sha = "0123456789abcdef0123456789abcdef01234567";
const { owner, repo, branch } = CORPUS_SOURCE;

// The API answers: reads pin to the commit SHA for an atomic view.
{
  const base = await resolveCorpusBase(async () => ({
    ok: true,
    json: async () => ({ sha }),
  }));
  check("sha-pinned base", base, `https://raw.githubusercontent.com/${owner}/${repo}/${sha}/`);
}

// The API is down or rate-limited: fall back to the branch name.
{
  const rateLimited = await resolveCorpusBase(async () => ({ ok: false }));
  check("rate-limited falls back to branch", rateLimited, `https://raw.githubusercontent.com/${owner}/${repo}/${branch}/`);

  const offline = await resolveCorpusBase(async () => {
    throw new Error("offline");
  });
  check("fetch failure falls back to branch", offline, `https://raw.githubusercontent.com/${owner}/${repo}/${branch}/`);

  const garbage = await resolveCorpusBase(async () => ({
    ok: true,
    json: async () => ({ sha: "not-a-sha" }),
  }));
  check("garbage sha falls back to branch", garbage, `https://raw.githubusercontent.com/${owner}/${repo}/${branch}/`);
}

if (failures > 0) {
  console.error(`${failures} failure(s)`);
  exit(1);
}

console.log("config test passed");
