// Package cli is the tcad command line: it compiles TCAD model files (a CSG
// language of solids and holes) into STL meshes and PNG previews, imports STL
// files and serves the live viewer. TCAD Studio runs it too, so the agent in
// its terminal has the same tcad commands.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"image/png"
	"io"
	"math"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/jespino/tcad-studio/internal/dsl"
	"github.com/jespino/tcad-studio/internal/geom"
	"github.com/jespino/tcad-studio/internal/importer"
	"github.com/jespino/tcad-studio/internal/model"
	"github.com/jespino/tcad-studio/internal/render"
	"github.com/jespino/tcad-studio/internal/shapes"
	"github.com/jespino/tcad-studio/internal/stl"
	"github.com/jespino/tcad-studio/internal/studio"
	"github.com/jespino/tcad-studio/internal/watch"
)

const usage = `tcad - build 3D models from TCAD files (a CSG language of solids and holes)

Usage:
  tcad build  [flags] model.tcad   generate an STL file
  tcad render [flags] model.tcad   generate a PNG preview
  tcad info   [flags] model.tcad   print dimensions, volume and mesh checks
  tcad check  [flags] model.tcad   only parse and evaluate (reports errors)
  tcad watch  [flags] path...      live 3D viewer in the browser, rebuilt on every save
                                   (paths: .tcad files and directories)
  tcad studio [flags] [folder]     TCAD Studio in the browser: live preview + agent terminal
  tcad import [flags] part.stl     convert an STL file into a TCAD model
  tcad shapes                      list every primitive and its parameters

Common flags:
  -D name=value   override a "param" of the model (repeatable)

build flags:
  -o file.stl     output file (default: model name with .stl)
  -ascii          write ASCII STL instead of binary

render flags:
  -o file.png     output file (default: model name with .png)
  -view name      all, iso, iso-back, iso-below, front, back, left, right, top, bottom (default all)
  -size pixels    size of each view (default 500)

watch flags:
  -addr host:port listen address (default localhost:8080)
  -R              include the .tcad files of subdirectories too
  -o file.stl     STL rewritten after every successful build (default: model name with .stl;
                  only with a single model)
  -no-stl         do not write STL files, only serve the viewer
  -open           open the viewer in the default browser

studio flags:
  -addr host:port listen address (default 127.0.0.1:0, a free port)
  -open           open the studio in the default browser

import flags:
  -o file.tcad    output file (default: STL name with .tcad)
  -mode name      shapes: rebuild bodies from primitives where possible (default)
                  embed:  exact copy of the mesh as a polyhedron
                  ref:    a stl("...") reference to the original file
  -center         move the model to the workplane (centered in XY, z = 0)

Use "-" as the model file to read from standard input.
`

type defines map[string]string

func (d defines) String() string { return "" }
func (d defines) Set(s string) error {
	k, v, ok := strings.Cut(s, "=")
	if !ok || strings.TrimSpace(k) == "" {
		return errors.New("expected name=value")
	}
	d[strings.TrimSpace(k)] = strings.TrimSpace(v)
	return nil
}

// Commands lists the subcommands, for programs that embed the command line.
var Commands = []string{"build", "render", "info", "check", "watch", "studio", "import", "shapes", "help", "-h", "--help"}

