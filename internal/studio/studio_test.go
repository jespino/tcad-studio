package studio

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jespino/agentstudio/browser"
	shared "github.com/jespino/agentstudio/studio"
)

// fakeDialogs answers the save dialog with a fixed path.
type fakeDialogs struct {
	save    string
	gotDir  *string
	gotName *string
}

func (fakeDialogs) ChooseDirectory(title, start string, canCreate bool) (string, error) {
	return "", nil
}
func (fakeDialogs) ChooseFiles(title, start string, filters []shared.Filter) ([]string, error) {
	return nil, nil
}
func (d fakeDialogs) SaveFile(title, dir, name string) (string, error) {
	*d.gotDir, *d.gotName = dir, name
	return d.save, nil
}

func TestStudio(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses /bin/sh")
	}
	cfg := t.TempDir()
	recent := filepath.Join(cfg, "recent.json")
	recentFile = func() string { return recent }
	// The agent's "tcad" is a script that says where it runs.
	exe := filepath.Join(t.TempDir(), "fake-tcad")
	os.WriteFile(exe, []byte("#!/bin/sh\necho \"fake tcad $1\"\n"), 0o755)
	st := New(Options{Exe: exe, Log: io.Discard})
	defer st.Shutdown()
	ui := fstest.MapFS{"index.html": {Data: []byte("<h1>TCAD Studio</h1>")}}
	srv, start, err := browser.Listen(st, browser.Options{ID: "tcad", Assets: ui, Routes: []string{"/preview"}}, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	u, _ := url.Parse(start)
	base := "http://" + u.Host

	// Without the token nothing is served, and the token must match.
	if r, _ := http.Get(base + "/"); r.StatusCode != http.StatusForbidden {
		t.Fatalf("no cookie: %d", r.StatusCode)
	}
	if r, _ := http.Get(base + "/?token=nope"); r.StatusCode != http.StatusForbidden {
		t.Fatalf("bad token: %d", r.StatusCode)
	}
	// Another host name for our address (DNS rebinding) is rejected.
	req, _ := http.NewRequest(http.MethodGet, start, nil)
	req.Host = "evil.example:" + u.Port()
	if r, _ := http.DefaultClient.Do(req); r.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign host: %d", r.StatusCode)
	}

	// The start URL sets the session cookie and shows the UI.
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}
	if r, err := c.Get(start); err != nil || r.StatusCode != http.StatusOK {
		t.Fatalf("start URL: %v %v", r.StatusCode, err)
	}
	rpc := func(method string, args ...any) (int, map[string]any) {
		t.Helper()
		if args == nil {
			args = []any{}
		}
		b, _ := json.Marshal(args)
		r, err := c.Post(base+"/rpc/"+method, "application/json", strings.NewReader(string(b)))
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		var out map[string]any
		json.NewDecoder(r.Body).Decode(&out)
		return r.StatusCode, out
	}
	// Requests from other origins are rejected even with the cookie.
	req, _ = http.NewRequest(http.MethodPost, base+"/rpc/CloseProject", strings.NewReader("[]"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://evil.example")
	if r, _ := c.Do(req); r.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin POST: %d", r.StatusCode)
	}

	events, err := c.Get(base + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer events.Body.Close()
	lines := bufio.NewReader(events.Body)
	next := func(name string) map[string]any {
		t.Helper()
		for {
			line, err := lines.ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			data, ok := strings.CutPrefix(line, "data: ")
			if !ok {
				continue
			}
			var ev struct {
				Name string
				Data map[string]any
			}
			json.Unmarshal([]byte(data), &ev)
			if ev.Name == name {
				return ev.Data
			}
		}
	}

	// With native dialogs, the preview saves STLs wherever the user picks.
	saved := filepath.Join(t.TempDir(), "copy") // the extension is added
	var gotDir, gotName string
	st.SetDialogs(fakeDialogs{save: saved, gotDir: &gotDir, gotName: &gotName})

	// Opening a folder writes the agent context and serves its preview.
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "part.tcad"), []byte("box(10, 10, 10)\n"), 0o644)
	if code, out := rpc("OpenProject", dir); code != http.StatusOK || out["result"].(map[string]any)["dir"] != dir {
		t.Fatalf("open: %d %v", code, out)
	}
	if ev := next(EventFolder); ev["name"] != filepath.Base(dir) {
		t.Fatalf("folder event %v", ev)
	}
	if _, out := rpc("PreviewURL"); out["result"] != "/preview/" {
		t.Fatalf("preview URL %v", out)
	}
	for _, f := range []string{"AGENTS.md", "CLAUDE.md", ".agents/skills/tcad/SKILL.md", ".claude/skills/tcad/reference.md"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("missing %s", f)
		}
	}
	var state struct {
		Model   string `json:"model"`
		OK      bool   `json:"ok"`
		CanSave bool   `json:"canSave"`
	}
	for deadline := time.Now().Add(10 * time.Second); !state.OK; time.Sleep(50 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the preview did not build the model")
		}
		r, err := c.Get(base + "/preview/api/state")
		if err != nil {
			t.Fatal(err)
		}
		json.NewDecoder(r.Body).Decode(&state)
		r.Body.Close()
	}
	if state.Model != "part.tcad" || !state.CanSave {
		t.Fatalf("preview state %+v", state)
	}
	r, err := c.Post(base+"/preview/api/save", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	json.NewDecoder(r.Body).Decode(&out)
	r.Body.Close()
	if r.StatusCode != http.StatusOK || out["path"] != saved+".stl" {
		t.Fatalf("save: %d %v", r.StatusCode, out)
	}
	if gotDir != dir || gotName != "part.stl" {
		t.Fatalf("the dialog started in %q with %q", gotDir, gotName)
	}
	if b, err := os.ReadFile(saved + ".stl"); err != nil || len(b) != 84+50*12 {
		t.Fatalf("saved STL: %d bytes, %v", len(b), err)
	}
	// What the user sees is written for the agent.
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(50 * time.Millisecond) {
		b, _ := os.ReadFile(filepath.Join(dir, ".tcad", "preview.json"))
		if strings.Contains(string(b), `"ok": true`) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("preview.json: %s", b)
		}
	}

	// The agent runs in the folder with tcad on its PATH.
	code, res := rpc("StartTerminal", "shell", 80, 24)
	if code != http.StatusOK || res["error"] != nil {
		t.Fatalf("terminal: %v", res)
	}
	id := res["result"].(string)
	rpc("TerminalWrite", id, "cd \"$TCAD_PROJECT\" && tcad check && pwd; exit\n")
	var output strings.Builder
	for !strings.Contains(output.String(), "fake tcad check") || !strings.Contains(output.String(), dir) {
		ev := next("terminal:data")
		b, _ := base64.StdEncoding.DecodeString(ev["data"].(string))
		output.Write(b)
	}
	next("terminal:exit")

	// Resume continues a saved conversation with the agent's own option.
	// The fake "claude" says how it was run.
	bin := t.TempDir()
	os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\necho \"claude args: $*\"\n"), 0o755)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if code, res := rpc("ResumeTerminal", "claude", 80, 24); code != http.StatusOK || res["error"] != nil {
		t.Fatalf("resume: %v", res)
	}
	output.Reset()
	for !strings.Contains(output.String(), "claude args: --resume") {
		ev := next("terminal:data")
		b, _ := base64.StdEncoding.DecodeString(ev["data"].(string))
		output.Write(b)
	}
	next("terminal:exit")
	// A shell has no conversations to resume.
	if _, res := rpc("ResumeTerminal", "shell", 80, 24); res["error"] == nil {
		t.Fatal("resuming a shell should fail")
	}

	// Closing the folder stops serving its preview.
	rpc("CloseProject")
	if r, _ := c.Get(base + "/preview/"); r.StatusCode != http.StatusNotFound {
		t.Fatalf("preview after close: %d", r.StatusCode)
	}
	if _, res := rpc("RecentProjects"); len(res["result"].([]any)) != 1 || res["result"].([]any)[0].(map[string]any)["path"] != dir {
		t.Fatalf("recent folders: %v", res)
	}
	// Internal methods are not part of the API.
	for _, m := range []string{"Open", "CloseFolder", "ServePreviewHTTP", "Shutdown", "SetProject"} {
		if code, _ := rpc(m); code != http.StatusNotFound {
			t.Errorf("%s is reachable: %d", m, code)
		}
	}
}

