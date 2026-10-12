package main

// Unit tests for the helpers the handlers are built on.

import (
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
)

func TestIsDirTraversal(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"project", false},
		{"my-run_2024.01.01", false},
		{"test_mod.py::test_x[a-1].log.jsonl", false},
		{"a..b", false},
		{"..", true},
		{".", true},
		{".hidden", true},
		{"../secret", true},
		{"a/b", true},
		{"a/../../b", true},
		// Unreachable through the router today (empty path segments never
		// match a route), but an empty name must not be accepted either
		{"", true},
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("%q", c.path), func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("isDirTraversal(%q) panicked: %v", c.path, r)
				}
			}()
			got := isDirTraversal(c.path)
			if got != c.want {
				t.Fatalf("isDirTraversal(%q) = %v, want %v", c.path, got, c.want)
			}
		})
	}
}

func TestCheckFileZipped(t *testing.T) {
	env := newTestEnv(t)
	env.writeFile("plain.json", "{}")
	env.writeDataFile("compressed.json", "{}", true)
	env.writeFile("both.json", "{}")
	env.writeDataFile("both.json", "{}", true)

	t.Run("plain", func(t *testing.T) {
		path, zipped, err := checkFileZipped(env.path("plain.json"))
		if err != nil || zipped || path != env.path("plain.json") {
			t.Fatalf("got (%q, %v, %v), want the plain file", path, zipped, err)
		}
	})
	t.Run("compressed", func(t *testing.T) {
		path, zipped, err := checkFileZipped(env.path("compressed.json"))
		if err != nil || !zipped || path != env.path("compressed.json.gz") {
			t.Fatalf("got (%q, %v, %v), want the .gz file", path, zipped, err)
		}
	})
	t.Run("both", func(t *testing.T) {
		_, _, err := checkFileZipped(env.path("both.json"))
		if !isCompressedAndUncompressedError(err) {
			t.Fatalf("got error %v, want a compressed-and-uncompressed error", err)
		}
		if !strings.Contains(err.Error(), env.path("both.json")) {
			t.Fatalf("error %q should name the ambiguous file", err)
		}
	})
	t.Run("missing", func(t *testing.T) {
		_, _, err := checkFileZipped(env.path("missing.json"))
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("got error %v, want os.ErrNotExist", err)
		}
		if isCompressedAndUncompressedError(err) {
			t.Fatalf("a missing file is not a compressed-and-uncompressed error")
		}
	})
}

func TestOpenDataFile(t *testing.T) {
	content := strings.Repeat("some data file content\n", 1000)
	forEachStorage(t, func(t *testing.T, compressed bool) {
		env := newTestEnv(t)
		env.writeDataFile("data.jsonl", content, compressed)

		reader, realPath, err := openDataFile(env.path("data.jsonl"))
		if err != nil {
			t.Fatalf("openDataFile: %v", err)
		}
		got, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if string(got) != content {
			t.Fatalf("read %d bytes of unexpected content, want the original %d bytes", len(got), len(content))
		}
		wantPath := env.path("data.jsonl")
		if compressed {
			wantPath += ".gz"
		}
		if realPath != wantPath {
			t.Fatalf("real path = %q, want %q", realPath, wantPath)
		}
		err = reader.Close()
		if err != nil {
			t.Fatalf("close: %v", err)
		}
	})
}

