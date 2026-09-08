// Package feed renders stored items as RSS 2.0 and Atom documents.
package feed

import (
	"fmt"
	"time"

	"github.com/gorilla/feeds"

	"github.com/Vuks69/phrack-rss-go/internal/model"
)

// Rendered holds the serialized feed documents.
type Rendered struct {
	// SourceID is the source the feed covers.
	SourceID string
	// Title is the human-readable feed title.
	Title string
	// Link is the upstream site URL.
	Link string
	// RSS is the RSS 2.0 document.
	RSS []byte
	// Atom is the Atom document.
	Atom []byte
}

// Render builds a feed for one source from its newest items.
func Render(sourceID, title, link string, items []model.Item) (Rendered, error) {
	model.SortItems(items)

	f := &feeds.Feed{
		Title:       title,
		Link:        &feeds.Link{Href: link},
		Description: fmt.Sprintf("%s — weekly-ish digest generated from %s", title, link),
		Id:          link,
		Created:     time.Now().UTC(),
	}
	for _, it := range items {
		f.Items = append(f.Items, &feeds.Item{
			Title:   it.Title,
			Link:    &feeds.Link{Href: it.URL},
			Author:  &feeds.Author{Name: it.Author},
			Created: it.IssuedAt,
			Id:      it.URL,
		})
	}

	rss, err := f.ToRss()
	if err != nil {
		return Rendered{}, fmt.Errorf("feed: rss: %w", err)
	}
	atom, err := f.ToAtom()
	if err != nil {
		return Rendered{}, fmt.Errorf("feed: atom: %w", err)
	}
	return Rendered{
		SourceID: sourceID,
		Title:    title,
		Link:     link,
		RSS:      []byte(rss),
		Atom:     []byte(atom),
	}, nil
}
