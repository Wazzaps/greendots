package main

// Test harness: isolated greendots servers backed by temporary projects dirs,
// plus helpers for writing the on-disk data the pytest plugin produces.

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
)

// serverProcessEnv makes the test binary act as the real greendots binary, so
// tests can run the server in a separate process (see newProcessEnv).
const serverProcessEnv = "GREENDOTS_TEST_RUN_SERVER"

func TestMain(m *testing.M) {
	if os.Getenv(serverProcessEnv) == "1" {
		main()
		return
	}
	os.Exit(m.Run())
}

// defaultTestConfig is a pristine copy of the server's built-in defaults,
// captured before any test modifies the global config.
var defaultTestConfig = config

// testConfig returns the default config pointed at projectsDir, with sleeps
// and timeouts shortened so the tests run fast.
func testConfig(projectsDir string) greendotsConfig {
	c := defaultTestConfig
	c.ProjectsDir = projectsDir
	c.StatusPoll.SleepMs = 10
	c.StatusPoll.TimeoutMs = 500
	c.StatusStream.EofSleepMs = 10
	return c
}

func resetRunPlanCache() {
	runPlanCache = make(map[runPlanCacheKey]runPlanCacheVal)
}

// testEnv is a greendots server backed by an empty temporary projects dir.
type testEnv struct {
	t       *testing.T
	dir     string
	baseURL string
	client  *http.Client

	// Only set for servers running in a separate process
	proc *serverProcess
}

// newTestEnv starts the server's real router in-process. The server keeps its
// config and plan cache in globals, so these tests must not use t.Parallel().
// `configure` can adjust the config before the server starts.
func newTestEnv(t *testing.T, configure ...func(c *greendotsConfig)) *testEnv {
	t.Helper()

	dir := t.TempDir()
	savedConfig := config
	config = testConfig(dir)
	for _, f := range configure {
		f(&config)
	}
	resetRunPlanCache()

	mux := http.NewServeMux()
	err := registerRoutes(mux)
	if err != nil {
		t.Fatalf("registerRoutes: %v", err)
	}
	server := httptest.NewServer(mux)

	t.Cleanup(func() {
		shutdownTestServer(t, server)
		config = savedConfig
		resetRunPlanCache()
	})

	return &testEnv{
		t:       t,
		dir:     dir,
		baseURL: server.URL,
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

// shutdownTestServer disconnects all clients and waits for every handler to
// return. A handler that keeps running after its client is gone is leaking
// (e.g. a stream that never notices the disconnect), which fails the test.
func shutdownTestServer(t *testing.T, server *httptest.Server) {
	server.CloseClientConnections()
	closed := make(chan struct{})
	go func() {
		server.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Errorf("server did not shut down: a request handler is still running after its client disconnected")
	}
}

// serverProcess is the test binary re-executed as the real greendots server.
type serverProcess struct {
	cmd    *exec.Cmd
	output *syncBuffer
	exited chan struct{}

	// Whether a test failure already included the crash output
	crashReported bool
}

// syncBuffer collects the server process' output.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// newProcessEnv runs the real server binary (main(), flags and config file
// included) in a child process. Use it for tests that can crash or wedge the
// server, so a bug fails only that test instead of killing the whole run.
// The test fails if the server crashes or reports a data race.
func newProcessEnv(t *testing.T, configure ...func(c *greendotsConfig)) *testEnv {
	t.Helper()

	dir := t.TempDir()
	cfg := testConfig(dir)
	for _, f := range configure {
		f(&cfg)
	}
	cfg.ListenAddress = freeLocalAddress(t)
	configPath := filepath.Join(t.TempDir(), "config.toml")
	writeTOML(t, configPath, cfg)

	proc := startServerProcess(t, "-config", configPath)
	env := &testEnv{
		t:       t,
		dir:     dir,
		baseURL: "http://" + cfg.ListenAddress,
		client:  &http.Client{Timeout: 10 * time.Second},
		proc:    proc,
	}
	waitForServer(t, env)

	// Runs before startServerProcess' cleanup, which kills the server
	t.Cleanup(func() {
		select {
		case <-proc.exited:
			if !proc.crashReported {
				t.Errorf("server process exited unexpectedly:\n%s", proc.output)
			}
			return
		default:
		}
		_ = proc.cmd.Process.Kill()
		<-proc.exited
		if strings.Contains(proc.output.String(), "WARNING: DATA RACE") {
			t.Errorf("server process reported a data race:\n%s", proc.output)
		}
	})
	return env
}

// startServerProcess re-executes the test binary as the greendots binary.
func startServerProcess(t *testing.T, args ...string) *serverProcess {
	t.Helper()
	cmd := exec.Command(os.Args[0], args...)
	// Several threads even on a single CPU (or with GOMAXPROCS=1 inherited),
	// so unsynchronized shared state is really accessed concurrently
	cmd.Env = append(os.Environ(), serverProcessEnv+"=1", "GOMAXPROCS=8")
	proc := &serverProcess{cmd: cmd, output: &syncBuffer{}, exited: make(chan struct{})}
	cmd.Stdout = proc.output
	cmd.Stderr = proc.output
	err := cmd.Start()
	if err != nil {
		t.Fatalf("failed to start the server process: %v", err)
	}
	go func() {
		_ = cmd.Wait()
		close(proc.exited)
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		<-proc.exited
	})
	return proc
}

func waitForServer(t *testing.T, env *testEnv) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-env.proc.exited:
			t.Fatalf("server process exited during startup:\n%s", env.proc.output)
		default:
		}
		resp, err := env.client.Get(env.baseURL + "/api/v1/version")
		if err == nil {
			resp.Body.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("server process did not start listening within 10s:\n%s", env.proc.output)
}

