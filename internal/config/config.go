// Package config builds runtime configuration from environment variables with
// sensible defaults. Every setting can be overridden for local testing.
package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

// Version is the application version. It is the single source of truth for the
// version reported by the binary (phrack-rss version, User-Agent)
var Version = "0.1.0"

const (
	// DefaultPort sits in the private 58xxx range used by sibling NixOS
	// modules (miniflux uses 58312); 58315 is currently unused there.
	DefaultPort = 58315
	// DefaultListen is loopback-only: the feed must never be exposed beyond
	// the host.
	DefaultListen = "127.0.0.1"
	// DefaultDataDir is used when no data directory is configured.
	DefaultDataDir = "./data"
	// DefaultBaseURL is the upstream site the phrack scraper reads from.
	DefaultBaseURL = "https://phrack.org"
	// DefaultInterval is how often the serve subcommand rescans.
	DefaultInterval = 1 * time.Hour
	// DefaultFeedLength is how many newest items land in the rendered feed.
	DefaultFeedLength = 20
)

// Config is the fully resolved runtime configuration.
type Config struct {
	// DataDir holds data.json and the rendered feed files.
	DataDir string
	// Listen is the bind address (loopback only by default).
	Listen string
	// Port is the TCP port to bind in serve mode.
	Port int
	// BaseURL is the upstream site to scrape.
	BaseURL string
	// Interval is the rescan period in serve mode.
	Interval time.Duration
	// FeedLength caps the number of items in the rendered feed.
	FeedLength int
	// Sources is the ordered list of enabled scraper ids.
	Sources []string
	// HTTPTimeout bounds each outbound HTTP request.
	HTTPTimeout time.Duration
	// UserAgent identifies the scraper to upstream sites.
	UserAgent string
	// MinGap is the minimum spacing between outbound requests (politeness).
	MinGap time.Duration
	// Concurrency is the number of parallel Enrich fetches.
	Concurrency int
	// MaxItems caps how many newest items the store retains per source.
	// 0 keeps everything (the historical default).
	MaxItems int
}

// Default returns a Config populated with the documented defaults.
func Default() Config {
	return Config{
		DataDir:     DefaultDataDir,
		Listen:      DefaultListen,
		Port:        DefaultPort,
		BaseURL:     DefaultBaseURL,
		Interval:    DefaultInterval,
		FeedLength:  DefaultFeedLength,
		Sources:     []string{"phrack"},
		HTTPTimeout: 30 * time.Second,
		UserAgent:   fmt.Sprintf("phrack-rss/%s (+https://github.com/Vuks69/phrack-rss-go)", Version),
		MinGap:      200 * time.Millisecond,
		Concurrency: 8,
		MaxItems:    0,
	}
}

// FromEnv resolves a Config from the PHRACK_RSS_* environment variables,
// falling back to Default() for anything unset or invalid.
func FromEnv() Config {
	cfg := Default()

	if v := os.Getenv("PHRACK_RSS_DATA_DIR"); v != "" {
		cfg.DataDir = v
	}
	if v := os.Getenv("PHRACK_RSS_LISTEN"); v != "" {
		cfg.Listen = v
	}
	if v, err := strconv.Atoi(os.Getenv("PHRACK_RSS_PORT")); v > 0 && err == nil {
		cfg.Port = v
	}
	if v := os.Getenv("PHRACK_RSS_BASE_URL"); v != "" {
		cfg.BaseURL = strings.TrimRight(v, "/")
	}
	if v, err := time.ParseDuration(os.Getenv("PHRACK_RSS_INTERVAL")); v > 0 && err == nil {
		cfg.Interval = v
	}
	if v, err := strconv.Atoi(os.Getenv("PHRACK_RSS_FEED_LENGTH")); v > 0 && err == nil {
		cfg.FeedLength = v
	}
	if v := os.Getenv("PHRACK_RSS_SOURCES"); v != "" {
		cfg.Sources = splitCSV(v)
	}
	if v, err := time.ParseDuration(os.Getenv("PHRACK_RSS_HTTP_TIMEOUT")); v > 0 && err == nil {
		cfg.HTTPTimeout = v
	}
	if v, err := time.ParseDuration(os.Getenv("PHRACK_RSS_MIN_GAP")); v >= 0 && err == nil {
		cfg.MinGap = v
	}
	if v, err := strconv.Atoi(os.Getenv("PHRACK_RSS_CONCURRENCY")); v > 0 && err == nil {
		cfg.Concurrency = v
	}
	if v, err := strconv.Atoi(os.Getenv("PHRACK_RSS_MAX_ITEMS")); v >= 0 && err == nil {
		cfg.MaxItems = v
	}
	if v := os.Getenv("PHRACK_RSS_USER_AGENT"); v != "" {
		cfg.UserAgent = v
	}
	return cfg
}

// Addr returns the full listen address for the HTTP server.
func (c Config) Addr() string {
	return net.JoinHostPort(c.Listen, strconv.Itoa(c.Port))
}

func splitCSV(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
