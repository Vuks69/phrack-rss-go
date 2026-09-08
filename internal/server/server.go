// Package server serves the rendered feed files over HTTP, bound to loopback
// only. The pipeline writes <source>.xml / <source>.atom into the data
// directory; this package just streams them out, mapping /feed.xml to the
// first enabled source and /feeds/<source>.xml to any other source.
package server

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Vuks69/phrack-rss-go/internal/config"
)

const retryAfter = "300"

// sourceNameRe constrains feed file names to avoid path traversal via the
// /feeds/ route (<source> may only be [A-Za-z0-9_-]+, so it can never escape
// the data directory).
var sourceNameRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// Server wires the configured data directory to a set of routes.
type Server struct {
	cfg config.Config
}

// New builds a Server from configuration.
func New(cfg config.Config) *Server {
	return &Server{cfg: cfg}
}

// Handler returns the routed handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/feed.xml", s.handleDefaultFeed("xml"))
	mux.HandleFunc("/feed.atom", s.handleDefaultFeed("atom"))
	mux.HandleFunc("/feeds/", s.handleFeed)
	mux.HandleFunc("/", s.handleIndex)
	return mux
}

// defaultSource is the feed served at the bare /feed.xml path.
func (s *Server) defaultSource() string {
	if len(s.cfg.Sources) > 0 {
		return s.cfg.Sources[0]
	}
	return ""
}

func (s *Server) handleDefaultFeed(ext string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		src := s.defaultSource()
		if src == "" {
			http.NotFound(w, r)
			return
		}
		s.serveFeed(w, r, src, ext)
	}
}

// handleFeed serves /feeds/<source>.<ext>.
func (s *Server) handleFeed(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/feeds/")
	name, ext, ok := strings.Cut(rest, ".")
	if !ok || name == "" || !sourceNameRe.MatchString(name) {
		http.NotFound(w, r)
		return
	}
	switch ext {
	case "xml", "atom":
		s.serveFeed(w, r, name, ext)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) serveFeed(w http.ResponseWriter, _ *http.Request, sourceID, ext string) {
	path := filepath.Join(s.cfg.DataDir, sourceID+"."+ext)
	// #nosec G703 G304 -- sourceID always passes sourceNameRe (see handleFeed /
	// defaultSource), so the joined path cannot escape the data directory.
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Feed not generated yet (first scrape still running or failed):
			// tell clients to retry later rather than caching a 404.
			w.Header().Set("Retry-After", retryAfter)
			http.Error(w, "feed not ready", http.StatusServiceUnavailable)
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	contentType := "application/xml; charset=utf-8"
	if ext == "atom" {
		contentType = "application/atom+xml; charset=utf-8"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-cache")
	// #nosec G705 -- serving externally-authored feed content is the point;
	// consumers are feed readers, and the Content-Type is application/xml.
	_, _ = w.Write(data)
}

func (s *Server) handleIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	for _, src := range s.cfg.Sources {
		if _, err := os.Stat(filepath.Join(s.cfg.DataDir, src+".xml")); err == nil {
			_, _ = fmt.Fprintf(w, "/feeds/%s.xml\n", src)
		}
	}
}
