package main

// Tests for the test log viewers: log_stream (the full log, following it as
// it grows) and log_tail (the end of the log, shown when hovering a test).

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
)

// A test id with a "/", which the plugin can't use as a file name as-is, so
// the plan maps it to a mangled log file name
const logTestId = "tests/test_logs.py::test_logging[a/b]"
const logTestFile = "tests_test_logs.py__test_logging_a_b_-ABCDEFGH.log.jsonl"

func (e *testEnv) writeLogRun() {
	e.t.Helper()
	tests := []runPlanTestItem{
		{Id: logTestId, LogFile: logTestFile, Name: "test_logging", Params: map[string]any{"p": "a/b"}},
		{Id: "test_x.py::test_escape", LogFile: "../../secret.log.jsonl", Name: "test_escape"},
		testItem("test_x.py::test_not_started", nil),
	}
	e.writeRun("proj", "run", 1, tests, false)
}

// writeLog writes the log of logTestId.
func (e *testEnv) writeLog(content string, compressed bool) {
	e.t.Helper()
	e.writeDataFile("proj/run/"+logTestFile, content, compressed)
}

// logURL is the URL of a log endpoint for a test in proj/run.
func logURL(test, endpoint string) string {
	return runLogURL("proj", "run", test, endpoint)
}

// runLogURL is the URL of a log endpoint, encoded like the frontend does.
func runLogURL(project, run, test, endpoint string) string {
	return "/api/v1/projects/" + project + "/runs/" + run + "/test/" + url.PathEscape(test) + "/" + endpoint
}

// numberedLog returns a log of `count` entries with messages msg-0001...
// padded to about `size` bytes each.
func numberedLog(t *testing.T, count, size int) string {
	var b strings.Builder
	for i := 1; i <= count; i++ {
		msg := fmt.Sprintf("msg-%04d", i)
		msg += strings.Repeat(".", max(0, size-len(msg)))
		b.WriteString(logEntry(t, testTime+float64(i), "INFO", "root", msg))
	}
	return b.String()
}

// shownMessages returns the numbered messages in a rendered log, in order.
var numberedMessage = regexp.MustCompile(`msg-\d{4}`)

func shownMessages(rendered string) []string {
	return numberedMessage.FindAllString(rendered, -1)
}

func messageRange(from, to int) []string {
	msgs := []string{}
	for i := from; i <= to; i++ {
		msgs = append(msgs, fmt.Sprintf("msg-%04d", i))
	}
	return msgs
}

func assertMessages(t *testing.T, rendered string, want []string) {
	t.Helper()
	got := shownMessages(rendered)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("shown messages = %v,\nwant %v", got, want)
	}
}

var htmlTag = regexp.MustCompile(`<[^>]*>`)

// visibleText approximates what the browser shows for rendered log HTML.
func visibleText(rendered string) string {
	return html.UnescapeString(htmlTag.ReplaceAllString(rendered, ""))
}

