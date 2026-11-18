package server

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/idestis/gort/internal/config"
	"github.com/idestis/gort/internal/policy"
	"github.com/idestis/gort/internal/runner"
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

type testOpts struct {
	files         map[string]string
	env           map[string]string // policy / auth settings
	maxConcurrent int
}

// newTestServer builds a Server over a temp scripts dir holding opts.files.
// Unless overridden, "sh" is an allowed executor and GORT_TEST_VAR an allowed env var.
func newTestServer(t *testing.T, opts testOpts) (*Server, *syncBuffer) {
	t.Helper()
	dir := t.TempDir()
	for name, body := range opts.files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	env := map[string]string{
		"GORT_ALLOWED_EXECUTORS": "sh",
		"GORT_ALLOWED_ENV_VARS":  "GORT_TEST_VAR",
	}
	for k, v := range opts.env {
		env[k] = v
	}
	if opts.maxConcurrent == 0 {
		opts.maxConcurrent = 10
	}
	cfg := config.Config{
		Port:          config.DefaultPort,
		ScriptsDir:    dir,
		MaxConcurrent: opts.maxConcurrent,
		AuthToken:     env["GORT_AUTH_TOKEN"],
		Policy:        policy.Load(func(k string) string { return env[k] }),
	}
	r := runner.New(cfg.MaxConcurrent, log.New(io.Discard, "", 0))
	s, err := New(cfg, r)
	if err != nil {
		t.Fatal(err)
	}
	out := &syncBuffer{}
	s.stdout, s.stderr = out, out
	return s, out
}

