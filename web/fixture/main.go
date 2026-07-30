// Command fixture writes the deterministic test corpus from
// web/internal/testcorpus into a directory, so harnesses outside `go test` —
// the Node smoke test in web/test, or a local `python -m http.server` against
// the assembled site — have a real generation to open.
//
// -scale multiplies the row count by cloning the fixture's sources under new
// keys, which is how the smoke test measures load and search cost at corpus
// volume without inventing data: the rows are labelled synthetic by their
// source keys and never leave the test.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/web/internal/testcorpus"
)

func main() {
	dir := flag.String("dir", "", "directory to publish the fixture corpus into (required)")
	scale := flag.Int("scale", 1, "clone the fixture's sources this many times")
	flag.Parse()

	if *dir == "" {
		fmt.Fprintln(os.Stderr, "usage: fixture -dir <path> [-scale n]")
		os.Exit(2)
	}

	if err := testcorpus.Build(context.Background(), *dir, *scale); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	expect := testcorpus.Expect()
	fmt.Printf("fixture corpus in %s: %d rows/clone × %d clones, pinned clock %s\n",
		*dir, expect.Rows, *scale, testcorpus.Now.Format("2006-01-02T15:04:05Z"))
}
