package main

// Tests for the server as a whole: static content, routing, caching headers,
// security, load, and the real binary's command line and config file.

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestVersionEndpoint(t *testing.T) {
	env := newTestEnv(t)
	resp := env.get("/api/v1/version")
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/plain" {
		t.Fatalf("got %d (%s)", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if !strings.HasPrefix(resp.Body, "version\t"+VERSION+"\n") {
		t.Errorf("version info should start with the version, got:\n%s", resp.Body)
	}
	if !strings.Contains(resp.Body, string(commit_info)) {
		t.Errorf("version info should include the commit info %q, got:\n%s", commit_info, resp.Body)
	}
}

func TestConfigEndpoint(t *testing.T) {
	env := newTestEnv(t, func(c *greendotsConfig) {
		c.Client.TestStatus.PollIntervalMs = 1234
		c.AdditionalLogLevels = map[string]logLevel{"TRACE": {Class: "d", Shortname: "T"}}
	})
	var got greendotsConfig
	env.getJSON("/api/v1/config", &got)
	if !reflect.DeepEqual(got, config) {
		t.Errorf("got  %+v\nwant the effective config %+v", got, config)
	}

	resp := env.get("/api/v1/config")
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

func TestDocs(t *testing.T) {
	env := newTestEnv(t)
	for _, path := range []string{"/api/", "/api/v1/"} {
		resp := env.get(path)
		if resp.StatusCode != http.StatusOK || resp.Body != string(apiDocs) {
			t.Errorf("GET %s: got %d %.100q..., want the API docs", path, resp.StatusCode, resp.Body)
		}
		if ct := resp.Header.Get("Content-Type"); ct != "text/plain" {
			t.Errorf("GET %s: Content-Type = %q, want text/plain", path, ct)
		}
	}
}

func readEmbedded(t *testing.T, name string) string {
	t.Helper()
	data, err := fs.ReadFile(dist, name)
	if err != nil {
		t.Fatalf("the embedded frontend is missing %s (run `go generate`): %v", name, err)
	}
	return string(data)
}

func TestFrontend(t *testing.T) {
	env := newTestEnv(t)
	index := readEmbedded(t, "frontend-dist/index.html")

	t.Run("app routes serve the app", func(t *testing.T) {
		for _, path := range []string{"/", "/proj", "/proj/run", "/proj/run/test_a.py::test_one%5Bx86%5D", "/no/such/page"} {
			resp := env.get(path)
			if resp.StatusCode != http.StatusOK || resp.Body != index {
				t.Errorf("GET %s: got %d, want index.html", path, resp.StatusCode)
			}
			if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
				t.Errorf("GET %s: Content-Type = %q, want text/html", path, resp.Header.Get("Content-Type"))
			}
			assertNotCached(t, path, resp)
		}
	})

	t.Run("other methods", func(t *testing.T) {
		resp := env.post("/", "")
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("POST /: got %d, want 404", resp.StatusCode)
		}
		resp = env.do(env.newRequest("HEAD", "/", nil))
		if resp.StatusCode != http.StatusOK {
			t.Errorf("HEAD /: got %d, want 200", resp.StatusCode)
		}
	})

	t.Run("favicon", func(t *testing.T) {
		resp := env.get("/favicon.ico")
		if resp.StatusCode != http.StatusOK || resp.Body != readEmbedded(t, "frontend-dist/favicon.ico") {
			t.Errorf("got %d, want the embedded favicon", resp.StatusCode)
		}
	})

	t.Run("assets", func(t *testing.T) {
		entries, err := fs.ReadDir(dist, "frontend-dist/assets")
		if err != nil || len(entries) == 0 {
			t.Fatalf("the embedded frontend has no assets (run `go generate`): %v", err)
		}
		for _, entry := range entries {
			resp := env.get("/assets/" + entry.Name())
			if resp.StatusCode != http.StatusOK || resp.Body != readEmbedded(t, "frontend-dist/assets/"+entry.Name()) {
				t.Errorf("GET /assets/%s: got %d, want the embedded file", entry.Name(), resp.StatusCode)
			}
			// Asset names contain a content hash, so they may be cached
			if strings.Contains(resp.Header.Get("Cache-Control"), "no-store") {
				t.Errorf("GET /assets/%s: assets should be cacheable", entry.Name())
			}
		}
		resp := env.get("/assets/no-such-asset.js")
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET a missing asset: got %d, want 404", resp.StatusCode)
		}
	})
}

func assertNotCached(t *testing.T, path string, resp testResponse) {
	t.Helper()
	cacheControl := resp.Header.Get("Cache-Control")
	if !strings.Contains(cacheControl, "no-cache") || !strings.Contains(cacheControl, "no-store") {
		t.Errorf("%s: Cache-Control = %q, want no-cache, no-store (the data changes while tests run)", path, cacheControl)
	}
}

