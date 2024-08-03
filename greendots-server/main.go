package main

import (
	"bufio"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync/atomic"
	"time"

	"github.com/andanhm/go-prettytime"
)

//go:embed api-docs.txt
var apiDocs []byte

//go:generate ./gen_logs_view.sh
//go:embed logs_view.min.html
var logsViewPrefix []byte

// -- Types --

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

type statusPollResponse struct {
	WorkersToCheck []int `json:"workers_to_check"`
}

type logLine struct {
	Level   string  `json:"level"`
	Message string  `json:"message"`
	Name    string  `json:"name"`
	Time    float64 `json:"time"`
}

var projectsDir string

// -- Helpers --

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

func fullWriteBytes(w http.ResponseWriter, data []byte) error {
	for len(data) > 0 {
		written, err := w.Write(data)
		if err != nil {
			return err
		}
		data = data[written:]
	}
	return nil
}

func fullWrite(w http.ResponseWriter, data string) error {
	return fullWriteBytes(w, []byte(data))
}

func isDirTraversal(path string) bool {
	return path[:1] == "."
}

// -- Handlers --

func docsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.Write(apiDocs)
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
		return
	}
}

func projectRunsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	project := r.PathValue("project")
	if isDirTraversal(project) {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}

	runs, err := getProjectRuns(project)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
	}

	result := runsList{Runs: runs}
	err = json.NewEncoder(w).Encode(result)
	if err != nil {
		log.Printf("%s %s: json encoder: %v", r.Method, r.URL.Path, err)
		return
	}
}

func runPlanHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	project := r.PathValue("project")
	run := r.PathValue("run")
	if isDirTraversal(project) || isDirTraversal(run) {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}
	planPath := filepath.Join(projectsDir, project, run, "plan.json")
	http.ServeFile(w, r, planPath)
}

func runStatusSummaryHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/jsonl")

	// Read plan file to get worker count
	project := r.PathValue("project")
	run := r.PathValue("run")
	if isDirTraversal(project) || isDirTraversal(run) {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}

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

func runStatusPollHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	var expected_sizes []int
	err := json.NewDecoder(r.Body).Decode(&expected_sizes)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	if len(expected_sizes) == 0 {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	project := r.PathValue("project")
	run := r.PathValue("run")
	if isDirTraversal(project) || isDirTraversal(run) {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}

	for {
		workers_to_check := []int{}
		for i, expected_size := range expected_sizes {
			statusPath := filepath.Join(projectsDir, project, run, fmt.Sprintf("status.%d.jsonl", i))
			stat, err := os.Stat(statusPath)
			if err != nil {
				http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
				return
			}
			if stat.Size() > int64(expected_size) {
				workers_to_check = append(workers_to_check, i)
			}
		}
		if len(workers_to_check) == 0 {
			time.Sleep(1000 * time.Millisecond)
			continue
		} else {
			err := json.NewEncoder(w).Encode(statusPollResponse{WorkersToCheck: workers_to_check})
			if err != nil {
				log.Printf("%s %s: json encoder: %v", r.Method, r.URL.Path, err)
				return
			}
			return
		}
	}
}

func runStatusStreamHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/jsonl")

	project := r.PathValue("project")
	run := r.PathValue("run")
	worker_id := r.PathValue("worker_id")
	if isDirTraversal(project) || isDirTraversal(run) || isDirTraversal(worker_id) {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}
	statusStreamPath := filepath.Join(projectsDir, project, run, fmt.Sprintf("status.%s.jsonl", worker_id))
	http.ServeFile(w, r, statusStreamPath)
}

func formatJsonLogLine(json_line []byte, last_date *string, last_time *float64) string {
	html_lines := ""
	var log_line logLine
	err := json.Unmarshal(json_line, &log_line)
	if err != nil {
		log_line = logLine{
			Level:   "INFO",
			Message: string(json_line),
			Name:    "unknown",
			Time:    *last_time,
		}
	}
	*last_time = log_line.Time

	var severity string
	var severity_class string
	switch log_line.Level {
	case "INFO":
		severity = "<span class=i>I</span>"
		severity_class = "i"
	case "WARN":
		severity = "<span class=w>W</span>"
		severity_class = "w"
	case "WARNING":
		severity = "<span class=w>W</span>"
		severity_class = "w"
	case "ERROR":
		severity = "<span class=e>E</span>"
		severity_class = "e"
	default:
		severity = fmt.Sprintf("<span class=i>%s</span>", log_line.Level)
		severity_class = "i"
	}
	ts := time.Unix(int64(log_line.Time), int64((log_line.Time-float64(int(log_line.Time)))*1e9))
	date := ts.Format("2006-01-02")
	if date != *last_date {
		html_lines += fmt.Sprintf("<span class=d>------- %s -------</span>\n", date)
		*last_date = date
	}

	html_lines += fmt.Sprintf(
		"<span class=t%s>%s </span>%s <span class=l%s>%s</span> %s\n",
		severity_class, ts.Format("15:04:05"), severity, severity_class, log_line.Name, log_line.Message,
	)

	return html_lines
}

func logStreamHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")

	no_truncate := r.URL.Query().Has("notrunc")

	// Open logfile
	project := r.PathValue("project")
	run := r.PathValue("run")
	test := r.PathValue("test")
	if isDirTraversal(project) || isDirTraversal(run) || isDirTraversal(test) {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}
	log_path := filepath.Join(projectsDir, project, run, fmt.Sprintf("%s.log.jsonl", test))
	log_fd, err := os.Open(log_path)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}
	defer log_fd.Close()

	// Send wrapper HTML
	err = fullWriteBytes(w, logsViewPrefix)
	if err != nil {
		return
	}

	err = fullWrite(w, "-- LOG START --\n")
	if err != nil {
		return
	}

	// Read the log file, and keep trying on EOF
	chunk_pipe_rd, chunk_pipe_wr := io.Pipe()
	go func() {
		chunk := make([]byte, 1024)
		for {
			n, err := log_fd.Read(chunk)
			if err != nil && err != io.EOF {
				chunk_pipe_wr.CloseWithError(err)
				break
			}
			if n == 0 || err == io.EOF {
				// Reached EOF, wait a bit before trying again
				time.Sleep(500 * time.Millisecond)
				continue
			}
			_, err = chunk_pipe_wr.Write(chunk[:n])
			if err != nil {
				log.Println("write error:", err)
				break
			}
		}
	}()

	// Flusher goroutine
	done := atomic.Bool{}
	defer done.Store(true)
	go func() {
		for !done.Load() {
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			time.Sleep(300 * time.Millisecond)
		}
	}()

	// Go over the log file and format it to html
	scanner := bufio.NewScanner(chunk_pipe_rd)
	byte_counter := 0
	last_date := ""
	last_time := 0.0
	for scanner.Scan() {
		json_line := scanner.Bytes()
		if len(json_line) == 0 {
			continue
		}

		html_lines := formatJsonLogLine(json_line, &last_date, &last_time)
		byte_counter += len(html_lines)
		if !no_truncate && byte_counter > 1024*1024 {
			err := fullWrite(w, "-- LOG TRUNCATED DUE TO LENGTH, <a href=log_stream?notrunc>Click here to keep going</a> --\n")
			if err != nil {
				return
			}
			break
		}
		err = fullWrite(w, html_lines)
		if err != nil {
			return
		}
	}
}

func logTailHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")

	// Get line count from query
	line_count := 5
	if r.URL.Query().Get("lines") != "" {
		_, err := fmt.Sscanf(r.URL.Query().Get("lines"), "%d", &line_count)
		if err != nil {
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
	}

	// Open logfile
	project := r.PathValue("project")
	run := r.PathValue("run")
	test := r.PathValue("test")
	if isDirTraversal(project) || isDirTraversal(run) || isDirTraversal(test) {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}
	log_path := filepath.Join(projectsDir, project, run, fmt.Sprintf("%s.log.jsonl", test))
	log_fd, err := os.Open(log_path)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}
	defer log_fd.Close()

	// Send wrapper HTML
	err = fullWriteBytes(w, logsViewPrefix)
	if err != nil {
		return
	}

	lines := []string{"-- LOG START --\n"}

	// Go over the log file and format it to html
	scanner := bufio.NewScanner(log_fd)
	last_date := ""
	last_time := 0.0
	for scanner.Scan() {
		json_line := scanner.Bytes()
		if len(json_line) == 0 {
			continue
		}

		html_lines := formatJsonLogLine(json_line, &last_date, &last_time)
		// Append to lines, but keep only the last N lines
		lines_start := min(len(lines), max(0, len(lines)-line_count+1))
		lines = append(lines[lines_start:], html_lines)
	}

	// Write the last lines
	for _, line := range lines {
		err = fullWrite(w, line)
		if err != nil {
			return
		}
	}
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
	http.HandleFunc("POST /api/v1/projects/{project}/runs/{run}/status_poll", runStatusPollHandler)
	http.HandleFunc("GET /api/v1/projects/{project}/runs/{run}/status_stream/{worker_id}", runStatusStreamHandler)
	http.HandleFunc("GET /api/v1/projects/{project}/runs/{run}/test/{test}/log_stream", logStreamHandler)
	http.HandleFunc("GET /api/v1/projects/{project}/runs/{run}/test/{test}/log_tail", logTailHandler)
	// TODO: http.HandleFunc("PUT /api/v1/projects/{project}/runs/{run}/test/{test}/status", todo)
	http.HandleFunc("/", NotFoundHandler)
	log.Println("Listening on :8080")
	http.ListenAndServe(":8080", nil)
}
