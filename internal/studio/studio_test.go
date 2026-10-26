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
	"time"
)

// fakeDialogs answers the save dialog with a fixed path.
type fakeDialogs struct {
	save    string
	gotDir  *string
	gotName *string
}

func (fakeDialogs) ChooseDirectory(title, start string) (string, error) { return "", nil }
func (d fakeDialogs) SaveFile(title, dir, name string) (string, error) {
	*d.gotDir, *d.gotName = dir, name
	return d.save, nil
}

func TestStudio(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses /bin/sh")
	}
	cfg := t.TempDir()
	recentFile = func() string { return filepath.Join(cfg, "recent.json") }
	// The agent's "tcad" is a script that says where it runs.
	exe := filepath.Join(t.TempDir(), "fake-tcad")
	os.WriteFile(exe, []byte("#!/bin/sh\necho \"fake tcad $1\"\n"), 0o755)
	st := New(Options{Exe: exe, Log: io.Discard})
	defer st.Close()
	start, err := st.Start("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(start)
	base := "http://" + u.Host

	// Without the token nothing is served, and the token must match.
	if r, _ := http.Get(base + "/api/state"); r.StatusCode != http.StatusForbidden {
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

	// The start URL sets the session cookie.
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}
	if r, err := c.Get(start); err != nil || r.StatusCode != http.StatusOK {
		t.Fatalf("start URL: %v %v", r.StatusCode, err)
	}
	post := func(path string, body any) (*http.Response, map[string]any) {
		t.Helper()
		b, _ := json.Marshal(body)
		r, err := c.Post(base+path, "application/json", strings.NewReader(string(b)))
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		var out map[string]any
		json.NewDecoder(r.Body).Decode(&out)
		return r, out
	}
	// Requests from other origins are rejected even with the cookie.
	req, _ = http.NewRequest(http.MethodPost, base+"/api/close", strings.NewReader("{}"))
	req.Header.Set("Origin", "http://evil.example")
	if r, _ := c.Do(req); r.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin POST: %d", r.StatusCode)
	}

	events, err := c.Get(base + "/api/events")
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
	if r, out := post("/api/open", map[string]string{"path": dir}); r.StatusCode != http.StatusOK || out["dir"] != dir {
		t.Fatalf("open: %d %v", r.StatusCode, out)
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
	r, out := post("/preview/api/save", nil)
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
	r, out = post("/api/terminal/start", map[string]any{"harness": "shell", "cols": 80, "rows": 24})
	if r.StatusCode != http.StatusOK {
		t.Fatalf("terminal: %v", out)
	}
	id := out["id"].(string)
	post("/api/terminal/input", map[string]string{"id": id, "data": "cd \"$TCAD_PROJECT\" && tcad check && pwd; exit\n"})
	var output strings.Builder
	for !strings.Contains(output.String(), "fake tcad check") || !strings.Contains(output.String(), dir) {
		ev := next("terminal:data")
		b, _ := base64.StdEncoding.DecodeString(ev["data"].(string))
		output.Write(b)
	}
	next("terminal:exit")

	// Closing the folder stops serving its preview.
	post("/api/close", map[string]any{})
	if r, _ := c.Get(base + "/preview/"); r.StatusCode != http.StatusNotFound {
		t.Fatalf("preview after close: %d", r.StatusCode)
	}
	var recent []Recent
	b, _ := os.ReadFile(recentFile())
	if json.Unmarshal(b, &recent); len(recent) != 1 || recent[0].Dir != dir {
		t.Fatalf("recent folders: %s", b)
	}
}
