package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/services"
)

const crawlManifestSchemaVersion = 1

type crawlManifest struct {
	SchemaVersion int                  `json:"schema_version"`
	StartedAt     time.Time            `json:"started_at"`
	FinishedAt    time.Time            `json:"finished_at"`
	DurationMS    int64                `json:"duration_ms"`
	Timeout       string               `json:"timeout"`
	Status        string               `json:"status"`
	Postings      int                  `json:"postings"`
	Companies     int                  `json:"companies"`
	SourceCounts  map[string]int       `json:"source_counts"`
	Sources       []services.SourceRun `json:"sources"`
}

func newCrawlManifest(
	startedAt time.Time,
	finishedAt time.Time,
	timeout time.Duration,
	status string,
	postings int,
	companies int,
	sources []services.SourceRun,
) crawlManifest {
	counts := map[string]int{}
	for _, source := range sources {
		counts[source.Status]++
	}

	return crawlManifest{
		SchemaVersion: crawlManifestSchemaVersion,
		StartedAt:     startedAt,
		FinishedAt:    finishedAt,
		DurationMS:    finishedAt.Sub(startedAt).Milliseconds(),
		Timeout:       timeout.String(),
		Status:        status,
		Postings:      postings,
		Companies:     companies,
		SourceCounts:  counts,
		Sources:       sources,
	}
}

func writeCrawlManifest(path string, manifest crawlManifest) error {
	dir := filepath.Dir(path)
	temp, err := os.CreateTemp(dir, ".crawl-manifest-*.json")
	if err != nil {
		return fmt.Errorf("create crawl manifest beside %q: %w", path, err)
	}

	tempPath := temp.Name()
	defer func() {
		// A successful rename makes this a harmless no-op. On failure, do not
		// leave a misleading partial manifest behind.
		_ = os.Remove(tempPath)
	}()

	enc := json.NewEncoder(temp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(manifest); err != nil {
		_ = temp.Close()

		return fmt.Errorf("encode crawl manifest %q: %w", path, err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close crawl manifest %q: %w", path, err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("publish crawl manifest %q: %w", path, err)
	}

	return nil
}
