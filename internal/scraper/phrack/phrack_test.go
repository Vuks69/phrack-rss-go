package phrack

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Vuks69/phrack-rss-go/internal/config"
	"github.com/Vuks69/phrack-rss-go/internal/scraper"
)

func candidate(id, title, base string, issueDate string) scraper.Candidate {
	var t time.Time
	if issueDate != "" {
		t, _ = time.Parse("2006-01-02", issueDate)
	}
	return scraper.Candidate{
		URL:       fmt.Sprintf("%s/issues/%s/%s.html", base, id, slug(title)),
		Title:     title,
		IssueDate: t,
	}
}

func slug(title string) string {
	return strings.ToLower(strings.ReplaceAll(title, " ", "-"))
}

// fixtureIndex mirrors the index_latest.html structure seen on the live site.
func fixtureIndex() string {
	return `<html><body>
<a href="/issues/72/introduction_md.html">Issue 72</a>
<a href="/issues/1/introduction.html">Issue 1</a>
<a href="/issues/28/toc3.html">Issue 28</a>
</body></html>`
}

// fixtureTOC mirrors an issue TOC page: a table of article rows plus a
// JSON-LD block carrying the issue-level publication date.
func fixtureTOC(issue string, date string, rows [][2]string) string {
	b := strings.Builder{}
	b.WriteString("<html><body>\n")
	b.WriteString(`<script type="application/ld+json">{"@type":"Article","headline":"Introduction","datePublished":"` + date + `"}</script>` + "\n")
	b.WriteString(`<table class="tissue">` + "\n")
	for _, r := range rows {
		fmt.Fprintf(&b, `<tr><td align="left"><a href="/issues/%s/%s.html#article">%s</a></td><td align="right">%s</td></tr>`+"\n",
			issue, slug(r[0]), r[0], r[1])
	}
	b.WriteString("</table>\n</body></html>")
	return b.String()
}

// fixtureArticle mirrors an article page carrying per-article JSON-LD.
func fixtureArticle(base, issue, title, author, date string) string {
	return `<html><body>
<script type="application/ld+json">{"@context":"https://schema.org","@type":"Article","headline":"` + title +
		`","url":"` + base + `/issues/` + issue + `/` + slug(title) + `.html"` +
		`,"author":{"@type":"Person","name":"` + author + `"},"datePublished":"` + date + `"}</script>
</body></html>`
}

// newTestSource spins up an httptest server serving the given paths.
func newTestSource(t *testing.T, paths map[string]string) *Phrack {
	t.Helper()
	mux := http.NewServeMux()
	for p, body := range paths {
		body := body
		mux.HandleFunc(p, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprint(w, body)
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	cfg := config.Default()
	cfg.BaseURL = srv.URL
	cfg.MinGap = 0
	src, _, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return src.(*Phrack)
}

func TestDiscoverEnumeratesAllIssuesDeduped(t *testing.T) {
	src := newTestSource(t, map[string]string{
		"/index_latest.html":              fixtureIndex(),
		"/issues/1/introduction.html":     fixtureTOC("1", "1996-01-01", [][2]string{{"Hacking SAM", "Unknown"}, {"Boot Tracing", "Phrack Staff"}}),
		"/issues/28/toc3.html":            fixtureTOC("28", "2004-03-01", [][2]string{{"Adventures", "Matt"}}),
		"/issues/72/introduction_md.html": fixtureTOC("72", "2025-08-19", [][2]string{{"A CPU Backdoor", "uty"}, {"Linenoise", "Phrack Staff"}}),
	})

	cands, err := src.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(cands) != 5 {
		t.Fatalf("expected 5 candidates, got %d: %+v", len(cands), cands)
	}
	urls := map[string]bool{}
	for _, c := range cands {
		if urls[c.URL] {
			t.Fatalf("duplicate candidate %q", c.URL)
		}
		urls[c.URL] = true
		if c.IssueDate.IsZero() {
			t.Fatalf("expected issue date on %q", c.URL)
		}
	}
	var sawSAM bool
	for _, c := range cands {
		if c.Title == "Hacking SAM" {
			sawSAM = true
			if c.Author != "Unknown" {
				t.Fatalf("expected row author, got %q", c.Author)
			}
		}
	}
	if !sawSAM {
		t.Fatal("missing candidate from issue 1")
	}
}

func TestDiscoverDoesNotListArticleLinksTwice(t *testing.T) {
	// A TOC page where one article link appears twice must produce a single
	// candidate.
	toc := `<html><body>` + fixtureTOC("72", "2025-08-19", [][2]string{{"Linenoise", "Phrack Staff"}}) + `</body></html>`
	src := newTestSource(t, map[string]string{
		"/index_latest.html":              `<a href="/issues/72/introduction_md.html">72</a>`,
		"/issues/72/introduction_md.html": toc,
	})
	cands, err := src.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(cands) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(cands))
	}
}

