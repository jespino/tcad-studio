// Package studio is the backend of TCAD Studio: a folder of models, the live
// viewer of internal/watch for them (left) and a terminal running a coding
// agent in that folder (right).
//
// It is a local web app. The desktop shell (cmd/tcad-studio, Wails 3) shows
// it in a native window, and "tcad studio" in a browser; both use the same
// server. Every request needs the session token: the start URL carries it,
// and the page then keeps it in a cookie only this origin gets.
//
//	GET  /                          the studio page
//	     /preview/                  the viewer of the open folder (internal/watch)
//	GET  /api/state                 open folder, recent folders, agents, terminals
//	POST /api/open    {path}        open a folder (it gets AGENTS.md and the skill)
//	POST /api/close                 back to the welcome screen
//	POST /api/choose                native folder dialog → {path} ("" if cancelled)
//	GET  /api/dirs?path=            folder listing, for the in-page folder browser
//	POST /api/mkdir   {path}        create a folder (in-page browser)
//	POST /api/terminal/start   {harness, cols, rows} → {id}
//	POST /api/terminal/input   {id, data}
//	POST /api/terminal/resize  {id, cols, rows}
//	POST /api/terminal/stop    {id}
//	GET  /api/terminal/buffer?id=   scrollback (base64), to reattach after a reload
//	GET  /api/events                server-sent events: {name, data}
package studio

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	tcad "github.com/jespino/tcad-studio"
	"github.com/jespino/tcad-studio/internal/agentctx"
	"github.com/jespino/tcad-studio/internal/model"
	"github.com/jespino/tcad-studio/internal/terminal"
	"github.com/jespino/tcad-studio/internal/watch"
)

//go:embed studio.html assets
var content embed.FS

// Events sent to the page, besides terminal.EventData and terminal.EventExit.
const (
	EventFolder        = "folder"         // payload: *Folder (nil when closed)
	EventTerminalStart = "terminal:start" // payload: {id, harness}
)

const cookieName = "tcad_studio"

// Dialogs are native dialogs, provided by the desktop shell. In a browser
// there are none and the page uses its own folder browser.
type Dialogs interface {
	// ChooseDirectory returns "" if the user cancels.
	ChooseDirectory(title, start string) (string, error)
	// SaveFile asks where to save a file, starting in dir with name
	// suggested. It returns "" if the user cancels.
	SaveFile(title, dir, name string) (string, error)
}

// Options configures the studio.
type Options struct {
	// Exe is the program the agent's terminal runs as "tcad". It must
	// understand the tcad commands; by default it is the running program.
	Exe string
	Log io.Writer
}

// Folder is the open folder.
type Folder struct {
	Dir  string `json:"dir"`
	Name string `json:"name"`
}

// Studio is the backend. It is an http.Handler; Start serves it.
type Studio struct {
	opts   Options
	logger *log.Logger
	token  string
	mux    *http.ServeMux
	terms  *terminal.Manager
	binDir string // holds the "tcad" link put first in the agent's PATH

	mu      sync.Mutex
	folder  *Folder
	preview *watch.Server
	dialogs Dialogs
	hosts   []string // Host headers accepted (the listen address)
	saveDir string   // where the last STL was saved
	subs    map[chan []byte]struct{}
	ln      net.Listener

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
	b := make([]byte, 24)
	rand.Read(b)
	s := &Studio{
		opts:   opts,
		logger: log.New(opts.Log, "", log.Ltime),
		token:  hex.EncodeToString(b),
		subs:   map[chan []byte]struct{}{},
		states: make(chan previewState, 1),
		done:   make(chan struct{}),
	}
	s.terms = terminal.NewManager(s.send)
	s.binDir = s.linkTCAD()
	go s.writePreviews()

	s.mux = http.NewServeMux()
	static, _ := fs.Sub(content, "assets")
	s.mux.HandleFunc("GET /{$}", s.handleIndex)
	s.mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(static))))
	s.mux.HandleFunc("/preview", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/preview/", http.StatusFound)
	})
	s.mux.HandleFunc("/preview/", s.handlePreview)
	s.mux.HandleFunc("GET /api/state", s.handleState)
	s.mux.HandleFunc("POST /api/open", s.handleOpen)
	s.mux.HandleFunc("POST /api/close", s.handleClose)
	s.mux.HandleFunc("POST /api/choose", s.handleChoose)
	s.mux.HandleFunc("GET /api/dirs", s.handleDirs)
	s.mux.HandleFunc("POST /api/mkdir", s.handleMkdir)
	s.mux.HandleFunc("POST /api/terminal/start", s.handleTermStart)
	s.mux.HandleFunc("POST /api/terminal/input", s.handleTermInput)
	s.mux.HandleFunc("POST /api/terminal/resize", s.handleTermResize)
	s.mux.HandleFunc("POST /api/terminal/stop", s.handleTermStop)
	s.mux.HandleFunc("GET /api/terminal/buffer", s.handleTermBuffer)
	s.mux.HandleFunc("GET /api/events", s.handleEvents)
	return s
}

