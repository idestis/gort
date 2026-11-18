// Package config loads gort's runtime settings from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/idestis/gort/internal/policy"
	"github.com/idestis/gort/internal/runner"
)

const (
	// DefaultPort is used when PORT is not set.
	DefaultPort = 5000
	// DefaultScriptsDir is used when SCRIPTS_DIR is not set.
	DefaultScriptsDir = "./dist"
)

// Config holds runtime settings read from the environment.
type Config struct {
	// Port to listen on (PORT).
	Port int
	// ScriptsDir is the directory scripts are served from (SCRIPTS_DIR).
	ScriptsDir string
	// RateLimit caps in-flight /v1 requests (GORT_RATE_LIMIT); 0 means unlimited.
	RateLimit int
	// MaxConcurrent caps concurrently running scripts (GORT_MAX_CONCURRENT).
	MaxConcurrent int
	// AuthToken is the shared bearer token (GORT_AUTH_TOKEN); empty disables auth.
	AuthToken string
	// Policy holds the executor and env var allowlists
	// (GORT_ALLOWED_EXECUTORS, GORT_ALLOWED_ENV_VARS).
	Policy policy.Policy
}

// Load reads configuration using getenv (usually os.Getenv).
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		Port:       DefaultPort,
		ScriptsDir: DefaultScriptsDir,
		AuthToken:  getenv("GORT_AUTH_TOKEN"),
		Policy:     policy.Load(getenv),
	}

	if v := getenv("PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil || p <= 0 || p > 65535 {
			return Config{}, fmt.Errorf("invalid PORT %q: must be an integer between 1 and 65535", v)
		}
		cfg.Port = p
	}

	if v := getenv("SCRIPTS_DIR"); v != "" {
		cfg.ScriptsDir = v
	}
	info, err := os.Stat(cfg.ScriptsDir)
	if err != nil {
		return Config{}, fmt.Errorf("scripts directory: %w", err)
	}
	if !info.IsDir() {
		return Config{}, fmt.Errorf("scripts directory %q is not a directory", cfg.ScriptsDir)
	}

	if v := getenv("GORT_RATE_LIMIT"); v != "" {
		rl, err := strconv.Atoi(v)
		if err != nil || rl <= 0 {
			return Config{}, fmt.Errorf("invalid GORT_RATE_LIMIT %q: must be an integer > 0", v)
		}
		cfg.RateLimit = rl
	}

	if cfg.MaxConcurrent, err = runner.LoadMaxConcurrent(getenv); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

// Warnings lists insecure or disabling settings worth logging at startup.
func (c Config) Warnings() []string {
	w := c.Policy.Warnings()
	if c.AuthToken == "" {
		w = append(w, "GORT_AUTH_TOKEN is not set: endpoints are unauthenticated; only run gort on a trusted network")
	}
	return w
}
