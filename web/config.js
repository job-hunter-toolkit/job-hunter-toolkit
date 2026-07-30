// config.js — the single place the corpus location is configured.
//
// WHERE THE CORPUS LIVES is the corpus pipeline's decision, recorded in
// .github/workflows/corpus.yml's header: an orphan branch `corpus` holding
// only the latest generation, replaced whole on every publish, served by
// raw.githubusercontent.com. The evidence (curl-measured 2026-07-29, twice
// and independently — see that header and web/README.md): ranged GETs answer
// 206 with `accept-ranges: bytes`, `content-range` and
// `access-control-allow-origin: *`; api.github.com also sends
// `access-control-allow-origin: *`. Preflighted requests fail (OPTIONS 403),
// which is fine only because single byte ranges (`bytes=N-M`) are
// CORS-safelisted request headers per the Fetch spec and trigger no
// preflight — web/corpus-store.js must therefore only ever send single
// ranges. None of this has run in a real browser yet; that caveat is
// load-bearing and lives in web/README.md.
//
// Everything else in web/ reaches the corpus through resolveCorpusBase() and
// nothing else hardcodes a URL.

export const CORPUS_SOURCE = {
  owner: "job-hunter-toolkit",
  repo: "job-hunter-toolkit",
  branch: "corpus",
};

// resolveCorpusBase returns the base URL corpus objects (manifest.json,
// corpus.jhtc, sources.json, runs.ndjson) are fetched from.
//
// The publish replaces the corpus branch while readers may be mid-read, so
// the branch name is resolved to its commit SHA first and every object is
// fetched pinned to that SHA: an atomic view, per the corpus workflow's
// design. If the API call fails (rate-limited, offline), the branch-name URL
// is the fallback — a torn read across a publish is then possible, but
// corpus.Open cross-checks the manifest's row count against the table and
// fails loudly rather than answering plausibly.
//
// `?corpus=<url>` overrides everything, which is how a local corpus (`python3
// -m http.server`) or a candidate host gets tested without editing this file.
export async function resolveCorpusBase(fetchImpl = fetch) {
  const override = new URLSearchParams(globalThis.location?.search ?? "").get(
    "corpus",
  );

  if (override) {
    return override.endsWith("/") ? override : `${override}/`;
  }

  const { owner, repo, branch } = CORPUS_SOURCE;
  const byBranch = `https://raw.githubusercontent.com/${owner}/${repo}/${branch}/`;

  try {
    const response = await fetchImpl(
      `https://api.github.com/repos/${owner}/${repo}/commits/${encodeURIComponent(branch)}`,
      { headers: { Accept: "application/vnd.github+json" } },
    );

    if (!response.ok) {
      return byBranch;
    }

    const commit = await response.json();

    return typeof commit.sha === "string" && /^[0-9a-f]{40}$/.test(commit.sha)
      ? `https://raw.githubusercontent.com/${owner}/${repo}/${commit.sha}/`
      : byBranch;
  } catch {
    return byBranch;
  }
}
