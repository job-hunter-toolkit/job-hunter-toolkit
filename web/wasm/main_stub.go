//go:build !js || !wasm

// The stub keeps `go build ./...` green on every platform: a main package
// whose only real file is constrained to js/wasm would otherwise fail the
// ordinary build with "build constraints exclude all Go files". Running it
// tells you what to do instead of doing something surprising.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "this binary is the browser engine; build it with: GOOS=js GOARCH=wasm go build ./web/wasm")
	os.Exit(2)
}
