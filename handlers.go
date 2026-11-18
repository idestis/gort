package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/idestis/gort/utils"
)

// maxRequestBodyBytes caps the size of a /v1/start payload.
const maxRequestBodyBytes = 1 << 20 // 1 MiB

// Script sctruct will hold an entity to define which script we should run
type Script struct {
	// Executor defines the name of
	// the interpreter program to be executed
	Executor string `json:"executor"`
	// Script will refer to script name from the dist folder
	Script string `json:"script"`
	// EnvVars slice will hold all environment variables for the script
	EnvVars []string `json:"env_vars"`
	// Args slice will hold additional arguments for the command
	Args []string `json:"args"`
}

// server holds the state the HTTP handlers need.
type server struct {
	cfg     config
	scripts []string
	// stdout and stderr receive the output of launched scripts.
	stdout io.Writer
	stderr io.Writer
}

// newServer builds a server whose scripts write to the process stdout/stderr.
func newServer(cfg config, scripts []string) *server {
	return &server{cfg: cfg, scripts: scripts, stdout: os.Stdout, stderr: os.Stderr}
}

// HealthHandler reports liveness.
func HealthHandler(w http.ResponseWriter, r *http.Request) {
	_, _ = w.Write([]byte("OK"))
}

// ListScriptsHandler will return scripts list from the SCRIPTS_DIR
func (s *server) ListScriptsHandler(w http.ResponseWriter, r *http.Request) {
	if len(s.scripts) == 0 {
		fmt.Fprintf(w, "%s seems like empty", s.cfg.ScriptsDir)
		return
	}
	for _, script := range s.scripts {
		fmt.Fprintln(w, script)
	}
}

// StartScriptHandler will start requested script and print output to stdout
func (s *server) StartScriptHandler(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)

	var script Script
	if err := json.NewDecoder(r.Body).Decode(&script); err != nil {
		http.Error(w, "Not able to parse data as valid JSON", http.StatusUnprocessableEntity)
		return
	}

	if script.Executor == "" || script.Script == "" {
		http.Error(w, "Required parameters 'executor' and 'script' were not found in the payload", http.StatusBadRequest)
		return
	}

	// SEAM(policy): executor + env var allowlist check goes here

	if _, err := exec.LookPath(script.Executor); err != nil {
		http.Error(w, "Requested executor is not installed", http.StatusInternalServerError)
		return
	}

	if _, found := utils.Find(s.scripts, script.Script); !found {
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

	// SEAM(runner): process launch + reaping goes here
	// Start and don't wait till execution ends.
	if err := cmd.Start(); err != nil {
		log.Printf("Failed to start %s: %v", script.Script, err)
		http.Error(w, "Failed to start requested script", http.StatusInternalServerError)
		return
	}
	log.Println("Just ran subprocess of", script.Script, "with PID", cmd.Process.Pid)
	fmt.Fprintf(w, "The function will be executed in the background. Refer to container logs to see the output")
}

// NotFoundHandler will return custom error message
func NotFoundHandler(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "This page does not exist!", http.StatusNotFound)
}
