package main

// Tests for the run plan and the test status endpoints, which the frontend
// uses to show live test results: it loads the status summary, then polls for
// status files that grew and streams the new lines from the last offset.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

var statusTests = []runPlanTestItem{
	testItem("test_a.py::test_one[x86]", map[string]any{"arch": "x86"}),
	testItem("test_a.py::test_one[arm]", map[string]any{"arch": "arm"}),
	testItem("test_a.py::test_two", nil),
}

// A test's full status sequence, as written by the plugin
func passedTestLines(t *testing.T, test string) string {
	return statusLine(t, "type", "start", "test", test) +
		statusLine(t, "type", "setup", "outcome", "passed", "test", test) +
		statusLine(t, "type", "progress", "percentage", 0.5, "test", test) +
		statusLine(t, "type", "call", "outcome", "passed", "test", test) +
		statusLine(t, "type", "teardown", "outcome", "passed", "test", test) +
		statusLine(t, "type", "finish", "outcome", "passed", "test", test)
}

func failedTestLines(t *testing.T, test, exception string) string {
	return statusLine(t, "type", "start", "test", test) +
		statusLine(t, "type", "setup", "outcome", "passed", "test", test) +
		statusLine(t, "type", "call", "outcome", "failed", "test", test, "exception", exception) +
		statusLine(t, "type", "teardown", "outcome", "passed", "test", test) +
		statusLine(t, "type", "finish", "outcome", "failed", "test", test)
}

type statusSummary struct {
	statuses map[string]map[string]any // By test id
	offsets  []int
	resp     testResponse
}

func (e *testEnv) statusSummary(project, run string) statusSummary {
	e.t.Helper()
	resp := e.get("/api/v1/projects/" + project + "/runs/" + run + "/status_summary")
	if resp.StatusCode != http.StatusOK {
		e.t.Fatalf("status_summary: got %d %q", resp.StatusCode, resp.Body)
	}
	summary := statusSummary{statuses: map[string]map[string]any{}, offsets: []int{}, resp: resp}
	for _, line := range strings.Split(strings.TrimSuffix(resp.Body, "\n"), "\n") {
		if line == "" {
			continue
		}
		var status map[string]any
		err := json.Unmarshal([]byte(line), &status)
		if err != nil {
			e.t.Fatalf("status_summary: invalid line %q: %v", line, err)
		}
		test, _ := status["test"].(string)
		if _, ok := summary.statuses[test]; ok {
			e.t.Fatalf("status_summary: test %q appears more than once", test)
		}
		summary.statuses[test] = status
	}
	for _, value := range resp.Header.Values("X-End-Offset") {
		offset, err := strconv.Atoi(value)
		if err != nil {
			e.t.Fatalf("status_summary: bad X-End-Offset %q", value)
		}
		summary.offsets = append(summary.offsets, offset)
	}
	return summary
}

// abbrev formats a value for an error message, eliding the middle of long ones.
func abbrev(v any) string {
	s := fmt.Sprintf("%#v", v)
	if len(s) > 300 {
		s = s[:150] + fmt.Sprintf("...(%d bytes)...", len(s)-300) + s[len(s)-150:]
	}
	return s
}

// assertStatus checks some fields of a test's summarized status.
func assertStatus(t *testing.T, summary statusSummary, test string, kv ...any) {
	t.Helper()
	status, ok := summary.statuses[test]
	if !ok {
		t.Errorf("no status for %s in the summary", test)
		return
	}
	for i := 0; i+1 < len(kv); i += 2 {
		key := kv[i].(string)
		if !reflect.DeepEqual(status[key], kv[i+1]) {
			t.Errorf("%s: %s = %s, want %s (full status: %s)", test, key, abbrev(status[key]), abbrev(kv[i+1]), abbrev(status))
		}
	}
}

