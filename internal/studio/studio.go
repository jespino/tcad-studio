// Package studio is the backend of TCAD Studio: a folder of models, the live
// viewer of internal/watch for them (left) and a terminal running a coding
// agent in that folder (right).
//
// What every agent studio has (native dialogs, recent folders, the folder
// browser, the agent terminal) is agentstudio's shared.Base, embedded in
// Studio. The UI is the Vite app in frontend/: the desktop shell
// (cmd/tcad-studio) binds Studio with Wails, and "tcad studio" serves it to a
// browser with agentstudio's browser package (session token, own origin
// only).
//
// The viewer streams its updates (server-sent events), which a webview's
// custom scheme can't carry, so the desktop app shows it from a loopback
// server (ServePreviewHTTP) whose path holds a secret: the viewer's own
// requests are relative, so they keep it. In a browser it is /preview/.
package studio

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	shared "github.com/jespino/agentstudio/studio"
	tcad "github.com/jespino/tcad-studio"
	"github.com/jespino/tcad-studio/internal/agentctx"
	"github.com/jespino/tcad-studio/internal/model"
	"github.com/jespino/tcad-studio/internal/watch"
)

// EventFolder is sent when a folder opens or closes. Payload: *Folder (nil
// when closed).
const EventFolder = shared.EventProject

// EventPreview is sent when the viewer shows another model or its build
// result changes. Payload: PreviewState.
const EventPreview = "preview:state"

// PreviewState is what the viewer shows, for the UI's source panel.
type PreviewState struct {
	Model string `json:"model"`           // the model shown, relative to the folder
	Error string `json:"error,omitempty"` // its build error ("file:line:col: message")
}

// Dialogs are native dialogs, provided by the desktop shell.
type Dialogs = shared.Dialogs

// Options configures the studio.
type Options struct {
	// Exe is the program the agent's terminal runs as "tcad". It must
	// understand the tcad commands; by default it is the running program.
	Exe string
	Log io.Writer
}

// Folder is the open folder.
type Folder struct {
	Path string `json:"path"` // the folder (as projects are named in the UI)
	Dir  string `json:"dir"`
	Name string `json:"name"`
}

// recentFile can be moved for tests ("" keeps the default,
// <config>/tcad/recent.json).
var recentFile = func() string { return "" }

// Studio is the backend. Its exported methods are the UI's API; it serves
// the viewer at /preview/ (ServeHTTP).
type Studio struct {
	*shared.Base
	opts   Options
	logger *log.Logger

	mu       sync.Mutex
	folder   *Folder
	preview  *watch.Server
	loopback string // the viewer's address on the loopback server ("" in a browser)
	shown    PreviewState

	states chan previewState // latest viewer state, written to .tcad/preview.json
	done   chan struct{}
	close  sync.Once
}

type previewState struct {
	dir  string
	data []byte
}

// New creates the studio.
func New(opts Options) *Studio {
	if opts.Log == nil {
		opts.Log = os.Stderr
	}
	s := &Studio{
		opts:   opts,
		logger: log.New(opts.Log, "", log.Ltime),
		states: make(chan previewState, 1),
		done:   make(chan struct{}),
	}
	s.Base = shared.New(shared.Config{
		ID:             "tcad",
		Ext:            ".tcad",
		FolderProjects: true,
		RecentFile:     recentFile(),
		Agent: shared.Agent{
			// Refresh the context: the user may have edited or deleted it.
			Context: func(dir, _ string) error { return agentctx.Write(dir, tcad.Skill, tcad.SkillDir) },
			Env:     func(dir, _ string) []string { return []string{"TCAD_PROJECT=" + dir} },
			// The skill tells the agent to run tcad: this program.
			Tool:    "tcad",
			ToolExe: opts.Exe,
		},
	})
	go s.writePreviews()
	return s
}

// HiddenMethods are the exported methods that are not part of the API.
func (s *Studio) HiddenMethods() []string {
	return append(s.Base.HiddenMethods(), "Open", "CloseFolder", "ServePreviewHTTP")
}

// Shutdown stops the agents, the viewer and the loopback server.
func (s *Studio) Shutdown() {
	s.close.Do(func() {
		close(s.done)
		s.Base.Shutdown()
		s.mu.Lock()
		if s.preview != nil {
			s.preview.Close()
		}
		s.mu.Unlock()
	})
}

// ---- Folders -----------------------------------------------------------------

// OpenProject opens a folder: it writes the agent context into it (AGENTS.md
// and the skill) and shows its models, including those in subfolders.
func (s *Studio) OpenProject(path string) (*Folder, error) { return s.Open(path) }

// Open is OpenProject.
func (s *Studio) Open(path string) (*Folder, error) {
	dir, err := s.Resolve(path)
	if err != nil {
		return nil, err
	}
	if err := agentctx.Write(dir, tcad.Skill, tcad.SkillDir); err != nil {
		return nil, fmt.Errorf("writing the agent context: %w", err)
	}
	opts := watch.Options{
		Paths: []string{dir}, Recursive: true, Base: dir, Follow: true,
		Build:   model.WatchBuild,
		OnState: func(data []byte) { s.queuePreview(dir, data); s.previewChanged(dir, data) },
		Log:     s.opts.Log,
	}
	if s.Dialogs() != nil {
		opts.Save = s.saveSTL
	}
	pv, err := watch.New(opts)
	if err != nil {
		return nil, err
	}
	f := &Folder{Path: dir, Dir: dir, Name: filepath.Base(dir)}
	s.mu.Lock()
	old := s.preview
	s.preview, s.folder, s.shown = pv, f, PreviewState{}
	s.mu.Unlock()
	if old != nil {
		old.Close()
	}
	// An agent started in the previous folder must not keep working there
	// from a panel that now belongs to this one.
	s.SetProject(dir, "")
	pv.Start()
	s.logger.Printf("opened %s", dir)
	s.Emit(EventFolder, f)
	return f, nil
}

