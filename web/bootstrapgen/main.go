// Command bootstrapgen builds an additive verified default-page projection
// from an existing corpus generation. It does not crawl or publish anything.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/corpus"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/web/bootstrap"
)

func main() {
	corpusDir := flag.String("corpus", "", "directory holding an assembled corpus generation (required)")
	output := flag.String("output", "", "path for the verified bootstrap JSON (required)")
	asOf := flag.String("as-of", "", "RFC 3339 lifecycle evaluation instant; defaults to generation run_at")
	flag.Parse()
	if *corpusDir == "" || *output == "" {
		fmt.Fprintln(os.Stderr, "usage: bootstrapgen -corpus <dir> -output <path> [-as-of RFC3339]")
		os.Exit(2)
	}

	var at time.Time
	var err error
	if *asOf != "" {
		at, err = time.Parse(time.RFC3339, *asOf)
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid -as-of: %v\n", err)
			os.Exit(2)
		}
	}
	started := time.Now()
	document, err := bootstrap.Generate(context.Background(), corpus.DirStore{Dir: *corpusDir}, at)
	if err == nil {
		err = bootstrap.WriteFile(*output, document)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	info, _ := os.Stat(*output)
	fmt.Printf("bootstrap generation %d: %d verified cards, %d bytes, %s, digest %s\n",
		document.Payload.Generation, len(document.Payload.Response.Items), info.Size(), time.Since(started).Round(time.Millisecond), document.PayloadDigest)
}
