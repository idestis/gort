package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/idestis/gort/internal/runner"
)

// maxRequestBodyBytes caps the size of a /v1/start payload.
const maxRequestBodyBytes = 1 << 20 // 1 MiB

// Script is the /v1/start payload describing which script to run.
type Script struct {
	// Executor is the interpreter program to run the script with.
	Executor string `json:"executor"`
	// Script is the file name inside SCRIPTS_DIR.
	Script string `json:"script"`
	// EnvVars are KEY=VALUE pairs added to the script's environment.
	EnvVars []string `json:"env_vars"`
	// Args are additional arguments passed to the script.
	Args []string `json:"args"`
}

// HealthHandler reports liveness.
func HealthHandler(w http.ResponseWriter, r *http.Request) {
	_, _ = w.Write([]byte("OK"))
}

// NotFoundHandler returns a custom 404 message.
func NotFoundHandler(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "This page does not exist!", http.StatusNotFound)
}

// ListScriptsHandler lists the scripts found in SCRIPTS_DIR.
func (s *Server) ListScriptsHandler(w http.ResponseWriter, r *http.Request) {
	if len(s.names) == 0 {
		fmt.Fprintf(w, "%s seems like empty", s.cfg.ScriptsDir)
		return
	}
	for _, name := range s.names {
		fmt.Fprintln(w, name)
	}
}

// StartScriptHandler validates the request and starts the script in the background.
func (s *Server) StartScriptHandler(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)

	var script Script
	if err := json.NewDecoder(r.Body).Decode(&script); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "Request body is too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "Not able to parse data as valid JSON", http.StatusUnprocessableEntity)
		return
	}

	if script.Executor == "" || script.Script == "" {
		http.Error(w, "Required parameters 'executor' and 'script' were not found in the payload", http.StatusBadRequest)
		return
	}

	if err := s.cfg.Policy.CheckExecutor(script.Executor); err != nil {
		log.Printf("Rejected /v1/start: %v", err)
		http.Error(w, "Requested executor is not allowed", http.StatusForbidden)
		return
	}
	if err := s.cfg.Policy.CheckEnvVars(script.EnvVars); err != nil {
		log.Printf("Rejected /v1/start: %v", err)
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}

	if _, err := exec.LookPath(script.Executor); err != nil {
		http.Error(w, "Requested executor is not installed", http.StatusInternalServerError)
		return
	}

	if _, found := s.scripts[script.Script]; !found {
		http.Error(w, "Requested script is not found in the scripts directory", http.StatusNotImplemented)
		return
	}

	args := make([]string, 0, 1+len(script.Args))
	args = append(args, filepath.Join(s.cfg.ScriptsDir, script.Script))
	args = append(args, script.Args...)

	cmd := exec.Command(script.Executor, args...)
	cmd.Stdout = s.stdout
	cmd.Stderr = s.stderr
	if len(script.EnvVars) > 0 {
		cmd.Env = append(os.Environ(), script.EnvVars...)
	}

	pid, err := s.runner.Start(script.Script, cmd)
	switch {
	case errors.Is(err, runner.ErrBusy):
		w.Header().Set("Retry-After", "5")
		http.Error(w, "Too many scripts are running, retry later", http.StatusTooManyRequests)
		return
	case errors.Is(err, runner.ErrShuttingDown):
		http.Error(w, "Gort is shutting down", http.StatusServiceUnavailable)
		return
	case err != nil:
		log.Printf("Failed to start %s: %v", script.Script, err)
		http.Error(w, "Failed to start requested script", http.StatusInternalServerError)
		return
	}
	log.Println("Just ran subprocess of", script.Script, "with PID", pid)
	fmt.Fprintf(w, "The function will be executed in the background. Refer to container logs to see the output")
}
