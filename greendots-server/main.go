package main

import (
	"bufio"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/andanhm/go-prettytime"
)

//go:embed api-docs.txt
var apiDocs []byte

type projectsList struct {
	Projects []project `json:"projects"`
}
type runsList struct {
	Runs []run `json:"runs"`
}
type project struct {
	Id   string `json:"id"`
	Name string `json:"name"`
	Runs []run  `json:"runs"`
}
type run struct {
	Id        string `json:"id"`
	Name      string `json:"name"`
	PrettyAge string `json:"pretty_age"`
	CreatedAt string `json:"created_at"`
}
type runPlan struct {
	WorkerCount int                          `json:"worker_count"`
	Groups      map[string][]runPlanTestItem `json:"groups"`
	RowParams   []string                     `json:"row_params"`
}

type runPlanTestItem struct {
	Id     string            `json:"id"`
	Name   string            `json:"name"`
	Params map[string]string `json:"params"`
}

var projectsDir string

func docsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.Write(apiDocs)
}

func getProjectRuns(project string) ([]run, error) {
	projectPath := filepath.Join(projectsDir, project)

	entries, err := os.ReadDir(projectPath)
	if err != nil {
		return nil, err
	}

	runs := make([]run, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}

		info, err := os.Stat(projectPath)
		if err != nil {
			continue
		}

		runs = append(runs, run{
			Id:        e.Name(),
			Name:      e.Name(),
			CreatedAt: info.ModTime().Format(time.RFC3339),
			PrettyAge: prettytime.Format(info.ModTime()),
		})
	}

	sort.Slice(runs, func(i int, j int) bool {
		return runs[i].CreatedAt < runs[j].CreatedAt
	})

	return runs, nil
}

func projectsListHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	projectsDir, err := os.ReadDir(projectsDir)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
	}

	projects := make([]project, 0, len(projectsDir))
	for _, projectEntry := range projectsDir {
		if !projectEntry.IsDir() {
			continue
		}

		runs, err := getProjectRuns(projectEntry.Name())
		if err != nil {
			continue
		}

		projects = append(projects, project{
			Id:   projectEntry.Name(),
			Name: projectEntry.Name(),
			Runs: runs[:min(10, len(runs))],
		})
	}

	result := projectsList{Projects: projects}
	err = json.NewEncoder(w).Encode(result)
	if err != nil {
		log.Printf("%s %s: json encoder: %v", r.Method, r.URL.Path, err)
	}
}

func projectRunsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	project := r.PathValue("project")

	runs, err := getProjectRuns(project)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
	}

	result := runsList{Runs: runs}
	err = json.NewEncoder(w).Encode(result)
	if err != nil {
		log.Printf("%s %s: json encoder: %v", r.Method, r.URL.Path, err)
	}
}

func runPlanHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	project := r.PathValue("project")
	run := r.PathValue("run")
	planPath := filepath.Join(projectsDir, project, run, "plan.json")
	http.ServeFile(w, r, planPath)
}

func runStatusSummaryHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/jsonl")

	// Read plan file to get worker count
	project := r.PathValue("project")
	run := r.PathValue("run")
	planPath := filepath.Join(projectsDir, project, run, "plan.json")
	planFd, err := os.Open(planPath)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}
	defer planFd.Close()

	var plan runPlan
	err = json.NewDecoder(planFd).Decode(&plan)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}

	// Read all status files, save the last line per test
	final_statuses := make(map[string]interface{})
	for status_idx := range plan.WorkerCount {
		statusSummaryPath := filepath.Join(
			projectsDir, project, run,
			fmt.Sprintf("status.%d.jsonl", status_idx),
		)
		fd, err := os.Open(statusSummaryPath)
		if err != nil {
			http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
			return
		}
		defer fd.Close()

		scanner := bufio.NewScanner(fd)
		final_offset := 0
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				break
			}

			var status_obj map[string]interface{}
			err := json.Unmarshal(line, &status_obj)
			if err != nil {
				// Probably a partial line, stop here
				break
			}

			// Store the final status of each test
			final_statuses[status_obj["test"].(string)] = status_obj
			// Keep a counter for the offset
			final_offset += len(line) + 1
		}
		w.Header().Add("X-End-Offset", fmt.Sprintf("%d", final_offset))
	}

	enc := json.NewEncoder(w)
	for _, status_obj := range final_statuses {
		enc.Encode(status_obj)
	}
}

func runStatusStreamHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/jsonl")

	project := r.PathValue("project")
	run := r.PathValue("run")
	statusStreamPath := filepath.Join(projectsDir, project, run, "status.0.jsonl")
	http.ServeFile(w, r, statusStreamPath)
}

func NotFoundHandler(w http.ResponseWriter, r *http.Request) {
	log.Printf("%s %s: not found", r.Method, r.URL.Path)
	http.Error(w, "Not found", http.StatusNotFound)
}

func main() {
	flag.StringVar(&projectsDir, "projects-dir", "../example-data", "The path to where the projects are stored")

	http.HandleFunc("GET /api/{$}", docsHandler)
	http.HandleFunc("GET /{$}", docsHandler) // TODO: serve frontend instead
	http.HandleFunc("GET /api/v1/projects", projectsListHandler)
	http.HandleFunc("GET /api/v1/projects/{project}/runs", projectRunsHandler)
	http.HandleFunc("GET /api/v1/projects/{project}/runs/{run}/plan", runPlanHandler)
	http.HandleFunc("GET /api/v1/projects/{project}/runs/{run}/status_summary", runStatusSummaryHandler)
	http.HandleFunc("GET /api/v1/projects/{project}/runs/{run}/status_stream/{worker_id}", runStatusStreamHandler)
	// TODO: http.HandleFunc("PUT /api/v1/projects/{project}/runs/{run}/test/{test}/status", todo)
	http.HandleFunc("/", NotFoundHandler)
	log.Println("Listening on :8080")
	http.ListenAndServe(":8080", nil)
}