func TestRunPlan(t *testing.T) {
	forEachStorage(t, func(t *testing.T, compressed bool) {
		env := newTestEnv(t)
		plan := planJSON(t, 4, map[string][]runPlanTestItem{"test_a": statusTests}, []string{"arch"})
		env.writeDataFile("proj/run/plan.json", plan, compressed)

		resp := env.get("/api/v1/projects/proj/runs/run/plan")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("got %d %q", resp.StatusCode, resp.Body)
		}
		if resp.Body != plan {
			t.Fatalf("plan should be served as-is, got:\n%s", resp.Body)
		}
		if resp.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("Content-Type = %q, want application/json", resp.Header.Get("Content-Type"))
		}
	})

	t.Run("missing", func(t *testing.T) {
		env := newTestEnv(t)
		env.mkdir("proj/run")
		resp := env.get("/api/v1/projects/proj/runs/run/plan")
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("got %d %q, want 404", resp.StatusCode, resp.Body)
		}
	})

	t.Run("compressed and uncompressed", func(t *testing.T) {
		env := newTestEnv(t)
		env.writeRun("proj", "run", 1, statusTests, false)
		env.writeRun("proj", "run", 1, statusTests, true)
		resp := env.get("/api/v1/projects/proj/runs/run/plan")
		if resp.StatusCode != http.StatusInternalServerError || !strings.Contains(resp.Body, "Both compressed (.gz) and uncompressed") {
			t.Fatalf("got %d %q, want a 500 explaining the ambiguity", resp.StatusCode, resp.Body)
		}
	})
}

