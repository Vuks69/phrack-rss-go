package store

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Vuks69/phrack-rss-go/internal/model"
)

func newItem(title, url string, t time.Time) model.Item {
	it := model.Item{Title: title, URL: url, IssuedAt: t, SourceID: "phrack"}
	it.Normalize()
	return it
}

func TestOpenCreatesEmptyStore(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if n := s.Count(); n != 0 {
		t.Fatalf("expected empty store, got %d", n)
	}
}

func TestUpsertDedupesByURL(t *testing.T) {
	s, _ := Open(t.TempDir())
	t0 := time.Now().Add(-2 * time.Hour)
	added := s.Upsert([]model.Item{
		newItem("a", "https://x.example/a", t0),
		newItem("a-v2", "https://x.example/a", t0.Add(time.Hour)), // same URL, newer title
	})
	if added != 1 {
		t.Fatalf("expected 1 added, got %d", added)
	}
	if n := s.Count(); n != 1 {
		t.Fatalf("expected 1 stored, got %d", n)
	}
	latest := s.Latest("phrack", 0)
	if len(latest) != 1 || latest[0].Title != "a-v2" {
		t.Fatalf("expected newer title, got %+v", latest)
	}
}

func TestLatestCapsAndSorts(t *testing.T) {
	s, _ := Open(t.TempDir())
	base := time.Now().Add(-time.Hour)
	items := []model.Item{
		newItem("old", "https://x.example/1", base),
		newItem("mid", "https://x.example/2", base.Add(time.Hour)),
		newItem("new", "https://x.example/3", base.Add(2*time.Hour)),
	}
	s.Upsert(items)
	latest := s.Latest("phrack", 2)
	if len(latest) != 2 {
		t.Fatalf("expected 2 latest, got %d", len(latest))
	}
	if latest[0].Title != "new" || latest[1].Title != "mid" {
		t.Fatalf("expected newest-first, got %+v", latest)
	}
}

func TestSaveAndReloadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir)
	s.Upsert([]model.Item{newItem("x", "https://x.example/x", time.Now())})
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	s2, err := Open(dir)
	if err != nil {
		t.Fatalf("Open after save: %v", err)
	}
	if n := s2.Count(); n != 1 {
		t.Fatalf("expected reloaded store to have 1 item, got %d", n)
	}
}

func TestSaveIgnoresUnusable(t *testing.T) {
	s, _ := Open(t.TempDir())
	if added := s.Upsert([]model.Item{{Title: "no url", IssuedAt: time.Now()}}); added != 0 {
		t.Fatalf("expected 0 added for unusable, got %d", added)
	}
}

func TestPruneKeepsNewest(t *testing.T) {
	s, _ := Open(t.TempDir())
	base := time.Now().Add(-10 * time.Hour)
	var items []model.Item
	for i := 0; i < 5; i++ {
		items = append(items, newItem(
			fmt.Sprintf("item-%d", i),
			fmt.Sprintf("https://x.example/%d", i),
			base.Add(time.Duration(i)*time.Hour),
		))
	}
	s.Upsert(items)

	if n := s.Prune("phrack", 2); n != 3 {
		t.Fatalf("expected 3 pruned, got %d", n)
	}
	latest := s.Latest("phrack", 0)
	if len(latest) != 2 {
		t.Fatalf("expected 2 stored, got %d", len(latest))
	}
	if latest[0].Title != "item-4" || latest[1].Title != "item-3" {
		t.Fatalf("expected newest retained, got %+v", latest)
	}
}

func TestPruneZeroKeepsEverything(t *testing.T) {
	s, _ := Open(t.TempDir())
	var items []model.Item
	for i := 0; i < 3; i++ {
		items = append(items, newItem(
			fmt.Sprintf("item-%d", i),
			fmt.Sprintf("https://x.example/%d", i),
			time.Now().Add(-time.Duration(i)*time.Hour),
		))
	}
	s.Upsert(items)
	if n := s.Prune("phrack", 0); n != 0 {
		t.Fatalf("max 0 should be a no-op, pruned %d", n)
	}
	if n := s.Prune("phrack", -1); n != 0 {
		t.Fatalf("negative max should be a no-op, pruned %d", n)
	}
	if s.Count() != 3 {
		t.Fatalf("expected 3 stored, got %d", s.Count())
	}
}

func TestPruneOnlyAffectsSource(t *testing.T) {
	s, _ := Open(t.TempDir())
	s.Upsert([]model.Item{
		newItem("phrack-1", "https://p.example/1", time.Now()),
		{Title: "other-1", URL: "https://o.example/1", IssuedAt: time.Now(), SourceID: "other"},
	})
	if n := s.Prune("other", 1); n != 0 {
		t.Fatalf("expected 0 pruned for source within cap, got %d", n)
	}
	if s.Count() != 2 {
		t.Fatalf("expected 2 stored, got %d", s.Count())
	}
}

func TestCorruptFileFailsOpen(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "data.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir); err == nil {
		t.Fatal("expected error opening corrupt store")
	}
}