// assertAlive fails the test if the server process has died (e.g. from a
// panic in a background goroutine, which net/http cannot recover).
func (e *testEnv) assertAlive() {
	e.t.Helper()
	if e.proc == nil {
		e.t.Fatalf("assertAlive is only meaningful for process servers")
	}
	select {
	case <-e.proc.exited:
		e.proc.crashReported = true
		e.t.Fatalf("server process crashed:\n%s", e.proc.output)
	case <-time.After(100 * time.Millisecond):
	}
	resp := e.get("/api/v1/version")
	if resp.StatusCode != http.StatusOK {
		e.t.Fatalf("server is unhealthy: GET /api/v1/version returned %d", resp.StatusCode)
	}
}

func freeLocalAddress(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to find a free port: %v", err)
	}
	defer l.Close()
	return l.Addr().String()
}

func writeTOML(t *testing.T, path string, v any) {
	t.Helper()
	var buf bytes.Buffer
	err := toml.NewEncoder(&buf).Encode(v)
	if err != nil {
		t.Fatalf("failed to encode toml: %v", err)
	}
	err = os.WriteFile(path, buf.Bytes(), 0o644)
	if err != nil {
		t.Fatal(err)
	}
}

// -- Requests --

type testResponse struct {
	StatusCode int
	Header     http.Header
	Body       string
}

