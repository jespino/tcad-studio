package watch

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jespino/tcad-studio/internal/dsl"
	"github.com/jespino/tcad-studio/internal/geom"
)

func cube() *geom.Mesh {
	m := &geom.Mesh{}
	v := func(x, y, z float64) geom.Vec3 { return geom.V(x, y, z) }
	quads := [][4]geom.Vec3{
		{v(0, 0, 0), v(0, 1, 0), v(1, 1, 0), v(1, 0, 0)},
		{v(0, 0, 1), v(1, 0, 1), v(1, 1, 1), v(0, 1, 1)},
		{v(0, 0, 0), v(1, 0, 0), v(1, 0, 1), v(0, 0, 1)},
		{v(0, 1, 0), v(0, 1, 1), v(1, 1, 1), v(1, 1, 0)},
		{v(0, 0, 0), v(0, 0, 1), v(0, 1, 1), v(0, 1, 0)},
		{v(1, 0, 0), v(1, 1, 0), v(1, 1, 1), v(1, 0, 1)},
	}
	for _, q := range quads {
		m.AddQuad(q[0], q[1], q[2], q[3])
	}
	return m
}

func TestWatchRebuildsAndNotifies(t *testing.T) {
	dir := t.TempDir()
	model := filepath.Join(dir, "a.tcad")
	other := filepath.Join(dir, "b.tcad")
	out := filepath.Join(dir, "a.stl")
	for _, f := range []string{model, other} {
		if err := os.WriteFile(f, []byte("ok"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	urls := make(chan string, 1)
	go Run(Options{
		Paths: []string{dir}, Addr: "127.0.0.1:0", Log: io.Discard,
		Output: func(m string) string { return strings.TrimSuffix(m, ".tcad") + ".stl" },
		Build: func(model string, defs map[string]string) (*Build, error) {
			src, _ := os.ReadFile(model)
			if strings.TrimSpace(string(src)) != "ok" {
				return nil, errors.New("broken model")
			}
			// A fake model with one param: "copies" cubes side by side.
			n := 1.0
			if e, ok := defs["copies"]; ok {
				n = dsl.EvalDefine(e).(float64)
			}
			m := &geom.Mesh{}
			for i := 0; i < int(n); i++ {
				c := cube()
				c.Transform(geom.Translate(geom.V(float64(i)*2, 0, 0)))
				m.Append(c)
			}
			params := []dsl.ParamValue{{Name: "copies", Type: "number", Value: n, Default: 1.0,
				Options: []any{1.0, 2.0, 3.0}}}
			return &Build{Mesh: m, Params: params}, nil
		},
		Ready: func(u string) { urls <- u },
	})
	url := <-urls

	resp, err := http.Get(url + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	events := bufio.NewReader(resp.Body)
	next := func() status {
		t.Helper()
		for {
			line, err := events.ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			if data, ok := strings.CutPrefix(line, "data: "); ok {
				var st status
				if err := json.Unmarshal([]byte(data), &st); err != nil {
					t.Fatal(err)
				}
				return st
			}
		}
	}
	// The first event may arrive before the initial build finishes.
	st := next()
	for !st.OK {
		st = next()
	}
	if st.Triangles != 12 || st.Model != model || !slices.Equal(st.Models, []string{model, other}) {
		t.Fatalf("unexpected status %+v", st)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("STL not written: %v", err)
	}

	time.Sleep(50 * time.Millisecond) // make sure the mtime changes
	os.WriteFile(model, []byte("broken!"), 0o644)
	if st = next(); st.OK || st.Error != "broken model" || !st.HasModel {
		t.Fatalf("expected an error that keeps the last model, got %+v", st)
	}
	os.WriteFile(model, []byte("ok"), 0o644)
	if st = next(); !st.OK || st.ModelStamp != st.Version {
		t.Fatalf("expected a successful rebuild, got %+v", st)
	}

	// Changing a param through the API rebuilds the model.
	put := func(body string) int {
		req, _ := http.NewRequest(http.MethodPut, url+"/api/params", strings.NewReader(body))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if code := put(`{"copies": "3"}`); code != http.StatusAccepted {
		t.Fatalf("PUT returned %d", code)
	}
	for st = next(); st.Triangles != 36; st = next() {
	}
	if st.Overrides["copies"] != "3" {
		t.Fatalf("override not reported: %+v", st.Overrides)
	}
	if code := put(`{"copies": "\"tres\""}`); code != http.StatusBadRequest {
		t.Fatalf("a string for a number param should be rejected, got %d", code)
	}
	if code := put(`{"copies": "5"}`); code != http.StatusBadRequest {
		t.Fatalf("a value outside the options should be rejected, got %d", code)
	}
	if code := put(`{"nope": "1"}`); code != http.StatusBadRequest {
		t.Fatalf("unknown params should be rejected, got %d", code)
	}
	req, _ := http.NewRequest(http.MethodDelete, url+"/api/params", nil)
	if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode != http.StatusAccepted {
		t.Fatalf("DELETE failed: %v", err)
	}
	for st = next(); st.Triangles != 12; st = next() {
	}

	// Every model keeps its own overrides when switching between them.
	pick := func(m string) int {
		req, _ := http.NewRequest(http.MethodPut, url+"/api/model", strings.NewReader(`{"model": "`+m+`"}`))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	put(`{"copies": "2"}`)
	for st = next(); st.Triangles != 24; st = next() {
	}
	if code := pick(other); code != http.StatusOK {
		t.Fatalf("selecting a model returned %d", code)
	}
	for st = next(); !st.OK || st.Model != other; st = next() {
	}
	if st.Triangles != 12 || len(st.Overrides) != 0 {
		t.Fatalf("the other model should start with its own params: %+v", st)
	}
	if code := pick(filepath.Join(dir, "nope.tcad")); code != http.StatusBadRequest {
		t.Fatalf("unknown models should be rejected, got %d", code)
	}
	pick(model)
	for st = next(); !st.OK || st.Model != model; st = next() {
	}
	if st.Triangles != 24 || st.Overrides["copies"] != "2" {
		t.Fatalf("the overrides of the first model were lost: %+v", st)
	}

	// New files in the watched directories show up in the list.
	added := filepath.Join(dir, "c.tcad")
	os.WriteFile(added, []byte("ok"), 0o644)
	for st = next(); !slices.Contains(st.Models, added); st = next() {
	}

	r, err := http.Get(url + "/model.stl")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if len(body) != 84+50*24 || !strings.Contains(string(body[:80]), "COLOR=") {
		t.Fatalf("bad viewer STL (%d bytes)", len(body))
	}
}

func TestDiscover(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"b.tcad", "a.tcad", "notes.txt", "sub/c.tcad", "sub/deep/d.TCAD", ".git/e.tcad", "z/f.tcad"} {
		p := filepath.Join(dir, f)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, nil, 0o644)
	}
	j := func(fs ...string) []string {
		for i, f := range fs {
			fs[i] = filepath.Join(dir, f)
		}
		return fs
	}
	got, err := Discover([]string{dir}, false)
	if err != nil || !slices.Equal(got, j("a.tcad", "b.tcad")) {
		t.Fatalf("flat: %v %v", got, err)
	}
	got, _ = Discover([]string{dir}, true)
	if want := j("a.tcad", "b.tcad", "sub/c.tcad", "sub/deep/d.TCAD", "z/f.tcad"); !slices.Equal(got, want) {
		t.Fatalf("recursive: got %v, want %v", got, want)
	}
	// Explicit files come first, as given, and are not repeated.
	got, _ = Discover([]string{filepath.Join(dir, "notes.txt"), dir, filepath.Join(dir, "a.tcad")}, false)
	if want := j("notes.txt", "a.tcad", "b.tcad"); !slices.Equal(got, want) {
		t.Fatalf("mixed: got %v, want %v", got, want)
	}
	if _, err := Discover([]string{filepath.Join(dir, "missing")}, false); err == nil {
		t.Fatal("missing paths should be reported")
	}
}

// fakeBuild builds a cube from a file that says "ok".
func fakeBuild(file string, defs map[string]string) (*Build, error) {
	src, _ := os.ReadFile(file)
	if strings.TrimSpace(string(src)) != "ok" {
		return nil, errors.New("broken model")
	}
	return &Build{Mesh: cube()}, nil
}

func TestFollowFromEmptyFolder(t *testing.T) {
	dir := t.TempDir()
	s, err := New(Options{Paths: []string{dir}, Recursive: true, Base: dir, Follow: true,
		Build: fakeBuild, Log: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	// Mounted under a prefix, as TCAD Studio does.
	srv := httptest.NewServer(http.StripPrefix("/preview", s))
	defer srv.Close()
	s.Start()

	resp, err := http.Get(srv.URL + "/preview/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	events := bufio.NewReader(resp.Body)
	next := func() status {
		t.Helper()
		for {
			line, err := events.ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			if data, ok := strings.CutPrefix(line, "data: "); ok {
				var st status
				json.Unmarshal([]byte(data), &st)
				return st
			}
		}
	}
	if st := next(); st.Model != "" || len(st.Models) != 0 {
		t.Fatalf("an empty folder should show no model: %+v", st)
	}
	write := func(name string) {
		p := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte("ok"), 0o644)
	}
	// The first model that appears is shown, named relative to the folder.
	write("sub/a.tcad")
	want := filepath.Join("sub", "a.tcad")
	for st := next(); !st.OK || st.Model != want; st = next() {
	}
	// New models and saved ones are followed.
	time.Sleep(50 * time.Millisecond)
	write("b.tcad")
	for st := next(); !st.OK || st.Model != "b.tcad"; st = next() {
	}
	time.Sleep(50 * time.Millisecond)
	write("sub/a.tcad")
	for st := next(); !st.OK || st.Model != want; st = next() {
	}
	if r, err := http.Get(srv.URL + "/preview/model.stl"); err != nil || r.StatusCode != http.StatusOK {
		t.Fatalf("model.stl under the prefix: %v %v", r.StatusCode, err)
	}

	// Close ends the event streams.
	s.Close()
	done := make(chan struct{})
	go func() { io.Copy(io.Discard, events); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the event stream did not end on Close")
	}
}
