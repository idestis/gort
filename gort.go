package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"

	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/idestis/gort/utils"
)

// Script sctruct will hold an entity to define which script we should run
type Script struct {
	Executor string   `json:"executor"`
	Script   string   `json:"script"`
	EnvVars  []string `json:"env_vars"`
	Args     []string `json:"args"`
}

const (
	defaultPort       = 5000
	defaultScriptsDir = "./dist"
)

var (
	port       int
	scriptsDir string
	scripts    []string
)

// init is here with one reason gort need to be initialized first
func init() {
	port, _ = strconv.Atoi(os.Getenv("PORT"))
	if port == 0 {
		port = defaultPort
	}
	scriptsDir = os.Getenv("SCRIPTS_DIR")
	if scriptsDir == "" {
		scriptsDir = defaultScriptsDir
		if _, err := os.Stat(scriptsDir); os.IsNotExist(err) {
			log.Panic(err)
		}
		scripts = utils.ScanScripts(scriptsDir)
	}
}

func main() {
	r := chi.NewRouter()

	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Compress(9))

	r.Route("/v1", func(r chi.Router) {
		if os.Getenv("GORT_RATE_LIMIT") != "" {
			rl, _ := strconv.Atoi(os.Getenv("GORT_RATE_LIMIT"))
			log.Println("GORT_RATE_LIMIT was set globally for", rl)
			r.Use(middleware.Throttle(rl))
		}
		r.Post("/start", StartScriptHandler)                            // /v1/start
		r.Get("/list-dist", ListScriptsHandler)                         // /v1/list-dist
		r.Get("/health", func(w http.ResponseWriter, r *http.Request) { // /v1/health
			w.Write([]byte("OK"))
		})
	})

	r.NotFound(NotFoundHandler)
	log.Printf("Gort is started on port %d\n", port)
	http.ListenAndServe(fmt.Sprintf(":%d", port), r)

}

// ListScriptsHandler will return scripts list from the SCRIPTS_DIR
func ListScriptsHandler(w http.ResponseWriter, r *http.Request) {
	if len(scripts) == 0 {
		fmt.Fprintf(w, "%s seems like empty", scriptsDir)
		return
	}
	for _, script := range scripts {
		fmt.Fprintln(w, script)
	}
}

// StartScriptHandler will start requested script and print output to stdout
func StartScriptHandler(w http.ResponseWriter, r *http.Request) {
	var script Script
	err := json.NewDecoder(r.Body).Decode(&script)
	if err != nil {
		http.Error(w, "Not able to parse data as valid JSON", 422)
		return
	}

	if script.Executor == "" || script.Script == "" {
		http.Error(w, "Required parameters 'executor' and 'script' were not found in the payload", 400)
		return
	}

	_, err = exec.LookPath(script.Executor)
	if err != nil {
		http.Error(w, "Requested executor is not installed", 500)
		return
	}

	_, found := utils.Find(scripts, script.Script)
	if !found {
		http.Error(w, "Requested script is not found in the scripts directory", 501)
		return
	}
	command := []string{scriptsDir + "/" + script.Script}
	cmd := exec.Command(script.Executor, command...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Append arguments to command if they are present in request
	if len(script.Args) >= 1 {
		command = append(command, script.Args...)
		cmd = exec.Command(script.Executor, command...)
	}

	// Append arguments to command.Environment if they are present in request
	if len(script.EnvVars) >= 1 {
		cmd.Env = os.Environ()
		for _, envVar := range script.EnvVars {
			cmd.Env = append(cmd.Env, envVar)
		}
	}
	// Start and don't wait till execution ends
	cmd.Start()
	log.Println("Just ran subprocess of", script.Script, "with PID", cmd.Process.Pid)
	fmt.Fprintf(w, "The function will be executed in the background. Refer to container logs to see the output")
}

// NotFoundHandler will return custom error message
func NotFoundHandler(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "This page does not exist!", 404)
}