func do(h http.Handler, method, path, body string, header ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func waitFor(t *testing.T, out *syncBuffer, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(out.String(), want) {
		if time.Now().After(deadline) {
			t.Fatalf("script output %q does not contain %q", out.String(), want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestListScripts(t *testing.T) {
	s, _ := newTestServer(t, testOpts{})
	rr := do(s.Handler(), http.MethodGet, "/v1/list-dist", "")
	if rr.Code != http.StatusOK {
		t.Errorf("got status %d want 200", rr.Code)
	}
	if want := s.cfg.ScriptsDir + " seems like empty"; rr.Body.String() != want {
		t.Errorf("got body %q want %q", rr.Body.String(), want)
	}
}

func TestListScriptsSkipsDirectories(t *testing.T) {
	s, _ := newTestServer(t, testOpts{files: map[string]string{"hello.sh": "echo hi\n"}})
	if err := os.Mkdir(filepath.Join(s.cfg.ScriptsDir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	names, err := scanScripts(s.cfg.ScriptsDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "hello.sh" {
		t.Errorf("scanScripts = %q, want [hello.sh]", names)
	}
	if rr := do(s.Handler(), http.MethodGet, "/v1/list-dist", ""); rr.Body.String() != "hello.sh\n" {
		t.Errorf("unexpected body: %q", rr.Body.String())
	}
}

func TestHealth(t *testing.T) {
	s, _ := newTestServer(t, testOpts{env: map[string]string{"GORT_AUTH_TOKEN": "s3cret"}})
	rr := do(s.Handler(), http.MethodGet, "/v1/health", "")
	if rr.Code != http.StatusOK || rr.Body.String() != "OK" {
		t.Errorf("health must not require auth: got %d %q", rr.Code, rr.Body.String())
	}
}

func TestNotFound(t *testing.T) {
	s, _ := newTestServer(t, testOpts{})
	rr := do(s.Handler(), http.MethodGet, "/any-url", "")
	if rr.Code != http.StatusNotFound {
		t.Errorf("got status %d want 404", rr.Code)
	}
	if got := strings.TrimSpace(rr.Body.String()); got != "This page does not exist!" {
		t.Errorf("unexpected body: %q", got)
	}
}

func TestAuthProtectsEndpoints(t *testing.T) {
	s, _ := newTestServer(t, testOpts{
		files: map[string]string{"ok.sh": "true\n"},
		env:   map[string]string{"GORT_AUTH_TOKEN": "s3cret"},
	})
	h := s.Handler()
	start := `{"executor":"sh","script":"ok.sh"}`

	if rr := do(h, http.MethodGet, "/v1/list-dist", ""); rr.Code != http.StatusUnauthorized {
		t.Errorf("list without token: got %d want 401", rr.Code)
	}
	if rr := do(h, http.MethodPost, "/v1/start", start, "Authorization", "Bearer wrong"); rr.Code != http.StatusUnauthorized {
		t.Errorf("start with wrong token: got %d want 401", rr.Code)
	}
	if rr := do(h, http.MethodPost, "/v1/start", start, "Authorization", "Bearer s3cret"); rr.Code != http.StatusOK {
		t.Errorf("start with token: got %d want 200 (%q)", rr.Code, rr.Body.String())
	}
}

func TestStartScriptErrors(t *testing.T) {
	s, _ := newTestServer(t, testOpts{
		files: map[string]string{"echo.sh": "echo hi\n"},
		env:   map[string]string{"GORT_ALLOWED_EXECUTORS": "sh,definitely-not-a-real-binary-xyz"},
	})
	h := s.Handler()

	tests := []struct {
		name string
		body string
		code int
	}{
		{"bad json", `{"executor":`, http.StatusUnprocessableEntity},
		{"missing script", `{"executor":"sh"}`, http.StatusBadRequest},
		{"missing executor", `{"script":"echo.sh"}`, http.StatusBadRequest},
		{"executor not allowed", `{"executor":"bash","script":"echo.sh"}`, http.StatusForbidden},
		{"executor path not allowed", `{"executor":"/bin/sh","script":"echo.sh"}`, http.StatusForbidden},
		{"env var not allowed", `{"executor":"sh","script":"echo.sh","env_vars":["OTHER=1"]}`, http.StatusForbidden},
		{"denylisted env var", `{"executor":"sh","script":"echo.sh","env_vars":["LD_PRELOAD=/x.so"]}`, http.StatusForbidden},
		{"malformed env var", `{"executor":"sh","script":"echo.sh","env_vars":["GORT_TEST_VAR"]}`, http.StatusForbidden},
		{"allowed but not installed", `{"executor":"definitely-not-a-real-binary-xyz","script":"echo.sh"}`, http.StatusInternalServerError},
		{"unknown script", `{"executor":"sh","script":"nope.sh"}`, http.StatusNotImplemented},
		{"path traversal", `{"executor":"sh","script":"../echo.sh"}`, http.StatusNotImplemented},
		{"oversized body", `{"executor":"sh","script":"` + strings.Repeat("a", maxRequestBodyBytes) + `"}`, http.StatusRequestEntityTooLarge},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rr := do(h, http.MethodPost, "/v1/start", tc.body)
			if rr.Code != tc.code {
				t.Errorf("got status %d want %d (body %q)", rr.Code, tc.code, rr.Body.String())
			}
		})
	}
}

func TestStartScriptDeniedByDefault(t *testing.T) {
	s, _ := newTestServer(t, testOpts{
		files: map[string]string{"echo.sh": "echo hi\n"},
		env:   map[string]string{"GORT_ALLOWED_EXECUTORS": ""},
	})
	rr := do(s.Handler(), http.MethodPost, "/v1/start", `{"executor":"sh","script":"echo.sh"}`)
	if rr.Code != http.StatusForbidden {
		t.Errorf("got status %d want 403 when GORT_ALLOWED_EXECUTORS is unset", rr.Code)
	}
}

func TestStartScriptRunsWithArgsAndEnv(t *testing.T) {
	s, out := newTestServer(t, testOpts{files: map[string]string{
		"echo.sh": "echo \"args:$1,$2 env:$GORT_TEST_VAR\"\n",
	}})
	body := `{"executor":"sh","script":"echo.sh","args":["first","second"],"env_vars":["GORT_TEST_VAR=yes"]}`
	rr := do(s.Handler(), http.MethodPost, "/v1/start", body)
	if rr.Code != http.StatusOK {
		t.Fatalf("got status %d want 200 (body %q)", rr.Code, rr.Body.String())
	}
	waitFor(t, out, "args:first,second env:yes")
}

func TestStartScriptConcurrencyLimit(t *testing.T) {
	s, out := newTestServer(t, testOpts{
		files:         map[string]string{"slow.sh": "echo started; sleep 0.5\n"},
		maxConcurrent: 1,
	})
	h := s.Handler()
	body := `{"executor":"sh","script":"slow.sh"}`

	if rr := do(h, http.MethodPost, "/v1/start", body); rr.Code != http.StatusOK {
		t.Fatalf("first start: got %d want 200", rr.Code)
	}
	waitFor(t, out, "started")
	rr := do(h, http.MethodPost, "/v1/start", body)
	if rr.Code != http.StatusTooManyRequests || rr.Header().Get("Retry-After") == "" {
		t.Fatalf("second start: got %d (Retry-After %q) want 429", rr.Code, rr.Header().Get("Retry-After"))
	}

	// The slot frees once the first script exits and is reaped.
	deadline := time.Now().Add(5 * time.Second)
	for s.runner.Running() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("script was not reaped")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if rr := do(h, http.MethodPost, "/v1/start", body); rr.Code != http.StatusOK {
		t.Fatalf("start after slot freed: got %d want 200", rr.Code)
	}
}

func TestNewHTTPServerTimeouts(t *testing.T) {
	srv := NewHTTPServer(":0", http.NotFoundHandler())
	if srv.ReadHeaderTimeout != 5*time.Second || srv.ReadTimeout != 15*time.Second ||
		srv.WriteTimeout != 30*time.Second || srv.IdleTimeout != 60*time.Second ||
		srv.MaxHeaderBytes != 1<<20 {
		t.Errorf("unexpected server settings: %+v", srv)
	}
}
