package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/schedule"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/services"
)

// TestScheduleFlagsRequireSchedule pins the guard against a silently ignored
// budget. A workflow that passes --schedule-budget without --schedule would look
// bounded and crawl everything, and nothing in its output would say so.
func TestScheduleFlagsRequireSchedule(t *testing.T) {
	t.Parallel()

	for _, flag := range []string{
		"--schedule-budget=1m",
		"--schedule-state=/dev/null",
		"--schedule-plan=/dev/null",
		"--schedule-dry-run",
	} {
		t.Run(flag, func(t *testing.T) {
			t.Parallel()

			cmd := newRootCommand()

			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			cmd.SetArgs([]string{"total", flag})

			err := cmd.ExecuteContext(t.Context())
			if err == nil {
				t.Fatalf("ExecuteContext() error = nil, want %s without --schedule to be refused", flag)
			}

			if !strings.Contains(err.Error(), "has no effect without --schedule") {
				t.Errorf("error = %v, want it to name the missing --schedule", err)
			}

			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want nothing: no crawl ran", stdout.String())
			}
		})
	}
}

// TestScheduledDryRunPrintsNoRow guards the record file. A dry run measures
// nothing, so a DATE row from one would be a fabricated data point in the
// project's only historical series.
func TestScheduledDryRunPrintsNoRow(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.jsonl")
	planPath := filepath.Join(dir, "plan.json")

	cmd := newRootCommand()

	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{
		"total",
		"--schedule",
		"--schedule-dry-run",
		"--schedule-budget=10m",
		"--schedule-state=" + statePath,
		"--schedule-plan=" + planPath,
	})

	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("ExecuteContext() error = %v, want a dry run to succeed", err)
	}

	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want no row: a dry run crawled nothing", stdout.String())
	}

	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Errorf("Stat(%q) err = %v, want the dry run to leave the state file alone", statePath, err)
	}

	data, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", planPath, err)
	}

	var plan schedule.Plan
	if err := json.Unmarshal(data, &plan); err != nil {
		t.Fatalf("Unmarshal(plan) error = %v", err)
	}

	if plan.SchemaVersion != schedule.PlanSchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", plan.SchemaVersion, schedule.PlanSchemaVersion)
	}

	if len(plan.Items) == 0 {
		t.Error("plan items = 0, want a ten-minute budget to plan some work")
	}
}

// TestScheduledTotalNeverRecordsCompleteWhenItSkipped is the invariant this
// whole change is most at risk of breaking. jobs_record.txt is the project's
// headline time series; one row that says complete while thousands of sources
// were never attempted corrupts it permanently.
//
// The crawl context is dead on arrival, so nothing is fetched and every source
// stays unattempted. The plan is still large, which is the point: a big plan and
// no work done must not read as a finished crawl.
func TestScheduledTotalNeverRecordsCompleteWhenItSkipped(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	statePath := filepath.Join(dir, "cache", "state.jsonl")
	manifestPath := filepath.Join(dir, "manifest.json")

	cmd := newRootCommand()

	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{
		"total",
		"--schedule",
		"--schedule-budget=1h",
		"--schedule-state=" + statePath,
		"--manifest=" + manifestPath,
		// Small enough that no source can be fetched, so the run is decided
		// entirely by what it did not do.
		"--timeout=1ns",
		"--allow-partial",
	})

	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatalf("ExecuteContext() error = %v, want an explicitly partial run to succeed", err)
	}

	fields := strings.Fields(stdout.String())
	if len(fields) != 4 {
		t.Fatalf("stdout fields = %q, want DATE POSTINGS COMPANIES STATUS", fields)
	}

	if fields[3] != "partial" {
		t.Errorf("status = %q, want partial: the run attempted nothing", fields[3])
	}

	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", manifestPath, err)
	}

	var manifest crawlManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("Unmarshal(manifest) error = %v", err)
	}

	if manifest.Status != "partial" {
		t.Errorf("manifest status = %q, want partial", manifest.Status)
	}

	if manifest.Complete() {
		t.Error("manifest.Complete() = true, want false for a run that attempted nothing")
	}

	// Every registered source is listed even though the plan selected a subset.
	// A manifest that listed only planned sources would let Complete() call a run
	// that skipped most of the registry finished.
	if got, want := len(manifest.Sources), len(services.SourcesMatching(nil)); got != want {
		t.Errorf("manifest sources = %d, want the whole registry, %d", got, want)
	}

	assertPrivateStateFile(t, statePath)
}

