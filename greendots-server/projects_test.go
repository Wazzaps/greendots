package main

// Tests for the project and run listing endpoints.

import (
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

type projectsResponse struct {
	Projects []struct {
		Id       string         `json:"id"`
		Runs     []runResponse  `json:"runs"`
		Metadata map[string]any `json:"metadata"`
	} `json:"projects"`
}

type runsResponse struct {
	Runs []runResponse `json:"runs"`
}

type runResponse struct {
	Id        string         `json:"id"`
	PrettyAge string         `json:"pretty_age"`
	CreatedAt string         `json:"created_at"`
	Metadata  map[string]any `json:"metadata"`
}

func runIds(runs []runResponse) []string {
	ids := []string{}
	for _, r := range runs {
		ids = append(ids, r.Id)
	}
	return ids
}

// makeRuns creates `count` runs named run-00, run-01... where higher numbers
// are newer, and returns their names newest first.
func makeRuns(env *testEnv, project string, count int) []string {
	base := time.Now().Add(-time.Duration(count) * time.Hour)
	newestFirst := []string{}
	for i := range count {
		run := fmt.Sprintf("run-%02d", i)
		env.mkdir(project + "/" + run)
		env.setModTime(project+"/"+run, base.Add(time.Duration(i)*time.Hour))
		newestFirst = append([]string{run}, newestFirst...)
	}
	return newestFirst
}

func TestProjectsList(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		env := newTestEnv(t)
		resp := env.get("/api/v1/projects")
		if resp.StatusCode != http.StatusOK || strings.TrimSpace(resp.Body) != `{"projects":[]}` {
			t.Fatalf("got %d %q, want an empty project list", resp.StatusCode, resp.Body)
		}
		if resp.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("Content-Type = %q, want application/json", resp.Header.Get("Content-Type"))
		}
	})

	t.Run("lists project directories by name", func(t *testing.T) {
		env := newTestEnv(t)
		env.mkdir("zeta/run")
		env.mkdir("alpha/run")
		env.mkdir("empty-project")
		env.writeFile("not-a-project.txt", "")

		var got projectsResponse
		env.getJSON("/api/v1/projects", &got)
		ids := []string{}
		for _, p := range got.Projects {
			ids = append(ids, p.Id)
		}
		want := []string{"alpha", "empty-project", "zeta"}
		if !reflect.DeepEqual(ids, want) {
			t.Fatalf("projects = %v, want %v", ids, want)
		}
	})

	t.Run("shows the 10 newest runs", func(t *testing.T) {
		env := newTestEnv(t)
		newestFirst := makeRuns(env, "proj", 13)
		env.writeFile("proj/metadata.toml", `name = "not a run"`)

		var got projectsResponse
		env.getJSON("/api/v1/projects", &got)
		if len(got.Projects) != 1 {
			t.Fatalf("got %d projects, want 1", len(got.Projects))
		}
		if ids := runIds(got.Projects[0].Runs); !reflect.DeepEqual(ids, newestFirst[:10]) {
			t.Fatalf("runs = %v, want the 10 newest %v", ids, newestFirst[:10])
		}
	})

	t.Run("run fields", func(t *testing.T) {
		env := newTestEnv(t)
		mtime := time.Now().Add(-2 * time.Hour).Truncate(time.Second)
		env.mkdir("proj/run")
		env.setModTime("proj/run", mtime)

		var got projectsResponse
		env.getJSON("/api/v1/projects", &got)
		r := got.Projects[0].Runs[0]
		createdAt, err := time.Parse(time.RFC3339, r.CreatedAt)
		if err != nil || !createdAt.Equal(mtime) {
			t.Errorf("created_at = %q (%v), want the run directory's mtime %v in RFC3339", r.CreatedAt, err, mtime)
		}
		if r.PrettyAge != "2 hours ago" {
			t.Errorf("pretty_age = %q, want %q", r.PrettyAge, "2 hours ago")
		}
	})

	forEachStorage(t, func(t *testing.T, compressed bool) {
		t.Run("project metadata", func(t *testing.T) {
			env := newTestEnv(t)
			env.mkdir("proj/run")
			env.writeDataFile("proj/metadata.toml", "name = \"My Project\"\n[owner]\nteam = \"infra\"\n", compressed)

			var got projectsResponse
			env.getJSON("/api/v1/projects", &got)
			want := map[string]any{"name": "My Project", "owner": map[string]any{"team": "infra"}}
			if !reflect.DeepEqual(got.Projects[0].Metadata, want) {
				t.Fatalf("metadata = %v, want %v", got.Projects[0].Metadata, want)
			}
		})

		t.Run("run metadata", func(t *testing.T) {
			// The frontend shows `run.metadata.name` instead of the run id
			env := newTestEnv(t)
			env.mkdir("proj/run")
			env.writeDataFile("proj/run/metadata.toml", `name = "Nightly #42"`, compressed)

			var got projectsResponse
			env.getJSON("/api/v1/projects", &got)
			want := map[string]any{"name": "Nightly #42"}
			if !reflect.DeepEqual(got.Projects[0].Runs[0].Metadata, want) {
				t.Fatalf("run metadata = %v, want %v from proj/run/metadata.toml", got.Projects[0].Runs[0].Metadata, want)
			}
		})
	})

	t.Run("missing or invalid metadata is null", func(t *testing.T) {
		env := newTestEnv(t)
		env.mkdir("no-metadata/run")
		env.mkdir("bad-metadata/run")
		env.writeFile("bad-metadata/metadata.toml", "this is = = not toml")
		env.writeFile("bad-metadata/run/metadata.toml", "[[[")

		var got projectsResponse
		env.getJSON("/api/v1/projects", &got)
		for _, p := range got.Projects {
			if p.Metadata != nil || p.Runs[0].Metadata != nil {
				t.Errorf("project %s: metadata = %v, run metadata = %v, want both null", p.Id, p.Metadata, p.Runs[0].Metadata)
			}
		}
	})

	t.Run("compressed and uncompressed metadata", func(t *testing.T) {
		for _, file := range []string{"proj/metadata.toml", "proj/run/metadata.toml"} {
			env := newTestEnv(t)
			env.mkdir("proj/run")
			env.writeDataFile(file, `name = "a"`, false)
			env.writeDataFile(file, `name = "b"`, true)
			resp := env.get("/api/v1/projects")
			if resp.StatusCode != http.StatusInternalServerError || !strings.Contains(resp.Body, "Both compressed (.gz) and uncompressed") {
				t.Errorf("%s: got %d %q, want a 500 explaining the ambiguity", file, resp.StatusCode, resp.Body)
			}
		}
	})

	t.Run("missing projects dir", func(t *testing.T) {
		env := newTestEnv(t, func(c *greendotsConfig) { c.ProjectsDir += "/does-not-exist" })
		resp := env.get("/api/v1/projects")
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", resp.StatusCode)
		}
		// The handler must stop after the error, not append a project list to it
		if strings.Contains(resp.Body, "projects") {
			t.Fatalf("404 body = %q, want only the error message", resp.Body)
		}
	})
}

