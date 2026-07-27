// Command enrichgen regenerates the committed enrichment tables.
//
// It is a separate program from the CLI on purpose. The toolkit's binary must
// stay portable and stateless and must never spend a request answering a
// question about a company; this program is the opposite of all three, and it
// runs in GitHub Actions, monthly, in a workflow of its own.
//
// Typical use, which is what the workflow runs:
//
//	go run ./tools/enrichgen -out internal/enrich/data -contact ops@example.com
//
// It writes internal/enrich/data/employers.tsv and candidates.tsv and changes
// nothing else. The result is a pull request a human reads: employers.tsv
// becomes data the binary ships, and candidates.tsv is the queue of matches
// that were refused for being ambiguous. Promoting one means confirming it and
// adding a row to manual.tsv by hand.
//
// The contact is mandatory. SEC EDGAR's access policy requires a User-Agent
// naming a reachable contact and Wikimedia's rejects generic agents, so an
// anonymous run is refused here rather than blocked there.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/enrich/edgar"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/enrich/fetch"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/enrich/gen"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/enrich/wikidata"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/httpx"
)

// ContactEnv supplies the contact address when the flag is not given, so a
// workflow can put it in the environment rather than in a command line that
// ends up in a log.
const ContactEnv = "JHT_ENRICH_CONTACT"

func main() {
	var (
		out          = flag.String("out", "internal/enrich/data", "directory to write the generated tables into")
		contact      = flag.String("contact", os.Getenv(ContactEnv), "contact address sent in the User-Agent; required by SEC and Wikimedia policy")
		timeout      = flag.Duration("timeout", 30*time.Minute, "overall time budget")
		skipWikidata = flag.Bool("skip-wikidata", false, "resolve and fetch SEC facts only")
	)

	flag.Parse()

	// Diagnostics to stderr, per docs/architecture-roadmap.md. This program
	// writes its data to files rather than to stdout, because a table nobody
	// reviews is a table nobody should trust.
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	if err := run(*out, *contact, *timeout, *skipWikidata, logger); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(out, contact string, timeout time.Duration, skipWikidata bool, logger *slog.Logger) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	ctx, cancelTimeout := context.WithTimeout(ctx, timeout)
	defer cancelTimeout()

	// One client per upstream, each with its own pacing. They are not shared:
	// EDGAR's budget is one request per 150ms across all of its hosts, and
	// Wikidata's is a query every couple of seconds, and a single client would
	// have to apply the stricter of the two to both.
	edgarClient, err := fetch.Client(contact, edgar.Interval, httpx.WithLogger(logger))
	if err != nil {
		return err
	}

	opts := gen.Options{
		EDGAR:  edgarClient,
		Now:    time.Now().UTC(),
		Logger: logger,
	}

	if !skipWikidata {
		if opts.Wikidata, err = fetch.Client(contact, wikidata.Interval, httpx.WithLogger(logger)); err != nil {
			return err
		}
	}

	result, err := gen.Run(ctx, opts)
	if err != nil {
		return err
	}

	if err := result.WriteTables(out, opts.Now); err != nil {
		return err
	}

	// Coverage first, and on stderr, because it is the number that decides
	// whether the diff below it is good news. It will be low: most companies
	// this project crawls are private and EDGAR has never heard of them.
	fmt.Fprintf(os.Stderr, "%s\n", result.Stats)

	return nil
}
