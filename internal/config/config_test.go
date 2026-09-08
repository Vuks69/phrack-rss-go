package config

import "testing"

func TestFromEnvDefaults(t *testing.T) {
	cfg := FromEnv()
	if cfg.MaxItems != 0 {
		t.Fatalf("default MaxItems should be 0 (keep all), got %d", cfg.MaxItems)
	}
	if cfg.Port != DefaultPort {
		t.Fatalf("default port should be %d, got %d", DefaultPort, cfg.Port)
	}
}

func TestFromEnvMaxItems(t *testing.T) {
	t.Setenv("PHRACK_RSS_MAX_ITEMS", "50")
	if cfg := FromEnv(); cfg.MaxItems != 50 {
		t.Fatalf("expected MaxItems 50, got %d", cfg.MaxItems)
	}

	t.Setenv("PHRACK_RSS_MAX_ITEMS", "0")
	if cfg := FromEnv(); cfg.MaxItems != 0 {
		t.Fatalf("expected MaxItems 0, got %d", cfg.MaxItems)
	}

	t.Setenv("PHRACK_RSS_MAX_ITEMS", "-5")
	if cfg := FromEnv(); cfg.MaxItems != 0 {
		t.Fatalf("negative env should be ignored (keep 0), got %d", cfg.MaxItems)
	}
}