func TestAPIResponsesAreNotCached(t *testing.T) {
	env := newTestEnv(t)
	env.writeLogRun()
	env.writeLog(numberedLog(t, 3, 0), false)
	env.writeFile("proj/run/status.0.jsonl", passedTestLines(t, logTestId))

	for _, path := range []string{
		"/api/v1/config",
		"/api/v1/version",
		"/api/v1/projects",
		"/api/v1/projects/proj/runs",
		"/api/v1/projects/proj/runs/run/plan",
		"/api/v1/projects/proj/runs/run/status_summary",
		"/api/v1/projects/proj/runs/run/status_stream/0",
		logURL(logTestId, "log_tail"),
	} {
		resp := env.get(path)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s: got %d", path, resp.StatusCode)
		}
		assertNotCached(t, path, resp)
	}

	resp := env.post("/api/v1/projects/proj/runs/run/status_poll", "[0]")
	assertNotCached(t, "status_poll", resp)

	s := env.openStream(logURL(logTestId, "log_stream"))
	s.waitFor("msg-0003", 5*time.Second)
	assertNotCached(t, "log_stream", testResponse{Header: s.resp.Header})
}

// TestNoAccessOutsideProjectsDir tries to reach files outside the projects
// dir through every path parameter of every endpoint.
func TestNoAccessOutsideProjectsDir(t *testing.T) {
	env := newTestEnv(t)
	env.writeLogRun()
	env.writeLog(numberedLog(t, 3, 0), false)

	// A run outside the projects dir, with secrets in all of its files
	outside := filepath.Join(filepath.Dir(env.dir), "outside")
	secretTest := "test_secret.py::test_secret"
	writeSecret := func(name, content string) {
		path := filepath.Join(outside, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeSecret("metadata.toml", `name = "SECRET"`)
	writeSecret("run/metadata.toml", `name = "SECRET"`)
	writeSecret("run/plan.json", planJSON(t, 1, map[string][]runPlanTestItem{"SECRET": {testItem(secretTest, nil)}}, nil))
	writeSecret("run/status.0.jsonl", statusLine(t, "type", "start", "test", "SECRET"))
	writeSecret("run/"+secretTest+".log.jsonl", logEntry(t, testTime, "INFO", "root", "SECRET"))

	// Ways to name the "outside" project and run from inside the projects dir
	escapes := []struct{ project, run string }{
		{"..%2Foutside", "run"},
		{"%2E%2E%2Foutside", "run"},
		{"%2E%2E", "outside%2Frun"},
		{"proj", "..%2F..%2Foutside%2Frun"},
		{"proj%2F..%2F..%2Foutside", "run"},
	}
	for _, e := range escapes {
		base := "/api/v1/projects/" + e.project + "/runs/" + e.run
		paths := []string{}
		if e.project != "proj" {
			paths = append(paths, "/api/v1/projects/"+e.project+"/runs")
		}
		for _, path := range append(paths,
			base+"/plan",
			base+"/status_summary",
			base+"/status_stream/0",
			base+"/test/"+url.PathEscape(secretTest)+"/log_tail",
			base+"/test/"+url.PathEscape(secretTest)+"/log_stream",
		) {
			resp := env.get(path)
			if strings.Contains(resp.Body, "SECRET") {
				t.Errorf("GET %s leaked a file outside the projects dir: %d %q", path, resp.StatusCode, resp.Body)
			} else if resp.StatusCode != http.StatusNotFound {
				t.Errorf("GET %s: got %d %.200q, want 404", path, resp.StatusCode, resp.Body)
			}
		}
		resp := env.post(base+"/status_poll", "[0]")
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("POST %s/status_poll: got %d %q, want 404", base, resp.StatusCode, resp.Body)
		}
	}

	// Path parameters that only name files inside the run
	for _, worker := range []string{"..%2F..%2F..%2Foutside%2Frun%2Fstatus.0", "%2E%2E", ".0"} {
		resp := env.get("/api/v1/projects/proj/runs/run/status_stream/" + worker)
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("status_stream for worker %q: got %d %q, want 404", worker, resp.StatusCode, resp.Body)
		}
	}

	resp := env.get("/api/v1/projects")
	if strings.Contains(resp.Body, "SECRET") || strings.Contains(resp.Body, "outside") {
		t.Errorf("the projects list leaked a directory outside the projects dir: %q", resp.Body)
	}
}