func TestStatusSummary(t *testing.T) {
	forEachStorage(t, func(t *testing.T, compressed bool) {
		env := newTestEnv(t)
		env.writeRun("proj", "run", 2, statusTests, compressed)
		worker0 := passedTestLines(t, statusTests[0].Id) +
			statusLine(t, "type", "start", "test", statusTests[2].Id) +
			statusLine(t, "type", "progress", "percentage", 0.25, "test", statusTests[2].Id)
		worker1 := failedTestLines(t, statusTests[1].Id, "E   assert 1 == 2\nAssertionError")
		env.writeDataFile("proj/run/status.0.jsonl", worker0, compressed)
		env.writeDataFile("proj/run/status.1.jsonl", worker1, compressed)

		summary := env.statusSummary("proj", "run")
		if len(summary.statuses) != 3 {
			t.Errorf("got %d statuses, want one per test: %v", len(summary.statuses), summary.statuses)
		}
		assertStatus(t, summary, statusTests[0].Id, "type", "finish", "outcome", "passed")
		assertStatus(t, summary, statusTests[1].Id, "type", "finish", "outcome", "failed")
		assertStatus(t, summary, statusTests[2].Id, "type", "progress", "percentage", 0.25)

		// The offsets are in uncompressed bytes, which is what status_stream serves
		if want := []int{len(worker0), len(worker1)}; !reflect.DeepEqual(summary.offsets, want) {
			t.Errorf("X-End-Offset = %v, want %v", summary.offsets, want)
		}
		if ct := summary.resp.Header.Get("Content-Type"); ct != "application/jsonl" {
			t.Errorf("Content-Type = %q, want application/jsonl", ct)
		}
	})

	t.Run("keeps the exception of a failed phase", func(t *testing.T) {
		// The plugin only attaches the exception to the failing phase, but
		// the frontend needs it on the final status
		env := newTestEnv(t)
		env.writeRun("proj", "run", 1, statusTests, false)
		test := statusTests[0].Id
		env.writeFile("proj/run/status.0.jsonl",
			failedTestLines(t, test, "call exception")+
				failedTestLines(t, statusTests[1].Id, "first exception")+
				statusLine(t, "type", "teardown", "outcome", "failed", "test", statusTests[1].Id, "exception", "teardown exception")+
				statusLine(t, "type", "finish", "outcome", "failed", "test", statusTests[1].Id))

		summary := env.statusSummary("proj", "run")
		assertStatus(t, summary, test, "type", "finish", "outcome", "failed", "exception", "call exception")
		// A later exception replaces the earlier one
		assertStatus(t, summary, statusTests[1].Id, "type", "finish", "exception", "teardown exception")
	})

	t.Run("missing status files", func(t *testing.T) {
		// Workers that didn't start yet have no status file
		env := newTestEnv(t)
		env.writeRun("proj", "run", 3, statusTests, false)
		worker1 := passedTestLines(t, statusTests[0].Id)
		env.writeFile("proj/run/status.1.jsonl", worker1)

		summary := env.statusSummary("proj", "run")
		if want := []int{0, len(worker1), 0}; !reflect.DeepEqual(summary.offsets, want) {
			t.Errorf("X-End-Offset = %v, want %v", summary.offsets, want)
		}
		if len(summary.statuses) != 1 {
			t.Errorf("got statuses %v, want only %s", summary.statuses, statusTests[0].Id)
		}
	})

	t.Run("no status files yet", func(t *testing.T) {
		env := newTestEnv(t)
		env.writeRun("proj", "run", 2, statusTests, false)
		summary := env.statusSummary("proj", "run")
		if len(summary.statuses) != 0 || !reflect.DeepEqual(summary.offsets, []int{0, 0}) {
			t.Errorf("got statuses %v and offsets %v, want none and [0 0]", summary.statuses, summary.offsets)
		}
	})

	t.Run("ignores a partially written line", func(t *testing.T) {
		env := newTestEnv(t)
		env.writeRun("proj", "run", 1, statusTests, false)
		complete := statusLine(t, "type", "start", "test", statusTests[0].Id)
		partial := `{"type": "finish", "outcome": "pas`
		env.writeFile("proj/run/status.0.jsonl", complete+partial)

		summary := env.statusSummary("proj", "run")
		assertStatus(t, summary, statusTests[0].Id, "type", "start")
		// The client streams the rest of the line from this offset later
		if want := []int{len(complete)}; !reflect.DeepEqual(summary.offsets, want) {
			t.Errorf("X-End-Offset = %v, want %v (the end of the last complete line)", summary.offsets, want)
		}
	})

	t.Run("CRLF line endings", func(t *testing.T) {
		// The plugin terminates status lines with os.linesep, which is "\r\n"
		// on Windows. The offset must still be the exact byte position the
		// client should continue streaming from.
		env := newTestEnv(t)
		env.writeRun("proj", "run", 1, statusTests, false)
		content := strings.ReplaceAll(passedTestLines(t, statusTests[0].Id), "\n", "\r\n")
		env.writeFile("proj/run/status.0.jsonl", content)

		summary := env.statusSummary("proj", "run")
		assertStatus(t, summary, statusTests[0].Id, "type", "finish", "outcome", "passed")
		if want := []int{len(content)}; !reflect.DeepEqual(summary.offsets, want) {
			t.Errorf("X-End-Offset = %v, want %v (the file size)", summary.offsets, want)
		}
	})

	t.Run("long status lines", func(t *testing.T) {
		// Failure tracebacks (e.g. big assertion diffs) easily exceed 64KB
		env := newTestEnv(t)
		env.writeRun("proj", "run", 1, statusTests, false)
		exception := "E   AssertionError: " + strings.Repeat("x", 200*1024)
		content := failedTestLines(t, statusTests[0].Id, exception) + passedTestLines(t, statusTests[1].Id)
		env.writeFile("proj/run/status.0.jsonl", content)

		summary := env.statusSummary("proj", "run")
		assertStatus(t, summary, statusTests[0].Id, "type", "finish", "outcome", "failed", "exception", exception)
		assertStatus(t, summary, statusTests[1].Id, "type", "finish", "outcome", "passed")
		if want := []int{len(content)}; !reflect.DeepEqual(summary.offsets, want) {
			t.Errorf("X-End-Offset = %v, want %v (the file size)", summary.offsets, want)
		}
	})

	t.Run("many workers", func(t *testing.T) {
		env := newTestEnv(t)
		workers := 40
		tests := []runPlanTestItem{}
		wantOffsets := []int{}
		for i := range workers {
			tests = append(tests, testItem(fmt.Sprintf("test_w.py::test_worker[%d]", i), nil))
		}
		env.writeRun("proj", "run", workers, tests, false)
		for i, test := range tests {
			// Vary the lengths so mixed up offsets are noticed
			content := passedTestLines(t, test.Id) + strings.Repeat(statusLine(t, "type", "start", "test", test.Id), i%3)
			env.writeFile(fmt.Sprintf("proj/run/status.%d.jsonl", i), content)
			wantOffsets = append(wantOffsets, len(content))
		}

		summary := env.statusSummary("proj", "run")
		if !reflect.DeepEqual(summary.offsets, wantOffsets) {
			t.Errorf("X-End-Offset = %v, want %v", summary.offsets, wantOffsets)
		}
		if len(summary.statuses) != workers {
			t.Errorf("got %d statuses, want %d", len(summary.statuses), workers)
		}
	})

	t.Run("lines that aren't test statuses", func(t *testing.T) {
		// Each status file is read in a background goroutine, where a panic
		// takes down the whole server, so this runs in a separate process
		for _, line := range []string{
			`{"type": "session_start", "time": 1722625667.0}`,
			`{"type": "start", "test": 42}`,
			`null`,
		} {
			t.Run(line, func(t *testing.T) {
				env := newProcessEnv(t)
				env.writeRun("proj", "run", 1, statusTests, false)
				env.writeFile("proj/run/status.0.jsonl", line+"\n"+passedTestLines(t, statusTests[0].Id))

				resp := env.get("/api/v1/projects/proj/runs/run/status_summary")
				env.assertAlive()
				if resp.StatusCode != http.StatusOK || !strings.Contains(resp.Body, statusTests[0].Id) {
					t.Fatalf("got %d %q, want the statuses of the valid lines", resp.StatusCode, resp.Body)
				}
			})
		}
	})

	t.Run("missing plan", func(t *testing.T) {
		env := newTestEnv(t)
		env.writeFile("proj/run/status.0.jsonl", passedTestLines(t, statusTests[0].Id))
		resp := env.get("/api/v1/projects/proj/runs/run/status_summary")
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("got %d %q, want 404", resp.StatusCode, resp.Body)
		}
	})

	t.Run("compressed and uncompressed plan", func(t *testing.T) {
		env := newTestEnv(t)
		env.writeRun("proj", "run", 1, statusTests, false)
		env.writeRun("proj", "run", 1, statusTests, true)
		resp := env.get("/api/v1/projects/proj/runs/run/status_summary")
		if resp.StatusCode != http.StatusInternalServerError {
			t.Fatalf("got %d %q, want 500", resp.StatusCode, resp.Body)
		}
	})
}

