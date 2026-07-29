# Public API extraction: pass 1

`docs/design/package-taxonomy.md` is the design. This file records what has
actually been extracted, what has not, and what the shims look like while the
migration is partway through. It is a status record, not a second design.

Measured on 2026-07-29 in this container, Go 1.26.5, `CGO_ENABLED=0`.

## What is public now

Two root-level packages, in the existing single module, exactly as the taxonomy
§3 lays out:

| Package | Contents | Depends on |
| --- | --- | --- |
| `jobposting` | `JobPosting`, `PostingSource`, `EmploymentType`, `WorkplaceType`, `Period`, `Provenance`, `Compensation`, the normalizers, `Seq`, `Dedupe` | standard library only |
| `query` | `Query` (`Match`, `Apply`, `IsZero`) | `jobposting`, standard library |

That is the taxonomy's steps 2 and 3, and nothing else. `source`, `snapshot`,
`postingio`, `storage`, `sources/<platform>` and the `internal/cli` move are all
still ahead; each of them forces a decision this pass deliberately did not take.

`jobposting`'s dependency set is stdlib-only and contains no `net/http`,
asserted by `TestDependenciesAreStandardLibraryOnly`, which shells out to
`go list -deps`. The full set is:

```
cmp errors io iter math/bits slices strings sync sync/atomic
syscall time unicode unicode/utf8 unsafe runtime  (+ internal/* runtime support)
```

`query` adds nothing beyond `jobposting`, `slices`, `strings` and `time`. Neither
package names an internal type in an exported signature; `go doc -all` over both
mentions no `internal.`, `httpx.`, `ats.`, `shard.` or `enrich.` qualifier.

## What a consumer actually gets

The taxonomy §1.2 argued from two throwaway modules that a consumer importing a
leaf package pulls no cobra. That is now measurable against the real tree. A
separate module whose entire `main` is

```go
p := &jobposting.JobPosting{Title: "Security Engineer", Location: "Remote"}
fmt.Println(query.Query{Remote: true}.Match(p))
```

with a `replace` onto this repository:

| | |
| --- | ---: |
| `go.sum` after `go mod tidy` | absent — never created |
| deps matching `net/http`, `cobra` or `golang.org/x/net` | none |
| linux/amd64 binary | 2,319,608 bytes |
| js/wasm binary | 2,579,935 bytes |

For scale, the CLI as it ships is 12,410,475 bytes native and 15,767,959 bytes
`js/wasm`, and the taxonomy measured "a program that can do one `http.GET`" at
8,503,843 / 10,344,455. A consumer of the vocabulary links neither.

### Cost to the CLI: none worth reporting

Same tree with this change reverted, versus with it, `CGO_ENABLED=0`:

| target | before | after | delta |
| --- | ---: | ---: | ---: |
| linux/amd64 | 12,413,626 | 12,410,475 | −3,151 |
| js/wasm | 15,765,918 | 15,767,959 | +2,041 |

0.03% and 0.01%. The same definitions are linked either way; the deltas are
symbol-name length, not code.

## What is internal, and why these two went first

`internal/` still holds everything else: the crawler (`internal.All`), the
adapters, `httpx`, `shard`, `enrich`, the pay parsers. The rule from
`docs/surfaces-and-extensibility.md` §2 — a public type may not transitively
expose an internal one — is what set the boundary of this pass.

`internal.JobsFunc` is the clearest case. It is `func(context.Context,
*http.Client) Jobs`, so promoting it puts `net/http` into the vocabulary every
consumer links: 3.31 MB native and 2.98 MB on `js/wasm`, measured in the
taxonomy §1.1. `source` is where that cost is paid, and `source` is not in this
pass. So `Jobs` (the sequence) is public as `jobposting.Seq`, and `JobsFunc`
(the fetch contract) is not.

`internal/paydetect` did not happen either. The pay parsers still live in
`internal/compensation_text.go` and `internal/compensation_markup.go`, which is
fine: the taxonomy's reason for splitting them was that
`compensation_markup.go` imports `golang.org/x/net/html` and `jobposting` must
not. Only the *types* moved — `Compensation`, `Period`, `Provenance`,
`MoreTrustedThan` — and the parsers kept using them through the alias. The
`internal` package still imports `golang.org/x/net/html`; `jobposting` does not.

## The shims

`internal` keeps every name it had. Each is an alias or a one-line forward, so
there is exactly one definition of each type and no conversion at the boundary:

```go
type JobPosting = jobposting.JobPosting
type Compensation = jobposting.Compensation
type Filter = query.Query
type Jobs = jobposting.Seq

const EmploymentTypeFullTime = jobposting.EmploymentTypeFullTime
// … the rest of the vocabulary constants …

func Dedupe(jobs Jobs) Jobs { return jobposting.Dedupe(jobs) }
```

Type aliases carry method sets, so `internal.Filter{…}.Match(p)` and
`posting.IsRemote()` compile untouched, and `reflect.TypeFor[internal.Filter]()`
still walks the same fields — which is what `TestFilterFieldsAreWiredIn` in
`internal/filter_test.go` relies on. Constants have to be re-declared rather than
aliased; they keep their type through the alias, so `[]internal.EmploymentType`
and `[]jobposting.EmploymentType` are the same type.

Nothing under `internal/services`, `internal/companies`, `internal/shard`,
`internal/enrich` or the root `main` package was edited. Every existing test
passes unmodified.

Two behaviour-preserving mechanical details worth naming, because they are the
only places the move was not a straight copy:

- `Filter` is called `Query` in its new home. `query.Query` stutters and is still
  the right name for the reasons the taxonomy §5 gives; `internal.Filter` is an
  alias, so no caller changed.
- `matchesAnyWorkplaceType` was a method on `*JobPosting` in `filter.go`. A
  method must live in its type's package, and `query` is the only caller, so it
  became a package-level unexported function in `query` taking the posting as its
  first argument. Same body, same behaviour.

## Compatibility

Both package docs open with the pre-v1 statement: the Go API may change in any
release, with no deprecation period and no major-version bump. What is *not*
free to change is the JSON encoding of `JobPosting` — field names and the
`omitempty`/`omitzero` choices — because NDJSON output is already a documented
shell-pipeline format. That freeze is stated in `jobposting`'s package doc and
in the taxonomy §7.

## Not done in this pass

- `source`, `snapshot`, `postingio`, `storage` and `sources/<platform>`. None
  started.
- The `internal/cli` move (taxonomy step 1). The root still holds `main.go`,
  `shard_cmd.go`, `merge_cmd.go`, `enrich_cmd.go`, `crawl_report.go`.
- Deleting the shims (taxonomy step 9). Every caller still goes through
  `internal`; no caller imports `jobposting` or `query` directly yet, which means
  the ergonomics are exercised only by the testable examples in
  `jobposting/example_test.go` and `query/example_test.go`.
- The layering test from the taxonomy §4 that decodes `go list -json -deps ./...`
  and fails on an upward edge. Only the stdlib-only guard on `jobposting` exists.
- `apidiff` in CI. Nothing was added to `ci.yml`, and no CI job builds or vets
  the consumer probe above; it was run by hand.
- No live crawl was run. Every number here is a build or a module resolution.
  "No behaviour change" rests on the existing test suite passing unmodified, not
  on a crawl against live boards.