// htmlInjectionCases are log lines with HTML special characters, which must
// be shown as text: test logs often contain HTML (e.g. HTTP responses), and
// markup in them would be rendered, or executed, on the greendots page.
var htmlInjectionCases = []struct {
	name    string
	logLine func(t *testing.T) string
	visible string // The text that must be shown
}{
	{
		name: "script tag",
		logLine: func(t *testing.T) string {
			return logEntry(t, testTime, "INFO", "root", `<script>alert("xss")</script>`)
		},
		visible: `<script>alert("xss")</script>`,
	},
	{
		name: "event handler",
		logLine: func(t *testing.T) string {
			return logEntry(t, testTime, "INFO", "root", `<img src=x onerror=alert(1)>`)
		},
		visible: `<img src=x onerror=alert(1)>`,
	},
	{
		name:    "logger name",
		logLine: func(t *testing.T) string { return logEntry(t, testTime, "INFO", "<b>evil</b>", "message") },
		visible: "<b>evil</b> message",
	},
	{
		name:    "level name",
		logLine: func(t *testing.T) string { return logEntry(t, testTime, "<i>LEVEL</i>", "root", "message") },
		visible: "<i>LEVEL</i> root message",
	},
	{
		// Go's json.Marshal (and other encoders) escape "<" as a JSON unicode escape
		name: "JSON-escaped markup",
		logLine: func(t *testing.T) string {
			line, err := json.Marshal(map[string]any{"time": testTime, "level": "INFO", "name": "root", "message": "<script>alert(1)</script>"})
			if err != nil || strings.Contains(string(line), "<") {
				t.Fatalf("expected a log line with only JSON-escaped markup, got %s (%v)", line, err)
			}
			return string(line) + "\n"
		},
		visible: "<script>alert(1)</script>",
	},
	{
		name: "ampersands and comparisons",
		logLine: func(t *testing.T) string {
			return logEntry(t, testTime, "INFO", "root", "x < y && y > z, AT&amp;T &lt;tag&gt;")
		},
		visible: "x < y && y > z, AT&amp;T &lt;tag&gt;",
	},
	{
		name:    "non-JSON line",
		logLine: func(t *testing.T) string { return "<b>raw output</b>\n" },
		visible: "<b>raw output</b>",
	},
}

// assertShownAsText checks that the rendered log shows `visible` literally,
// and that the log content added no markup of its own.
func assertShownAsText(t *testing.T, renderedLog, visible string) {
	t.Helper()
	if !strings.Contains(visibleText(renderedLog), visible) {
		t.Errorf("the log should show %q as text, but the page renders as:\n%s\nfrom HTML:\n%s", visible, visibleText(renderedLog), renderedLog)
	}
	for _, tag := range []string{"<script", "<img", "<b>", "<i>"} {
		if strings.Contains(renderedLog, tag) {
			t.Errorf("log content was injected into the page as markup (%s):\n%s", tag, renderedLog)
		}
	}
}

