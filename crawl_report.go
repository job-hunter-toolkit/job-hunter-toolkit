package main

import (
	"runtime/debug"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/services"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/shard"
)

// crawlManifestSchemaVersion is the version of the manifest this binary writes.
//
// Version 2 adds one optional object, "shard", and changes nothing else: every
// version 1 field keeps its name, type and meaning. That compatibility is not
// cosmetic. The nightly workflow's Python summarizer reads this file key by key
// with no schema negotiation, and a breaking change there reds the job and
// destroys the day's data row.
//
// The version was still bumped, because the meaning of the file changed even
// though its shape did not. In a version 1 manifest "postings" is a whole
// crawl. In a version 2 manifest that carries a "shard" object it is one
// shard's contribution, before global deduplication, and adding those up across
// shards produces a wrong total — the same posting URL can arrive through two
// integrations. A reader has to be able to tell those two documents apart, and
// the schema version is how.
const crawlManifestSchemaVersion = shard.ManifestSchemaVersion

// crawlManifest is the versioned record of one crawl.
//
// It is an alias rather than a copy so that the merge, which reads manifests
// written by other processes, cannot drift from the writer. Two structs
// decoding the same file is how a field silently stops being populated.
type crawlManifest = shard.Manifest

func newCrawlManifest(
	startedAt time.Time,
	finishedAt time.Time,
	timeout time.Duration,
	status string,
	postings int,
	companies int,
	sources []services.SourceRun,
) crawlManifest {
	return shard.NewManifest(startedAt, finishedAt, timeout, status, postings, companies, sources)
}

func writeCrawlManifest(path string, manifest crawlManifest) error {
	return shard.WriteManifest(path, manifest)
}

// buildCommit reports the VCS revision this binary was built from, or "" when
// the build did not stamp one (`go build -buildvcs=false`, or `go run` of a
// file outside a repository).
//
// It is recorded in plans and shard manifests so a merge can refuse results
// that were produced by different builds. It is deliberately not the only such
// check: shard.SourceSetID compares the actual source registries and still
// works when this is empty.
func buildCommit() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}

	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" {
			return setting.Value
		}
	}

	return ""
}
