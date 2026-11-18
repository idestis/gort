package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/idestis/gort/internal/config"
	"github.com/idestis/gort/internal/runner"
	"github.com/idestis/gort/internal/server"
)

// shutdownTimeout bounds how long in-flight requests and running scripts
// may take to finish after SIGTERM/SIGINT.
const shutdownTimeout = 30 * time.Second

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	for _, w := range cfg.Warnings() {
		log.Println("WARNING:", w)
	}

	r := runner.New(cfg.MaxConcurrent, log.Default())
	s, err := server.New(cfg, r)
	if err != nil {
		return err
	}
	srv := server.NewHTTPServer(fmt.Sprintf(":%d", cfg.Port), s.Handler())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		log.Printf("Gort is started on port %d (max %d concurrent scripts)", cfg.Port, cfg.MaxConcurrent)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}
	stop() // a second signal now kills the process immediately

	log.Println("Shutdown signal received, draining connections and running scripts")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	if err := <-errCh; !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("server: %w", err)
	}
	if err := r.Shutdown(shutdownCtx); err != nil {
		log.Printf("Running scripts did not finish in time and were terminated: %v", err)
	}
	log.Println("Gort stopped")
	return nil
}
