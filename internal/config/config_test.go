package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadDefaults(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(env(map[string]string{"SCRIPTS_DIR": dir}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != DefaultPort || cfg.ScriptsDir != dir || cfg.RateLimit != 0 ||
		cfg.MaxConcurrent != 10 || cfg.AuthToken != "" {
		t.Errorf("unexpected defaults: %+v", cfg)
	}
	if err := cfg.Policy.CheckExecutor("sh"); err == nil {
		t.Error("executors must be denied by default")
	}
}

func TestLoadValues(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(env(map[string]string{
		"SCRIPTS_DIR":            dir,
		"PORT":                   "8080",
		"GORT_RATE_LIMIT":        "3",
		"GORT_MAX_CONCURRENT":    "4",
		"GORT_AUTH_TOKEN":        "s3cret",
		"GORT_ALLOWED_EXECUTORS": "sh",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 8080 || cfg.RateLimit != 3 || cfg.MaxConcurrent != 4 || cfg.AuthToken != "s3cret" {
		t.Errorf("unexpected config: %+v", cfg)
	}
	if err := cfg.Policy.CheckExecutor("sh"); err != nil {
		t.Errorf("sh should be allowed: %v", err)
	}
}

func TestLoadErrors(t *testing.T) {
	dir := t.TempDir()
	bad := []map[string]string{
		{"SCRIPTS_DIR": filepath.Join(dir, "missing")},
		{"SCRIPTS_DIR": dir, "GORT_RATE_LIMIT": "abc"},
		{"SCRIPTS_DIR": dir, "GORT_RATE_LIMIT": "0"},
		{"SCRIPTS_DIR": dir, "GORT_RATE_LIMIT": "-1"},
		{"SCRIPTS_DIR": dir, "GORT_MAX_CONCURRENT": "0"},
		{"SCRIPTS_DIR": dir, "PORT": "notaport"},
		{"SCRIPTS_DIR": dir, "PORT": "70000"},
	}
	for _, m := range bad {
		if _, err := Load(env(m)); err == nil {
			t.Errorf("Load(%v) succeeded, want error", m)
		}
	}
}

func TestWarnings(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(env(map[string]string{"SCRIPTS_DIR": dir}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(cfg.Warnings(), "\n"), "GORT_AUTH_TOKEN") {
		t.Errorf("expected auth warning, got %q", cfg.Warnings())
	}

	cfg, err = Load(env(map[string]string{
		"SCRIPTS_DIR": dir, "GORT_AUTH_TOKEN": "x",
		"GORT_ALLOWED_EXECUTORS": "sh", "GORT_ALLOWED_ENV_VARS": "FOO",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if w := cfg.Warnings(); len(w) != 0 {
		t.Errorf("expected no warnings, got %q", w)
	}
}