func TestOpenDataFileErrors(t *testing.T) {
	env := newTestEnv(t)
	env.writeFile("corrupt.jsonl.gz", "this is not gzip data")
	env.writeFile("both.jsonl", "")
	env.writeDataFile("both.jsonl", "", true)

	_, _, err := openDataFile(env.path("corrupt.jsonl"))
	if err == nil {
		t.Errorf("opening a corrupt .gz file should fail")
	}
	_, _, err = openDataFile(env.path("missing.jsonl"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("opening a missing file: got %v, want os.ErrNotExist", err)
	}
	_, _, err = openDataFile(env.path("both.jsonl"))
	if !isCompressedAndUncompressedError(err) {
		t.Errorf("opening an ambiguous file: got %v, want a compressed-and-uncompressed error", err)
	}
}

func TestDataFileSize(t *testing.T) {
	content := strings.Repeat("x", 12345)
	forEachStorage(t, func(t *testing.T, compressed bool) {
		env := newTestEnv(t)
		env.writeDataFile("status.0.jsonl", content, compressed)
		size, err := dataFileSize(env.path("status.0.jsonl"))
		if err != nil {
			t.Fatalf("dataFileSize: %v", err)
		}
		// For compressed files this is the decompressed size, since that's
		// what the status offsets refer to
		if size != int64(len(content)) {
			t.Fatalf("size = %d, want %d", size, len(content))
		}
	})

	t.Run("missing", func(t *testing.T) {
		env := newTestEnv(t)
		_, err := dataFileSize(env.path("status.0.jsonl"))
		if !os.IsNotExist(err) {
			t.Fatalf("got %v, want a not-exist error (status_poll relies on it to skip missing workers)", err)
		}
	})
}

func TestFormatJsonLogLine(t *testing.T) {
	when := testTime + 0.25
	clock := time.Unix(int64(testTime), 0).Format("15:04:05")
	date := time.Unix(int64(testTime), 0).Format("2006-01-02")

	format := func(line string) string {
		lastDate := date // Suppress the date separator
		lastTime := 0.0
		return formatJsonLogLine([]byte(line), &lastDate, &lastTime)
	}

	t.Run("exact output", func(t *testing.T) {
		got := format(logEntry(t, when, "INFO", "root", "hello world"))
		want := `<span class="i l-root"><span class=t>` + clock + ` </span>` +
			`<span class=s><span class=i>I</span> </span><span class=l>root</span> hello world` + "\n</span>"
		if got != want {
			t.Fatalf("got:\n%s\nwant:\n%s", got, want)
		}
	})

	t.Run("levels", func(t *testing.T) {
		cases := []struct{ level, class, short string }{
			{"DEBUG", "d", "D"},
			{"INFO", "i", "I"},
			{"WARN", "w", "W"},
			{"WARNING", "w", "W"},
			{"ERROR", "e", "E"},
			{"CRITICAL", "c", "C"},
		}
		for _, c := range cases {
			got := format(logEntry(t, when, c.level, "root", "msg"))
			wantClass := fmt.Sprintf(`<span class="%s l-root">`, c.class)
			wantSeverity := fmt.Sprintf(`<span class=s><span class=%s>%s</span> </span>`, c.class, c.short)
			if !strings.HasPrefix(got, wantClass) || !strings.Contains(got, wantSeverity) {
				t.Errorf("level %s: got %q, want class %q and severity %q", c.level, got, c.class, c.short)
			}
		}
	})

	t.Run("additional levels from config", func(t *testing.T) {
		newTestEnv(t, func(c *greendotsConfig) {
			c.AdditionalLogLevels = map[string]logLevel{"TRACE": {Class: "d", Shortname: "T"}}
		})
		got := format(logEntry(t, when, "TRACE", "root", "msg"))
		if !strings.HasPrefix(got, `<span class="d l-root">`) || !strings.Contains(got, `<span class=i>T</span>`) {
			t.Fatalf("got %q, want class d with shortname T", got)
		}
	})

	t.Run("unknown level", func(t *testing.T) {
		got := format(logEntry(t, when, "NOTICE", "root", "msg"))
		if !strings.HasPrefix(got, `<span class="i l-root">`) || !strings.Contains(got, `<span class=i>NOTICE</span>`) {
			t.Fatalf("got %q, want an info-styled line showing the level name", got)
		}
	})

	t.Run("logger name", func(t *testing.T) {
		got := format(logEntry(t, when, "INFO", "my.app logger", "msg"))
		// Sanitized for the CSS class used by the logger filter, verbatim in the text
		if !strings.HasPrefix(got, `<span class="i l-my_app_logger">`) {
			t.Errorf("got %q, want the logger class to be sanitized", got)
		}
		if !strings.Contains(got, `<span class=l>my.app logger</span>`) {
			t.Errorf("got %q, want the logger name shown as-is", got)
		}
	})

	t.Run("date separators", func(t *testing.T) {
		lastDate := ""
		lastTime := 0.0
		nextDay := testTime + 24*60*60
		lines := []string{
			formatJsonLogLine([]byte(logEntry(t, testTime, "INFO", "root", "a")), &lastDate, &lastTime),
			formatJsonLogLine([]byte(logEntry(t, testTime+1, "INFO", "root", "b")), &lastDate, &lastTime),
			formatJsonLogLine([]byte(logEntry(t, nextDay, "INFO", "root", "c")), &lastDate, &lastTime),
		}
		separator := func(ts float64) string {
			return fmt.Sprintf("<span class=date>------- %s -------\n</span>", time.Unix(int64(ts), 0).Format("2006-01-02"))
		}
		if !strings.HasPrefix(lines[0], separator(testTime)) {
			t.Errorf("first line should start with a date separator, got %q", lines[0])
		}
		if strings.Contains(lines[1], "class=date") {
			t.Errorf("a line on the same date should not repeat the separator, got %q", lines[1])
		}
		if !strings.HasPrefix(lines[2], separator(nextDay)) {
			t.Errorf("a line on a new date should start with a separator, got %q", lines[2])
		}
	})

	t.Run("non-JSON line", func(t *testing.T) {
		lastDate := ""
		lastTime := 0.0
		formatJsonLogLine([]byte(logEntry(t, testTime, "ERROR", "root", "before")), &lastDate, &lastTime)
		got := formatJsonLogLine([]byte("Traceback (most recent call last):"), &lastDate, &lastTime)
		// Shown as an info line from "unknown", at the previous line's time
		want := `<span class="i l-unknown"><span class=t>` + clock + ` </span>` +
			`<span class=s><span class=i>I</span> </span><span class=l>unknown</span> Traceback (most recent call last):` + "\n</span>"
		if got != want {
			t.Fatalf("got:\n%s\nwant:\n%s", got, want)
		}
	})
}

func TestGetRunPlan(t *testing.T) {
	tests := []runPlanTestItem{testItem("test_a.py::test_one", map[string]any{"arch": "x86"})}

	forEachStorage(t, func(t *testing.T, compressed bool) {
		env := newTestEnv(t)
		env.writeRun("proj", "run", 3, tests, compressed)
		plan, err := getRunPlan("proj", "run")
		if err != nil {
			t.Fatalf("getRunPlan: %v", err)
		}
		want := runPlan{WorkerCount: 3, Groups: map[string][]runPlanTestItem{"test_module": tests}, RowParams: []string{}}
		if !reflect.DeepEqual(*plan, want) {
			t.Fatalf("got %+v, want %+v", *plan, want)
		}
	})

	t.Run("cached", func(t *testing.T) {
		env := newTestEnv(t)
		env.writeRun("proj", "run", 3, tests, false)
		_, err := getRunPlan("proj", "run")
		if err != nil {
			t.Fatal(err)
		}
		env.writeRun("proj", "run", 7, tests, false)
		plan, err := getRunPlan("proj", "run")
		if err != nil {
			t.Fatal(err)
		}
		if plan.WorkerCount != 3 {
			t.Fatalf("worker_count = %d, want the cached plan's 3", plan.WorkerCount)
		}
	})

	t.Run("cache expires", func(t *testing.T) {
		env := newTestEnv(t, func(c *greendotsConfig) { c.Caching.PlanCacheMs = 50 })
		env.writeRun("proj", "run", 3, tests, false)
		_, err := getRunPlan("proj", "run")
		if err != nil {
			t.Fatal(err)
		}
		env.writeRun("proj", "run", 7, tests, false)
		time.Sleep(100 * time.Millisecond)
		plan, err := getRunPlan("proj", "run")
		if err != nil {
			t.Fatal(err)
		}
		if plan.WorkerCount != 7 {
			t.Fatalf("worker_count = %d, want the updated plan's 7", plan.WorkerCount)
		}
	})

	t.Run("cache is per run", func(t *testing.T) {
		env := newTestEnv(t)
		env.writeRun("proj", "run1", 1, tests, false)
		env.writeRun("proj", "run2", 2, tests, false)
		env.writeRun("other", "run1", 3, tests, false)
		for _, c := range []struct {
			project, run string
			workers      int
		}{{"proj", "run1", 1}, {"proj", "run2", 2}, {"other", "run1", 3}, {"proj", "run1", 1}} {
			plan, err := getRunPlan(c.project, c.run)
			if err != nil {
				t.Fatal(err)
			}
			if plan.WorkerCount != c.workers {
				t.Errorf("%s/%s: worker_count = %d, want %d", c.project, c.run, plan.WorkerCount, c.workers)
			}
		}
	})

	t.Run("missing plan is not cached", func(t *testing.T) {
		env := newTestEnv(t)
		_, err := getRunPlan("proj", "run")
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("got %v, want os.ErrNotExist", err)
		}
		// The plan shows up once pytest finishes collecting
		env.writeRun("proj", "run", 2, tests, false)
		plan, err := getRunPlan("proj", "run")
		if err != nil {
			t.Fatalf("getRunPlan after the plan was written: %v", err)
		}
		if plan.WorkerCount != 2 {
			t.Fatalf("worker_count = %d, want 2", plan.WorkerCount)
		}
	})

	t.Run("invalid plan", func(t *testing.T) {
		env := newTestEnv(t)
		env.writeFile("proj/run/plan.json", `{"worker_count": 2, "groups": {`)
		_, err := getRunPlan("proj", "run")
		if err == nil {
			t.Fatalf("a truncated plan.json should fail to load")
		}
	})

	t.Run("compressed and uncompressed", func(t *testing.T) {
		env := newTestEnv(t)
		env.writeRun("proj", "run", 2, tests, false)
		env.writeRun("proj", "run", 2, tests, true)
		_, err := getRunPlan("proj", "run")
		if !isCompressedAndUncompressedError(err) {
			t.Fatalf("got %v, want a compressed-and-uncompressed error", err)
		}
	})
}

