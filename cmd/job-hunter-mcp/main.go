// Command job-hunter-mcp serves the job-hunter toolkit over the Model Context
// Protocol on stdio, so an agent can search postings, enumerate coverage and
// look up employers as tool calls.
//
// It is a separate binary from job-hunter-toolkit rather than a subcommand.
// The CLI's promise is that it works with no storage, no daemon and no
// configuration; an MCP server is a long-lived process speaking a wire protocol
// on stdin and stdout, which is a different shape of program with a different
// failure mode. Keeping them apart also keeps the protocol layer out of the
// default binary entirely.
//
// Usage:
//
//	job-hunter-mcp [flags]
//
// The server reads JSON-RPC from stdin and writes it to stdout. Every
// diagnostic goes to stderr: a stray line on stdout corrupts the session.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/enrich"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/httpx"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/mcp"
)

// version is the reported server version. It is a build-time variable so a
// release can stamp it without editing source.
var version = "dev"

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "job-hunter-mcp: %v\n", err)
		os.Exit(1)
	}
}

// run wires and starts the server. It takes its streams as arguments so the
// whole program is exercisable from a test.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("job-hunter-mcp", flag.ContinueOnError)
	flags.SetOutput(stderr)

	var (
		logLevel = flags.String("log-level", "warn",
			"log verbosity: debug, info, warn, or error. Logs go to stderr; stdout is the protocol stream")
		maxSources = flags.Int("max-sources", mcp.DefaultMaxSources,
			"largest number of job boards one search may crawl before it is refused")
		timeout = flags.Duration("timeout", mcp.DefaultTimeout,
			"wall-clock budget for one search; exceeding it returns a partial answer marked incomplete")
		concurrency = flags.Int("concurrency", mcp.DefaultConcurrency,
			"number of job boards to fetch at once")
		perHostLimit = flags.Int("per-host-limit", httpx.DefaultPerHostLimit,
			"maximum concurrent requests to any single job board backend")
		userAgent = flags.String("user-agent", httpx.DefaultUserAgent,
			"User-Agent sent to job boards")
		showVersion = flags.Bool("version", false, "print the version and exit")
	)

	flags.Usage = func() {
		fmt.Fprintf(stderr, "Usage: job-hunter-mcp [flags]\n\n"+
			"Serves job-hunter-toolkit over the Model Context Protocol on stdio.\n"+
			"Reads JSON-RPC from stdin, writes JSON-RPC to stdout, logs to stderr.\n\n"+
			"Flags:\n")
		flags.PrintDefaults()
	}

	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}

		return err
	}

	if *showVersion {
		fmt.Fprintln(stderr, version)

		return nil
	}

	level, err := parseLevel(*logLevel)
	if err != nil {
		return err
	}

	logger := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: level}))

	// Politeness is not a flag this server is willing to lose. A crawl driven by
	// a conversation is still a crawl of somebody else's job board, and the
	// per-host limiter is what keeps it from becoming pressure.
	client := httpx.NewClient(
		httpx.WithLogger(logger),
		httpx.WithPerHostLimit(*perHostLimit),
		httpx.WithUserAgent(*userAgent),
	)

	catalog := mcp.NewBuiltinCatalog(client, *concurrency)

	// A missing enrichment table is not fatal. The employer tool reports that
	// nothing is loaded and the other three are unaffected, which is a better
	// outcome than refusing to start a job search because a side table failed to
	// parse.
	var employers mcp.Employers

	if table, err := enrich.Default(); err != nil {
		logger.Warn("employer table unavailable; lookup_employer will report no data",
			slog.String("cause", err.Error()))
	} else {
		employers = table
	}

	server := &mcp.Server{
		Name:      "job-hunter-toolkit",
		Version:   version,
		Catalog:   catalog,
		Employers: employers,
		Logger:    logger,
		Limits: mcp.Limits{
			MaxSources: *maxSources,
			Timeout:    *timeout,
		},
	}

	// SIGINT and SIGTERM cancel in-flight crawls. The session itself ends when
	// the client closes stdin, which is how an MCP stdio server is meant to be
	// shut down.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Info("job-hunter-mcp ready",
		slog.String("version", version),
		slog.Int("sources", len(catalog.Sources())),
		slog.Int("max_sources_per_search", *maxSources),
		slog.Duration("search_timeout", *timeout),
		slog.Int("concurrency", *concurrency))

	if err := server.Run(ctx, stdin, stdout); err != nil {
		return fmt.Errorf("serve: %w", err)
	}

	return nil
}

// parseLevel maps a log level name onto its slog level.
func parseLevel(name string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	}

	return 0, fmt.Errorf("invalid --log-level %q: want debug, info, warn, or error", name)
}
