// Package watch implements "tcad watch": it rebuilds a model whenever its
// files change and serves a web page with a live 3D viewer that reloads the
// new STL through Server-Sent Events. The page can also change the model's
// params through a small JSON API, which rebuilds it with the new values.
//
// It can watch several models at once (files and directories of .tcad
// files): the page picks which one is shown, and only that one is built.
//
// Run serves it on its own; New returns the same viewer as an http.Handler
// that can be mounted inside another server (TCAD Studio does that, one per
// open folder).
package watch

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log"
	"maps"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jespino/tcad-studio/internal/dsl"
	"github.com/jespino/tcad-studio/internal/geom"
	"github.com/jespino/tcad-studio/internal/stl"
)

//go:embed index.html assets
var content embed.FS

// Build is the outcome of compiling the model once.
type Build struct {
	Mesh      *geom.Mesh
	Files     []string // external files the model depends on
	OpenEdges int
	Took      time.Duration
	Params    []dsl.ParamValue
}

// Options configures the watcher.
type Options struct {
	// Paths are the .tcad files and directories to watch. Directories
	// contribute the .tcad files they hold (also those in subdirectories
	// with Recursive) and are scanned again while watching, so new files
	// show up in the page.
	Paths     []string
	Recursive bool
	// Base, when set, is the folder the page names models relative to
	// ("piezas/tapa.tcad" instead of the full path).
	Base string
	// Follow switches the page to a model as soon as it is created or saved
	// (by an agent, another editor...), so it shows the file being edited.
	Follow bool
	// Output returns the STL written after every successful build of a
	// model file, or "" to write none.
	Output func(file string) string
	Addr   string // listen address for Run, e.g. "localhost:8080"
	// Defines are the initial param overrides (from -D) of every model.
	Defines map[string]string
	// Build compiles a model file with the given param overrides. It is
	// called again after every change.
	Build func(file string, defines map[string]string) (*Build, error)
	// OnState receives the state sent to the page (JSON) after every change.
	// It is called with the server locked, so it must not block.
	OnState func(state []byte)
	// Ready is called with the URL once Run is listening.
	Ready func(url string)
	Log   io.Writer
}

// status is what the page receives on every update.
type status struct {
	Version    int        `json:"version"`
	OK         bool       `json:"ok"`
	Error      string     `json:"error,omitempty"`
	Model      string     `json:"model"`  // the model shown ("" if there are none)
	Models     []string   `json:"models"` // every model that can be shown
	File       string     `json:"file"`
	Output     string     `json:"output"`
	Triangles  int        `json:"triangles"`
	Size       [3]float64 `json:"size"`
	Volume     float64    `json:"volume"`
	OpenEdges  int        `json:"openEdges"`
	BuildMs    int64      `json:"buildMs"`
	Time       string     `json:"time"`
	HasModel   bool       `json:"hasModel"`
	ModelStamp int        `json:"modelStamp"` // version of the last good model
	// Params come from the last successful build; Overrides are the values
	// set from the page or with -D, as TCAD expressions.
	Params    []dsl.ParamValue  `json:"params"`
	Overrides map[string]string `json:"overrides"`
}

// Server is the viewer of a set of models: an http.Handler plus the
// goroutines that watch and build them (Start, Close).
type Server struct {
	opts    Options
	mux     *http.ServeMux
	done    chan struct{}
	close   sync.Once
	logger  *log.Logger
	mu      sync.Mutex
	st      status
	colored []byte // last good model, with colors, for the viewer
	plain   []byte // the same model as a standard STL, for downloading
	clients map[chan []byte]struct{}

	model     string                       // name of the model shown
	stamps    map[string]stamp             // of every model, to follow edits
	overrides map[string]string            // of the model shown
	saved     map[string]map[string]string // overrides of every model
	files     []string                     // model plus its dependencies
	built     []stamp                      // of files, as the last build read them
	trigger   chan struct{}                // requests a build; builds never overlap
}

// Run blocks serving the viewer and rebuilding on changes.
func Run(opts Options) error {
	s, err := New(opts)
	if err != nil {
		return err
	}
	if len(s.st.Models) == 0 {
		return fmt.Errorf("no .tcad files in %s", strings.Join(opts.Paths, ", "))
	}
	ln, err := net.Listen("tcp", opts.Addr)
	if err != nil {
		return fmt.Errorf("cannot listen on %s: %w", opts.Addr, err)
	}
	if opts.Ready != nil {
		opts.Ready("http://" + ln.Addr().String())
	}
	s.Start()
	defer s.Close()
	return (&http.Server{Handler: s}).Serve(ln)
}

