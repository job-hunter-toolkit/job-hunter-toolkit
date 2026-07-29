package schedule

import (
	"context"
	"testing"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/services"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestGateDeclinesWorkItCannotFinish(t *testing.T) {
	t.Parallel()

	// A run stops by declining to start work it cannot finish, never by being
	// killed mid-source. Preemption throws away the cost already spent, produces
	// a truncated source that cannot advance last_seen, and pressures a backend
	// for nothing.
	store := NewStore()
	store.PutSource(SourceState{Platform: "greenhouse", Key: "quick", DurationMS: []int32{1000}})
	store.PutSource(SourceState{Platform: "greenhouse", Key: "slow", DurationMS: []int32{60_000}})

	registry := []services.Source{
		{Platform: "greenhouse", Key: "quick"},
		{Platform: "greenhouse", Key: "slow"},
	}

	plan, err := Build(registry, store, Options{Now: planningNow, Budget: Budget{Wall: 10 * time.Minute}, Workers: 8})
	must.NoError(t, err)
	must.SliceLen(t, 2, plan.Items)

	// A fake clock is a closure over a variable, because the gate takes now as a
	// function. There is no clock interface anywhere in this package.
	clock := planningNow
	gate := plan.Gate(func() time.Time { return clock }, planningNow.Add(10*time.Second))

	ctx := context.Background()

	test.True(t, gate(ctx, registry[0]))
	test.False(t, gate(ctx, registry[1]))

	// Nine and a half seconds later even the quick one no longer fits, so the run
	// ends by starting nothing rather than by cutting something off.
	clock = clock.Add(9500 * time.Millisecond)
	test.False(t, gate(ctx, registry[0]))
}

func TestGateDeclinesUnplannedWork(t *testing.T) {
	t.Parallel()

	// The plan is the run's promise. Quietly running unplanned work would break
	// the coverage proof a merge verifies against.
	registry := []services.Source{{Platform: "greenhouse", Key: "planned"}}

	plan, err := Build(registry, NewStore(), Options{Now: planningNow, Budget: Budget{Wall: time.Minute}, Workers: 8})
	must.NoError(t, err)

	gate := plan.Gate(func() time.Time { return planningNow }, planningNow.Add(time.Hour))

	test.True(t, gate(context.Background(), registry[0]))
	test.False(t, gate(context.Background(), services.Source{Platform: "greenhouse", Key: "unplanned"}))
}

func TestGateDeclinesOnACancelledContext(t *testing.T) {
	t.Parallel()

	registry := []services.Source{{Platform: "greenhouse", Key: "planned"}}

	plan, err := Build(registry, NewStore(), Options{Now: planningNow, Budget: Budget{Wall: time.Minute}, Workers: 8})
	must.NoError(t, err)

	gate := plan.Gate(func() time.Time { return planningNow }, planningNow.Add(time.Hour))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	test.False(t, gate(ctx, registry[0]))
}

func TestGateAdmitsASourceThatExactlyFits(t *testing.T) {
	t.Parallel()

	// The boundary matters: an off-by-one here declines the last source of every
	// run forever, which looks like a scheduler that simply cannot fill its
	// budget.
	store := NewStore()
	store.PutSource(SourceState{Platform: "greenhouse", Key: "exact", DurationMS: []int32{5000}})

	registry := []services.Source{{Platform: "greenhouse", Key: "exact"}}

	plan, err := Build(registry, store, Options{Now: planningNow, Budget: Budget{Wall: time.Minute}, Workers: 8})
	must.NoError(t, err)

	gate := plan.Gate(func() time.Time { return planningNow }, planningNow.Add(5*time.Second))
	test.True(t, gate(context.Background(), registry[0]))

	tight := plan.Gate(func() time.Time { return planningNow }, planningNow.Add(4999*time.Millisecond))
	test.False(t, tight(context.Background(), registry[0]))
}
