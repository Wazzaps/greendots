package main

// Tests against output recorded from the real pytest plugin, to catch the
// server and the plugin disagreeing on the data format.

import (
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

// pluginRunDir is the output of the real plugin for
// testdata/plugin-run/suite/sample_tests.py, see regenerate.sh there.
const pluginRunDir = "testdata/plugin-run/projects"

// copyPluginRun copies the recorded plugin output into the projects dir,
// gzip-compressing every file if `compressed` (like an archived run).
func copyPluginRun(t *testing.T, env *testEnv, compressed bool) {
	t.Helper()
	err := filepath.WalkDir(pluginRunDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(pluginRunDir, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		env.writeDataFile(filepath.ToSlash(rel), string(data), compressed)
		return nil
	})
	if err != nil {
		t.Fatalf("failed to copy the recorded plugin output: %v", err)
	}
}

func readPluginRunFile(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(pluginRunDir, "sample-project/sample-run", name))
	if err != nil {
		t.Fatalf("missing recorded plugin output (run testdata/plugin-run/regenerate.sh): %v", err)
	}
	return string(data)
}

func TestPluginOutput(t *testing.T) {
	const runURL = "/api/v1/projects/sample-project/runs/sample-run"
	const prefix = "suite/sample_tests.py::"

	// The outcome of every test in suite/sample_tests.py
	wantOutcomes := map[string]string{
		prefix + "test_matrix[x86-1]":       "passed",
		prefix + "test_matrix[x86-2]":       "passed",
		prefix + "test_matrix[arm-1]":       "passed",
		prefix + "test_matrix[arm-2]":       "failed",
		prefix + "test_skipped":             "skipped",
		prefix + "test_setup_error":         "failed",
		prefix + "test_special_chars[a/b]":  "passed",
		prefix + "test_special_chars[c<d>]": "passed",
	}
	wantTests := []string{}
	for test := range wantOutcomes {
		wantTests = append(wantTests, test)
	}
	sort.Strings(wantTests)

	forEachStorage(t, func(t *testing.T, compressed bool) {
		env := newTestEnv(t)
		copyPluginRun(t, env, compressed)

		var projects projectsResponse
		env.getJSON("/api/v1/projects", &projects)
		if len(projects.Projects) != 1 || projects.Projects[0].Id != "sample-project" ||
			!reflect.DeepEqual(runIds(projects.Projects[0].Runs), []string{"sample-run"}) {
			t.Fatalf("projects = %+v, want sample-project with sample-run", projects)
		}

		var plan runPlan
		env.getJSON(runURL+"/plan", &plan)
		gotTests := []string{}
		for _, group := range plan.Groups {
			for _, test := range group {
				gotTests = append(gotTests, test.Id)
			}
		}
		sort.Strings(gotTests)
		if !reflect.DeepEqual(gotTests, wantTests) {
			t.Fatalf("plan tests = %v, want %v", gotTests, wantTests)
		}
		if plan.WorkerCount != 2 {
			t.Fatalf("worker_count = %d, want 2 (the tests ran with -n 2)", plan.WorkerCount)
		}

		// The final status of every test, as shown in the results grid
		summary := env.statusSummary("sample-project", "sample-run")
		for test, outcome := range wantOutcomes {
			assertStatus(t, summary, test, "type", "finish", "outcome", outcome)
		}
		for test, exception := range map[string]string{
			prefix + "test_matrix[arm-2]": "AssertionError: arm variant 2 is broken",
			prefix + "test_setup_error":   `RuntimeError: fixture setup failed`,
		} {
			got, _ := summary.statuses[test]["exception"].(string)
			if !strings.Contains(got, exception) {
				t.Errorf("%s: exception = %q, want it to contain %q", test, got, exception)
			}
		}

		// The run is over: offsets are at the end of the status files, from
		// which nothing more will be streamed
		worker0 := readPluginRunFile(t, "status.0.jsonl")
		worker1 := readPluginRunFile(t, "status.1.jsonl")
		if want := []int{len(worker0), len(worker1)}; !reflect.DeepEqual(summary.offsets, want) {
			t.Errorf("X-End-Offset = %v, want the status file sizes %v", summary.offsets, want)
		}
		for worker, content := range []string{worker0, worker1} {
			resp := env.get(runURL + "/status_stream/" + []string{"0", "1"}[worker])
			if resp.StatusCode != http.StatusOK || resp.Body != content {
				t.Errorf("status_stream/%d: got %d, want the status file as written by the plugin", worker, resp.StatusCode)
			}
		}
		assertWorkersToCheck(t, env.poll("sample-project", "sample-run", []int{0, 0}), []int{0, 1})

		// Every test's log can be opened, through its (mangled) log file name
		wantLogs := map[string][]string{
			prefix + "test_matrix[arm-2]": {
				"checking arm variant 2 <b>not bold</b> & co",
				"stdout from arm-2",
				"<greendots_plugin.plugin.ProgressLogger object at ",
				"AssertionError: arm variant 2 is broken",
			},
			prefix + "test_setup_error":         {"RuntimeError: fixture setup failed"},
			prefix + "test_skipped":             {"Skipped: not supported here"},
			prefix + "test_special_chars[a/b]":  {"path is a/b"},
			prefix + "test_special_chars[c<d>]": {"path is c<d>"},
		}
		for _, test := range wantTests {
			resp := env.get(runLogURL("sample-project", "sample-run", test, "log_tail") + "?lines=1000")
			if resp.StatusCode != http.StatusOK {
				t.Errorf("log_tail of %s: got %d %q", test, resp.StatusCode, resp.Body)
				continue
			}
			for _, text := range wantLogs[test] {
				if !strings.Contains(visibleText(resp.Body), text) {
					t.Errorf("log_tail of %s should show %q, but the page renders as:\n%s", test, text, visibleText(resp.Body))
				}
			}

			// Compressed logs end, so log_stream is tested on them in
			// TestLogStream, in a separate process
			if compressed {
				continue
			}
			s := env.openStream(runLogURL("sample-project", "sample-run", test, "log_stream"))
			s.waitFor("-- LOG START --", 5*time.Second)
			for _, text := range wantLogs[test] {
				shown := func(body string) bool { return strings.Contains(visibleText(body), text) }
				s.waitUntil(shown, fmt.Sprintf("log_stream of %s showing %q", test, text), 5*time.Second)
			}
			s.close()
		}
	})
}