func TestStatusStream(t *testing.T) {
	content := passedTestLines(t, statusTests[0].Id) + failedTestLines(t, statusTests[1].Id, "boom")

	forEachStorage(t, func(t *testing.T, compressed bool) {
		env := newTestEnv(t)
		env.writeRun("proj", "run", 2, statusTests, compressed)
		env.writeDataFile("proj/run/status.1.jsonl", content, compressed)

		resp := env.get("/api/v1/projects/proj/runs/run/status_stream/1")
		if resp.StatusCode != http.StatusOK || resp.Body != content {
			t.Fatalf("got %d %q, want the whole status file", resp.StatusCode, resp.Body)
		}
		if ct := resp.Header.Get("Content-Type"); ct != "application/jsonl" {
			t.Errorf("Content-Type = %q, want application/jsonl", ct)
		}

		// Ranges are in uncompressed bytes, matching status_summary's offsets
		offset := len(passedTestLines(t, statusTests[0].Id))
		resp = env.get("/api/v1/projects/proj/runs/run/status_stream/1", "Range", fmt.Sprintf("bytes=%d-", offset))
		if resp.StatusCode != http.StatusPartialContent || resp.Body != content[offset:] {
			t.Fatalf("got %d %q, want 206 with the file from offset %d", resp.StatusCode, resp.Body, offset)
		}
		// The frontend checks the start of the Content-Range
		wantRange := fmt.Sprintf("bytes %d-%d/%d", offset, len(content)-1, len(content))
		if got := resp.Header.Get("Content-Range"); got != wantRange {
			t.Fatalf("Content-Range = %q, want %q", got, wantRange)
		}
	})

	t.Run("unknown worker", func(t *testing.T) {
		env := newTestEnv(t)
		env.writeRun("proj", "run", 2, statusTests, false)
		env.writeFile("proj/run/status.0.jsonl", content)
		for _, worker := range []string{"1", "7", "abc"} {
			resp := env.get("/api/v1/projects/proj/runs/run/status_stream/" + worker)
			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("worker %q: got %d %q, want 404", worker, resp.StatusCode, resp.Body)
			}
		}
	})

	t.Run("compressed and uncompressed", func(t *testing.T) {
		env := newTestEnv(t)
		env.writeRun("proj", "run", 1, statusTests, false)
		env.writeDataFile("proj/run/status.0.jsonl", content, false)
		env.writeDataFile("proj/run/status.0.jsonl", content, true)
		resp := env.get("/api/v1/projects/proj/runs/run/status_stream/0")
		if resp.StatusCode != http.StatusInternalServerError {
			t.Fatalf("got %d %q, want 500", resp.StatusCode, resp.Body)
		}
	})
}