func (e *testEnv) do(req *http.Request) testResponse {
	e.t.Helper()
	resp, err := e.client.Do(req)
	if err != nil {
		e.failTransport(req, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		e.failTransport(req, err)
	}
	return testResponse{StatusCode: resp.StatusCode, Header: resp.Header, Body: string(body)}
}

func (e *testEnv) failTransport(req *http.Request, err error) {
	e.t.Helper()
	if e.proc != nil {
		select {
		case <-e.proc.exited:
			e.proc.crashReported = true
			e.t.Fatalf("%s %s: %v\nthe server process crashed:\n%s", req.Method, req.URL.Path, err, e.proc.output)
		case <-time.After(500 * time.Millisecond):
		}
	}
	e.t.Fatalf("%s %s: %v", req.Method, req.URL.Path, err)
}

func (e *testEnv) newRequest(method, path string, body io.Reader) *http.Request {
	e.t.Helper()
	req, err := http.NewRequest(method, e.baseURL+path, body)
	if err != nil {
		e.t.Fatalf("bad request %s %s: %v", method, path, err)
	}
	return req
}

// get performs a GET request; headers are given as name, value pairs.
func (e *testEnv) get(path string, headers ...string) testResponse {
	e.t.Helper()
	req := e.newRequest("GET", path, nil)
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	return e.do(req)
}

func (e *testEnv) post(path string, body string) testResponse {
	e.t.Helper()
	return e.do(e.newRequest("POST", path, strings.NewReader(body)))
}

func (e *testEnv) getJSON(path string, v any) testResponse {
	e.t.Helper()
	resp := e.get(path)
	if resp.StatusCode != http.StatusOK {
		e.t.Fatalf("GET %s: status %d, body: %q", path, resp.StatusCode, resp.Body)
	}
	err := json.Unmarshal([]byte(resp.Body), v)
	if err != nil {
		e.t.Fatalf("GET %s: invalid JSON response: %v\nbody: %q", path, err, resp.Body)
	}
	return resp
}

// stream is an in-flight response that is read incrementally, for endpoints
// that keep streaming (like log_stream following a log file).
type stream struct {
	t       *testing.T
	resp    *http.Response
	cancel  context.CancelFunc
	chunks  chan []byte
	data    strings.Builder
	ended   bool
	readErr error
}

func (e *testEnv) openStream(path string) *stream {
	e.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, "GET", e.baseURL+path, nil)
	if err != nil {
		cancel()
		e.t.Fatal(err)
	}
	// No client timeout: streams are bounded by waitFor/waitForEnd instead
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		cancel()
		e.failTransport(req, err)
	}
	s := &stream{t: e.t, resp: resp, cancel: cancel, chunks: make(chan []byte)}
	go func() {
		defer close(s.chunks)
		for {
			buf := make([]byte, 32*1024)
			n, err := resp.Body.Read(buf)
			if n > 0 {
				s.chunks <- buf[:n]
			}
			if err != nil {
				if err != io.EOF {
					s.readErr = err
				}
				return
			}
		}
	}()
	e.t.Cleanup(s.close)
	return s
}

// waitFor reads until the response so far contains `want`, and returns it.
func (s *stream) waitFor(want string, timeout time.Duration) string {
	s.t.Helper()
	return s.waitUntil(func(data string) bool { return strings.Contains(data, want) }, fmt.Sprintf("%q", want), timeout)
}

// waitUntil reads until the response so far satisfies `done`, and returns it.
func (s *stream) waitUntil(done func(data string) bool, desc string, timeout time.Duration) string {
	s.t.Helper()
	deadline := time.After(timeout)
	for !done(s.data.String()) {
		if s.ended {
			s.t.Fatalf("stream ended without %s, got:\n%s", desc, s.data.String())
		}
		select {
		case chunk, ok := <-s.chunks:
			if !ok {
				s.ended = true
				continue
			}
			s.data.Write(chunk)
		case <-deadline:
			s.t.Fatalf("%s did not arrive within %v, got:\n%s", desc, timeout, s.data.String())
		}
	}
	return s.data.String()
}

// waitForEnd reads until the server ends the response, and returns it all.
func (s *stream) waitForEnd(timeout time.Duration) string {
	s.t.Helper()
	deadline := time.After(timeout)
	for !s.ended {
		select {
		case chunk, ok := <-s.chunks:
			if !ok {
				s.ended = true
				continue
			}
			s.data.Write(chunk)
		case <-deadline:
			s.t.Fatalf("the server did not end the response within %v, got so far:\n%s", timeout, s.data.String())
		}
	}
	if s.readErr != nil {
		s.t.Fatalf("response ended with an error: %v, got:\n%s", s.readErr, s.data.String())
	}
	return s.data.String()
}

// close disconnects from the server.
func (s *stream) close() {
	s.cancel()
	s.resp.Body.Close()
	for range s.chunks {
	}
}

// -- On-disk fixtures --

// path returns the absolute path of a file in the projects dir.
func (e *testEnv) path(rel string) string {
	return filepath.Join(e.dir, filepath.FromSlash(rel))
}

func (e *testEnv) mkdir(rel string) {
	e.t.Helper()
	err := os.MkdirAll(e.path(rel), 0o755)
	if err != nil {
		e.t.Fatal(err)
	}
}