// Main runs the command line with args (without the program name) and
// returns the exit code.
func Main(args []string) int {
	if len(args) < 1 {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	var err error
	switch cmd := args[0]; cmd {
	case "build":
		err = cmdBuild(args[1:])
	case "render":
		err = cmdRender(args[1:])
	case "info":
		err = cmdInfo(args[1:])
	case "check":
		err = cmdCheck(args[1:])
	case "watch":
		err = cmdWatch(args[1:])
	case "studio":
		err = cmdStudio(args[1:])
	case "import":
		err = cmdImport(args[1:])
	case "shapes":
		cmdShapes(os.Stdout)
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		err = fmt.Errorf("unknown command %q\n\n%s", cmd, usage)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}

// parseArgs parses flags that may appear before or after the model path.
func parseArgs(fs *flag.FlagSet, args []string) (string, error) {
	pos, err := parsePositional(fs, args)
	if err != nil {
		return "", err
	}
	if len(pos) != 1 {
		return "", fmt.Errorf("expected exactly one model file, got %d", len(pos))
	}
	return pos[0], nil
}

// parsePositional parses flags mixed with any number of positional args.
func parsePositional(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return pos, nil
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
}

func load(path string, defs defines) (*model.Model, error) { return model.Load(path, defs, false) }

func defaultOutput(in, ext string) string {
	if in == "-" {
		return "model" + ext
	}
	return strings.TrimSuffix(in, filepath.Ext(in)) + ext
}

func cmdBuild(args []string) error {
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	defs := defines{}
	fs.Var(defs, "D", "override a param (name=value)")
	out := fs.String("o", "", "output STL file")
	ascii := fs.Bool("ascii", false, "write ASCII STL")
	path, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	m, err := load(path, defs)
	if err != nil {
		return err
	}
	if *out == "" {
		*out = defaultOutput(path, ".stl")
	}
	f, err := os.Create(*out)
	if err != nil {
		return err
	}
	if *ascii {
		err = stl.WriteASCII(f, m.Mesh, strings.TrimSuffix(filepath.Base(*out), filepath.Ext(*out)))
	} else {
		err = stl.WriteBinary(f, m.Mesh)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	fmt.Printf("wrote %s (%d triangles, %s)\n", *out, len(m.Mesh.Tris), m.Took.Round(time.Millisecond))
	warnOpen(m)
	return nil
}

func warnOpen(m *model.Model) {
	if m.OpenEdges > 0 {
		fmt.Fprintf(os.Stderr, "warning: mesh has %d open/non-manifold edges; most slicers repair this automatically\n", m.OpenEdges)
	}
}

func cmdRender(args []string) error {
	fs := flag.NewFlagSet("render", flag.ContinueOnError)
	defs := defines{}
	fs.Var(defs, "D", "override a param (name=value)")
	out := fs.String("o", "", "output PNG file")
	view := fs.String("view", "all", "view: "+strings.Join(render.ViewNames(), ", "))
	size := fs.Int("size", 500, "size in pixels of each view")
	path, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if *size < 64 || *size > 4000 {
		return errors.New("-size must be between 64 and 4000")
	}
	m, err := load(path, defs)
	if err != nil {
		return err
	}
	img, err := render.Render(m.Mesh, *view, *size)
	if err != nil {
		return err
	}
	if *out == "" {
		*out = defaultOutput(path, ".png")
	}
	f, err := os.Create(*out)
	if err != nil {
		return err
	}
	err = png.Encode(f, img)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	fmt.Printf("wrote %s\n", *out)
	return nil
}

func cmdInfo(args []string) error {
	fs := flag.NewFlagSet("info", flag.ContinueOnError)
	defs := defines{}
	fs.Var(defs, "D", "override a param (name=value)")
	path, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	m, err := load(path, defs)
	if err != nil {
		return err
	}
	mn, mx, _ := m.Mesh.BBox()
	mn, mx = tidy(mn), tidy(mx)
	d := mx.Sub(mn)
	fmt.Printf("file:        %s\n", path)
	fmt.Printf("size:        X %.3f  Y %.3f  Z %.3f mm\n", d.X, d.Y, d.Z)
	fmt.Printf("bbox min:    (%.3f, %.3f, %.3f)\n", mn.X, mn.Y, mn.Z)
	fmt.Printf("bbox max:    (%.3f, %.3f, %.3f)\n", mx.X, mx.Y, mx.Z)
	fmt.Printf("volume:      %.3f mm³ (%.3f cm³)\n", m.Mesh.Volume(), m.Mesh.Volume()/1000)
	fmt.Printf("triangles:   %d\n", len(m.Mesh.Tris))
	if m.OpenEdges == 0 {
		fmt.Printf("watertight:  yes\n")
	} else {
		fmt.Printf("watertight:  no (%d open/non-manifold edges)\n", m.OpenEdges)
	}
	fmt.Printf("time:        %s\n", m.Took.Round(time.Millisecond))
	if len(m.Result.Params) > 0 {
		fmt.Printf("params:\n")
		for _, p := range m.Result.Params {
			mark := ""
			if p.Overridden {
				mark = "  (overridden)"
			}
			opts := ""
			if len(p.Options) > 0 {
				opts = "  in " + dsl.FormatLiteral(p.Options)
			}
			fmt.Printf("  %s = %s%s%s\n", p.Name, p.Expr, opts, mark)
		}
	}
	return nil
}

// tidy rounds away floating point noise so the output never shows "-0.000".
func tidy(v geom.Vec3) geom.Vec3 {
	r := func(x float64) float64 {
		if math.Abs(x) < 5e-7 {
			return 0
		}
		return x
	}
	return geom.V(r(v.X), r(v.Y), r(v.Z))
}

func cmdCheck(args []string) error {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	defs := defines{}
	fs.Var(defs, "D", "override a param (name=value)")
	path, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	m, err := load(path, defs)
	if err != nil {
		return err
	}
	fmt.Printf("ok: %d triangles\n", len(m.Mesh.Tris))
	warnOpen(m)
	return nil
}

func cmdShapes(w io.Writer) {
	for _, p := range shapes.All() {
		names := make([]string, len(p.Params))
		for i, pr := range p.Params {
			names[i] = pr.Name
		}
		fmt.Fprintf(w, "%s(%s)\n    %s\n", p.Name, strings.Join(names, ", "), p.Doc)
		for _, pr := range p.Params {
			def := "required"
			switch v := pr.Default.(type) {
			case float64:
				def = fmt.Sprintf("default %g", v)
			case string:
				def = fmt.Sprintf("default %q", v)
			case bool:
				def = fmt.Sprintf("default %v", v)
			}
			extra := ""
			if len(pr.Aliases) > 0 {
				extra += ", alias " + strings.Join(pr.Aliases, "/")
			}
			if pr.Quality != "" {
				extra += ", or $" + pr.Quality
			}
			fmt.Fprintf(w, "      %-13s %s (%s%s)\n", pr.Name, pr.Doc, def, extra)
		}
		fmt.Fprintln(w)
	}
	fmt.Fprint(w, `modifiers (applied left to right, around the origin):
    .move(x, y, z)      translate
    .rotate(x, y, z)    rotate in degrees: X first, then Y, then Z
    .scale(s) / .scale(x, y, z)
    .mirror("x"|"y"|"z"|"xy"...)
    .drop()             move down/up so the lowest point is at z = 0
    .center("xy")       center the bounding box on the given axes (default "xy")
    .color("red" | "#rrggbb" | r, g, b)   preview only
`)
}

func cmdImport(args []string) error {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	out := fs.String("o", "", "output TCAD file")
	mode := fs.String("mode", "shapes", "shapes, embed or ref")
	center := fs.Bool("center", false, "move the model to the workplane")
	path, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if path == "-" {
		return errors.New("import needs an STL file path")
	}
	mesh, err := stl.ReadFile(path)
	if err != nil {
		return err
	}
	if *out == "" {
		*out = strings.TrimSuffix(path, filepath.Ext(path)) + ".tcad"
	}
	ref, err := filepath.Rel(filepath.Dir(*out), path)
	if err != nil {
		ref, _ = filepath.Abs(path)
	}
	src, rep, err := importer.Convert(mesh, importer.Options{
		Mode: importer.Mode(*mode), Source: path, RefPath: ref, Center: *center,
	})
	if err != nil {
		return err
	}
	if err := os.WriteFile(*out, []byte(src), 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s (%d bytes)\n", *out, len(src))
	fmt.Printf("  input:   %d triangles, %d bodies", rep.InputTriangles, rep.Bodies)
	if rep.Cavities > 0 {
		fmt.Printf(" (%d internal cavities)", rep.Cavities)
	}
	fmt.Println()
	if rep.OpenEdges > 0 {
		fmt.Printf("  warning: the STL is not watertight (%d open edges)\n", rep.OpenEdges)
	}
	kinds := make([]string, 0, len(rep.Kinds))
	for k, n := range rep.Kinds {
		kinds = append(kinds, fmt.Sprintf("%d %s", n, k))
	}
	sort.Strings(kinds)
	fmt.Printf("  output:  %s\n", strings.Join(kinds, ", "))
	if rep.Unrecognized > 0 && *mode == "shapes" {
		fmt.Printf("  note:    %d bodies could not be rebuilt from primitives and were kept as exact meshes\n", rep.Unrecognized)
	}

	// Check the result by evaluating it and comparing with the original.
	m, err := load(*out, defines{})
	if err != nil {
		return fmt.Errorf("the generated model does not evaluate: %w", err)
	}
	orig, _ := stl.Prepare(mesh)
	v0, v1 := math.Abs(orig.Volume()), m.Mesh.Volume()
	diff := 0.0
	if v0 > 0 {
		diff = (v1 - v0) / v0 * 100
	}
	fmt.Printf("  check:   volume %.3f mm³ (original %.3f, %+.3f%%)", v1, v0, diff)
	if m.OpenEdges == 0 {
		fmt.Printf(", watertight\n")
	} else {
		fmt.Printf(", %d open edges\n", m.OpenEdges)
	}
	return nil
}

func cmdWatch(args []string) error {
	fs := flag.NewFlagSet("watch", flag.ContinueOnError)
	defs := defines{}
	fs.Var(defs, "D", "override a param (name=value)")
	addr := fs.String("addr", "localhost:8080", "listen address")
	out := fs.String("o", "", "STL file rewritten after every build (single model only)")
	noSTL := fs.Bool("no-stl", false, "do not write STL files")
	open := fs.Bool("open", false, "open the viewer in the browser")
	recursive := fs.Bool("R", false, "also watch the .tcad files in subdirectories")
	paths, err := parsePositional(fs, args)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return errors.New("watch needs at least one model file or directory")
	}
	for _, p := range paths {
		if p == "-" {
			return errors.New("watch needs model file or directory paths, not stdin")
		}
	}
	models, err := watch.Discover(paths, *recursive)
	if err != nil {
		return err
	}
	if len(models) == 0 {
		return fmt.Errorf("no .tcad files in %s (use -R to include subdirectories)", strings.Join(paths, ", "))
	}
	if *out != "" && len(models) > 1 {
		return errors.New("-o needs a single model; with several, each STL is written next to its model")
	}
	output := func(model string) string {
		switch {
		case *noSTL:
			return ""
		case *out != "":
			return *out
		}
		return defaultOutput(model, ".stl")
	}
	what := models[0]
	if len(models) > 1 {
		what = fmt.Sprintf("%d models in %s", len(models), strings.Join(paths, ", "))
	}
	return watch.Run(watch.Options{
		Paths:     paths,
		Recursive: *recursive,
		Output:    output,
		Addr:      *addr,
		Defines:   defs,
		Build:     model.WatchBuild,
		Ready: func(url string) {
			fmt.Printf("watching %s, viewer at %s (Ctrl+C to stop)\n", what, url)
			if *open {
				openBrowser(url)
			}
		},
	})
}

func cmdStudio(args []string) error {
	fs := flag.NewFlagSet("studio", flag.ContinueOnError)
	addr := fs.String("addr", "127.0.0.1:0", "listen address")
	open := fs.Bool("open", false, "open the studio in the browser")
	pos, err := parsePositional(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 1 {
		return errors.New("studio opens a single folder")
	}
	st := studio.New(studio.Options{})
	defer st.Close()
	if len(pos) == 1 {
		if _, err := st.Open(pos[0]); err != nil {
			return err
		}
	}
	url, err := st.Start(*addr)
	if err != nil {
		return err
	}
	fmt.Printf("TCAD Studio at %s (Ctrl+C to stop)\n", url)
	if *open {
		openBrowser(url)
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	return nil
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "could not open the browser: %v\n", err)
	}
}