// The desktop app shows the viewer from a loopback server, at a secret path.
func TestPreviewLoopback(t *testing.T) {
	recentFile = func() string { return filepath.Join(t.TempDir(), "recent.json") }
	st := New(Options{Log: io.Discard})
	defer st.Shutdown()
	stop, err := st.ServePreviewHTTP()
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "part.tcad"), []byte("box(10, 10, 10)\n"), 0o644)
	if _, err := st.Open(dir); err != nil {
		t.Fatal(err)
	}
	pv, err := url.Parse(st.PreviewURL())
	if err != nil || pv.Host == "" || !strings.HasSuffix(pv.Path, "/preview/") || len(pv.Path) < 40 {
		t.Fatalf("preview URL %v", st.PreviewURL())
	}
	get := func(u string, host string) int {
		req, _ := http.NewRequest(http.MethodGet, u, nil)
		if host != "" {
			req.Host = host
		}
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		return r.StatusCode
	}
	if code := get(pv.String()+"api/state", ""); code != http.StatusOK {
		t.Fatalf("viewer state: %d", code)
	}
	if code := get("http://"+pv.Host+"/preview/api/state", ""); code != http.StatusNotFound {
		t.Fatalf("without the secret: %d", code)
	}
	if code := get(pv.String()+"api/state", "evil.example:"+pv.Port()); code != http.StatusNotFound {
		t.Fatalf("foreign host: %d", code)
	}
	req, _ := http.NewRequest(http.MethodPost, pv.String()+"api/save", nil)
	req.Header.Set("Origin", "http://evil.example")
	if r, _ := http.DefaultClient.Do(req); r.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin save: %d", r.StatusCode)
	}
}