// New creates the viewer of the models in opts.Paths, which may hold none
// yet: the first one that appears is shown. Call Start to begin building.
func New(opts Options) (*Server, error) {
	if opts.Log == nil {
		opts.Log = os.Stderr
	}
	if opts.Output == nil {
		opts.Output = func(string) string { return "" }
	}
	s := &Server{
		opts:    opts,
		done:    make(chan struct{}),
		logger:  log.New(opts.Log, "", log.Ltime),
		clients: map[chan []byte]struct{}{},
		saved:   map[string]map[string]string{},
		trigger: make(chan struct{}, 1),
	}
	files, err := Discover(opts.Paths, opts.Recursive)
	if err != nil {
		return nil, err
	}
	s.st.Models = s.names(files)
	s.stamps = map[string]stamp{}
	for _, m := range s.st.Models {
		s.stamps[m] = stampsOf([]string{s.file(m)})[0]
	}
	if len(s.st.Models) > 0 {
		s.selectModel(s.st.Models[0])
	}

	s.mux = http.NewServeMux()
	assets, _ := fs.Sub(content, "assets")
	s.mux.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(assets))))
	s.mux.HandleFunc("/", s.handleIndex)
	s.mux.HandleFunc("/model.stl", s.handleModel)
	s.mux.HandleFunc("/download.stl", s.handleDownload)
	s.mux.HandleFunc("/events", s.handleEvents)
	s.mux.HandleFunc("/api/state", s.handleState)
	s.mux.HandleFunc("/api/params", s.handleParams)
	s.mux.HandleFunc("/api/model", s.handleModelSelect)
	return s, nil
}

// ServeHTTP serves the viewer page and its API. Its URLs are relative, so
// it also works mounted under a prefix (with http.StripPrefix).
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

// Start begins watching the files and builds the first model.
func (s *Server) Start() {
	go func() {
		for {
			select {
			case <-s.done:
				return
			case <-s.trigger:
				s.rebuild()
			}
		}
	}()
	s.mu.Lock()
	if s.model != "" {
		s.requestBuild()
	}
	s.mu.Unlock()
	go s.watchLoop()
}

// Close stops watching and ends the event streams of open pages.
func (s *Server) Close() {
	s.close.Do(func() { close(s.done) })
}

// file is the path of a model name; names returns the names of model files.
func (s *Server) file(name string) string {
	if s.opts.Base == "" {
		return name
	}
	return filepath.Join(s.opts.Base, name)
}

func (s *Server) names(files []string) []string {
	if s.opts.Base == "" {
		return files
	}
	out := make([]string, len(files))
	for i, f := range files {
		if rel, err := filepath.Rel(s.opts.Base, f); err == nil {
			f = rel
		}
		out[i] = f
	}
	return out
}

// requestBuild queues a build. Requests made while a build is running are
// merged into a single new build.
func (s *Server) requestBuild() {
	select {
	case s.trigger <- struct{}{}:
	default:
	}
}

// selectModel makes model the one shown and built. The page keeps nothing
// of the previous model. Callers hold s.mu (or own s, before serving).
func (s *Server) selectModel(model string) {
	ov, ok := s.saved[model]
	if !ok {
		ov = maps.Clone(s.opts.Defines)
		if ov == nil {
			ov = map[string]string{}
		}
		s.saved[model] = ov
	}
	s.model, s.overrides = model, ov
	s.files, s.built = []string{s.file(model)}, nil
	s.colored, s.plain = nil, nil
	s.st = status{Version: s.st.Version, Models: s.st.Models, Time: s.st.Time}
	s.st.Model = model
	s.st.File = filepath.Base(model)
	if out := s.opts.Output(s.file(model)); out != "" {
		s.st.Output = filepath.Base(out)
	}
}

