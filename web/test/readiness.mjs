import { LOAD_PHASES, advanceProgress, failureState, sameVerifiedSnapshot } from "../readiness.js";

function check(label, got, want) {
  if (JSON.stringify(got) !== JSON.stringify(want)) {
    throw new Error(`${label}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`);
  }
}

let progress = null;
for (const [completed, phase] of LOAD_PHASES.entries()) {
  progress = advanceProgress(progress, { phase, completed, total: LOAD_PHASES.length, label: phase });
  check(`${phase} accepted`, progress.phase, phase);
}
const ready = progress;
check("late stale progress is ignored", advanceProgress(progress, { phase: "decode", completed: 2, total: 8 }), ready);
check("malformed progress is ignored", advanceProgress(progress, { phase: "unknown", completed: 9, total: 8 }), ready);

const verified = { generation: 11, content_digest: "sha256:generation-11", rows: 2_005_791 };
check("verified metadata survives retry", sameVerifiedSnapshot(verified, { ...verified }), true);
check("retry refuses another digest", sameVerifiedSnapshot(verified, { ...verified, content_digest: "sha256:other" }), false);
check("retry refuses another row count", sameVerifiedSnapshot(verified, { ...verified, rows: 1 }), false);

for (const phase of ["metadata", ...LOAD_PHASES.slice(0, -1), "worker"]) {
  check(`${phase} failure identifies phase`, failureState({ phase, retryable: true }, "metadata"), {
    phase, retryable: true, action: "Retry in this tab",
  });
}
check("non-retryable recovery action", failureState({ phase: "metadata", retryable: false }, "metadata").action, "Reload the page");

console.log("readiness state tests passed");
