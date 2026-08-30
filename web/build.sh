#!/usr/bin/env sh
# build.sh — assemble the deployable site into one directory.
#
#   web/build.sh [output-dir]      (default: web/dist, which stays untracked)
#
# The output is exactly what GitHub Pages serves: the static files from web/,
# the Go runtime shim (wasm_exec.js) from the active toolchain, and the query
# engine compiled to WebAssembly. The corpus itself is NOT part of the site —
# it is published separately by the corpus pipeline and reached via
# web/config.js.
set -eu

cd "$(dirname "$0")/.."

out="${1:-web/dist}"
mkdir -p "$out"

cp web/index.html web/style.css web/app.js web/card.js web/config.js web/snapshot.js web/corpus-store.js \
   web/freshness.js \
   web/rollup.js web/worker.js web/engine-client.js web/webmcp.js \
   web/manifest.webmanifest web/icon.svg web/icon-maskable.svg \
   web/apple-touch-icon.png "$out/"

# The service worker's precache name is stamped per deploy: new bytes mean a
# fresh install, which is how every shell update reaches installed apps.
stamp="$(git rev-parse --short HEAD 2>/dev/null || date -u +%Y%m%d%H%M%S)"
sed "s/__BUILD__/${stamp}/" web/sw.js > "$out/sw.js"

# wasm_exec.js must come from the same toolchain that compiles the wasm; a
# committed copy would drift from the Go version CI builds with.
cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" "$out/"

GOOS=js GOARCH=wasm go build -trimpath -ldflags='-s -w' -o "$out/engine.wasm" ./web/wasm

# Report what the payload weighs; the gzipped number is what a first visit
# actually downloads on any host that compresses (GitHub Pages does).
raw=$(wc -c <"$out/engine.wasm")
gz=$(gzip -9 -c "$out/engine.wasm" | wc -c)
echo "site assembled in $out"
echo "engine.wasm: ${raw} bytes raw, ${gz} bytes gzipped"

# Startup network budget. The browser cannot search until this payload starts,
# and generation-10 already spends tens of seconds loading corpus columns. Keep
# additive engine work from quietly taxing every cold visit.
if [ "$gz" -gt 1180000 ]; then
  echo "engine.wasm exceeds the 1,180,000-byte gzip budget" >&2
  exit 1
fi