// rebuild compiles the model, updates the state and notifies the pages.
func (s *Server) rebuild() {
	s.mu.Lock()
	model := s.model
	defs := maps.Clone(s.overrides)
	s.mu.Unlock()
	if model == "" {
		return
	}
	files := []string{s.file(model)}
	// Stamp the model before reading it: a save during the build must
	// trigger another one.
	built := stampsOf(files)
	b, err := s.opts.Build(files[0], defs)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.model != model {
		// Another model was picked meanwhile; its build is already queued.
		return
	}
	output := s.opts.Output(files[0])
	name := filepath.Base(model)
	s.st.Version++
	s.st.Time = time.Now().Format("15:04:05")
	if err != nil {
		s.st.OK = false
		s.st.Error = err.Error()
		s.logger.Printf("%s: error: %v", name, err)
	} else {
		files = append(files, b.Files...)
		built = append(built, stampsOf(b.Files)...)
		var colored, plain bytes.Buffer
		err = stl.WriteBinaryColored(&colored, b.Mesh)
		if err == nil {
			err = stl.WriteBinary(&plain, b.Mesh)
		}
		if err == nil && output != "" {
			err = os.WriteFile(output, plain.Bytes(), 0o644)
		}
		if err != nil {
			s.st.OK = false
			s.st.Error = err.Error()
			s.logger.Printf("%s: error: %v", name, err)
		} else {
			mn, mx, _ := b.Mesh.BBox()
			d := mx.Sub(mn)
			s.colored, s.plain = colored.Bytes(), plain.Bytes()
			s.st.OK = true
			s.st.Error = ""
			s.st.HasModel = true
			s.st.ModelStamp = s.st.Version
			s.st.Triangles = len(b.Mesh.Tris)
			s.st.Size = [3]float64{d.X, d.Y, d.Z}
			s.st.Volume = b.Mesh.Volume()
			s.st.OpenEdges = b.OpenEdges
			s.st.BuildMs = b.Took.Milliseconds()
			s.st.Params = b.Params
			// Forget overrides of params that no longer exist in the file.
			for name := range s.overrides {
				if !slices.ContainsFunc(b.Params, func(p dsl.ParamValue) bool { return p.Name == name }) {
					delete(s.overrides, name)
				}
			}
			msg := fmt.Sprintf("%s: built %d triangles in %s", name, len(b.Mesh.Tris), b.Took.Round(time.Millisecond))
			if output != "" {
				msg += " -> " + output
			}
			s.logger.Print(msg)
		}
	}
	s.files, s.built = files, built
	s.broadcast()
}

// broadcast sends the state to every page. Callers hold s.mu.
func (s *Server) broadcast() {
	data := s.stateJSON()
	if s.opts.OnState != nil {
		s.opts.OnState(data)
	}
	for c := range s.clients {
		select {
		case c <- data:
		default: // slow client: it will get the next update
		}
	}
}

func (s *Server) stateJSON() []byte {
	s.st.Overrides = maps.Clone(s.overrides)
	data, _ := json.Marshal(s.st)
	return data
}

type stamp struct {
	mod  time.Time
	size int64
	ok   bool
}

func stampsOf(files []string) []stamp {
	out := make([]stamp, len(files))
	for i, f := range files {
		if fi, err := os.Stat(f); err == nil {
			out[i] = stamp{fi.ModTime(), fi.Size(), true}
		}
	}
	return out
}

func changed(a, b []stamp) bool {
	if len(a) != len(b) {
		return true
	}
	for i := range a {
		if a[i].ok != b[i].ok || a[i].size != b[i].size || !a[i].mod.Equal(b[i].mod) {
			return true
		}
	}
	return false
}