// NewProject creates the folder dir (if needed) and opens it.
func (s *Studio) NewProject(dir string) (*Folder, error) {
	if dir == "" {
		return nil, errors.New("choose a folder")
	}
	dir, err := filepath.Abs(shared.ExpandHome(dir))
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return s.Open(dir)
}

// CloseProject goes back to the welcome screen.
func (s *Studio) CloseProject() { s.CloseFolder() }

// CloseFolder is CloseProject.
func (s *Studio) CloseFolder() {
	s.mu.Lock()
	if s.preview != nil {
		s.preview.Close()
	}
	s.preview, s.folder, s.shown = nil, nil, PreviewState{}
	s.mu.Unlock()
	s.SetProject("", "")
	s.Emit(EventFolder, nil)
}

// CurrentProject returns the open folder, or nil.
func (s *Studio) CurrentProject() *Folder {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.folder == nil {
		return nil
	}
	f := *s.folder
	return &f
}

// PreviewURL is where the UI loads the viewer of the open folder from.
func (s *Studio) PreviewURL() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loopback != "" {
		return s.loopback
	}
	return "/preview/"
}

// ---- The viewer ----------------------------------------------------------------

// ServeHTTP serves the viewer of the open folder at /preview/.
func (s *Studio) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/preview" {
		http.Redirect(w, r, "/preview/", http.StatusFound)
		return
	}
	s.mu.Lock()
	pv := s.preview
	s.mu.Unlock()
	if pv == nil || !strings.HasPrefix(r.URL.Path, "/preview/") {
		http.Error(w, "no folder is open", http.StatusNotFound)
		return
	}
	http.StripPrefix("/preview", pv).ServeHTTP(w, r)
}

// ServePreviewHTTP serves the viewer on a loopback address, at a path only
// the UI learns (PreviewURL), for webviews that can't stream from their own
// scheme. Requests must name that address as their Host (no DNS rebinding)
// and writes must come from the viewer's own origin. It returns the
// function that stops it.
func (s *Studio) ServePreviewHTTP() (func(), error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	b := make([]byte, 24)
	rand.Read(b)
	prefix := "/" + hex.EncodeToString(b)
	host := ln.Addr().String()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rest, ok := strings.CutPrefix(r.URL.Path, prefix)
		if r.Host != host || !ok {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if o := r.Header.Get("Origin"); o != "" && o != "http://"+host {
				http.Error(w, "cross-origin request", http.StatusForbidden)
				return
			}
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path = rest
		s.ServeHTTP(w, r2)
	})
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	go srv.Serve(ln)
	s.mu.Lock()
	s.loopback = "http://" + host + prefix + "/preview/"
	s.mu.Unlock()
	return func() { srv.Close() }, nil
}

// previewChanged follows the viewer: what it shows goes to the UI, and the
// model it rebuilt may have been edited by the agent (SourceChanged tells the
// source panel).
func (s *Studio) previewChanged(dir string, data []byte) {
	var st PreviewState
	if json.Unmarshal(data, &st) != nil {
		return
	}
	if st.Model != "" {
		s.SourceChanged(filepath.Join(dir, filepath.FromSlash(st.Model)))
	}
	s.mu.Lock()
	same := st == s.shown
	s.shown = st
	s.mu.Unlock()
	if !same {
		s.Emit(EventPreview, st)
	}
}

// Preview returns what the viewer shows.
func (s *Studio) Preview() PreviewState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.shown
}

// ShowModel switches the viewer to a model of the folder (a relative path).
func (s *Studio) ShowModel(model string) error {
	s.mu.Lock()
	pv := s.preview
	s.mu.Unlock()
	if pv == nil {
		return errors.New("no folder is open")
	}
	return pv.ShowModel(model)
}

// queuePreview keeps the latest viewer state for writePreviews, without
// blocking the viewer.
func (s *Studio) queuePreview(dir string, data []byte) {
	st := previewState{dir, data}
	for {
		select {
		case s.states <- st:
			return
		default:
			select {
			case <-s.states:
			default:
			}
		}
	}
}

// writePreviews writes .tcad/preview.json: what the user is looking at, for
// the agent (see agentctx/templates/AGENTS.md).
func (s *Studio) writePreviews() {
	for {
		select {
		case <-s.done:
			return
		case st := <-s.states:
			var out bytes.Buffer
			if json.Indent(&out, st.data, "", "  ") != nil {
				continue
			}
			out.WriteByte('\n')
			dir := filepath.Join(st.dir, ".tcad")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				continue
			}
			// The folder holds studio state only: keep it out of git.
			if _, err := os.Stat(filepath.Join(dir, ".gitignore")); os.IsNotExist(err) {
				os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("*\n"), 0o644)
			}
			if err := shared.WriteAtomic(filepath.Join(dir, "preview.json"), out.Bytes()); err != nil {
				s.logger.Printf("preview.json: %v", err)
			}
		}
	}
}

// saveSTL asks where to save a model's STL and writes it there. The dialog
// starts where the last STL went, or next to the model.
func (s *Studio) saveSTL(file, name string, stl []byte) (string, error) {
	return s.SaveAs("Save STL", filepath.Dir(file), name, stl)
}