// TestConcurrentRequests loads the server like many browser tabs watching
// many runs at once. It runs in a separate process because unsynchronized
// shared state can crash the whole server; run the tests with -race to also
// catch data races that didn't (yet) crash it.
func TestConcurrentRequests(t *testing.T) {
	if !raceEnabled && runtime.NumCPU() < 2 {
		t.Fatalf("on a single CPU, unsynchronized access is rarely caught in the act, " +
			"so this test can't check for it: run the tests with -race")
	}
	env := newProcessEnv(t, func(c *greendotsConfig) {
		// Plans expire quickly, so the plan cache is written all the time,
		// while also holding many plans, so it takes a while to go over
		c.Caching.PlanCacheMs = 200
	})
	runs := 300
	for i := range runs {
		run := fmt.Sprintf("run%d", i)
		tests := []runPlanTestItem{testItem("test_a.py::test_one", nil)}
		env.writeRun("proj", run, 2, tests, false)
		env.writeFile("proj/"+run+"/status.0.jsonl", passedTestLines(t, tests[0].Id))
		env.writeFile("proj/"+run+"/"+tests[0].LogFile, numberedLog(t, 10, 0))
	}

	failures := make(chan string, 100)
	var wg sync.WaitGroup
	deadline := time.Now().Add(2 * time.Second)
	for worker := range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rng := rand.New(rand.NewSource(int64(worker)))
			for time.Now().Before(deadline) {
				run := fmt.Sprintf("run%d", rng.Intn(runs))
				path := "/api/v1/projects/proj/runs/" + run + "/status_summary"
				if rng.Intn(2) == 0 {
					path = "/api/v1/projects/proj/runs/" + run + "/test/test_a.py::test_one/log_tail"
				}
				resp, err := env.client.Get(env.baseURL + path)
				if err != nil {
					failures <- fmt.Sprintf("GET %s: %v", path, err)
					return
				}
				resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					failures <- fmt.Sprintf("GET %s: status %d", path, resp.StatusCode)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(failures)
	for failure := range failures {
		t.Errorf("%s", failure)
	}
	env.assertAlive()
}

func TestBinary(t *testing.T) {
	waitForExit := func(t *testing.T, proc *serverProcess) int {
		t.Helper()
		select {
		case <-proc.exited:
			return proc.cmd.ProcessState.ExitCode()
		case <-time.After(10 * time.Second):
			_ = proc.cmd.Process.Kill()
			t.Fatalf("the process did not exit, output:\n%s", proc.output)
			return -1
		}
	}

	t.Run("-version", func(t *testing.T) {
		proc := startServerProcess(t, "-version")
		code := waitForExit(t, proc)
		if code != 0 || !strings.HasPrefix(proc.output.String(), "version\t"+VERSION+"\n") {
			t.Fatalf("exit code %d, output:\n%s", code, proc.output)
		}
	})

	t.Run("missing config file", func(t *testing.T) {
		proc := startServerProcess(t, "-config", filepath.Join(t.TempDir(), "nope.toml"))
		code := waitForExit(t, proc)
		if code == 0 || !strings.Contains(proc.output.String(), "Failed to parse config file") {
			t.Fatalf("exit code %d, output:\n%s", code, proc.output)
		}
	})

	t.Run("invalid config file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.toml")
		if err := os.WriteFile(path, []byte("projects_dir = [unclosed"), 0o644); err != nil {
			t.Fatal(err)
		}
		proc := startServerProcess(t, "-config", path)
		code := waitForExit(t, proc)
		if code == 0 || !strings.Contains(proc.output.String(), "Failed to parse config file") {
			t.Fatalf("exit code %d, output:\n%s", code, proc.output)
		}
	})

	t.Run("serves the configured projects dir", func(t *testing.T) {
		env := newProcessEnv(t, func(c *greendotsConfig) { c.LogTail.DefaultLineCount = 3 })
		env.mkdir("my-project/my-run")
		env.writeLogRun()
		env.writeLog(numberedLog(t, 10, 0), false)

		var projects projectsResponse
		env.getJSON("/api/v1/projects", &projects)
		ids := []string{}
		for _, p := range projects.Projects {
			ids = append(ids, p.Id)
		}
		if !reflect.DeepEqual(ids, []string{"my-project", "proj"}) {
			t.Errorf("projects = %v, want [my-project proj]", ids)
		}

		// Settings come from the config file
		var cfg map[string]any
		env.getJSON("/api/v1/config", &cfg)
		got, _ := json.Marshal(cfg["log_tail"])
		if string(got) != `{"default_line_count":3}` {
			t.Errorf("log_tail config = %s, want default_line_count 3 from the config file", got)
		}
		assertMessages(t, env.get(logURL(logTestId, "log_tail")).Body, messageRange(8, 10))

		// The frontend is served too
		if resp := env.get("/"); resp.Body != readEmbedded(t, "frontend-dist/index.html") {
			t.Errorf("GET / did not serve the frontend")
		}
	})

	t.Run("serves pprof", func(t *testing.T) {
		env := newProcessEnv(t)
		resp := env.get("/debug/pprof/")
		if resp.StatusCode != http.StatusOK || !strings.Contains(resp.Body, "goroutine") {
			t.Fatalf("GET /debug/pprof/: got %d, want the pprof index", resp.StatusCode)
		}
	})
}