func TestProjectRuns(t *testing.T) {
	t.Run("lists all runs newest first", func(t *testing.T) {
		env := newTestEnv(t)
		newestFirst := makeRuns(env, "proj", 13)
		env.writeFile("proj/metadata.toml", `name = "not a run"`)

		var got runsResponse
		resp := env.getJSON("/api/v1/projects/proj/runs", &got)
		if ids := runIds(got.Runs); !reflect.DeepEqual(ids, newestFirst) {
			t.Fatalf("runs = %v, want %v", ids, newestFirst)
		}
		if resp.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("Content-Type = %q, want application/json", resp.Header.Get("Content-Type"))
		}
	})

	t.Run("project name with spaces", func(t *testing.T) {
		env := newTestEnv(t)
		env.mkdir("my project/run 1")
		var got runsResponse
		env.getJSON("/api/v1/projects/my%20project/runs", &got)
		if ids := runIds(got.Runs); !reflect.DeepEqual(ids, []string{"run 1"}) {
			t.Fatalf("runs = %v, want [run 1]", ids)
		}
	})

	forEachStorage(t, func(t *testing.T, compressed bool) {
		t.Run("run metadata", func(t *testing.T) {
			env := newTestEnv(t)
			env.mkdir("proj/run")
			env.writeDataFile("proj/run/metadata.toml", `name = "Nightly #42"`, compressed)

			var got runsResponse
			env.getJSON("/api/v1/projects/proj/runs", &got)
			want := map[string]any{"name": "Nightly #42"}
			if !reflect.DeepEqual(got.Runs[0].Metadata, want) {
				t.Fatalf("run metadata = %v, want %v from proj/run/metadata.toml", got.Runs[0].Metadata, want)
			}
		})
	})

	t.Run("compressed and uncompressed run metadata", func(t *testing.T) {
		env := newTestEnv(t)
		env.mkdir("proj/run")
		env.writeDataFile("proj/run/metadata.toml", `name = "a"`, false)
		env.writeDataFile("proj/run/metadata.toml", `name = "b"`, true)
		resp := env.get("/api/v1/projects/proj/runs")
		if resp.StatusCode != http.StatusInternalServerError {
			t.Fatalf("got %d %q, want 500", resp.StatusCode, resp.Body)
		}
	})

	t.Run("unknown project", func(t *testing.T) {
		env := newTestEnv(t)
		resp := env.get("/api/v1/projects/nope/runs")
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", resp.StatusCode)
		}
		// The handler must stop after the error, not append a run list to it
		if strings.Contains(resp.Body, "runs") {
			t.Fatalf("404 body = %q, want only the error message", resp.Body)
		}
	})

	t.Run("rejects paths outside the projects dir", func(t *testing.T) {
		env := newTestEnv(t)
		env.mkdir(".hidden/run")
		env.mkdir("proj/sub/run")
		// (A literal ".." segment is cleaned up by the router before matching)
		for _, project := range []string{".hidden", "%2E%2E", "..%2F..", "proj%2Fsub"} {
			resp := env.get("/api/v1/projects/" + project + "/runs")
			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("project %q: got %d %q, want 404", project, resp.StatusCode, resp.Body)
			}
		}
	})
}