func TestDiscoverSortsIssueNumbersNumerically(t *testing.T) {
	src := newTestSource(t, map[string]string{
		"/index_latest.html": `<a href="/issues/10/a.html">10</a><a href="/issues/9/b.html">9</a><a href="/issues/1/c.html">1</a>`,
		"/issues/10/a.html":  fixtureTOC("10", "2000-01-01", [][2]string{{"Ten", "A"}}),
		"/issues/9/b.html":   fixtureTOC("9", "1999-01-01", [][2]string{{"Nine", "B"}}),
		"/issues/1/c.html":   fixtureTOC("1", "1996-01-01", [][2]string{{"One", "C"}}),
	})
	cands, err := src.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	got := candsTitles(cands)
	want := []string{"One", "Nine", "Ten"} // ascending issue order, not lexicographic
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected order %v, got %v", want, got)
		}
	}
}

func candsTitles(cands []scraper.Candidate) []string {
	out := make([]string, len(cands))
	for i, c := range cands {
		out[i] = c.Title
	}
	return out
}

func TestEnrichOverridesFromJSONLD(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, fixtureArticle("https://phrack.org", "72", "A CPU Backdoor", "uty", "2025-08-19T12:00:00Z"))
	}))
	t.Cleanup(srv.Close)

	src := newFromURL(t, srv.URL)
	item, err := src.Enrich(context.Background(), candidate("72", "Stale Title", srv.URL, "2020-01-01"))
	if err != nil {
		t.Fatalf("Enrich: %v", err)
	}
	if item.Title != "A CPU Backdoor" {
		t.Errorf("expected JSON-LD headline, got %q", item.Title)
	}
	if item.Author != "uty" {
		t.Errorf("expected JSON-LD author, got %q", item.Author)
	}
	if item.IssuedAt.Format(time.RFC3339) != "2025-08-19T12:00:00Z" {
		t.Errorf("expected parsed datePublished, got %v", item.IssuedAt)
	}
	if !strings.Contains(item.URL, "/issues/72/a-cpu-backdoor.html") {
		t.Errorf("expected canonical URL, got %q", item.URL)
	}
}

func TestEnrichFallsBackWhenNoJSONLD(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, "<html><body><h1>old article</h1></body></html>")
	}))
	t.Cleanup(srv.Close)

	src := newFromURL(t, srv.URL)
	c := candidate("1", "Old Paper", srv.URL, "1996-06-07")
	item, err := src.Enrich(context.Background(), c)
	if err != nil {
		t.Fatalf("Enrich: %v", err)
	}
	if item.Title != c.Title || item.Author != c.Author {
		t.Errorf("expected candidate fallback title/author, got %+v", item)
	}
	if item.IssuedAt.Format("2006-01-02") != "1996-06-07" {
		t.Errorf("expected issue-date fallback, got %v", item.IssuedAt)
	}
}

func TestFetchBytesRetriesOn5xx(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		if hits < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = fmt.Fprint(w, "ok")
	}))
	t.Cleanup(srv.Close)

	src := newFromURL(t, srv.URL)
	body, err := src.fetchBytes(context.Background(), srv.URL+"/retry")
	if err != nil {
		t.Fatalf("expected eventual success, got %v after %d hits", err, hits)
	}
	if string(body) != "ok" {
		t.Fatalf("unexpected body %q", body)
	}
}

func TestFetchBytesStopsOn404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	src := newFromURL(t, srv.URL)
	if _, err := src.fetchBytes(context.Background(), srv.URL+"/missing"); err == nil {
		t.Fatal("expected error on 404")
	}
}

func TestParseDate(t *testing.T) {
	for _, in := range []string{"2025-08-19", "2025-08-19T12:00:00Z", " 1996-01-01 "} {
		ts, ok := parseDate(in)
		if !ok || ts.IsZero() {
			t.Errorf("parseDate(%q) failed", in)
		}
	}
	if _, ok := parseDate("not-a-date"); ok {
		t.Error("parseDate accepted garbage")
	}
}

func TestThrottleSerializesConcurrentCallers(t *testing.T) {
	const gap = 50 * time.Millisecond
	// Wall-clock spacing between returns includes timer+scheduler jitter, so
	// assert with a tolerance: the bug (back-to-back bursts) leaves spacing at
	// ~0, orders of magnitude below the gap.
	const tolerance = 5 * time.Millisecond
	p := &Phrack{gap: gap}
	const n = 8
	ctx := context.Background()
	start := make(chan struct{})
	times := make([]time.Time, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			if err := p.throttle(ctx); err != nil {
				t.Errorf("throttle: %v", err)
			}
			times[i] = time.Now()
		}(i)
	}
	close(start)
	wg.Wait()

	sort.Slice(times, func(i, j int) bool { return times[i].Before(times[j]) })
	for i := 1; i < len(times); i++ {
		if d := times[i].Sub(times[i-1]); d < gap-tolerance {
			t.Fatalf("successive approvals %v apart: < gap %v - tol", d, gap)
		}
	}
}

func newFromURL(t *testing.T, base string) *Phrack {
	t.Helper()
	cfg := config.Default()
	cfg.BaseURL = base
	cfg.MinGap = 0
	src, _, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return src.(*Phrack)
}
