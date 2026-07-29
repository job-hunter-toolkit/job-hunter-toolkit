// Package jobposting defines the record this project is built around — a single
// job posting — together with the small vocabulary that describes one: the
// platform and tenant it came from, the engagement and workplace types, and the
// pay range an employer published.
//
// # Compatibility
//
// This package is pre-v1 and carries no stability guarantee. Its Go API may
// change in any release, with no deprecation period and no major-version bump.
// Pin a commit if you need one.
//
// The JSON encoding of [JobPosting] is the exception: field names and the
// omitempty/omitzero choices are frozen ahead of the Go API, because NDJSON
// output is already a documented shell-pipeline format. Changing those is a
// schema event with a migration. See docs/design/package-taxonomy.md §7.
//
// # Dependencies
//
// This package imports nothing outside the standard library, and deliberately
// not net/http. A corpus reader, a manifest parser or a filter has no business
// linking an HTTP stack: merely importing net/http costs roughly 3.3 MB of
// linked binary on linux/amd64 and 3.0 MB on js/wasm, measured on 2026-07-28.
// The crawler, its adapters and its rate limiter stay internal to this module.
// TestDependenciesAreStandardLibraryOnly enforces the rule.
package jobposting
