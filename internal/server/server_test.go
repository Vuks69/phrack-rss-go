package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Vuks69/phrack-rss-go/internal/config"
)

func newTestServer(t *testing.T, files map[string]string) *httptest.Server {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Default()
	cfg.DataDir = dir
	cfg.Sources = []string{"phrack", "other"}
	return httptest.NewServer(New(cfg).Handler())
}

func get(t *testing.T, srv *httptest.Server, path string) (*http.Response, string) {
	t.Helper()
	resp, err := srv.Client().Get(srv.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	b := make([]byte, 65536)
	n, _ := resp.Body.Read(b)
	return resp, string(b[:n])
}

func TestDefaultFeedRoutes(t *testing.T) {
	srv := newTestServer(t, map[string]string{
		"phrack.xml":  "<rss>x</rss>",
		"phrack.atom": "<feed>a</feed>",
	})
	t.Cleanup(srv.Close)

	for path, wantContentType := range map[string]string{
		"/feed.xml":  "application/xml",
		"/feed.atom": "application/atom+xml",
	} {
		resp, body := get(t, srv, path)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: got %d", path, resp.StatusCode)
		}
		if !strings.HasPrefix(resp.Header.Get("Content-Type"), wantContentType) {
			t.Fatalf("%s: content type %q", path, resp.Header.Get("Content-Type"))
		}
		if body == "" {
			t.Fatalf("%s: empty body", path)
		}
	}
}

func TestNamedFeedRoutes(t *testing.T) {
	srv := newTestServer(t, map[string]string{"other.xml": "<rss>other</rss>"})
	t.Cleanup(srv.Close)

	if resp, body := get(t, srv, "/feeds/other.xml"); resp.StatusCode != http.StatusOK || !strings.Contains(body, "other") {
		t.Fatalf("named feed: got %d %q", resp.StatusCode, body)
	}
	if resp, _ := get(t, srv, "/feeds/other.atom"); resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("missing atom should be 503, got %d", resp.StatusCode)
	}
	if resp, _ := get(t, srv, "/feeds/unknown.xml"); resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("unknown feed should be 503, got %d", resp.StatusCode)
	}
}

func TestFeedReadyRetryAfter(t *testing.T) {
	srv := newTestServer(t, map[string]string{})
	t.Cleanup(srv.Close)
	resp, _ := get(t, srv, "/feed.xml")
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", resp.StatusCode)
	}
	if resp.Header.Get("Retry-After") == "" {
		t.Error("expected Retry-After header")
	}
}

func TestTraversalRejected(t *testing.T) {
	srv := newTestServer(t, map[string]string{"phrack.xml": "<rss>x</rss>"})
	t.Cleanup(srv.Close)
	for _, path := range []string{
		"/feeds/..%2f..%2fetc.xml", // encoded traversal
		"/feeds/..%2Fdata.json.xml",
		"/feeds/secret.xml/", // trailing slash
	} {
		resp, _ := get(t, srv, path)
		if resp.StatusCode == http.StatusOK {
			t.Fatalf("%s: traversal must not succeed", path)
		}
	}
}

func TestIndexListsUsableFeedLinks(t *testing.T) {
	srv := newTestServer(t, map[string]string{
		"phrack.xml": "<rss>x</rss>",
		"other.xml":  "<rss>o</rss>",
	})
	t.Cleanup(srv.Close)

	resp, body := get(t, srv, "/")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("index: got %d", resp.StatusCode)
	}
	for _, path := range []string{"/feeds/phrack.xml", "/feeds/other.xml"} {
		if !strings.Contains(body, path) {
			t.Errorf("index missing %s", path)
		}
	}
	if strings.Contains(body, "/feed-phrack.xml") || strings.Contains(body, "/feed-other.xml") {
		t.Error("index links to /feed-<source>.xml which is not a route")
	}
}

func TestHealthz(t *testing.T) {
	srv := newTestServer(t, map[string]string{})
	t.Cleanup(srv.Close)
	if resp, _ := get(t, srv, "/healthz"); resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz: got %d", resp.StatusCode)
	}
}
