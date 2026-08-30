// Select one complete published snapshot. A SHA is preferred for an immutable
// multi-file view; the branch candidate recovers if GitHub's ref API and raw
// object CDN briefly disagree after an orphan-branch replacement.
export async function openSnapshot(candidates, open, onFallback = () => {}) {
  let lastError;
  for (const [index, base] of candidates.entries()) {
    try {
      return { base, summary: await open(base), recovered: index > 0 };
    } catch (err) {
      lastError = err;
      if (err?.name === "TimeoutError" || index === candidates.length - 1) throw err;
      onFallback(err);
    }
  }
  throw lastError ?? new Error("No published snapshot was found");
}
