package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"

	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
)

// Script will hold our entity for run
type Script struct {
	Executor string   `json:"executor"`
	Script   string   `json:"script"`
	EnvVars  []string `json:"env_vars"`
}

const (
	defaultPort       = 5000
	defaultScriptsDir = "./dist"
)

var (
	scriptsDir string
	port       int
	scripts    []string
)

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
		scanScripts()
	}
}

func main() {
	r := chi.NewRouter()

	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Route("/v1", func(r chi.Router) {
		r.Post("/start", startScript)                                   // /v1/start
		r.Get("/list-dist", listScripts)                                // /v1/list-dist
		r.Get("/health", func(w http.ResponseWriter, r *http.Request) { // /v1/health
			w.Write([]byte("OK"))
		})
	})

	r.NotFound(NotFoundHandler)
	log.Printf("Gort is started on port %d\n", port)
	http.ListenAndServe(fmt.Sprintf(":%d", port), r)

}

// listScripts will return scripts from SCRIPTS_DIR
func listScripts(w http.ResponseWriter, r *http.Request) {
	if len(scripts) == 0 {
		fmt.Fprintf(w, "%s seems like empty", scriptsDir)
		return
	}
	for _, script := range scripts {
		fmt.Fprintln(w, script)
	}
}

// scanScripts will fill our slice of scripts on startup
// TODO: implement background scanner
func scanScripts() {
	scriptsList, _ := ioutil.ReadDir(scriptsDir)
	for _, s := range scriptsList {
		scripts = append(scripts, s.Name())
	}
}

// startScript will start script and output will be in stdoutput
func startScript(w http.ResponseWriter, r *http.Request) {
	var script Script
	err := json.NewDecoder(r.Body).Decode(&script)
	// Parse JSON body
	if err != nil {
		http.Error(w, "Not able to parse data as valid JSON", 422)
		return
	}

	// Check for required parameters in JSON body
	if script.Executor == "" || script.Script == "" {
		http.Error(w, "Required parameters 'executor' and 'script' were not found in the payload", 400)
		return
	}

	// We should check if executor is installed
	_, err = exec.LookPath(script.Executor)
	if err != nil {
		http.Error(w, "Requested executor is not installed", 500)
		return
	}

	// Check if requested script exist in directory
	_, found := Find(scripts, script.Script)
	if !found {
		http.Error(w, "Requested script is not found in the scripts directory", 501)
		return
	}
	cmd := exec.Command(script.Executor, scriptsDir+"/"+script.Script)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if len(script.EnvVars) >= 1 {
		cmd.Env = os.Environ()
		for _, envVar := range script.EnvVars {
			cmd.Env = append(cmd.Env, envVar)
		}
	}
	cmd.Start()
	log.Println("Just ran subprocess of", script.Script, "with PID", cmd.Process.Pid)
	fmt.Fprintf(w, "The function will be executed in the background. Refer to container logs to see the output")
}

// NotFoundHandler will return custom message
func NotFoundHandler(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "This page does not exist!", 404)
}

// Find takes a slice and looks for an element in it. If found it will
// return it's key, otherwise it will return -1 and a bool of false.
func Find(slice []string, val string) (int, bool) {
	for i, item := range slice {
		if item == val {
			return i, true
		}
	}
	return -1, false
}