type pollResult struct {
	resp    testResponse
	elapsed time.Duration
}

func (e *testEnv) startPoll(project, run string, sizes []int) chan pollResult {
	e.t.Helper()
	body, err := json.Marshal(sizes)
	if err != nil {
		e.t.Fatal(err)
	}
	req := e.newRequest("POST", "/api/v1/projects/"+project+"/runs/"+run+"/status_poll", bytes.NewReader(body))
	result := make(chan pollResult, 1)
	go func() {
		start := time.Now()
		resp, err := e.client.Do(req)
		if err != nil {
			result <- pollResult{resp: testResponse{StatusCode: -1, Body: err.Error()}}
			return
		}
		defer resp.Body.Close()
		buf := new(bytes.Buffer)
		_, _ = buf.ReadFrom(resp.Body)
		result <- pollResult{testResponse{resp.StatusCode, resp.Header, buf.String()}, time.Since(start)}
	}()
	return result
}

func (e *testEnv) poll(project, run string, sizes []int) pollResult {
	e.t.Helper()
	return <-e.startPoll(project, run, sizes)
}

func assertWorkersToCheck(t *testing.T, result pollResult, want []int) {
	t.Helper()
	if result.resp.StatusCode != http.StatusOK {
		t.Fatalf("status_poll: got %d %q", result.resp.StatusCode, result.resp.Body)
	}
	var got statusPollResponse
	err := json.Unmarshal([]byte(result.resp.Body), &got)
	if err != nil {
		t.Fatalf("status_poll: invalid JSON response %q: %v", result.resp.Body, err)
	}
	if !reflect.DeepEqual(got.WorkersToCheck, want) {
		t.Fatalf("workers_to_check = %v, want %v", got.WorkersToCheck, want)
	}
}

