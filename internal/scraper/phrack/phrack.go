// Package phrack implements scraper.Source for the live Phrack site
// (https://phrack.org/).
//
// Scrape flow (verified against the site on 2026-09-08):
//
//  1. Discover: fetch /index_latest.html, read every link of the form
//     "/issues/N/<toc>.html", dedupe by issue number, then fetch each issue's
//     TOC page and read its article rows (links ending in "#article").
//  2. Enrich: fetch each article page and decode its JSON-LD block for the
//     authoritative title, author and datePublished.
//
// HTTP requests are throttled to a politeness gap and retried with a growing
// backoff on 429/5xx responses.
package phrack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/Vuks69/phrack-rss-go/internal/config"
	"github.com/Vuks69/phrack-rss-go/internal/model"
	"github.com/Vuks69/phrack-rss-go/internal/scraper"
)

const (
	sourceID    = "phrack"
	sourceLabel = "Phrack Magazine"
	indexPath   = "/index_latest.html"

	maxAttempts = 4
	backoffBase = 500 * time.Millisecond
)

var retryStatusCodes = map[int]bool{429: true, 500: true, 502: true, 503: true, 504: true}

var issueLinkRe = regexp.MustCompile(`^/issues/(\d+)/(.+)$`)

// Phrack scrapes the Phrack site.
type Phrack struct {
	baseURL   string
	client    *http.Client
	userAgent string

	mu   sync.Mutex
	last time.Time
	gap  time.Duration
}

// New builds Phrack and its HTTP client from the shared config.
func New(cfg config.Config) (scraper.Source, *http.Client, error) {
	client := &http.Client{Timeout: cfg.HTTPTimeout}
	return &Phrack{
		baseURL:   cfg.BaseURL,
		client:    client,
		userAgent: cfg.UserAgent,
		gap:       cfg.MinGap,
	}, client, nil
}

// ID implements scraper.Source.
func (p *Phrack) ID() string { return sourceID }

// Label implements scraper.Source.
func (p *Phrack) Label() string { return sourceLabel }

// Discover implements scraper.Source: index_latest -> issue TOC pages.
func (p *Phrack) Discover(ctx context.Context) ([]scraper.Candidate, error) {
	doc, err := p.fetchDoc(ctx, p.abs(indexPath))
	if err != nil {
		return nil, fmt.Errorf("phrack: index: %w", err)
	}

	tocs := map[string]string{} // issue number -> TOC page path
	doc.Find("a[href]").Each(func(_ int, s *goquery.Selection) {
		href, ok := s.Attr("href")
		if !ok {
			return
		}
		if m := issueLinkRe.FindStringSubmatch(href); m != nil {
			if _, dup := tocs[m[1]]; !dup {
				tocs[m[1]] = href
			}
		}
	})

	issues := sortedIssueNumbers(tocs)
	var candidates []scraper.Candidate
	seen := map[string]bool{}
	for _, n := range issues {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		tocDoc, err := p.fetchDoc(ctx, p.abs(tocs[n]))
		if err != nil {
			return nil, fmt.Errorf("phrack: issue %s TOC: %w", n, err)
		}
		issueDate := parseJSONLDAuthor(tocDoc) // fallback publication date for the issue
		tocDoc.Find(`a[href$="#article"]`).Each(func(_ int, a *goquery.Selection) {
			href, ok := a.Attr("href")
			if !ok {
				return
			}
			link := canonicalize(p.abs(strings.TrimSuffix(href, "#article")))
			if seen[link] {
				return
			}
			seen[link] = true
			c := scraper.Candidate{
				URL:       link,
				Title:     strings.TrimSpace(a.Text()),
				IssueDate: issueDate,
			}
			// Author sits in the trailing <td> of the enclosing row, when a
			// table row exists (the site renders TOCs as tables).
			if row := a.Closest("tr"); row.Length() > 0 {
				if tds := row.Children(); tds.Length() > 1 {
					c.Author = strings.TrimSpace(tds.Eq(1).Text())
				}
			}
			candidates = append(candidates, c)
		})
	}
	return candidates, nil
}

// Enrich implements scraper.Source: fetch the article page and decode JSON-LD.
func (p *Phrack) Enrich(ctx context.Context, c scraper.Candidate) (model.Item, error) {
	doc, err := p.fetchDoc(ctx, c.URL)
	if err != nil {
		return model.Item{}, fmt.Errorf("phrack: article %s: %w", c.URL, err)
	}
	item := model.Item{
		Title:    c.Title,
		URL:      c.URL,
		Author:   c.Author,
		IssuedAt: c.IssueDate,
		SourceID: sourceID,
	}
	if ld, ok := firstArticleJSONLD(doc); ok {
		if ld.Headline != "" {
			item.Title = strings.TrimSpace(ld.Headline)
		}
		if ld.URL != "" {
			item.URL = canonicalize(p.abs(ld.URL))
		}
		if name := strings.TrimSpace(ld.Author.Name); name != "" {
			item.Author = name
		}
		if t, ok := parseDate(ld.DatePublished); ok {
			item.IssuedAt = t
		}
	}
	return item, nil
}

