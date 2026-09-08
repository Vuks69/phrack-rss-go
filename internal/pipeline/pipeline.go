// Package pipeline orchestrates a full scrape cycle: Discover candidates,
// Enrich only the new ones, dedupe into the store, and render feed files.
//
// It is deliberately source-agnostic; each enabled scraper config.Source runs
// through the same path, which is what makes adding another site a matter of
// implementing scraper.Source and registering it in providers.
package pipeline

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/Vuks69/phrack-rss-go/internal/config"
	"github.com/Vuks69/phrack-rss-go/internal/feed"
	"github.com/Vuks69/phrack-rss-go/internal/model"
	"github.com/Vuks69/phrack-rss-go/internal/scraper"
	"github.com/Vuks69/phrack-rss-go/internal/store"
)

// Result summarizes one Run.
type Result struct {
	// Discovered is the number of unique candidate items found.
	Discovered int
	// Added is the number of brand-new items persisted during the run.
	Added int
	// Failed is the number of Enrich calls that errored (non-fatal).
	Failed int
	// FirstErr is the first Enrich error seen, if any.
	FirstErr error
	// Pruned is the number of items dropped by the retention cap.
	Pruned int
	// FeedsWritten is the number of rendered feed documents written.
	FeedsWritten int
}

// Pipeline drives one source through the scrape -> store -> render flow.
type Pipeline struct {
	cfg     config.Config
	sources map[string]scraper.Source
	st      *store.Store
}

// New builds a Pipeline.
func New(cfg config.Config, sources map[string]scraper.Source, st *store.Store) *Pipeline {
	return &Pipeline{cfg: cfg, sources: sources, st: st}
}

// Run performs a full cycle across every enabled source.
func (p *Pipeline) Run(ctx context.Context) (Result, error) {
	var res Result
	for _, id := range p.cfg.Sources {
		src, ok := p.sources[id]
		if !ok {
			continue
		}
		if err := p.runSource(ctx, src, &res); err != nil {
			return res, err
		}
	}
	if err := p.st.Save(); err != nil {
		return res, err
	}
	return res, nil
}

func (p *Pipeline) runSource(ctx context.Context, src scraper.Source, res *Result) error {
	cands, err := src.Discover(ctx)
	if err != nil {
		return fmt.Errorf("pipeline: %s: discover: %w", src.ID(), err)
	}
	res.Discovered += len(cands)

	known := p.st.Known()
	toEnrich := cands[:0:0]
	for _, c := range cands {
		if c.URL == "" {
			continue
		}
		if !known[c.URL] {
			toEnrich = append(toEnrich, c)
		}
	}

	items, failed, firstErr := p.enrichAll(ctx, src, toEnrich)
	res.Failed += failed
	if res.FirstErr == nil {
		res.FirstErr = firstErr
	}

	added := p.st.Upsert(items)
	res.Added += added
	if len(items) > 0 || failed == 0 {
		if err := p.st.Save(); err != nil {
			return fmt.Errorf("pipeline: %s: save: %w", src.ID(), err)
		}
	}

	// Enforce the store retention cap (0 = keep everything).
	if max := p.cfg.MaxItems; max > 0 {
		if pruned := p.st.Prune(src.ID(), max); pruned > 0 {
			res.Pruned += pruned
			if err := p.st.Save(); err != nil {
				return fmt.Errorf("pipeline: %s: save after prune: %w", src.ID(), err)
			}
		}
	}

	written, err := p.renderFeeds(ctx)
	if err != nil {
		return err
	}
	res.FeedsWritten += written
	return nil
}

// enrichAll runs Enrich across a fixed worker pool, tolerating per-item
// failures (a flaky article should not abort the whole run).
func (p *Pipeline) enrichAll(ctx context.Context, src scraper.Source, cands []scraper.Candidate) ([]model.Item, int, error) {
	n := p.cfg.Concurrency
	if n < 1 {
		n = 1
	}
	if len(cands) == 0 {
		return nil, 0, nil
	}
	if n > len(cands) {
		n = len(cands)
	}

	work := make(chan scraper.Candidate)
	results := make(chan model.Item, len(cands))
	var wg sync.WaitGroup
	var mu sync.Mutex
	var failed int
	var firstErr error

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for c := range work {
				it, err := src.Enrich(ctx, c)
				if err != nil {
					mu.Lock()
					failed++
					if firstErr == nil {
						firstErr = err
					}
					mu.Unlock()
					continue
				}
				it.SourceID = src.ID()
				results <- it
			}
		}()
	}

	for _, c := range cands {
		work <- c
	}
	close(work)
	go func() {
		wg.Wait()
		close(results)
	}()

	items := make([]model.Item, 0, len(cands))
	for it := range results {
		items = append(items, it)
	}
	return items, failed, firstErr
}

// renderFeeds writes per-source feed files plus a default copy for the first
// enabled source.
func (p *Pipeline) renderFeeds(ctx context.Context) (int, error) {
	_ = ctx // feeds render from local state; no I/O beyond disk
	written := 0
	for _, id := range p.cfg.Sources {
		items := p.st.Latest(id, p.cfg.FeedLength)
		title := p.sources[id].Label()
		rendered, err := feed.Render(id, title, p.cfg.BaseURL, items)
		if err != nil {
			return written, fmt.Errorf("pipeline: render %s: %w", id, err)
		}
		if err := writeFile(p.filepath(id, "xml"), rendered.RSS); err != nil {
			return written, err
		}
		if err := writeFile(p.filepath(id, "atom"), rendered.Atom); err != nil {
			return written, err
		}
		written++
	}
	return written, nil
}

// filepath returns the rendered feed path for a source and format extension.
func (p *Pipeline) filepath(sourceID, ext string) string {
	return filepath.Join(p.cfg.DataDir, "feed-"+sourceID+"."+ext)
}

// writeFile atomically writes a feed document.
func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("pipeline: mkdir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "feed.*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}
