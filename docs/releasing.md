# Releasing

How a versioned release of `job-hunter-toolkit` and `job-hunter-mcp` is cut,
what the tag triggers, and how anyone can verify what they downloaded.

## Cutting a release

There is no version file to bump and no release branch. A release is a tag:

```console
$ git tag -a v1.0.0 -m "v1.0.0"
$ git push origin v1.0.0
```

Any tag starting with `v` triggers `.github/workflows/release.yml`. Use
semantic versions (`vMAJOR.MINOR.PATCH`); the tag name is used verbatim as the
version string, the release title, and the archive names.

Tag the commit you mean. The workflow builds whatever the tag points at, and
the changelog stub lists commits since the previous `v*` tag reachable from
it.

## What the tag triggers

One `ubuntu-latest` job that:

1. **Builds both binaries for the four supported platforms** — the same four
   pairs CI's portability job asserts on every push: `linux/amd64`,
   `linux/arm64`, `darwin/arm64`, `windows/amd64`. Every build is
   `CGO_ENABLED=0 go build -trimpath -ldflags "-s -w"`, so the binaries are
   static, stripped, and free of build-machine paths.
2. **Proves the build is reproducible.** Everything is built and packaged a
   second time after `go clean -cache`, and the run fails if any archive's
   SHA-256 differs between the two passes. `-trimpath` is what should make
   this hold; proving it on every release rather than assuming it is the
   point. Archive metadata is pinned too: tar/zip entries are sorted, tar
   ownership is zeroed, and every file timestamp is the tagged commit's time
   (`SOURCE_DATE_EPOCH`), never the clock — so the same tag always yields
   byte-identical archives.
3. **Packages predictably.** One archive per platform, containing both
   binaries plus `LICENSE` and `README.md`:

   ```
   job-hunter-toolkit_<version>_<os>_<arch>.tar.gz   (linux, darwin)
   job-hunter-toolkit_<version>_windows_amd64.zip
   ```

4. **Writes `SHA256SUMS`** covering every archive, and a **changelog stub**
   (`RELEASE_NOTES.md`) generated from `git log` since the previous `v*` tag.
   The stub is the release body; edit the release afterwards to say what the
   release means, not just what it contains.
5. **Publishes a GitHub release** for the tag with the archives and
   `SHA256SUMS` attached, via `gh release create --verify-tag`. The archives
   are also uploaded as a 14-day workflow artifact, so even a dry run leaves
   something to inspect.

## Dry runs

A tag cannot be un-pushed, so the workflow is exercisable without one: run it
manually from the Actions tab (`workflow_dispatch`) on any branch. A branch
dispatch builds version `v0.0.0-dry.g<short-sha>`, runs every step including
the determinism check, uploads the artifact, and **never publishes** — the
`publish` input is only honoured when the dispatch itself runs from a `v*`
tag. A tag push always publishes.

## Verifying a download

`SHA256SUMS` is attached to every release. Download it next to the archive:

```console
$ sha256sum --ignore-missing -c SHA256SUMS
job-hunter-toolkit_v1.0.0_linux_amd64.tar.gz: OK
```

macOS ships `shasum` instead:

```console
$ shasum -a 256 --ignore-missing -c SHA256SUMS
```

Windows, in PowerShell (compare against the matching line in `SHA256SUMS`):

```powershell
> Get-FileHash job-hunter-toolkit_v1.0.0_windows_amd64.zip -Algorithm SHA256
```

Because the build is reproducible, anyone with the same Go toolchain can also
rebuild the tag with the flags above and expect the same binary hashes.

## Version stamping

- `job-hunter-mcp` has a `var version` seam in its main package; the workflow
  stamps it with `-X main.version=<tag>`, so `job-hunter-mcp -version` prints
  the release version.
- `job-hunter-toolkit` (the CLI) has **no version variable and no `--version`
  flag yet**, so the workflow deliberately stamps nothing into it — `-X`
  against a symbol that does not exist is silently ignored, which would look
  like success while doing nothing. The CLI still records the commit it was
  built from via `debug.ReadBuildInfo` (see `buildCommit` in
  `crawl_report.go`). **Follow-up:** add a `version` seam and a `--version`
  flag to the root command, then extend the workflow's `-X` flag to it.

## What it costs

Nothing between releases: the workflow has no schedule and no per-commit
trigger — it runs only on `v*` tag pushes and manual dispatch. A release is a
single `ubuntu-latest` job; the two build passes (8 binaries each) measured
72s and 69s on a local machine, so a full run with checkout, module download,
packaging and upload should stay under ~10 runner-minutes per release.

## When a release goes wrong

The publish step fails if a release for the tag already exists, so re-running
a failed job cannot silently overwrite assets. To redo a release: delete the
GitHub release and the tag, fix, re-tag, re-push. Prefer cutting a new patch
version over rewriting a tag anyone may already have fetched.