// ---------------------------------------------------------------------------
// HTTP helpers

// fetchDoc fetches a URL and parses it as HTML.
func (p *Phrack) fetchDoc(ctx context.Context, rawURL string) (*goquery.Document, error) {
	body, err := p.fetchBytes(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", rawURL, err)
	}
	return doc, nil
}

// fetchBytes fetches rawURL with politeness throttling and retry/backoff.
func (p *Phrack) fetchBytes(ctx context.Context, rawURL string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if err := p.throttle(ctx); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", p.userAgent)
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

		resp, err := p.client.Do(req)
		switch {
		case err != nil:
			lastErr = err
		case resp.StatusCode == http.StatusOK:
			body, readErr := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if readErr != nil {
				lastErr = readErr
				break
			}
			return body, nil
		default:
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, rawURL)
			if !retryStatusCodes[resp.StatusCode] {
				return nil, lastErr
			}
		}

		select {
		case <-time.After(backoff(attempt)):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return nil, fmt.Errorf("giving up after %d attempts: %w", maxAttempts, lastErr)
}

// throttle enforces the minimum gap between successive outbound requests.
//
// p.last is reserved atomically: the current slot is claimed (and advanced to
// the next-earliest legal time) while holding the mutex, so concurrent callers
// cannot compute the same wait and fire back-to-back. The actual sleep happens
// outside the lock so it stays cancellable.
func (p *Phrack) throttle(ctx context.Context) error {
	if p.gap <= 0 {
		return nil
	}
	p.mu.Lock()
	slots := p.last.Add(p.gap)
	now := time.Now()
	var wait time.Duration
	if slots.After(now) {
		wait = slots.Sub(now)
	} else {
		slots = now
	}
	p.last = slots
	p.mu.Unlock()
	if wait > 0 {
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func backoff(attempt int) time.Duration {
	if attempt == 0 {
		return 0
	}
	n := backoffBase
	for i := 1; i < attempt; i++ {
		n *= 2
	}
	// #nosec G404 -- jitter for retry spacing is timing material, not a secret.
	return n + time.Duration(rand.Int63n(int64(n/2)+1))
}

// ---------------------------------------------------------------------------
// URL helpers

func (p *Phrack) abs(href string) string {
	if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
		return href
	}
	return strings.TrimRight(p.baseURL, "/") + "/" + strings.TrimLeft(href, "/")
}

// canonicalize strips any URL fragment so items dedupe on the clean URL.
func canonicalize(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return strings.TrimSuffix(raw, "#article")
	}
	u.Fragment = ""
	return u.String()
}

// ---------------------------------------------------------------------------
// JSON-LD helpers

type jsonLD struct {
	Type          string   `json:"@type"`
	Headline      string   `json:"headline"`
	URL           string   `json:"url"`
	Author        jsonLDAt `json:"author"`
	DatePublished string   `json:"datePublished"`
}

type jsonLDAt struct {
	Name string `json:"name"`
}

// firstArticleJSONLD returns the first JSON-LD block tagged as an Article
// (i.e. the structured metadata site authors maintain).
func firstArticleJSONLD(doc *goquery.Document) (jsonLD, bool) {
	var result jsonLD
	found := false
	doc.Find(`script[type="application/ld+json"]`).EachWithBreak(func(_ int, s *goquery.Selection) bool {
		var candidate jsonLD
		if err := json.Unmarshal([]byte(s.Text()), &candidate); err != nil {
			return true // try the next block
		}
		if candidate.Type == "Article" && candidate.Headline != "" {
			result = candidate
			found = true
			return false
		}
		return true
	})
	return result, found
}

// parseJSONLDAuthor extracts the publication date from a page carrying an
// article-level JSON-LD block (used as the per-issue fallback date).
func parseJSONLDAuthor(doc *goquery.Document) time.Time {
	if ld, ok := firstArticleJSONLD(doc); ok {
		if t, ok := parseDate(ld.DatePublished); ok {
			return t
		}
	}
	return time.Time{}
}

// parseDate accepts RFC3339 or the bare "2006-01-02" format used by Phrack.
func parseDate(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// sortedIssueNumbers returns the map keys numerically sorted (earliest issue
// first), so TOCs are fetched in a predictable order.
func sortedIssueNumbers(tocs map[string]string) []string {
	keys := make([]string, 0, len(tocs))
	for k := range tocs {
		keys = append(keys, k)
	}
	// Numeric-ish sort: issue numbers are small, plain integer strings.
	return quickSortNums(keys)
}

func quickSortNums(keys []string) []string {
	// Insertion sort is plenty for ~72 elements.
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && lessNum(keys[j], keys[j-1]); j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

func lessNum(a, b string) bool {
	if len(a) == len(b) {
		return a < b
	}
	return len(a) < len(b)
}
