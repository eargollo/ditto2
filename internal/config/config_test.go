package config_test

import (
	"os"
	"testing"

	"github.com/eargollo/ditto/internal/config"
)

func TestLoad_DefaultsApplied(t *testing.T) {
	f, err := os.CreateTemp("", "ditto-config-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	if _, err := f.WriteString("scan_paths:\n  - /tmp/test\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	cfg, err := config.Load(f.Name())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Schedule == "" {
		t.Error("expected default schedule to be set")
	}
	if cfg.HTTPAddr == "" {
		t.Error("expected default http_addr to be set")
	}
}

func TestLoad_MissingFile(t *testing.T) {
	// A missing config file is not an error — Load returns defaults so the
	// server can start without a mounted config file (bare Docker run).
	cfg, err := config.Load("/nonexistent/path/config.yaml")
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	if cfg.HTTPAddr == "" {
		t.Error("expected default http_addr to be set")
	}
	if cfg.Schedule == "" {
		t.Error("expected default schedule to be set")
	}
}
