package feed

import (
	"strings"
	"testing"
	"time"

	"github.com/Vuks69/phrack-rss-go/internal/model"
)

func TestRenderProducesValidRssAndAtom(t *testing.T) {
	items := []model.Item{
		{Title: "First", URL: "https://phrack.org/issues/72/a.html", Author: "uty", IssuedAt: time.Now().Add(-time.Hour)},
		{Title: "Second", URL: "https://phrack.org/issues/72/b.html", Author: "mr_me", IssuedAt: time.Now()},
	}
	r, err := Render("phrack", "Phrack Magazine", "https://phrack.org", items)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if len(r.RSS) == 0 || len(r.Atom) == 0 {
		t.Fatal("expected both documents")
	}
	if !strings.Contains(string(r.RSS), "<rss") {
		t.Error("RSS missing <rss root")
	}
	if !strings.Contains(string(r.Atom), "<feed") {
		t.Error("Atom missing <feed root")
	}
	for _, want := range []string{"First", "Second", "https://phrack.org/issues/72/a.html"} {
		if !strings.Contains(string(r.RSS), want) {
			t.Errorf("RSS missing %q", want)
		}
	}
	for _, want := range []string{"First", "https://phrack.org/issues/72/b.html"} {
		if !strings.Contains(string(r.Atom), want) {
			t.Errorf("Atom missing %q", want)
		}
	}
}

func TestRenderSortsNewestFirst(t *testing.T) {
	items := []model.Item{
		{Title: "older", URL: "https://x.example/1", IssuedAt: time.Now().Add(-2 * time.Hour)},
		{Title: "newer", URL: "https://x.example/2", IssuedAt: time.Now()},
	}
	r, _ := Render("phrack", "t", "https://x.example", items)
	posNewer := strings.Index(string(r.RSS), "newer")
	posOlder := strings.Index(string(r.RSS), "older")
	if posNewer == -1 || posOlder == -1 || posNewer > posOlder {
		t.Fatalf("expected newer before older (newer=%d older=%d)", posNewer, posOlder)
	}
}
