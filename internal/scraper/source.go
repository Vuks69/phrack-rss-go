// Package scraper defines the interface a site scraper must implement.
//
// A Source splits work into two phases so the pipeline can keep network cost
// low on rescan:
//
//   - Discover cheaply enumerates candidate items (e.g. links on an index
//     page). It should not fetch per-item pages.
//   - Enrich fetches full metadata for a single candidate (e.g. a per-item
//     page). The pipeline calls Enrich only for candidates it has not already
//     stored, and does so concurrently.
//
// Adding a new site means implementing Source for it and registering a
// constructor in internal/providers.
package scraper

import (
	"context"
	"time"

	"github.com/Vuks69/phrack-rss-go/internal/model"
)

// Candidate is a lightly-sourced potential item produced by Discover.
type Candidate struct {
	// URL is the fully qualified page URL.
	URL string
	// Title is the display title known from the index listing.
	Title string
	// Author is the author known from the index listing.
	Author string
	// IssueDate is an optional coarse publication date (e.g. the issue-level
	// date) used as a fallback when Enrich cannot find a per-item date.
	IssueDate time.Time
}

// Source scrapes one website for feed items.
type Source interface {
	// ID returns a stable identifier used in the store and feed filenames
	// (e.g. "phrack"). It must match the constructor key in providers.
	ID() string
	// Label returns a human-readable title for the rendered feed.
	Label() string
	// Discover enumerates candidate items cheaply.
	Discover(ctx context.Context) ([]Candidate, error)
	// Enrich fetches the full item metadata for a candidate.
	Enrich(ctx context.Context, c Candidate) (model.Item, error)
}