func TestLogStream(t *testing.T) {
	t.Run("renders the log", func(t *testing.T) {
		env := newTestEnv(t)
		env.writeLogRun()
		env.writeLog(numberedLog(t, 3, 0), false)

		s := env.openStream(logURL(logTestId, "log_stream"))
		body := s.waitFor("msg-0003", 5*time.Second)
		if s.resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", s.resp.StatusCode)
		}
		if ct := s.resp.Header.Get("Content-Type"); ct != "text/html" {
			t.Errorf("Content-Type = %q, want text/html", ct)
		}
		wantStart := string(logsViewPrefix) + "-- LOG START --\n"
		if !strings.HasPrefix(body, wantStart) {
			t.Fatalf("the log should start with the viewer page and a start marker, got:\n%s", body)
		}
		assertMessages(t, body, messageRange(1, 3))
	})

	t.Run("follows the log as it grows", func(t *testing.T) {
		env := newTestEnv(t)
		env.writeLogRun()
		env.writeLog(logEntry(t, testTime, "INFO", "root", "first entry"), false)

		s := env.openStream(logURL(logTestId, "log_stream"))
		s.waitFor("first entry", 5*time.Second)
		env.appendFile("proj/run/"+logTestFile, logEntry(t, testTime+1, "INFO", "root", "second entry"))
		s.waitFor("second entry", 5*time.Second)
	})

	t.Run("log created after opening the test", func(t *testing.T) {
		env := newTestEnv(t)
		env.writeLogRun()
		resp := env.get(logURL(logTestId, "log_stream"))
		// The test hasn't started yet: its log doesn't exist
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("got %d %q, want 404 for a log that wasn't created yet", resp.StatusCode, resp.Body)
		}
	})

	t.Run("truncates long logs", func(t *testing.T) {
		env := newTestEnv(t, func(c *greendotsConfig) { c.StatusStream.LogTruncationSize = 5000 })
		env.writeLogRun()
		env.writeLog(numberedLog(t, 100, 100), false)

		s := env.openStream(logURL(logTestId, "log_stream"))
		body := s.waitFor("-- LOG TRUNCATED DUE TO LENGTH, <a href=log_stream?notrunc>Click here to keep going</a> --\n", 5*time.Second)
		if strings.Contains(body, "msg-0100") {
			t.Fatalf("the log should be cut at about 5000 bytes, got:\n%s", body)
		}
		if !strings.Contains(body, "msg-0001") {
			t.Fatalf("the log should show its start before truncating, got:\n%s", body)
		}
	})

	t.Run("ends the response after truncating", func(t *testing.T) {
		// Nothing follows the truncation notice, so the browser should not
		// keep loading the page
		env := newTestEnv(t, func(c *greendotsConfig) { c.StatusStream.LogTruncationSize = 5000 })
		env.writeLogRun()
		env.writeLog(numberedLog(t, 100, 100), false)

		s := env.openStream(logURL(logTestId, "log_stream"))
		body := s.waitForEnd(3 * time.Second)
		if !strings.HasSuffix(body, "Click here to keep going</a> --\n") {
			t.Fatalf("the response should end with the truncation notice, got:\n%s", body)
		}
	})

	t.Run("notrunc shows the whole log", func(t *testing.T) {
		env := newTestEnv(t, func(c *greendotsConfig) { c.StatusStream.LogTruncationSize = 5000 })
		env.writeLogRun()
		env.writeLog(numberedLog(t, 100, 100), false)

		s := env.openStream(logURL(logTestId, "log_stream") + "?notrunc")
		body := s.waitFor("msg-0100", 5*time.Second)
		if strings.Contains(body, "LOG TRUNCATED") {
			t.Fatalf("?notrunc should not truncate the log")
		}
		assertMessages(t, body, messageRange(1, 100))
	})

	t.Run("long lines", func(t *testing.T) {
		env := newTestEnv(t)
		env.writeLogRun()
		long := "start-" + strings.Repeat("y", 1024*1024) + "-end"
		env.writeLog(logEntry(t, testTime, "INFO", "root", long)+logEntry(t, testTime+1, "INFO", "root", "after the long line"), false)

		s := env.openStream(logURL(logTestId, "log_stream") + "?notrunc")
		body := s.waitFor("after the long line", 5*time.Second)
		if !strings.Contains(body, long) {
			t.Fatalf("the long line should be shown in full")
		}
	})

	t.Run("non-JSON lines", func(t *testing.T) {
		env := newTestEnv(t)
		env.writeLogRun()
		env.writeLog(logEntry(t, testTime, "INFO", "root", "before")+"Segmentation fault (core dumped)\n", false)

		s := env.openStream(logURL(logTestId, "log_stream"))
		body := s.waitFor("Segmentation fault (core dumped)", 5*time.Second)
		if !strings.Contains(body, `<span class=l>unknown</span> Segmentation fault (core dumped)`) {
			t.Fatalf("a non-JSON line should be shown as coming from an unknown logger, got:\n%s", body)
		}
	})

	for _, c := range htmlInjectionCases {
		t.Run("HTML in log: "+c.name, func(t *testing.T) {
			env := newTestEnv(t)
			env.writeLogRun()
			env.writeLog(c.logLine(t)+logEntry(t, testTime+1, "INFO", "root", "end of log"), false)

			s := env.openStream(logURL(logTestId, "log_stream"))
			body := s.waitFor("end of log", 5*time.Second)
			assertShownAsText(t, strings.TrimPrefix(body, string(logsViewPrefix)), c.visible)
		})
	}

	t.Run("stops when the client disconnects", func(t *testing.T) {
		// The test server's shutdown fails the test if the handler keeps
		// following the log after the client is gone
		env := newTestEnv(t)
		env.writeLogRun()
		env.writeLog(logEntry(t, testTime, "INFO", "root", "entry"), false)

		s := env.openStream(logURL(logTestId, "log_stream"))
		s.waitFor("entry", 5*time.Second)
		s.close()
	})

	t.Run("compressed log", func(t *testing.T) {
		// A compressed log is complete, so the whole log is sent and the
		// response ends. This runs in a separate process: if the end of the
		// log is missed, its handler spins forever.
		env := newProcessEnv(t)
		env.writeLogRun()
		env.writeLog(numberedLog(t, 50, 0), true)

		s := env.openStream(logURL(logTestId, "log_stream"))
		body := s.waitForEnd(5 * time.Second)
		if !strings.HasPrefix(body, string(logsViewPrefix)+"-- LOG START --\n") {
			t.Fatalf("the log should start with the viewer page and a start marker, got:\n%s", body)
		}
		assertMessages(t, body, messageRange(1, 50))
	})

	testLogErrors(t, "log_stream")
}

