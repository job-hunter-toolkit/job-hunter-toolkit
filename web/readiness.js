export const LOAD_PHASES = ["network", "decode", "fold", "state", "sort", "query", "paint", "ready"];

const phaseRank = new Map(LOAD_PHASES.map((phase, index) => [phase, index]));

// advanceProgress refuses stale worker messages. A replaced worker can still
// have one message queued when it is terminated, and readiness must never move
// backwards because that old message arrived late.
export function advanceProgress(current, update) {
  if (!update || !phaseRank.has(update.phase)) return current;
  const completed = Number(update.completed ?? current?.completed ?? 0);
  const total = Number(update.total ?? current?.total ?? 1);
  if (!Number.isFinite(completed) || !Number.isFinite(total) || total <= 0) return current;
  if (current && (completed < current.completed || phaseRank.get(update.phase) < phaseRank.get(current.phase))) {
    return current;
  }
  return { ...current, ...update, completed, total };
}

export function sameVerifiedSnapshot(left, right) {
  return Boolean(
    left && right &&
    left.generation === right.generation &&
    left.content_digest && left.content_digest === right.content_digest &&
    left.rows === right.rows,
  );
}

export function failureState(error, fallbackPhase) {
  const phase = LOAD_PHASES.includes(error?.phase) || error?.phase === "metadata" || error?.phase === "worker"
    ? error.phase
    : fallbackPhase;
  return {
    phase,
    retryable: error?.retryable !== false,
    action: error?.retryable === false ? "Reload the page" : "Retry in this tab",
  };
}