// SetDialogs installs native dialogs. Call it before opening a folder: the
// preview of a folder opened with dialogs saves STLs through them.
func (s *Studio) SetDialogs(d Dialogs) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dialogs = d
}

// Start serves the studio on addr (use a loopback address) and returns the
// URL to open, which carries the session token.
func (s *Studio) Start(addr string) (string, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", fmt.Errorf("cannot listen on %s: %w", addr, err)
	}
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	s.mu.Lock()
	s.ln = ln
	s.hosts = []string{ln.Addr().String(), "localhost:" + port, "127.0.0.1:" + port}
	s.mu.Unlock()
	go (&http.Server{Handler: s}).Serve(ln)
	return fmt.Sprintf("http://%s/?token=%s", ln.Addr(), s.token), nil
}

// Close stops the agents, the viewer and the server.
func (s *Studio) Close() {
	s.close.Do(func() {
		close(s.done)
		s.terms.StopAll()
		s.mu.Lock()
		if s.preview != nil {
			s.preview.Close()
		}
		if s.ln != nil {
			s.ln.Close()
		}
		s.mu.Unlock()
		if s.binDir != "" {
			os.RemoveAll(s.binDir)
		}
	})
}

// ServeHTTP checks the session and serves the studio.
func (s *Studio) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	hosts := s.hosts
	s.mu.Unlock()
	// Only our own address: a page elsewhere can't reach us by pointing
	// one of its names at 127.0.0.1 (DNS rebinding).
	if hosts != nil && !contains(hosts, r.Host) {
		http.Error(w, "unexpected host", http.StatusForbidden)
		return
	}
	if t := r.URL.Query().Get("token"); t != "" && r.URL.Path == "/" {
		if !s.validToken(t) {
			http.Error(w, "invalid token", http.StatusForbidden)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: cookieName, Value: s.token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	c, err := r.Cookie(cookieName)
	if err != nil || !s.validToken(c.Value) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprintln(w, "TCAD Studio: open the full address (with ?token=...) it prints when it starts.")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		if o := r.Header.Get("Origin"); o != "" && !contains(hosts, strings.TrimPrefix(o, "http://")) {
			http.Error(w, "cross-origin request", http.StatusForbidden)
			return
		}
	}
	s.mux.ServeHTTP(w, r)
}

