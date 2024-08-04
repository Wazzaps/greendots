package main

import (
	"bufio"
	"embed"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/andanhm/go-prettytime"
)

const VERSION = "0.1.0"

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
	Id     string                 `json:"id"`
	Name   string                 `json:"name"`
	Params map[string]interface{} `json:"params"`
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

type statusPollConfig struct {
	SleepMs int `toml:"sleep_ms" json:"sleep_ms"`
}

type statusStreamConfig struct {
	ChunkSize         int `toml:"chunk_size" json:"chunk_size"`
	EofSleepMs        int `toml:"eof_sleep_ms" json:"eof_sleep_ms"`
	FlushSleepMs      int `toml:"flush_sleep_ms" json:"flush_sleep_ms"`
	LogTruncationSize int `toml:"log_truncation_size" json:"log_truncation_size"`
}

type logTailConfig struct {
	DefaultLineCount int `toml:"default_line_count" json:"default_line_count"`
}

type logLevel struct {
	Class     string `toml:"class" json:"class"`
	Shortname string `toml:"shortname" json:"shortname"`
}

type clientTestStatusConfig struct {
	PollIntervalMs  int `toml:"poll_interval_ms" json:"poll_interval_ms"`
	BackoffMs       int `toml:"backoff_ms" json:"backoff_ms"`
	BackoffJitterMs int `toml:"backoff_jitter_ms" json:"backoff_jitter_ms"`
}

type clientConfig struct {
	TestStatus clientTestStatusConfig `toml:"test_status" json:"test_status"`
}

type greendotsConfig struct {
	StatusPoll          statusPollConfig    `toml:"status_poll" json:"status_poll"`
	StatusStream        statusStreamConfig  `toml:"status_stream" json:"status_stream"`
	LogTail             logTailConfig       `toml:"log_tail" json:"log_tail"`
	Client              clientConfig        `toml:"client" json:"client"`
	AdditionalLogLevels map[string]logLevel `toml:"additional_log_levels" json:"additional_log_levels"`
	ProjectsDir         string              `toml:"projects_dir" json:"projects_dir"`
	ListenAddress       string              `toml:"listen_address" json:"listen_address"`
}

var config = greendotsConfig{
	StatusPoll: statusPollConfig{
		SleepMs: 1000,
	},
	StatusStream: statusStreamConfig{
		ChunkSize:         1024,
		EofSleepMs:        500,
		FlushSleepMs:      300,
		LogTruncationSize: 1024 * 1024,
	},
	LogTail: logTailConfig{
		DefaultLineCount: 5,
	},
	Client: clientConfig{
		TestStatus: clientTestStatusConfig{
			PollIntervalMs:  500,
			BackoffMs:       2000,
			BackoffJitterMs: 1000,
		},
	},
	ProjectsDir:   "",
	ListenAddress: ":8080",
}

// -- Helpers --

func getProjectRuns(project string) ([]run, error) {
	projectPath := filepath.Join(config.ProjectsDir, project)

	entries, err := os.ReadDir(projectPath)
	if err != nil {
		return nil, err
	}

	runs := make([]run, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}

		info, err := os.Stat(filepath.Join(projectPath, e.Name()))
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
		return runs[i].CreatedAt > runs[j].CreatedAt
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

func getFullVersion() string {
	build_info_str := ""
	build_info, ok := debug.ReadBuildInfo()
	if ok {
		build_info_str = fmt.Sprintf("%v", build_info)
	}
	return fmt.Sprintf("version\t%s\n%s", VERSION, build_info_str)
}

func versionHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	fullWrite(w, getFullVersion())
}

func projectsListHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	projectsDir, err := os.ReadDir(config.ProjectsDir)
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
	planPath := filepath.Join(config.ProjectsDir, project, run, "plan.json")
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

	planPath := filepath.Join(config.ProjectsDir, project, run, "plan.json")
	planFd, err := os.Open(planPath)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}
	defer planFd.Close()

	var plan runPlan
	err = json.NewDecoder(planFd).Decode(&plan)
	if err != nil {
		log.Printf("Failed to decode plan file '%s': %v", planPath, err)
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}

	// Read all status files, save the last line per test
	final_statuses := make(map[string]interface{})
	for status_idx := range plan.WorkerCount {
		statusSummaryPath := filepath.Join(
			config.ProjectsDir, project, run,
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
			statusPath := filepath.Join(config.ProjectsDir, project, run, fmt.Sprintf("status.%d.jsonl", i))
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
			time.Sleep(time.Duration(config.StatusPoll.SleepMs) * time.Millisecond)
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
	statusStreamPath := filepath.Join(config.ProjectsDir, project, run, fmt.Sprintf("status.%s.jsonl", worker_id))
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
	case "DEBUG":
		severity = "<span class=d>D</span>"
		severity_class = "d"
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
	case "CRITICAL":
		severity = "<span class=c>C</span>"
		severity_class = "c"
	default:
		if val, ok := config.AdditionalLogLevels[log_line.Level]; ok {
			severity = fmt.Sprintf("<span class=i>%s</span>", val.Shortname)
			severity_class = val.Class
		} else {
			severity = fmt.Sprintf("<span class=i>%s</span>", log_line.Level)
			severity_class = "i"
		}
	}
	ts := time.Unix(int64(log_line.Time), int64((log_line.Time-float64(int(log_line.Time)))*1e9))
	date := ts.Format("2006-01-02")
	if date != *last_date {
		html_lines += fmt.Sprintf("<span class=date>------- %s -------\n</span>", date)
		*last_date = date
	}

	html_lines += fmt.Sprintf(
		"<span class=%s><span class=t>%s </span><span class=s>%s </span><span class=l>%s</span> %s\n</span>",
		severity_class, ts.Format("15:04:05"), severity, log_line.Name, log_line.Message,
	)

	return html_lines
}

func logStreamHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")

	no_truncate := r.URL.Query().Has("notrunc")

	// Open logfile
	project := r.PathValue("project")
	run := r.PathValue("run")
	test := strings.ReplaceAll(r.PathValue("test"), "/", "_")
	if isDirTraversal(project) || isDirTraversal(run) || isDirTraversal(test) {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}
	log_path := filepath.Join(config.ProjectsDir, project, run, fmt.Sprintf("%s.log.jsonl", test))
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
		chunk := make([]byte, config.StatusStream.ChunkSize)
		for {
			n, err := log_fd.Read(chunk)
			if err != nil && err != io.EOF {
				chunk_pipe_wr.CloseWithError(err)
				break
			}
			if n == 0 || err == io.EOF {
				// Reached EOF, wait a bit before trying again
				time.Sleep(time.Duration(config.StatusStream.EofSleepMs) * time.Millisecond)
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
			time.Sleep(time.Duration(config.StatusStream.FlushSleepMs) * time.Millisecond)
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
		if !no_truncate && byte_counter > config.StatusStream.LogTruncationSize {
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
	line_count := config.LogTail.DefaultLineCount
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
	test := strings.ReplaceAll(r.PathValue("test"), "/", "_")
	if isDirTraversal(project) || isDirTraversal(run) || isDirTraversal(test) {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}
	log_path := filepath.Join(config.ProjectsDir, project, run, fmt.Sprintf("%s.log.jsonl", test))
	log_fd, err := os.Open(log_path)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}
	defer log_fd.Close()

	// TODO: Seek to last 128KB of log file, it will probably be enough

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

func configHandler(w http.ResponseWriter, r *http.Request) {
	err := json.NewEncoder(w).Encode(config)
	if err != nil {
		log.Printf("%s %s: json encoder: %v", r.Method, r.URL.Path, err)
		return
	}
}

//go:generate ./copy_frontend_dist.sh
//go:embed frontend-dist/*
var dist embed.FS

func serveFrontendHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" || r.Method == "HEAD" {
		http.ServeFileFS(w, r, dist, "frontend-dist/index.html")
	} else {
		log.Printf("%s %s: not found", r.Method, r.URL.Path)
		http.Error(w, "Not found", http.StatusNotFound)
	}
}

func serveIconHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFileFS(w, r, dist, "frontend-dist/favicon.ico")
}

func nocache(handler func(w http.ResponseWriter, r *http.Request)) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Cache-Control", "no-cache, no-store, no-transform, must-revalidate, private, max-age=0")
		handler(w, r)
	}
}

func main() {
	var showVersion bool
	flag.BoolVar(&showVersion, "version", false, "Show the version and exit")

	var configPath string
	flag.StringVar(&configPath, "config", "config.toml", "The path to the server configuration")

	flag.Parse()

	if showVersion {
		fmt.Print(getFullVersion())
		return
	}

	_, err := toml.DecodeFile(configPath, &config)
	if err != nil {
		log.Fatalln("Failed to parse config file:", err)
	}

	sub, err := fs.Sub(dist, "frontend-dist")
	if err != nil {
		log.Fatalln("Failed to parse config file:", err)
	}
	http.Handle("GET /assets/", http.FileServer(http.FS(sub)))

	http.HandleFunc("GET /favicon.ico", nocache(serveIconHandler))

	http.HandleFunc("GET /api/v1/config", nocache(configHandler))
	http.HandleFunc("GET /api/v1/version", nocache(versionHandler))
	http.HandleFunc("GET /api/v1/projects", nocache(projectsListHandler))
	http.HandleFunc("GET /api/v1/projects/{project}/runs", nocache(projectRunsHandler))
	http.HandleFunc("GET /api/v1/projects/{project}/runs/{run}/plan", nocache(runPlanHandler))
	http.HandleFunc("GET /api/v1/projects/{project}/runs/{run}/status_summary", nocache(runStatusSummaryHandler))
	http.HandleFunc("POST /api/v1/projects/{project}/runs/{run}/status_poll", nocache(runStatusPollHandler))
	http.HandleFunc("GET /api/v1/projects/{project}/runs/{run}/status_stream/{worker_id}", nocache(runStatusStreamHandler))
	http.HandleFunc("GET /api/v1/projects/{project}/runs/{run}/test/{test}/log_stream", nocache(logStreamHandler))
	http.HandleFunc("GET /api/v1/projects/{project}/runs/{run}/test/{test}/log_tail", nocache(logTailHandler))
	http.HandleFunc("GET /api/", docsHandler)

	// TODO: use etag caching instead of nocache
	// the assets doesn't need nocache nor etag since it has hashes in the name
	// of the files so it handles it on its own
	http.HandleFunc("/", nocache(serveFrontendHandler))
	log.Println("Listening on :8080")
	err = http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatalf("failed to start server: %v", err)
	}
}