func TestGetTestLogFile(t *testing.T) {
	env := newTestEnv(t)
	groups := map[string][]runPlanTestItem{
		"test_a": {testItem("test_a.py::test_one", nil)},
		"test_b": {
			testItem("test_b.py::test_two", nil),
			{Id: "test_b.py::test_long_name", LogFile: "test_b.py__test_long-ABCDEFGH.log.jsonl", Name: "test_long_name"},
			{Id: "test_b.py::test_no_log_file", Name: "test_no_log_file"},
			{Id: "test_b.py::test_escape", LogFile: "../../secret", Name: "test_escape"},
			{Id: "test_b.py::test_subdir", LogFile: "logs/test.log.jsonl", Name: "test_subdir"},
			{Id: "test_b.py::test_hidden", LogFile: ".hidden.log.jsonl", Name: "test_hidden"},
		},
	}
	env.writeFile("proj/run/plan.json", planJSON(t, 1, groups, nil))

	found := map[string]string{
		"test_a.py::test_one":       "test_a.py::test_one.log.jsonl",
		"test_b.py::test_two":       "test_b.py::test_two.log.jsonl",
		"test_b.py::test_long_name": "test_b.py__test_long-ABCDEFGH.log.jsonl",
	}
	for test, want := range found {
		got, err := getTestLogFile("proj", "run", test)
		if err != nil || got != want {
			t.Errorf("getTestLogFile(%q) = (%q, %v), want %q", test, got, err, want)
		}
	}

	for _, test := range []string{
		"test_a.py::test_unknown",
		"test_b.py::test_no_log_file",
		// Log files must stay inside the run directory
		"test_b.py::test_escape",
		"test_b.py::test_subdir",
		"test_b.py::test_hidden",
	} {
		got, err := getTestLogFile("proj", "run", test)
		if err == nil {
			t.Errorf("getTestLogFile(%q) = %q, want an error", test, got)
		}
	}

	_, err := getTestLogFile("proj", "missing-run", "test_a.py::test_one")
	if err == nil {
		t.Errorf("getTestLogFile on a run without a plan should fail")
	}
}