func (s *Studio) validToken(t string) bool {
	return subtle.ConstantTimeCompare([]byte(t), []byte(s.token)) == 1
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// ---- Folders -----------------------------------------------------------------

// Open opens a folder: it writes the agent context into it (AGENTS.md and the
// skill) and shows its models, including those in subfolders.
func (s *Studio) Open(dir string) (*Folder, error) {
	dir, err := filepath.Abs(expandHome(dir))
	if err != nil {
		return nil, err
	}
	if st, err := os.Stat(dir); err != nil {
		return nil, fmt.Errorf("%s does not exist", dir)
	} else if !st.IsDir() {
		return nil, fmt.Errorf("%s is not a folder", dir)
	}
	if err := agentctx.Write(dir, tcad.Skill, tcad.SkillDir); err != nil {
		return nil, fmt.Errorf("writing the agent context: %w", err)
	}
	opts := watch.Options{
		Paths: []string{dir}, Recursive: true, Base: dir, Follow: true,
		Build:   model.WatchBuild,
		OnState: func(data []byte) { s.queuePreview(dir, data) },
		Log:     s.opts.Log,
	}
	s.mu.Lock()
	if s.dialogs != nil {
		opts.Save = s.saveSTL
	}
	s.mu.Unlock()
	pv, err := watch.New(opts)
	if err != nil {
		return nil, err
	}
	f := &Folder{Dir: dir, Name: filepath.Base(dir)}
	s.mu.Lock()
	old, oldFolder := s.preview, s.folder
	s.preview, s.folder = pv, f
	s.mu.Unlock()
	if old != nil {
		old.Close()
	}
	if oldFolder != nil && oldFolder.Dir != dir {
		// An agent started in the previous folder must not keep working
		// there from a panel that now belongs to this one.
		s.terms.StopAll()
	}
	pv.Start()
	rememberFolder(dir)
	s.logger.Printf("opened %s", dir)
	s.send(EventFolder, f)
	return f, nil
}

// CloseFolder goes back to the welcome screen.
func (s *Studio) CloseFolder() {
	s.terms.StopAll()
	s.mu.Lock()
	if s.preview != nil {
		s.preview.Close()
	}
	s.preview, s.folder = nil, nil
	s.mu.Unlock()
	s.send(EventFolder, nil)
}

func (s *Studio) current() *Folder {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.folder == nil {
		return nil
	}
	f := *s.folder
	return &f
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
			if err := writeAtomic(filepath.Join(dir, "preview.json"), out.Bytes()); err != nil {
				s.logger.Printf("preview.json: %v", err)
			}
		}
	}
}

// saveSTL asks where to save a model's STL and writes it there. The dialog
// starts where the last STL went, or next to the model.
func (s *Studio) saveSTL(file, name string, stl []byte) (string, error) {
	s.mu.Lock()
	d, dir := s.dialogs, s.saveDir
	s.mu.Unlock()
	if d == nil {
		return "", errors.New("no native dialogs in this shell")
	}
	if dir == "" {
		dir = filepath.Dir(file)
	}
	path, err := d.SaveFile("Save STL", dir, name)
	if err != nil || path == "" {
		return "", err
	}
	if !strings.EqualFold(filepath.Ext(path), ".stl") {
		path += ".stl"
	}
	if err := writeAtomic(path, stl); err != nil {
		return "", err
	}
	s.mu.Lock()
	s.saveDir = filepath.Dir(path)
	s.mu.Unlock()
	return path, nil
}

// ---- Agent terminal ------------------------------------------------------------

// linkTCAD makes a folder with a "tcad" link to the program, for the
// agent's PATH: the skill tells the agent to run tcad.
func (s *Studio) linkTCAD() string {
	exe := s.opts.Exe
	if exe == "" {
		var err error
		if exe, err = os.Executable(); err != nil {
			return ""
		}
	}
	dir, err := os.MkdirTemp("", "tcad-studio-")
	if err != nil {
		return ""
	}
	name := "tcad"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	link := filepath.Join(dir, name)
	if os.Symlink(exe, link) != nil && os.Link(exe, link) != nil {
		os.RemoveAll(dir)
		s.logger.Printf("cannot link %s into the agent's PATH; it will use the tcad on PATH", exe)
		return ""
	}
	return dir
}

