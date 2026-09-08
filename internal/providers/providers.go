// Package providers maps configured source ids to concrete scraper instances.
package providers

import (
	"fmt"
	"net/http"

	"github.com/Vuks69/phrack-rss-go/internal/config"
	"github.com/Vuks69/phrack-rss-go/internal/scraper"
	"github.com/Vuks69/phrack-rss-go/internal/scraper/phrack"
)

// constructor builds a scraper.Source from the shared config and an HTTP
// client. New sites register here.
type constructor func(source config.Config) (scraper.Source, *http.Client, error)

// registry is the single list of known sources. The key must equal the
// Source.ID() of the built instance.
var registry = map[string]constructor{
	"phrack": phrack.New,
}

// Build instantiates every enabled source in configuration order.
// It returns a map of source id -> Source and a shared HTTP client.
func Build(cfg config.Config) (map[string]scraper.Source, *http.Client, error) {
	sources := make(map[string]scraper.Source, len(cfg.Sources))
	var client *http.Client
	for _, id := range cfg.Sources {
		mk, ok := registry[id]
		if !ok {
			return nil, nil, fmt.Errorf("unknown source %q (known: %v)", id, knownIDs())
		}
		src, c, err := mk(cfg)
		if err != nil {
			return nil, nil, fmt.Errorf("source %q: %w", id, err)
		}
		sources[src.ID()] = src
		if client == nil {
			client = c
		}
	}
	return sources, client, nil
}

// Known reports whether a source id is registered.
func Known(id string) bool {
	_, ok := registry[id]
	return ok
}

func knownIDs() []string {
	ids := make([]string, 0, len(registry))
	for id := range registry {
		ids = append(ids, id)
	}
	return ids
}
