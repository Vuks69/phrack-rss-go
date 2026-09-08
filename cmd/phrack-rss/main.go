// Command phrack-rss is a self-hosted RSS/Atom feed generator for the Phrack
// magazine site. It scrapes upstream content through pluggable providers,
// dedupes items into an on-disk store, renders feed files, and serves them on
// loopback only.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Vuks69/phrack-rss-go/internal/config"
	"github.com/Vuks69/phrack-rss-go/internal/pipeline"
	"github.com/Vuks69/phrack-rss-go/internal/providers"
	"github.com/Vuks69/phrack-rss-go/internal/server"
	"github.com/Vuks69/phrack-rss-go/internal/store"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return serve()
	}
	switch args[0] {
	case "serve":
		return serve()
	case "generate":
		return generate()
	case "version", "--version", "-v":
		fmt.Printf("phrack-rss %s\n", config.Version)
		return nil
	case "help", "--help", "-h":
		usage()
		return nil
	default:
		usage()
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func generate() error {
	cfg := config.FromEnv()
	st, err := store.Open(cfg.DataDir)
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	sources, _, err := providers.Build(cfg)
	if err != nil {
		return err
	}

	started := time.Now()
	res, err := pipeline.New(cfg, sources, st).Run(context.Background())
	if err != nil {
		return err
	}
	slog.Info("generate complete",
		"discovered", res.Discovered,
		"added", res.Added,
		"stored", st.Count(),
		"pruned", res.Pruned,
		"failed", res.Failed,
		"took", time.Since(started).Round(time.Millisecond),
	)
	if res.FirstErr != nil {
		slog.Warn("some items failed to enrich", "first_error", res.FirstErr)
	}
	return nil
}

func serve() error {
	cfg := config.FromEnv()
	st, err := store.Open(cfg.DataDir)
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	sources, _, err := providers.Build(cfg)
	if err != nil {
		return err
	}
	pl := pipeline.New(cfg, sources, st)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Serve immediately; the first scrape runs in the background so /feed.xml
	// answers 503 Retry-After until data exists. Existing feed files keep
	// serving if a rescan fails.
	go func() {
		report(pl.Run(ctx))
		ticker := time.NewTicker(cfg.Interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				rctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
				report(pl.Run(rctx))
				cancel()
			}
		}
	}()

	srv := &http.Server{
		Addr:              cfg.Addr(),
		Handler:           server.New(cfg).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		slog.Info("serving feed", "addr", cfg.Addr(), "interval", cfg.Interval)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

func report(res pipeline.Result, err error) {
	if err != nil {
		slog.Error("scrape failed", "error", err)
		return
	}
	slog.Info("scrape complete",
		"discovered", res.Discovered,
		"added", res.Added,
		"pruned", res.Pruned,
		"failed", res.Failed,
	)
	if res.FirstErr != nil {
		slog.Warn("some items failed to enrich", "first_error", res.FirstErr)
	}
}

func usage() {
	fmt.Print(`phrack-rss — self-hosted RSS/Atom feed generator

Usage:
  phrack-rss [command]

Commands:
  serve      run the scrape loop and HTTP server (default)
  generate   run one scrape cycle then exit (smoke tests / CI)
  version    print the version
  help       print this help

Configuration is via PHRACK_RSS_* environment variables:
  DATA_DIR     data + feed directory        (default ./data)
  LISTEN       bind address                 (default 127.0.0.1)
  PORT         listen port                  (default 58315)
  BASE_URL     upstream site                (default https://phrack.org)
  INTERVAL     rescan interval              (default 1h)
  FEED_LENGTH  newest items in feed         (default 20)
  MAX_ITEMS    capped stored items/source   0 = keep all (default)
  SOURCES      comma-separated providers    (default phrack)
  HTTP_TIMEOUT per-request timeout          (default 30s)
  MIN_GAP      politeness gap between reqs  (default 200ms)
  CONCURRENCY  parallel enrich fetches      (default 8)
  USER_AGENT   outbound request User-Agent  (default phrack-rss/<version>)
`)
}
