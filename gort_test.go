package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// syncBuffer is a goroutine-safe bytes.Buffer for capturing script output.
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

// newTestServer builds a server over a temp scripts dir holding the given files.
func newTestServer(t *testing.T, files map[string]string) (*server, *syncBuffer) {
	t.Helper()
	dir := t.TempDir()
	scripts := make([]string, 0, len(files))
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
		scripts = append(scripts, name)
	}
	out := &syncBuffer{}
	s := &server{cfg: config{Port: defaultPort, ScriptsDir: dir}, scripts: scripts, stdout: out, stderr: out}
	return s, out
}

func TestListScripts(t *testing.T) {
	s, _ := newTestServer(t, nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/list-dist", nil)
	rr := httptest.NewRecorder()
	newRouter(s).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v", rr.Code, http.StatusOK)
	}
	expected := s.cfg.ScriptsDir + " seems like empty"
	if rr.Body.String() != expected {
		t.Errorf("handler returned unexpected body: got %v want %v", rr.Body.String(), expected)
	}
}

func TestListScriptsNonEmpty(t *testing.T) {
	s, _ := newTestServer(t, map[string]string{"hello.sh": "echo hi\n"})
	rr := httptest.NewRecorder()
	newRouter(s).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/list-dist", nil))
	if rr.Body.String() != "hello.sh\n" {
		t.Errorf("unexpected body: %q", rr.Body.String())
	}
}

func TestHealth(t *testing.T) {
	s, _ := newTestServer(t, nil)
	rr := httptest.NewRecorder()
	newRouter(s).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/health", nil))
	if rr.Code != http.StatusOK || rr.Body.String() != "OK" {
		t.Errorf("health: got %d %q", rr.Code, rr.Body.String())
	}
}

func TestNotFound(t *testing.T) {
	s, _ := newTestServer(t, nil)
	rr := httptest.NewRecorder()
	newRouter(s).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/any-url", nil))
	if rr.Code != http.StatusNotFound {
		t.Errorf("got status %d want %d", rr.Code, http.StatusNotFound)
	}
	if got := strings.TrimSpace(rr.Body.String()); got != "This page does not exist!" {
		t.Errorf("handler returned unexpected body: %q", got)
	}
}

func TestStartScript(t *testing.T) {
	const script = "echo.sh"
	s, out := newTestServer(t, map[string]string{script: "echo \"args:$1,$2 env:$GORT_TEST_VAR\"\n"})
	h := newRouter(s)

	tests := []struct {
		name string
		body string
		code int
	}{
		{"bad json", `{"executor":`, http.StatusUnprocessableEntity},
		{"missing fields", `{"executor":"sh"}`, http.StatusBadRequest},
		{"missing executor", `{"script":"echo.sh"}`, http.StatusBadRequest},
		{"unknown executor", `{"executor":"definitely-not-a-real-binary-xyz","script":"echo.sh"}`, http.StatusInternalServerError},
		{"unknown script", `{"executor":"sh","script":"nope.sh"}`, http.StatusNotImplemented},
		{"oversized body", `{"executor":"sh","script":"` + strings.Repeat("a", maxRequestBodyBytes) + `"}`, http.StatusUnprocessableEntity},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/start", strings.NewReader(tc.body)))
			if rr.Code != tc.code {
				t.Errorf("got status %d want %d (body %q)", rr.Code, tc.code, rr.Body.String())
			}
		})
	}

	t.Run("success with args", func(t *testing.T) {
		body := `{"executor":"sh","script":"echo.sh","args":["first","second"],"env_vars":["GORT_TEST_VAR=yes"]}`
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/start", strings.NewReader(body)))
		if rr.Code != http.StatusOK {
			t.Fatalf("got status %d want 200 (body %q)", rr.Code, rr.Body.String())
		}
		want := "args:first,second env:yes"
		deadline := time.Now().Add(5 * time.Second)
		for !strings.Contains(out.String(), want) {
			if time.Now().After(deadline) {
				t.Fatalf("script output %q does not contain %q", out.String(), want)
			}
			time.Sleep(20 * time.Millisecond)
		}
	})
}

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	env := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}

	cfg, err := loadConfig(env(map[string]string{"SCRIPTS_DIR": dir}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != defaultPort || cfg.ScriptsDir != dir || cfg.RateLimit != 0 {
		t.Errorf("unexpected defaults: %+v", cfg)
	}

	cfg, err = loadConfig(env(map[string]string{"SCRIPTS_DIR": dir, "PORT": "8080", "GORT_RATE_LIMIT": "3"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 8080 || cfg.RateLimit != 3 {
		t.Errorf("unexpected config: %+v", cfg)
	}

	bad := []map[string]string{
		{"SCRIPTS_DIR": filepath.Join(dir, "missing")},
		{"SCRIPTS_DIR": dir, "GORT_RATE_LIMIT": "abc"},
		{"SCRIPTS_DIR": dir, "GORT_RATE_LIMIT": "0"},
		{"SCRIPTS_DIR": dir, "GORT_RATE_LIMIT": "-1"},
		{"SCRIPTS_DIR": dir, "PORT": "notaport"},
	}
	for _, m := range bad {
		if _, err := loadConfig(env(m)); err == nil {
			t.Errorf("loadConfig(%v) succeeded, want error", m)
		}
	}
}

func TestNewHTTPServerTimeouts(t *testing.T) {
	srv := newHTTPServer(":0", http.NotFoundHandler())
	if srv.ReadHeaderTimeout != 5*time.Second || srv.ReadTimeout != 15*time.Second ||
		srv.WriteTimeout != 30*time.Second || srv.IdleTimeout != 60*time.Second ||
		srv.MaxHeaderBytes != 1<<20 {
		t.Errorf("unexpected server settings: %+v", srv)
	}
}