func (s *Studio) startTerminal(harness string, cols, rows int) (string, error) {
	f := s.current()
	if f == nil {
		return "", errors.New("no folder is open")
	}
	var h *terminal.Harness
	for _, x := range terminal.DefaultHarnesses() {
		if x.ID == harness {
			h = &x
			break
		}
	}
	if h == nil {
		return "", fmt.Errorf("unknown agent %q", harness)
	}
	// Refresh the context: the user may have edited or deleted it.
	if err := agentctx.Write(f.Dir, tcad.Skill, tcad.SkillDir); err != nil {
		return "", fmt.Errorf("writing the agent context: %w", err)
	}
	env := []string{"TCAD_PROJECT=" + f.Dir}
	if s.binDir != "" {
		env = append(env, "PATH="+s.binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
	id, err := s.terms.Start(*h, f.Dir, env, cols, rows)
	if err == nil {
		// Other pages (or the one that asked, before it gets the reply)
		// attach to it if they show no agent.
		s.send(EventTerminalStart, map[string]string{"id": id, "harness": h.ID})
	}
	return id, err
}

// ---- Events ------------------------------------------------------------------

func (s *Studio) send(name string, data any) {
	msg, err := json.Marshal(map[string]any{"name": name, "data": data})
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for c := range s.subs {
		select {
		case c <- msg:
		default: // slow page: it reloads the state on reconnect
		}
	}
}

func (s *Studio) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	c := make(chan []byte, 256)
	s.mu.Lock()
	s.subs[c] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.subs, c)
		s.mu.Unlock()
	}()
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()
	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-s.done:
			return
		case msg := <-c:
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

// ---- Handlers ----------------------------------------------------------------

func reply(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, err error) {
	reply(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(v); err != nil {
		fail(w, fmt.Errorf("invalid JSON: %w", err))
		return false
	}
	return true
}

func (s *Studio) handleIndex(w http.ResponseWriter, r *http.Request) {
	page, _ := content.ReadFile("studio.html")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(page)
}

func (s *Studio) handlePreview(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	pv := s.preview
	s.mu.Unlock()
	if pv == nil {
		http.Error(w, "no folder is open", http.StatusNotFound)
		return
	}
	http.StripPrefix("/preview", pv).ServeHTTP(w, r)
}

func (s *Studio) handleState(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	native := s.dialogs != nil
	s.mu.Unlock()
	home, _ := os.UserHomeDir()
	terms := s.terms.List()
	sort.Slice(terms, func(i, j int) bool { return terms[i].ID < terms[j].ID })
	reply(w, http.StatusOK, map[string]any{
		"folder":    s.current(),
		"recent":    recentFolders(),
		"harnesses": terminal.DefaultHarnesses(),
		"terminals": terms,
		"native":    native,
		"home":      home,
	})
}

func (s *Studio) handleOpen(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if !decode(w, r, &req) {
		return
	}
	f, err := s.Open(req.Path)
	if err != nil {
		fail(w, err)
		return
	}
	reply(w, http.StatusOK, f)
}

func (s *Studio) handleClose(w http.ResponseWriter, r *http.Request) {
	s.CloseFolder()
	reply(w, http.StatusOK, map[string]any{})
}

func (s *Studio) handleChoose(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	d := s.dialogs
	s.mu.Unlock()
	if d == nil {
		fail(w, errors.New("no native dialogs in this shell"))
		return
	}
	start, _ := os.UserHomeDir()
	if f := s.current(); f != nil {
		start = filepath.Dir(f.Dir)
	}
	path, err := d.ChooseDirectory("Open folder", start)
	if err != nil {
		fail(w, err)
		return
	}
	reply(w, http.StatusOK, map[string]string{"path": path})
}

// DirEntry is a subfolder in a listing.
type DirEntry struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	HasModels bool   `json:"hasModels"` // holds .tcad files
}

func (s *Studio) handleDirs(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		path, _ = os.UserHomeDir()
	}
	path, err := filepath.Abs(expandHome(path))
	if err != nil {
		fail(w, err)
		return
	}
	ents, err := os.ReadDir(path)
	if err != nil {
		fail(w, err)
		return
	}
	out := struct {
		Path    string     `json:"path"`
		Parent  string     `json:"parent,omitempty"`
		Models  int        `json:"models"` // .tcad files directly inside
		Entries []DirEntry `json:"entries"`
	}{Path: path, Entries: []DirEntry{}}
	if parent := filepath.Dir(path); parent != path {
		out.Parent = parent
	}
	out.Models = countModels(path)
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		full := filepath.Join(path, e.Name())
		if st, err := os.Stat(full); err != nil || !st.IsDir() {
			continue
		}
		out.Entries = append(out.Entries, DirEntry{Name: e.Name(), Path: full, HasModels: countModels(full) > 0})
	}
	sort.Slice(out.Entries, func(i, j int) bool {
		return strings.ToLower(out.Entries[i].Name) < strings.ToLower(out.Entries[j].Name)
	})
	reply(w, http.StatusOK, out)
}