func TestConfigFile(t *testing.T) {
	t.Run("overrides only the given keys", func(t *testing.T) {
		cfg := defaultTestConfig
		_, err := toml.Decode(`
projects_dir = "/data/projects"
listen_address = "127.0.0.1:9000"
max_line_size = 1024

[status_poll]
timeout_ms = 1234

[status_stream]
log_truncation_size = 2048

[log_tail]
default_line_count = 7

[caching]
plan_cache_ms = 99

[client.test_status]
poll_interval_ms = 250

[additional_log_levels.TRACE]
class = "d"
shortname = "T"
`, &cfg)
		if err != nil {
			t.Fatalf("failed to parse config: %v", err)
		}

		want := defaultTestConfig
		want.ProjectsDir = "/data/projects"
		want.ListenAddress = "127.0.0.1:9000"
		want.MaxLineSize = 1024
		want.StatusPoll.TimeoutMs = 1234
		want.StatusStream.LogTruncationSize = 2048
		want.LogTail.DefaultLineCount = 7
		want.Caching.PlanCacheMs = 99
		want.Client.TestStatus.PollIntervalMs = 250
		want.AdditionalLogLevels = map[string]logLevel{"TRACE": {Class: "d", Shortname: "T"}}
		if !reflect.DeepEqual(cfg, want) {
			t.Fatalf("got  %+v\nwant %+v", cfg, want)
		}
	})

	t.Run("bundled config.toml", func(t *testing.T) {
		cfg := defaultTestConfig
		_, err := toml.DecodeFile("config.toml", &cfg)
		if err != nil {
			t.Fatalf("failed to parse the bundled config.toml: %v", err)
		}
		if cfg.ProjectsDir == "" {
			t.Fatalf("the bundled config.toml should set projects_dir")
		}
	})
}
