package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/idestis/gort/utils"
)

const (
	// defaultPort if not specified as environment variable
	defaultPort = 5000
	// defaultScriptsDir is a path to find scripts
	defaultScriptsDir = "./dist"
	// shutdownTimeout bounds how long in-flight requests may take to drain
	shutdownTimeout = 30 * time.Second
)

// config holds runtime settings read from the environment.
type config struct {
	// Port to listen on (PORT).
	Port int
	// ScriptsDir is the directory scripts are served from (SCRIPTS_DIR).
	ScriptsDir string
	// RateLimit is the max number of concurrent /v1 requests
	// (GORT_RATE_LIMIT); 0 means unlimited.
	RateLimit int
}

// loadConfig reads configuration using getenv (usually os.Getenv).
func loadConfig(getenv func(string) string) (config, error) {
	cfg := config{Port: defaultPort, ScriptsDir: defaultScriptsDir}

	if v := getenv("PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil || p <= 0 || p > 65535 {
			return config{}, fmt.Errorf("invalid PORT %q: must be an integer between 1 and 65535", v)
		}
		cfg.Port = p
	}

	if v := getenv("SCRIPTS_DIR"); v != "" {
		cfg.ScriptsDir = v
	}
	info, err := os.Stat(cfg.ScriptsDir)
	if err != nil {
		return config{}, fmt.Errorf("scripts directory: %w", err)
	}
	if !info.IsDir() {
		return config{}, fmt.Errorf("scripts directory %q is not a directory", cfg.ScriptsDir)
	}

	if v := getenv("GORT_RATE_LIMIT"); v != "" {
		rl, err := strconv.Atoi(v)
		if err != nil || rl <= 0 {
			return config{}, fmt.Errorf("invalid GORT_RATE_LIMIT %q: must be an integer > 0", v)
		}
		cfg.RateLimit = rl
	}

	return cfg, nil
}

// newRouter wires middleware and routes for the given server.
func newRouter(s *server) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Route("/v1", func(r chi.Router) {
		if s.cfg.RateLimit > 0 {
			log.Println("GORT_RATE_LIMIT was set globally for", s.cfg.RateLimit)
			r.Use(middleware.Throttle(s.cfg.RateLimit))
		}
		r.Post("/start", s.StartScriptHandler)    // /v1/start
		r.Get("/list-dist", s.ListScriptsHandler) // /v1/list-dist
		r.Get("/health", HealthHandler)           // /v1/health
	})

	r.NotFound(NotFoundHandler)
	return r
}

// newHTTPServer returns an http.Server with conservative timeouts.
func newHTTPServer(addr string, h http.Handler) *http.Server {
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

func main() {
	cfg, err := loadConfig(os.Getenv)
	if err != nil {
		log.Fatal(err)
	}

	s := newServer(cfg, utils.ScanScripts(cfg.ScriptsDir))
	srv := newHTTPServer(fmt.Sprintf(":%d", cfg.Port), newRouter(s))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		log.Printf("Gort is started on port %d\n", cfg.Port)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
		return
	case <-ctx.Done():
	}
	stop() // a second signal now kills the process immediately

	log.Println("Shutdown signal received, draining connections")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("Graceful shutdown failed: %v", err)
	}
	if err := <-errCh; !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("Server error: %v", err)
	}
	log.Println("Gort stopped")
}