// testLogErrors tests the error responses shared by both log endpoints.
func testLogErrors(t *testing.T, endpoint string) {
	t.Run("unknown test", func(t *testing.T) {
		env := newTestEnv(t)
		env.writeLogRun()
		env.writeLog(numberedLog(t, 3, 0), false)
		resp := env.get(logURL("test_x.py::test_unknown", endpoint))
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("got %d %q, want 404", resp.StatusCode, resp.Body)
		}
	})

	t.Run("unknown run", func(t *testing.T) {
		env := newTestEnv(t)
		resp := env.get("/api/v1/projects/proj/runs/nope/test/" + url.PathEscape(logTestId) + "/" + endpoint)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("got %d %q, want 404", resp.StatusCode, resp.Body)
		}
	})

	t.Run("log file outside the run directory", func(t *testing.T) {
		env := newTestEnv(t)
		env.writeLogRun()
		env.writeFile("secret.log.jsonl", logEntry(t, testTime, "INFO", "root", "secret"))
		resp := env.get(logURL("test_x.py::test_escape", endpoint))
		if resp.StatusCode != http.StatusNotFound || strings.Contains(resp.Body, "secret") {
			t.Fatalf("got %d %q, want 404", resp.StatusCode, resp.Body)
		}
	})

	t.Run("compressed and uncompressed", func(t *testing.T) {
		env := newTestEnv(t)
		env.writeLogRun()
		env.writeLog(numberedLog(t, 3, 0), false)
		env.writeLog(numberedLog(t, 3, 0), true)
		resp := env.get(logURL(logTestId, endpoint))
		if resp.StatusCode != http.StatusInternalServerError || !strings.Contains(resp.Body, "Both compressed (.gz) and uncompressed") {
			t.Fatalf("got %d %q, want a 500 explaining the ambiguity", resp.StatusCode, resp.Body)
		}
	})
}

