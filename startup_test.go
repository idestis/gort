package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
)

// freePort asks the kernel for an unused TCP port.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// TestBinaryStartsAndStops builds the gort binary, runs it, waits for
// /v1/health and checks it shuts down cleanly on SIGTERM.
func TestBinaryStartsAndStops(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping binary startup test in -short mode")
	}
	if runtime.GOOS == "windows" {
		t.Skip("SIGTERM is not supported on windows")
	}

	tmp := t.TempDir()
	bin := filepath.Join(tmp, "gort")
	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}

	scriptsDir := filepath.Join(tmp, "scripts")
	if err := os.Mkdir(scriptsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	port := freePort(t)
	logs := &syncBuffer{}
	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(), fmt.Sprintf("PORT=%d", port), "SCRIPTS_DIR="+scriptsDir, "GORT_RATE_LIMIT=")
	cmd.Stdout = logs
	cmd.Stderr = logs
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	// done is closed once the process has exited; waitErr is then valid.
	done := make(chan struct{})
	var waitErr error
	go func() { waitErr = cmd.Wait(); close(done) }()
	t.Cleanup(func() {
		select {
		case <-done:
		default:
			_ = cmd.Process.Kill()
			<-done
		}
	})

	url := fmt.Sprintf("http://127.0.0.1:%d/v1/health", port)
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(10 * time.Second)
	for {
		select {
		case <-done:
			t.Fatalf("gort exited before becoming healthy: %v\n%s", waitErr, logs.String())
		default:
		}
		resp, err := client.Get(url)
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK && string(body) == "OK" {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("gort did not become healthy within 10s (last error: %v)", err)
		}
		time.Sleep(50 * time.Millisecond)
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
		err := waitErr
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			t.Fatalf("gort exited with code %d after SIGTERM\n%s", exitErr.ExitCode(), logs.String())
		}
		if err != nil {
			t.Fatalf("gort wait: %v", err)
		}
		if cmd.ProcessState.ExitCode() != 0 {
			t.Fatalf("exit code %d, want 0", cmd.ProcessState.ExitCode())
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("gort did not exit within 10s of SIGTERM\n%s", logs.String())
	}
}