// Discover lists the models in paths: files are taken as given, and
// directories contribute the .tcad files they hold, also those in their
// subdirectories when recursive (hidden directories are skipped). Models
// keep the order of paths, sorted by directory within each one. Paths that
// cannot be read are reported in err, but the rest are still listed.
func Discover(paths []string, recursive bool) (models []string, err error) {
	seen := map[string]bool{}
	var errs []string
	for _, p := range paths {
		fi, e := os.Stat(p)
		if e != nil {
			errs = append(errs, e.Error())
			continue
		}
		var found []string
		if !fi.IsDir() {
			found = []string{filepath.Clean(p)}
		} else {
			filepath.WalkDir(p, func(path string, d fs.DirEntry, e error) error {
				if e != nil {
					errs = append(errs, e.Error())
					return nil
				}
				if d.IsDir() {
					if path != p && (!recursive || strings.HasPrefix(d.Name(), ".")) {
						return filepath.SkipDir
					}
					return nil
				}
				if strings.EqualFold(filepath.Ext(path), ".tcad") {
					found = append(found, path)
				}
				return nil
			})
			// Files of a directory before those of its subdirectories.
			slices.SortStableFunc(found, func(a, b string) int {
				return strings.Compare(filepath.Dir(a), filepath.Dir(b))
			})
		}
		for _, f := range found {
			if !seen[f] {
				seen[f] = true
				models = append(models, f)
			}
		}
	}
	if len(errs) > 0 {
		err = fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return models, err
}

// rescan updates the list of models when files appear or go away. The model
// shown stays in the list even while it is missing: editors may delete and
// recreate a file when saving it. With no model shown, the first one that
// appears is; with Follow, so is any model created or saved.
func (s *Server) rescan() {
	files, _ := Discover(s.opts.Paths, s.opts.Recursive)
	models := s.names(files)
	now := make(map[string]stamp, len(models))
	for i, m := range models {
		now[m] = stampsOf(files[i : i+1])[0]
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	edited := ""
	var newest time.Time
	for _, m := range models {
		old, known := s.stamps[m]
		if m != s.model && (!known || changed([]stamp{old}, []stamp{now[m]})) && !now[m].mod.Before(newest) {
			edited, newest = m, now[m].mod
		}
	}
	s.stamps = now
	if s.model != "" && !slices.Contains(models, s.model) {
		models = append(models, s.model)
	}
	listChanged := !slices.Equal(models, s.st.Models)
	s.st.Models = models
	switch {
	case s.model == "" && len(models) > 0:
		edited = models[0]
	case !s.opts.Follow:
		edited = ""
	}
	if edited != "" {
		s.selectModel(edited)
		s.requestBuild()
	}
	if listChanged || edited != "" {
		s.broadcast()
	}
}

// watchLoop polls the model and its dependencies, and rebuilds when they
// differ from what the last build read: an edit made while a build runs, or
// while another model is being picked, is never lost. Polling is simple,
// needs no dependencies and copes with editors that replace files on save.
func (s *Server) watchLoop() {
	var requested []stamp // what the pending build will read
	for tick := 1; ; tick++ {
		select {
		case <-s.done:
			return
		case <-time.After(250 * time.Millisecond):
		}
		if tick%4 == 0 {
			s.rescan()
		}
		s.mu.Lock()
		files, built := slices.Clone(s.files), slices.Clone(s.built)
		s.mu.Unlock()
		if len(built) != len(files) {
			continue // a model was just picked and its first build is queued
		}
		now := stampsOf(files)
		if !changed(built, now) || (requested != nil && !changed(requested, now)) {
			continue
		}
		// Let the editor finish writing before building.
		for {
			time.Sleep(100 * time.Millisecond)
			again := stampsOf(files)
			if !changed(now, again) {
				break
			}
			now = again
		}
		requested = now
		s.requestBuild()
	}
}

var indexTmpl = template.Must(template.ParseFS(content, "index.html"))

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	s.mu.Lock()
	file := s.st.File
	s.mu.Unlock()
	if err := indexTmpl.Execute(w, map[string]string{"File": file}); err != nil {
		s.logger.Printf("template: %v", err)
	}
}

func (s *Server) handleModel(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	data := s.colored
	s.mu.Unlock()
	if data == nil {
		http.Error(w, "no model built yet", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "model/stl")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(data)
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	data, name := s.plain, s.st.Output
	if name == "" {
		name = strings.TrimSuffix(s.st.File, filepath.Ext(s.st.File)) + ".stl"
	}
	s.mu.Unlock()
	if data == nil {
		http.Error(w, "no model built yet", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "model/stl")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	w.Write(data)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	c := make(chan []byte, 4)
	s.mu.Lock()
	s.clients[c] = struct{}{}
	initial := s.stateJSON()
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.clients, c)
		s.mu.Unlock()
	}()

	send := func(data []byte) bool {
		if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	if !send(initial) {
		return
	}
	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-s.done:
			return
		case data := <-c:
			if !send(data) {
				return
			}
		case <-ping.C:
			// Comments keep proxies and browsers from closing the stream.
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	data := s.stateJSON()
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

// handleParams changes param overrides:
//
//	PUT    /api/params  {"ancho": "80", "nombre": "\"Ana\"", "alto": null}
//	DELETE /api/params  (back to the values in the file)
//
// Values are TCAD expressions, like -D; null removes an override. The model
// is rebuilt right away and the result arrives through /events.
func (s *Server) handleParams(w http.ResponseWriter, r *http.Request) {
	reply := func(code int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		json.NewEncoder(w).Encode(v)
	}
	switch r.Method {
	case http.MethodDelete:
		s.mu.Lock()
		clear(s.overrides)
		s.broadcast()
		s.mu.Unlock()
	case http.MethodPut, http.MethodPost:
		var req map[string]*string
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
			reply(http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
			return
		}
		s.mu.Lock()
		// Validate everything before applying anything.
		for name, expr := range req {
			if expr == nil {
				continue
			}
			i := slices.IndexFunc(s.st.Params, func(p dsl.ParamValue) bool { return p.Name == name })
			if i < 0 && s.st.HasModel {
				s.mu.Unlock()
				reply(http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("unknown param %q", name)})
				return
			}
			if i >= 0 {
				p := s.st.Params[i]
				v := dsl.EvalDefine(*expr)
				if t := dsl.TypeName(v); t != p.Type {
					s.mu.Unlock()
					reply(http.StatusBadRequest, map[string]string{
						"error": fmt.Sprintf("param %s expects a %s, %s is a %s", name, p.Type, *expr, t)})
					return
				}
				if len(p.Options) > 0 && !slices.ContainsFunc(p.Options, func(o any) bool {
					return dsl.FormatLiteral(o) == dsl.FormatLiteral(v)
				}) {
					s.mu.Unlock()
					reply(http.StatusBadRequest, map[string]string{
						"error": fmt.Sprintf("param %s must be one of %s", name, dsl.FormatLiteral(p.Options))})
					return
				}
			}
		}
		for name, expr := range req {
			i := slices.IndexFunc(s.st.Params, func(p dsl.ParamValue) bool { return p.Name == name })
			// Setting a param back to the value in the file removes the
			// override, so the page no longer marks it as changed.
			if expr == nil || (i >= 0 && dsl.FormatLiteral(dsl.EvalDefine(*expr)) == dsl.FormatLiteral(s.st.Params[i].Default)) {
				delete(s.overrides, name)
			} else {
				s.overrides[name] = *expr
			}
		}
		s.broadcast()
		s.mu.Unlock()
	case http.MethodGet:
		s.mu.Lock()
		o := maps.Clone(s.overrides)
		s.mu.Unlock()
		reply(http.StatusOK, o)
		return
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		reply(http.StatusMethodNotAllowed, map[string]string{"error": "use GET, PUT or DELETE"})
		return
	}
	s.requestBuild()
	s.mu.Lock()
	o := maps.Clone(s.overrides)
	s.mu.Unlock()
	reply(http.StatusAccepted, map[string]any{"overrides": o})
}

// handleModelSelect picks the model shown:
//
//	GET /api/model                                     {"model": ..., "models": [...]}
//	PUT /api/model  {"model": "diseños/pieza.tcad"}
//
// The model must be one of the listed ones. Each model keeps its own param
// overrides while switching between them.
func (s *Server) handleModelSelect(w http.ResponseWriter, r *http.Request) {
	reply := func(code int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		json.NewEncoder(w).Encode(v)
	}
	switch r.Method {
	case http.MethodGet:
	case http.MethodPut, http.MethodPost:
		var req struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
			reply(http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
			return
		}
		s.mu.Lock()
		if !slices.Contains(s.st.Models, req.Model) {
			s.mu.Unlock()
			reply(http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("unknown model %q", req.Model)})
			return
		}
		if req.Model != s.model {
			s.selectModel(req.Model)
			s.broadcast()
			s.requestBuild()
		}
		s.mu.Unlock()
	default:
		w.Header().Set("Allow", "GET, PUT")
		reply(http.StatusMethodNotAllowed, map[string]string{"error": "use GET or PUT"})
		return
	}
	s.mu.Lock()
	model, models := s.model, slices.Clone(s.st.Models)
	s.mu.Unlock()
	reply(http.StatusOK, map[string]any{"model": model, "models": models})
}