func TestLogTail(t *testing.T) {
	forEachStorage(t, func(t *testing.T, compressed bool) {
		t.Run("shows the last lines", func(t *testing.T) {
			env := newTestEnv(t)
			env.writeLogRun()
			env.writeLog(numberedLog(t, 50, 0), compressed)

			resp := env.get(logURL(logTestId, "log_tail"))
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("got %d %q", resp.StatusCode, resp.Body)
			}
			if ct := resp.Header.Get("Content-Type"); ct != "text/html" {
				t.Errorf("Content-Type = %q, want text/html", ct)
			}
			wantStart := string(tailLogsViewPrefix) + string(logsViewPrefix)
			if !strings.HasPrefix(resp.Body, wantStart) {
				t.Fatalf("the tail should start with the tail viewer page, got:\n%s", resp.Body)
			}
			if strings.Contains(resp.Body, "-- LOG START --") {
				t.Errorf("the start marker should be cut off with the start of the log")
			}
			// default_line_count is 25
			assertMessages(t, resp.Body, messageRange(26, 50))
		})

		t.Run("lines parameter", func(t *testing.T) {
			env := newTestEnv(t)
			env.writeLogRun()
			env.writeLog(numberedLog(t, 50, 0), compressed)
			resp := env.get(logURL(logTestId, "log_tail") + "?lines=5")
			assertMessages(t, resp.Body, messageRange(46, 50))
		})

		t.Run("short log", func(t *testing.T) {
			env := newTestEnv(t)
			env.writeLogRun()
			env.writeLog(numberedLog(t, 3, 0), compressed)
			resp := env.get(logURL(logTestId, "log_tail"))
			if !strings.HasPrefix(resp.Body, string(tailLogsViewPrefix)+string(logsViewPrefix)+"-- LOG START --\n") {
				t.Fatalf("a log shorter than the tail should show the start marker, got:\n%s", resp.Body)
			}
			assertMessages(t, resp.Body, messageRange(1, 3))
		})

		t.Run("large log", func(t *testing.T) {
			// Only the end of large logs is read; the first, partial line in
			// it must not be shown as garbage
			env := newTestEnv(t)
			env.writeLogRun()
			env.writeLog(numberedLog(t, 3000, 200), compressed)
			resp := env.get(logURL(logTestId, "log_tail"))
			assertMessages(t, resp.Body, messageRange(2976, 3000))
			if strings.Contains(resp.Body, "unknown") || strings.Contains(resp.Body, "-- LOG START --") {
				t.Fatalf("the tail should only contain whole log entries, got:\n%s", resp.Body)
			}
		})

		t.Run("long lines", func(t *testing.T) {
			env := newTestEnv(t)
			env.writeLogRun()
			long := logEntry(t, testTime, "INFO", "root", "long-"+strings.Repeat("y", 100*1024))
			env.writeLog(numberedLog(t, 5, 0)+long+numberedLog(t, 3, 0), compressed)
			resp := env.get(logURL(logTestId, "log_tail") + "?lines=3")
			// The 3 entries after the long one (renumbered from 1)
			assertMessages(t, resp.Body, messageRange(1, 3))
		})

		for _, c := range htmlInjectionCases {
			t.Run("HTML in log: "+c.name, func(t *testing.T) {
				env := newTestEnv(t)
				env.writeLogRun()
				env.writeLog(c.logLine(t)+logEntry(t, testTime+1, "INFO", "root", "end of log"), compressed)
				resp := env.get(logURL(logTestId, "log_tail"))
				prefix := string(tailLogsViewPrefix) + string(logsViewPrefix)
				assertShownAsText(t, strings.TrimPrefix(resp.Body, prefix), c.visible)
			})
		}
	})

	t.Run("invalid lines parameter", func(t *testing.T) {
		env := newTestEnv(t)
		env.writeLogRun()
		env.writeLog(numberedLog(t, 3, 0), false)
		resp := env.get(logURL(logTestId, "log_tail") + "?lines=abc")
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("got %d %q, want 400", resp.StatusCode, resp.Body)
		}
	})

	t.Run("log not created yet", func(t *testing.T) {
		env := newTestEnv(t)
		env.writeLogRun()
		resp := env.get(logURL("test_x.py::test_not_started", "log_tail"))
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("got %d %q, want 404", resp.StatusCode, resp.Body)
		}
	})

	t.Run("does not read the whole log", func(t *testing.T) {
		// The tail is fetched every time a test is hovered, and logs can be
		// huge, so only their end may be read. This uses a huge sparse file,
		// which takes many seconds to read through.
		env := newProcessEnv(t)
		env.writeLogRun()
		tail := "\n" + numberedLog(t, 1000, 200)
		makeSparseFile(t, env.path("proj/run/"+logTestFile), 256<<30, []byte(tail))

		result := make(chan testResponse, 1)
		go func() {
			resp, err := env.client.Get(env.baseURL + logURL(logTestId, "log_tail"))
			if err != nil {
				result <- testResponse{StatusCode: -1, Body: err.Error()}
				return
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			result <- testResponse{StatusCode: resp.StatusCode, Body: string(body)}
		}()
		select {
		case resp := <-result:
			assertMessages(t, resp.Body, messageRange(976, 1000))
		case <-time.After(2 * time.Second):
			t.Fatalf("log_tail did not respond within 2s: it reads through the whole log file")
		}
	})

	testLogErrors(t, "log_tail")
}
