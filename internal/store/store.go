// Package store persists scraped items to a JSON file and answers queries for
// the feed renderer. Items dedupe on canonical URL; the file is written
// atomically (tmp + rename) so a crash never corrupts state.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/Vuks69/phrack-rss-go/internal/model"
)

// defaultFile is the state file name within the data directory.
const defaultFile = "data.json"

// ErrClosed is returned for operations after Close, though Open keeps the file
// handle only briefly, so this is defensive.
var ErrClosed = errors.New("store: closed")

type fileState struct {
	Items []model.Item `json:"items"`
}

// Store holds the deduplicated item map in memory, backed by a JSON file.
type Store struct {
	mu    sync.Mutex
	path  string
	items map[string]model.Item
}

// Open loads state from <dir>/data.json, creating the file if absent.
func Open(dir string) (*Store, error) {
	if dir == "" {
		return nil, errors.New("store: empty data dir")
	}
	path := filepath.Join(dir, defaultFile)
	s := &Store{path: path, items: map[string]model.Item{}}
	// #nosec G304 -- path is always <dir>/data.json where dir is the
	// operator-supplied data directory, never request-derived.
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, fmt.Errorf("store: read %s: %w", path, err)
	}
	var state fileState
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, fmt.Errorf("store: corrupt %s: %w", path, err)
	}
	for _, it := range state.Items {
		it.Normalize()
		if it.Usable() {
			s.items[it.URL] = it
		}
	}
	return s, nil
}

// Known returns a set of URLs already stored, for skipping Enrich fetches.
func (s *Store) Known() map[string]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	known := make(map[string]bool, len(s.items))
	for u := range s.items {
		known[u] = true
	}
	return known
}

// Upsert adds items, replacing any with the same URL, and returns the number
// newly added. Unusable items are ignored.
func (s *Store) Upsert(items []model.Item) (added int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, it := range items {
		it.Normalize()
		if !it.Usable() {
			continue
		}
		if _, exists := s.items[it.URL]; !exists {
			added++
		}
		s.items[it.URL] = it
	}
	return added
}

// Latest returns the newest n items for a source, newest first.
func (s *Store) Latest(sourceID string, n int) []model.Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	all := make([]model.Item, 0, len(s.items))
	for _, it := range s.items {
		if sourceID == "" || it.SourceID == sourceID {
			all = append(all, it)
		}
	}
	model.SortItems(all)
	if n > 0 && len(all) > n {
		all = all[:n]
	}
	return all
}

// Prune trims a source's stored items down to the newest max, dropping the
// rest. A max <= 0 is a no-op ("keep everything"). It returns the number of
// items removed.
func (s *Store) Prune(sourceID string, max int) int {
	if max <= 0 {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var items []model.Item
	for _, it := range s.items {
		if it.SourceID == sourceID {
			items = append(items, it)
		}
	}
	if len(items) <= max {
		return 0
	}
	model.SortItems(items)
	removed := 0
	for _, it := range items[max:] {
		// Only remove when the stored item still belongs to this source, so
		// URL-key collisions across sources can never delete the wrong item.
		if cur, ok := s.items[it.URL]; ok && cur.SourceID == sourceID {
			delete(s.items, it.URL)
			removed++
		}
	}
	return removed
}

// Sources returns the sorted set of source ids present in the store.
func (s *Store) Sources() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := map[string]bool{}
	for _, it := range s.items {
		ids[it.SourceID] = true
	}
	seen := make([]string, 0, len(ids))
	for id := range ids {
		seen = append(seen, id)
	}
	return seen
}

// Count returns the total number of stored items (across all sources).
func (s *Store) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.items)
}

// Save writes the full state to disk atomically.
func (s *Store) Save() error {
	s.mu.Lock()
	items := make([]model.Item, 0, len(s.items))
	for _, it := range s.items {
		items = append(items, it)
	}
	s.mu.Unlock()

	state := fileState{Items: items}
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o750); err != nil {
		return fmt.Errorf("store: mkdir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), "data.json.*")
	if err != nil {
		return fmt.Errorf("store: temp: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("store: write: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("store: sync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("store: close: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("store: rename: %w", err)
	}
	return nil
}
