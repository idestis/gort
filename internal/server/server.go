// Package server implements gort's HTTP API.
package server

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/idestis/gort/internal/config"
	"github.com/idestis/gort/internal/runner"
)

// Server holds the state the HTTP handlers need.
type Server struct {
	cfg    config.Config
	runner *runner.Runner
	// names lists the scripts in cfg.ScriptsDir in directory order;
	// scripts indexes them for lookups.
	names   []string
	scripts map[string]struct{}
	// stdout and stderr receive the output of launched scripts.
	stdout io.Writer
	stderr io.Writer
}

// New scans cfg.ScriptsDir and returns a Server that launches scripts through r.
// Script output goes to the process stdout/stderr.
func New(cfg config.Config, r *runner.Runner) (*Server, error) {
	names, err := scanScripts(cfg.ScriptsDir)
	if err != nil {
		return nil, err
	}
	s := &Server{
		cfg:     cfg,
		runner:  r,
		names:   names,
		scripts: make(map[string]struct{}, len(names)),
		stdout:  os.Stdout,
		stderr:  os.Stderr,
	}
	for _, n := range names {
		s.scripts[n] = struct{}{}
	}
	return s, nil
}

// scanScripts returns the names of the regular files in dir.
func scanScripts(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("scan scripts: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.Type().IsRegular() {
			names = append(names, e.Name())
		}
	}
	return names, nil
}

// Handler wires middleware and routes.
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Route("/v1", func(r chi.Router) {
		if s.cfg.RateLimit > 0 {
			log.Println("GORT_RATE_LIMIT was set globally for", s.cfg.RateLimit)
			r.Use(middleware.Throttle(s.cfg.RateLimit))
		}
		// Health stays unauthenticated so container probes keep working.
		r.Get("/health", HealthHandler)
		r.Group(func(r chi.Router) {
			r.Use(RequireToken(s.cfg.AuthToken))
			r.Post("/start", s.StartScriptHandler)
			r.Get("/list-dist", s.ListScriptsHandler)
		})
	})

	r.NotFound(NotFoundHandler)
	return r
}

// NewHTTPServer returns an http.Server with conservative timeouts.
func NewHTTPServer(addr string, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
}