// assertPrivateStateFile checks the directory and file modes the default path
// argument depends on. docs/posting-cache.md rejects a
// predictable path in a world-writable directory as a phishing primitive, and
// the state file decides which boards the next run talks to.
func assertPrivateStateFile(t *testing.T, path string) {
	t.Helper()

	if runtime.GOOS == "windows" {
		return
	}

	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("Stat(%q) error = %v", filepath.Dir(path), err)
	}

	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Errorf("state directory mode = %04o, want 0700", got)
	}

	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%q) error = %v", path, err)
	}

	if got := fileInfo.Mode().Perm(); got != 0o600 {
		t.Errorf("state file mode = %04o, want 0600", got)
	}
}

// TestDefaultSchedulerStatePathIsUnderTheUserCache pins the default away from
// /tmp.
func TestDefaultSchedulerStatePathIsUnderTheUserCache(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))

	cacheDir, err := os.UserCacheDir()
	if err != nil {
		t.Skipf("os.UserCacheDir() error = %v", err)
	}

	got, err := defaultSchedulerStatePath()
	if err != nil {
		t.Fatalf("defaultSchedulerStatePath() error = %v", err)
	}

	want := filepath.Join(cacheDir, "job-hunter-toolkit", "scheduler-state.jsonl")
	if got != want {
		t.Errorf("defaultSchedulerStatePath() = %q, want %q", got, want)
	}
}

// TestOrderForPlanKeepsExecutionOrder pins the two properties the dispatch order
// has to have: the plan's own order is preserved, and no registered source is
// dropped, so the manifest still describes the whole registry.
func TestOrderForPlanKeepsExecutionOrder(t *testing.T) {
	t.Parallel()

	registry := []services.Source{
		{Platform: "a", Key: "1"},
		{Platform: "a", Key: "2"},
		{Platform: "b", Key: "1"},
		{Platform: "b", Key: "2"},
	}

	planned := []services.Source{
		{Platform: "b", Key: "2"},
		{Platform: "a", Key: "1"},
	}

	got := orderForPlan(planned, registry)

	want := []string{"b/2", "a/1", "a/2", "b/1"}
	if len(got) != len(want) {
		t.Fatalf("orderForPlan() length = %d, want %d", len(got), len(want))
	}

	for i, source := range got {
		if key := source.Platform + "/" + source.Key; key != want[i] {
			t.Errorf("orderForPlan()[%d] = %q, want %q", i, key, want[i])
		}
	}
}

// TestLabelDeclinedMarksDeferred checks the one relabelling this command does.
// services.Observe leaves an unstarted source "planned", which does not
// distinguish work the process never reached from work this run decided not to
// do; schedule.Fold ignores both, but only "deferred" says which happened.
func TestLabelDeclinedMarksDeferred(t *testing.T) {
	t.Parallel()

	run := &scheduledRun{declined: map[schedule.SourceID]struct{}{
		{Platform: "a", Key: "1"}: {},
	}}

	runs := []services.SourceRun{
		{Platform: "a", Key: "1", Status: "planned"},
		{Platform: "a", Key: "2", Status: "complete"},
	}

	if got := run.labelDeclined(runs); got != 1 {
		t.Errorf("labelDeclined() = %d, want 1", got)
	}

	if runs[0].Status != schedule.StatusDeferred {
		t.Errorf("runs[0].Status = %q, want %q", runs[0].Status, schedule.StatusDeferred)
	}

	if runs[1].Status != "complete" {
		t.Errorf("runs[1].Status = %q, want it untouched", runs[1].Status)
	}
}
