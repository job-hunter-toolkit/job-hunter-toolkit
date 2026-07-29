// Package corpus is the persistent record of postings over time: what each
// integration published, when it was first seen, and when the evidence says it
// closed.
//
// A crawl answers "what is on the boards right now". It cannot answer "when did
// this role appear", "what closed this week" or "how stale is this source",
// because today a run emits NDJSON and a manifest and forgets both. This package
// is the thing in between. Nothing in the crawler requires it: `postings`,
// `total` and `health` keep working with no corpus at all, which is what
// docs/architecture-roadmap.md means by storage being something a surface
// attaches rather than something the crawler depends on.
//
// # The two halves
//
// The **format** is a purpose-built columnar file, `.jhtc`, written with nothing
// but the standard library. docs/design/storage-engine.md measured the
// alternatives: modernc.org/sqlite, bbolt and pebble all fail to build for
// GOOS=js and GOOS=wasip1, and parquet-go clears the portability gate but costs
// ten modules and produced a larger file (25.0 vs 19.0 MiB) and slower cold
// aggregates than a ~300-line stdlib prototype. The corpus has to be readable in
// a browser tab, so the format needs no mmap, no file locking, no transactions
// and no syscall: it is bytes, [compress/flate] and [encoding/binary].
//
// The **algorithm** is identity and closure. docs/design/corpus-format.md §1
// derives a posting's identity from the integration it came through plus the
// most specific stable key that integration published ([Identify]), and §2
// derives closure from a *qualifying observation of a posting's own source*
// ([Qualifies]) rather than from absence. That second rule is the dangerous one
// and it has the narrowest possible statement:
//
//	A posting may be marked missing only by a qualifying observation of its own
//	source. Absence from the corpus, from a run, or from any other source is not
//	evidence of anything.
//
// docs/architecture-roadmap.md states as an invariant that "a failed source
// cannot make all of its previously seen jobs look removed", and under a budget
// model most sources are not visited in any given run, so absence is the normal
// case rather than the exceptional one. On the real 07/28 crawl two truncated
// sources held 177,296 postings — 21.1% of the run — and a rule that closed
// anything absent from the latest run would have retired most of two of the
// largest employers in the registry the first time the crawl hit its deadline
// mid-source.
//
// # Rewrite, never update
//
// [Apply] reads a generation, folds a run into it and writes a new one. Nothing
// is updated in place. That single decision removes the need for transactions, a
// write-ahead log, file locking, mmap and a concurrent-writer story — which is
// exactly the list of things that broke js/wasm for every embedded engine
// storage-engine.md §1 tested. The corpus is derived state: it is rebuildable
// from the immutable NDJSON shard artifacts, so losing it costs a merge, not
// data.
//
// # Determinism
//
// Same input, byte-identical output. One clock reading per run
// ([RunInput.RunAt]); no [time.Now] is reachable below the run boundary. Rows in
// one total order; dictionaries sorted; no map iteration in a serialized shape;
// flate at a fixed level with no header metadata. The honest caveat is that
// [compress/flate]'s output is not contractually stable across Go toolchains, so
// the corpus's identity is [Manifest.ContentDigest] — a SHA-256 over the
// *uncompressed* column payloads — and not the file's bytes. Compression is a
// transport detail. See docs/design/corpus-format.md §5.
//
// # Dependencies and portability
//
// Standard library only, and deliberately not net/http and not internal/services
// or internal/shard: a corpus reader has no business linking an HTTP stack or
// the ATS adapters. [SourceRun] mirrors services.SourceRun field for field and
// JSON tag for JSON tag, so a caller folds a crawl manifest in with
// [DecodeManifestSources] and no import edge is created in either direction.
package corpus
