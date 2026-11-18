package gort

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"
)

// defaultMaxConcurrent is the number of scripts allowed to run at the same
// time when GORT_MAX_CONCURRENT is not set.
const defaultMaxConcurrent = 10

var (
	// ErrBusy is returned by Runner.Start when the concurrency limit is reached.
	ErrBusy = errors.New("runner: concurrency limit reached")
	// ErrShuttingDown is returned by Runner.Start after Shutdown was called.
	ErrShuttingDown = errors.New("runner: shutting down")
)

// LoadMaxConcurrent reads GORT_MAX_CONCURRENT through getenv. An unset or
// empty value yields defaultMaxConcurrent; otherwise it must be an integer >= 1.
func LoadMaxConcurrent(getenv func(string) string) (int, error) {
	v := getenv("GORT_MAX_CONCURRENT")
	if v == "" {
		return defaultMaxConcurrent, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("GORT_MAX_CONCURRENT must be an integer, got %q", v)
	}
	if n < 1 {
		return 0, fmt.Errorf("GORT_MAX_CONCURRENT must be >= 1, got %d", n)
	}
	return n, nil
}

// Runner starts background processes, bounds how many run at once and reaps
// every one of them with cmd.Wait so no zombies are left behind.
type Runner struct {
	sem    chan struct{}
	wg     sync.WaitGroup
	logger *log.Logger

	mu      sync.Mutex // guards closed, running, procs and wg.Add
	closed  bool
	running int
	procs   map[*os.Process]struct{}
}

// NewRunner returns a Runner allowing at most maxConcurrent processes at once
// (values below 1 are treated as 1). A nil logger logs to stderr.
func NewRunner(maxConcurrent int, logger *log.Logger) *Runner {
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}
	if logger == nil {
		logger = log.New(os.Stderr, "", log.LstdFlags)
	}
	return &Runner{
		sem:    make(chan struct{}, maxConcurrent),
		logger: logger,
		procs:  make(map[*os.Process]struct{}),
	}
}

// Start acquires a slot without blocking (ErrBusy if none is free), starts cmd
// and reaps it in a background goroutine that logs its outcome and releases
// the slot. If cmd.Start fails the slot is released and the error returned.
// After Shutdown it returns ErrShuttingDown.
func (r *Runner) Start(name string, cmd *exec.Cmd) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return 0, ErrShuttingDown
	}
	select {
	case r.sem <- struct{}{}:
	default:
		return 0, ErrBusy
	}
	if err := cmd.Start(); err != nil {
		<-r.sem
		return 0, err
	}
	r.running++
	r.procs[cmd.Process] = struct{}{}
	r.wg.Add(1)
	go r.wait(name, cmd, time.Now())
	return cmd.Process.Pid, nil
}

func (r *Runner) wait(name string, cmd *exec.Cmd, started time.Time) {
	defer r.wg.Done()
	err := cmd.Wait()
	dur := time.Since(started)
	code := -1
	if cmd.ProcessState != nil {
		code = cmd.ProcessState.ExitCode()
	}
	if err != nil {
		r.logger.Printf("script %s (pid %d) finished with exit code %d after %s: %v", name, cmd.Process.Pid, code, dur, err)
	} else {
		r.logger.Printf("script %s (pid %d) finished with exit code %d after %s", name, cmd.Process.Pid, code, dur)
	}
	r.mu.Lock()
	delete(r.procs, cmd.Process)
	r.running--
	r.mu.Unlock()
	<-r.sem
}

// Running reports how many started processes have not been reaped yet.
func (r *Runner) Running() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.running
}

// Shutdown stops accepting new starts and waits for running processes to
// finish. If ctx is done first, SIGTERM is sent to the remaining processes
// (they are still reaped in the background) and ctx.Err() is returned.
func (r *Runner) Shutdown(ctx context.Context) error {
	r.mu.Lock()
	r.closed = true
	r.mu.Unlock()

	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		r.mu.Lock()
		for p := range r.procs {
			if err := p.Signal(syscall.SIGTERM); err != nil {
				r.logger.Printf("failed to send SIGTERM to pid %d: %v", p.Pid, err)
			} else {
				r.logger.Printf("sent SIGTERM to pid %d", p.Pid)
			}
		}
		r.mu.Unlock()
		return ctx.Err()
	}
}
