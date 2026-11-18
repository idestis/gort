package runner

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func skipWithoutShell(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("requires a POSIX shell and kill(2)")
	}
}

func quietLogger() *log.Logger { return log.New(io.Discard, "", 0) }

// syncBuffer is a goroutine-safe log sink.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func waitIdle(t *testing.T, r *Runner, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for r.Running() != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("still %d running after %s", r.Running(), timeout)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestRunnerReapsProcess(t *testing.T) {
	skipWithoutShell(t)
	logs := &syncBuffer{}
	r := New(2, log.New(logs, "", 0))
	pid, err := r.Start("exit3", exec.Command("sh", "-c", "exit 3"))
	if err != nil {
		t.Fatal(err)
	}
	if pid <= 0 {
		t.Fatalf("bad pid %d", pid)
	}
	waitIdle(t, r, 2*time.Second)

	// The pid must be gone (reaped), not lingering as a zombie.
	if err := syscall.Kill(pid, 0); err != syscall.ESRCH {
		t.Fatalf("kill(%d, 0) = %v, want ESRCH", pid, err)
	}
	out := logs.String()
	if !strings.Contains(out, "exit3") || !strings.Contains(out, "exit code 3") {
		t.Fatalf("unexpected log output: %q", out)
	}
}

func TestRunnerBusyAndSlotFrees(t *testing.T) {
	skipWithoutShell(t)
	r := New(1, quietLogger())
	if _, err := r.Start("short", exec.Command("sleep", "0.2")); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Start("second", exec.Command("true")); !errors.Is(err, ErrBusy) {
		t.Fatalf("got %v, want ErrBusy", err)
	}
	waitIdle(t, r, 2*time.Second)
	if _, err := r.Start("third", exec.Command("true")); err != nil {
		t.Fatalf("slot not released: %v", err)
	}
	waitIdle(t, r, 2*time.Second)
}

func TestRunnerStartErrorReleasesSlot(t *testing.T) {
	skipWithoutShell(t)
	r := New(1, quietLogger())
	if _, err := r.Start("missing", exec.Command("/nonexistent/gort-binary")); err == nil {
		t.Fatal("expected start error")
	}
	if n := r.Running(); n != 0 {
		t.Fatalf("Running() = %d, want 0", n)
	}
	if _, err := r.Start("ok", exec.Command("true")); err != nil {
		t.Fatalf("slot leaked: %v", err)
	}
	waitIdle(t, r, 2*time.Second)
}

func TestRunnerShutdownWaits(t *testing.T) {
	skipWithoutShell(t)
	r := New(2, quietLogger())
	if _, err := r.Start("short", exec.Command("sleep", "0.2")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := r.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if n := r.Running(); n != 0 {
		t.Fatalf("Running() = %d after Shutdown", n)
	}
	if _, err := r.Start("late", exec.Command("true")); !errors.Is(err, ErrShuttingDown) {
		t.Fatalf("Start after Shutdown = %v, want ErrShuttingDown", err)
	}
}

func TestRunnerShutdownTerminatesOnTimeout(t *testing.T) {
	skipWithoutShell(t)
	r := New(2, quietLogger())
	pid, err := r.Start("long", exec.Command("sleep", "30"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := r.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown = %v, want context.DeadlineExceeded", err)
	}
	waitIdle(t, r, 2*time.Second)
	if time.Since(start) > 2*time.Second {
		t.Fatal("process was not terminated promptly")
	}
	if err := syscall.Kill(pid, 0); err != syscall.ESRCH {
		t.Fatalf("kill(%d, 0) = %v, want ESRCH", pid, err)
	}
	// A second Shutdown and further Starts must not panic.
	if err := r.Shutdown(context.Background()); err != nil {
		t.Fatalf("second Shutdown: %v", err)
	}
	if _, err := r.Start("late", exec.Command("true")); err == nil {
		t.Fatal("Start after Shutdown succeeded")
	}
}

func TestRunnerConcurrentStartsRespectLimit(t *testing.T) {
	skipWithoutShell(t)
	const limit, callers = 3, 40
	r := New(limit, quietLogger())

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		ok      int
		maxSeen int
	)
	gate := make(chan struct{})
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-gate
			_, err := r.Start("sleep", exec.Command("sleep", "0.5"))
			n := r.Running()
			mu.Lock()
			defer mu.Unlock()
			if n > maxSeen {
				maxSeen = n
			}
			switch {
			case err == nil:
				ok++
			case errors.Is(err, ErrBusy):
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	close(gate)
	wg.Wait()
	if ok < 1 || ok > limit {
		t.Fatalf("%d starts succeeded, want 1..%d", ok, limit)
	}
	if maxSeen > limit {
		t.Fatalf("observed %d running, limit %d", maxSeen, limit)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := r.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}

func TestLoadMaxConcurrent(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    int
		wantErr bool
	}{
		{"unset", "", defaultMaxConcurrent, false},
		{"one", "1", 1, false},
		{"many", "25", 25, false},
		{"zero", "0", 0, true},
		{"negative", "-2", 0, true},
		{"not a number", "ten", 0, true},
		{"float", "1.5", 0, true},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, err := LoadMaxConcurrent(func(k string) string {
				if k != "GORT_MAX_CONCURRENT" {
					t.Errorf("unexpected key %q", k)
				}
				return tc.value
			})
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Fatalf("got %d, want %d", got, tc.want)
			}
		})
	}
}
