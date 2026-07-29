package schedule

import (
	"context"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/services"
)

// Gate reports whether a source should still be started.
//
// It is consulted at dispatch, inside the worker, so it sees real elapsed time
// rather than the plan's prediction. That is the whole mechanism by which a run
// is bounded and interruptible: it stops by declining to start work it cannot
// finish, never by being killed mid-source.
//
// Preemption was the obvious alternative and is worse in three ways at once. It
// throws away the cost already spent, it produces a truncated source that cannot
// advance last_seen, and it pressures a backend for nothing.
type Gate func(ctx context.Context, source services.Source) bool

// Gate returns a dispatch gate for this plan.
//
// A source is admitted when now() plus its predicted cost still lands before the
// deadline. A source the plan never selected is declined: the plan is the run's
// promise, and quietly running unplanned work would break the coverage proof a
// merge verifies against.
//
// A declined source should be recorded [StatusDeferred], which [Fold]
// deliberately treats as a no-op. It is not a failure and it is not a
// truncation — nothing was requested, so nothing was learned, and in particular
// its zero duration must never reach the cost estimator or deferred sources
// would look free and be re-admitted forever.
//
// The run's own context timeout stays as a backstop, so a source whose
// prediction was badly wrong is still cut off, still recorded truncated, and —
// per [Fold] — still not blamed for it.
func (p Plan) Gate(now func() time.Time, deadline time.Time) Gate {
	predicted := make(map[SourceID]time.Duration, len(p.Items))
	for _, item := range p.Items {
		predicted[item.Source] = time.Duration(item.PredictedMS) * time.Millisecond
	}

	if now == nil {
		now = time.Now
	}

	return func(ctx context.Context, source services.Source) bool {
		if ctx != nil && ctx.Err() != nil {
			return false
		}

		cost, ok := predicted[SourceID{Platform: source.Platform, Key: source.Key}]
		if !ok {
			return false
		}

		return !now().Add(cost).After(deadline)
	}
}
