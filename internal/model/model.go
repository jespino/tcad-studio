// Package model loads TCAD files: it parses and evaluates them and prepares
// the mesh for writing. The command line and TCAD Studio share it.
package model

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/jespino/tcad-studio/internal/dsl"
	"github.com/jespino/tcad-studio/internal/geom"
	"github.com/jespino/tcad-studio/internal/stl"
	"github.com/jespino/tcad-studio/internal/watch"
)

// Model is an evaluated TCAD file.
type Model struct {
	Path      string
	Result    *dsl.Result
	Mesh      *geom.Mesh // welded and repaired, ready to write
	OpenEdges int
	Took      time.Duration
}

// Load evaluates the model in path ("-" reads standard input) with the given
// param overrides. ignoreUnknown tolerates overrides of params that are not
// in the file (the viewer keeps them while the file changes).
func Load(path string, defs map[string]string, ignoreUnknown bool) (*Model, error) {
	var src []byte
	var err error
	name := path
	if path == "-" {
		src, err = io.ReadAll(os.Stdin)
		name = "<stdin>"
	} else {
		src, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, err
	}
	start := time.Now()
	prog, err := dsl.Parse(name, string(src))
	if err != nil {
		return nil, err
	}
	base := "."
	if path != "-" {
		base = filepath.Dir(path)
	}
	res, err := dsl.Evaluate(prog, dsl.Options{Defines: defs, Echo: os.Stderr, BaseDir: base, IgnoreUnknownDefines: ignoreUnknown})
	if err != nil {
		return nil, err
	}
	if len(res.Mesh.Tris) == 0 {
		return nil, errors.New("the model is empty (no solids, or holes removed everything)")
	}
	mesh, open := stl.Prepare(res.Mesh)
	return &Model{Path: path, Result: res, Mesh: mesh, OpenEdges: open, Took: time.Since(start)}, nil
}

// WatchBuild is the build function of the live viewer (watch.Options.Build).
func WatchBuild(file string, defs map[string]string) (*watch.Build, error) {
	m, err := Load(file, defs, true)
	if err != nil {
		return nil, err
	}
	return &watch.Build{Mesh: m.Mesh, Files: m.Result.Files, OpenEdges: m.OpenEdges,
		Took: m.Took, Params: m.Result.Params}, nil
}