func TestStatusPoll(t *testing.T) {
	worker0 := passedTestLines(t, statusTests[0].Id)
	worker1 := passedTestLines(t, statusTests[1].Id)

	forEachStorage(t, func(t *testing.T, compressed bool) {
		env := newTestEnv(t, func(c *greendotsConfig) { c.StatusPoll.TimeoutMs = 5000 })
		env.writeRun("proj", "run", 2, statusTests, compressed)
		env.writeDataFile("proj/run/status.0.jsonl", worker0, compressed)
		env.writeDataFile("proj/run/status.1.jsonl", worker1, compressed)

		// Sizes are compared in uncompressed bytes
		result := env.poll("proj", "run", []int{len(worker0), len(worker1) - 1})
		assertWorkersToCheck(t, result, []int{1})
		result = env.poll("proj", "run", []int{0, 0})
		assertWorkersToCheck(t, result, []int{0, 1})
		if result.elapsed > time.Second {
			t.Errorf("status_poll took %v to report already grown files, want an immediate response", result.elapsed)
		}
	})

	t.Run("waits for a status file to grow", func(t *testing.T) {
		env := newTestEnv(t, func(c *greendotsConfig) { c.StatusPoll.TimeoutMs = 5000 })
		env.writeRun("proj", "run", 2, statusTests, false)
		env.writeFile("proj/run/status.0.jsonl", worker0)
		env.writeFile("proj/run/status.1.jsonl", worker1)

		pending := env.startPoll("proj", "run", []int{len(worker0), len(worker1)})
		select {
		case result := <-pending:
			t.Fatalf("status_poll returned before any status file grew: %d %q", result.resp.StatusCode, result.resp.Body)
		case <-time.After(300 * time.Millisecond):
		}
		env.appendFile("proj/run/status.1.jsonl", statusLine(t, "type", "start", "test", statusTests[2].Id))
		select {
		case result := <-pending:
			assertWorkersToCheck(t, result, []int{1})
		case <-time.After(3 * time.Second):
			t.Fatalf("status_poll did not notice the status file growing")
		}
	})

	t.Run("notices a new status file", func(t *testing.T) {
		env := newTestEnv(t, func(c *greendotsConfig) { c.StatusPoll.TimeoutMs = 5000 })
		env.writeRun("proj", "run", 3, statusTests, false)
		env.writeFile("proj/run/status.0.jsonl", worker0)

		pending := env.startPoll("proj", "run", []int{len(worker0), 0, 0})
		time.Sleep(100 * time.Millisecond)
		env.writeFile("proj/run/status.2.jsonl", worker1)
		select {
		case result := <-pending:
			assertWorkersToCheck(t, result, []int{2})
		case <-time.After(3 * time.Second):
			t.Fatalf("status_poll did not notice the new status file")
		}
	})

	t.Run("times out with no workers to check", func(t *testing.T) {
		// The frontend parses every poll response as JSON, and polls again
		env := newTestEnv(t, func(c *greendotsConfig) { c.StatusPoll.TimeoutMs = 300 })
		env.writeRun("proj", "run", 1, statusTests, false)
		env.writeFile("proj/run/status.0.jsonl", worker0)

		result := env.poll("proj", "run", []int{len(worker0)})
		if result.elapsed < 300*time.Millisecond || result.elapsed > 3*time.Second {
			t.Errorf("status_poll returned after %v, want about the 300ms timeout", result.elapsed)
		}
		assertWorkersToCheck(t, result, []int{})
	})

	t.Run("bad request", func(t *testing.T) {
		env := newTestEnv(t)
		env.writeRun("proj", "run", 1, statusTests, false)
		for _, body := range []string{"", "nope", "{}", "[]", `["a"]`} {
			resp := env.post("/api/v1/projects/proj/runs/run/status_poll", body)
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("body %q: got %d %q, want 400", body, resp.StatusCode, resp.Body)
			}
		}
	})

	t.Run("stops when the client disconnects", func(t *testing.T) {
		// The test server's shutdown fails the test if the handler is still waiting
		env := newTestEnv(t, func(c *greendotsConfig) { c.StatusPoll.TimeoutMs = 60000 })
		env.writeRun("proj", "run", 1, statusTests, false)
		env.writeFile("proj/run/status.0.jsonl", worker0)

		ctx, cancel := context.WithCancel(context.Background())
		req := env.newRequest("POST", "/api/v1/projects/proj/runs/run/status_poll", strings.NewReader(fmt.Sprintf("[%d]", len(worker0))))
		done := make(chan struct{})
		go func() {
			defer close(done)
			resp, err := env.client.Do(req.WithContext(ctx))
			if err == nil {
				resp.Body.Close()
			}
		}()
		time.Sleep(100 * time.Millisecond)
		cancel()
		<-done
	})

	t.Run("does not read status files to size them", func(t *testing.T) {
		// status_poll checks every status file every sleep_ms, for every open
		// browser tab, so it must not read whole (multi-megabyte, on a
		// network drive) files to learn their size. This uses a huge sparse
		// file, which takes many seconds to read but no time to stat.
		env := newProcessEnv(t)
		env.writeRun("proj", "run", 1, statusTests, false)
		makeSparseFile(t, env.path("proj/run/status.0.jsonl"), 256<<30, nil)

		pending := env.startPoll("proj", "run", []int{0})
		select {
		case result := <-pending:
			assertWorkersToCheck(t, result, []int{0})
		case <-time.After(2 * time.Second):
			t.Fatalf("status_poll did not respond within 2s: it reads status files to find their size")
		}
	})
}