func (e *testEnv) writeFile(rel string, content string) {
	e.t.Helper()
	e.writeBytes(rel, []byte(content))
}

func (e *testEnv) writeBytes(rel string, content []byte) {
	e.t.Helper()
	err := os.MkdirAll(filepath.Dir(e.path(rel)), 0o755)
	if err != nil {
		e.t.Fatal(err)
	}
	err = os.WriteFile(e.path(rel), content, 0o644)
	if err != nil {
		e.t.Fatal(err)
	}
}

func (e *testEnv) appendFile(rel string, content string) {
	e.t.Helper()
	f, err := os.OpenFile(e.path(rel), os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		e.t.Fatal(err)
	}
	defer f.Close()
	_, err = f.WriteString(content)
	if err != nil {
		e.t.Fatal(err)
	}
}

// writeDataFile writes a data file in one of the two ways the server accepts
// it: as-is, or gzip-compressed with a ".gz" suffix.
func (e *testEnv) writeDataFile(rel string, content string, compressed bool) {
	e.t.Helper()
	if compressed {
		e.writeBytes(rel+".gz", gzipBytes(e.t, []byte(content)))
	} else {
		e.writeFile(rel, content)
	}
}

func (e *testEnv) setModTime(rel string, mtime time.Time) {
	e.t.Helper()
	err := os.Chtimes(e.path(rel), mtime, mtime)
	if err != nil {
		e.t.Fatal(err)
	}
}

func gzipBytes(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	_, err := gz.Write(data)
	if err != nil {
		t.Fatal(err)
	}
	err = gz.Close()
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// forEachStorage runs `f` once with plain data files and once with
// gzip-compressed ones, since every data file may be stored either way.
func forEachStorage(t *testing.T, f func(t *testing.T, compressed bool)) {
	t.Run("plain", func(t *testing.T) { f(t, false) })
	t.Run("gzip", func(t *testing.T) { f(t, true) })
}

// -- Data formats written by the pytest plugin --

// testItem is a plan entry, as written by the plugin.
func testItem(id string, params map[string]any) runPlanTestItem {
	name := id
	if i := strings.LastIndex(name, "::"); i >= 0 {
		name = name[i+2:]
	}
	if i := strings.Index(name, "["); i >= 0 {
		name = name[:i]
	}
	if params == nil {
		params = map[string]any{}
	}
	return runPlanTestItem{Id: id, LogFile: id + ".log.jsonl", Name: name, Params: params}
}

func planJSON(t *testing.T, workerCount int, groups map[string][]runPlanTestItem, rowParams []string) string {
	t.Helper()
	if rowParams == nil {
		rowParams = []string{}
	}
	data, err := json.MarshalIndent(runPlan{WorkerCount: workerCount, Groups: groups, RowParams: rowParams}, "", "    ")
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// writeRun writes a run's plan.json, with every test in a single group.
func (e *testEnv) writeRun(project, run string, workerCount int, tests []runPlanTestItem, compressed bool) {
	e.t.Helper()
	plan := planJSON(e.t, workerCount, map[string][]runPlanTestItem{"test_module": tests}, nil)
	e.writeDataFile(project+"/"+run+"/plan.json", plan, compressed)
}

// encodeJSON encodes like Python's json.dumps: unlike Go's default, it does
// not escape <, > and & (so the server must take care of them).
func encodeJSON(t *testing.T, v any) string {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	err := enc.Encode(v)
	if err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// statusLine is a status file line, e.g. statusLine(t, "type", "start", "test", id).
func statusLine(t *testing.T, kv ...any) string {
	t.Helper()
	m := map[string]any{"time": 1722625667.0}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i].(string)] = kv[i+1]
	}
	return encodeJSON(t, m)
}

// logEntry is a test log file line.
func logEntry(t *testing.T, when float64, level, name, message string) string {
	t.Helper()
	return encodeJSON(t, map[string]any{"time": when, "level": level, "name": name, "message": message})
}

// testTime is a fixed timestamp at local noon (so nearby seconds share a date).
var testTime = float64(time.Date(2024, 3, 10, 12, 0, 0, 0, time.Local).Unix())
