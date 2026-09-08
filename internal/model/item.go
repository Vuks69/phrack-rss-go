// Package model defines the item type shared across the feed pipeline.
package model

import (
	"sort"
	"strings"
	"time"
)

// Item is a single normalized feed entry. Field names are stable so the on-disk
// JSON store can be read by future versions.
type Item struct {
	// Title is the article headline.
	Title string `json:"title"`
	// URL is the canonical, fully qualified article URL.
	URL string `json:"url"`
	// Author is the article author name, when known.
	Author string `json:"author,omitempty"`
	// IssuedAt is the publication date of the article.
	IssuedAt time.Time `json:"issuedAt"`
	// SourceID identifies which scraper produced the item (e.g. "phrack").
	SourceID string `json:"source,omitempty"`
}

// Normalize cleans up an item: trims whitespace and ensures the timestamp is
// in UTC.
func (i *Item) Normalize() {
	i.Title = strings.TrimSpace(i.Title)
	i.URL = strings.TrimSpace(i.URL)
	i.Author = strings.TrimSpace(i.Author)
	if !i.IssuedAt.IsZero() {
		i.IssuedAt = i.IssuedAt.UTC()
	}
}

// Usable reports whether the item can be persisted and rendered.
func (i Item) Usable() bool {
	return i.URL != "" && !i.IssuedAt.IsZero()
}

// SortItems sorts items newest-first, breaking ties by URL for stability.
func SortItems(items []Item) {
	sort.SliceStable(items, func(a, b int) bool {
		if items[a].IssuedAt.Equal(items[b].IssuedAt) {
			return items[a].URL < items[b].URL
		}
		return items[a].IssuedAt.After(items[b].IssuedAt)
	})
}