// makeSparseFile creates a file of `size` bytes, which are all zeros except
// for `tail` at its end, without allocating disk space for the zeros.
func makeSparseFile(t *testing.T, path string, size int64, tail []byte) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	err = f.Truncate(size - int64(len(tail)))
	if err != nil {
		t.Fatalf("failed to create a sparse file: %v", err)
	}
	_, err = f.WriteAt(tail, size-int64(len(tail)))
	if err != nil {
		t.Fatalf("failed to create a sparse file: %v", err)
	}
}

// TestLiveStatusUpdates follows a running test session the way the frontend
// does: summary, then poll and stream the new lines from the last offsets.
func TestLiveStatusUpdates(t *testing.T) {
	env := newTestEnv(t, func(c *greendotsConfig) { c.StatusPoll.TimeoutMs = 5000 })
	env.writeRun("proj", "run", 2, statusTests, false)
	initial := statusLine(t, "type", "start", "test", statusTests[0].Id) +
		statusLine(t, "type", "setup", "outcome", "passed", "test", statusTests[0].Id)
	env.writeFile("proj/run/status.0.jsonl", initial)

	summary := env.statusSummary("proj", "run")
	assertStatus(t, summary, statusTests[0].Id, "type", "setup")
	if !reflect.DeepEqual(summary.offsets, []int{len(initial), 0}) {
		t.Fatalf("X-End-Offset = %v, want %v", summary.offsets, []int{len(initial), 0})
	}

	// The session continues: worker 0 finishes its test, worker 1 starts one
	appended := statusLine(t, "type", "call", "outcome", "failed", "test", statusTests[0].Id, "exception", "boom") +
		statusLine(t, "type", "finish", "outcome", "failed", "test", statusTests[0].Id)
	worker1 := statusLine(t, "type", "start", "test", statusTests[1].Id)
	env.appendFile("proj/run/status.0.jsonl", appended)
	env.writeFile("proj/run/status.1.jsonl", worker1)

	assertWorkersToCheck(t, env.poll("proj", "run", summary.offsets), []int{0, 1})

	resp := env.get("/api/v1/projects/proj/runs/run/status_stream/0", "Range", fmt.Sprintf("bytes=%d-", summary.offsets[0]))
	if resp.StatusCode != http.StatusPartialContent || resp.Body != appended {
		t.Fatalf("streaming worker 0 from its offset: got %d %q, want exactly the new lines", resp.StatusCode, resp.Body)
	}
	resp = env.get("/api/v1/projects/proj/runs/run/status_stream/1", "Range", "bytes=0-")
	if resp.Body != worker1 {
		t.Fatalf("streaming worker 1: got %d %q, want the whole file", resp.StatusCode, resp.Body)
	}

	// A fresh page load sees the same final state
	summary = env.statusSummary("proj", "run")
	assertStatus(t, summary, statusTests[0].Id, "type", "finish", "outcome", "failed", "exception", "boom")
	assertStatus(t, summary, statusTests[1].Id, "type", "start")
	if want := []int{len(initial) + len(appended), len(worker1)}; !reflect.DeepEqual(summary.offsets, want) {
		t.Fatalf("X-End-Offset = %v, want %v", summary.offsets, want)
	}
}