func countModels(dir string) int {
	ents, _ := os.ReadDir(dir)
	n := 0
	for _, e := range ents {
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".tcad") {
			n++
		}
	}
	return n
}

func (s *Studio) handleMkdir(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if !decode(w, r, &req) {
		return
	}
	path, err := filepath.Abs(expandHome(req.Path))
	if err == nil {
		err = os.Mkdir(path, 0o755)
	}
	if err != nil {
		fail(w, err)
		return
	}
	reply(w, http.StatusOK, map[string]string{"path": path})
}

type termReq struct {
	ID      string `json:"id"`
	Harness string `json:"harness"`
	Data    string `json:"data"`
	Cols    int    `json:"cols"`
	Rows    int    `json:"rows"`
}

func (s *Studio) handleTermStart(w http.ResponseWriter, r *http.Request) {
	var req termReq
	if !decode(w, r, &req) {
		return
	}
	id, err := s.startTerminal(req.Harness, req.Cols, req.Rows)
	if err != nil {
		fail(w, err)
		return
	}
	reply(w, http.StatusOK, map[string]string{"id": id})
}

func (s *Studio) handleTermInput(w http.ResponseWriter, r *http.Request) {
	var req termReq
	if !decode(w, r, &req) {
		return
	}
	if err := s.terms.Write(req.ID, req.Data); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Studio) handleTermResize(w http.ResponseWriter, r *http.Request) {
	var req termReq
	if !decode(w, r, &req) {
		return
	}
	if err := s.terms.Resize(req.ID, req.Cols, req.Rows); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Studio) handleTermStop(w http.ResponseWriter, r *http.Request) {
	var req termReq
	if !decode(w, r, &req) {
		return
	}
	if err := s.terms.Stop(req.ID); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Studio) handleTermBuffer(w http.ResponseWriter, r *http.Request) {
	buf, err := s.terms.Buffer(r.URL.Query().Get("id"))
	if err != nil {
		fail(w, err)
		return
	}
	reply(w, http.StatusOK, map[string]string{"data": buf})
}

// ---- Helpers -------------------------------------------------------------------

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

// writeAtomic writes through a temporary file, so readers never see half of it.
func writeAtomic(path string, b []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	os.Chmod(tmp.Name(), 0o644)
	return os.Rename(tmp.Name(), path)
}

// ---- Recent folders --------------------------------------------------------------

const maxRecent = 12

// Recent is an entry of the recent folders list.
type Recent struct {
	Dir    string    `json:"dir"`
	Name   string    `json:"name"`
	Opened time.Time `json:"opened"`
	Exists bool      `json:"exists"`
}

// recentFile can be moved for tests.
var recentFile = func() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "tcad", "recent.json")
}

func loadRecent() []Recent {
	var list []Recent
	if f := recentFile(); f != "" {
		if b, err := os.ReadFile(f); err == nil {
			json.Unmarshal(b, &list)
		}
	}
	return list
}

func rememberFolder(dir string) {
	f := recentFile()
	if f == "" {
		return
	}
	list := []Recent{{Dir: dir, Name: filepath.Base(dir), Opened: time.Now()}}
	for _, r := range loadRecent() {
		if r.Dir != dir && len(list) < maxRecent {
			list = append(list, r)
		}
	}
	os.MkdirAll(filepath.Dir(f), 0o755)
	if b, err := json.MarshalIndent(list, "", "  "); err == nil {
		os.WriteFile(f, b, 0o644)
	}
}

func recentFolders() []Recent {
	list := loadRecent()
	for i := range list {
		st, err := os.Stat(list[i].Dir)
		list[i].Exists = err == nil && st.IsDir()
	}
	if list == nil {
		list = []Recent{}
	}
	return list
}
