package providers

import (
	"testing"

	"github.com/Vuks69/phrack-rss-go/internal/config"
)

func TestBuildPhrack(t *testing.T) {
	cfg := config.Default()
	sources, client, err := Build(cfg)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	src, ok := sources["phrack"]
	if !ok {
		t.Fatal("expected phrack source")
	}
	if src.ID() != "phrack" {
		t.Fatalf("got id %q", src.ID())
	}
	if client == nil {
		t.Fatal("expected shared http client")
	}
}

func TestBuildUnknownSource(t *testing.T) {
	cfg := config.Default()
	cfg.Sources = []string{"does-not-exist"}
	if _, _, err := Build(cfg); err == nil {
		t.Fatal("expected error for unknown source")
	}
}

func TestKnown(t *testing.T) {
	if !Known("phrack") {
		t.Error("phrack should be registered")
	}
}
