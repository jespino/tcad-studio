package shapes

import (
	"fmt"
	"math"
	"os"
	"sync"
	"time"

	"github.com/jespino/tcad-studio/internal/geom"
	"github.com/jespino/tcad-studio/internal/stl"
)

type cachedSTL struct {
	mesh    *geom.Mesh
	modTime time.Time
	size    int64
}

var (
	stlCacheMu sync.Mutex
	stlCache   = map[string]cachedSTL{}
)

// loadSTL reads an STL once while it does not change on disk; loops that
// place the same file many times, and "tcad watch" rebuilds, reuse it.
func loadSTL(path string) (*geom.Mesh, error) {
	stlCacheMu.Lock()
	defer stlCacheMu.Unlock()
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if c, ok := stlCache[path]; ok && c.modTime.Equal(fi.ModTime()) && c.size == fi.Size() {
		return c.mesh.Clone(), nil
	}
	m, err := stl.ReadFile(path)
	if err != nil {
		return nil, err
	}
	// Welding and closing T-junctions makes imported meshes safer for CSG.
	m, _ = stl.Prepare(m)
	m.FixOrientation()
	stlCache[path] = cachedSTL{mesh: m, modTime: fi.ModTime(), size: fi.Size()}
	return m.Clone(), nil
}

func init() {
	register(&Primitive{
		Name: "stl",
		Doc:  "Imports an STL file. Relative paths are relative to the .tcad file.",
		Params: []Param{
			{Name: "file", Default: "", IsPath: true, Doc: "path of the STL file"},
			{Name: "center", Default: true, Doc: "true: center in XY and rest on z = 0; false: keep the original coordinates"},
		},
		KeepCoords: true,
		build: func(a Args) (*geom.Mesh, error) {
			if a.Str("file") == "" {
				return nil, fmt.Errorf("file is required")
			}
			m, err := loadSTL(a.Str("file"))
			if err != nil {
				return nil, err
			}
			if a.Bool("center") {
				restOnWorkplane(m)
			}
			return m, nil
		},
	})

	register(&Primitive{
		Name: "polyhedron",
		Doc:  "Arbitrary closed mesh: a list of [x, y, z] points and faces as lists of point indices (counter-clockwise seen from outside). Coordinates are kept as given.",
		Params: []Param{
			{Name: "points", Doc: "list of [x, y, z] points"},
			{Name: "faces", Doc: "list of faces, each a list of point indices"},
		},
		KeepCoords: true,
		build: func(a Args) (*geom.Mesh, error) {
			pts, err := listOfNumbers(a["points"], "points", 3)
			if err != nil {
				return nil, err
			}
			faces, err := listOfNumbers(a["faces"], "faces", -1)
			if err != nil {
				return nil, err
			}
			vs := make([]Vec3, len(pts))
			for i, p := range pts {
				vs[i] = Vec3{X: p[0], Y: p[1], Z: p[2]}
			}
			m := &geom.Mesh{}
			for fi, f := range faces {
				if len(f) < 3 {
					return nil, fmt.Errorf("faces[%d] needs at least 3 indices", fi)
				}
				idx := make([]int, len(f))
				for k, x := range f {
					i := int(x)
					if float64(i) != x || i < 0 || i >= len(vs) {
						return nil, fmt.Errorf("faces[%d] has an invalid point index %v", fi, x)
					}
					idx[k] = i
				}
				for k := 1; k+1 < len(idx); k++ {
					m.AddTri(vs[idx[0]], vs[idx[k]], vs[idx[k+1]])
				}
			}
			if len(m.Tris) == 0 {
				return nil, fmt.Errorf("polyhedron has no faces")
			}
			if math.Abs(m.Volume()) < 1e-12 {
				return nil, fmt.Errorf("polyhedron has no volume (is it closed?)")
			}
			m.FixOrientation()
			return m, nil
		},
	})
}

// listOfNumbers converts a list of numeric lists; size < 0 allows any length.
func listOfNumbers(v any, name string, size int) ([][]float64, error) {
	list, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("%s must be a list", name)
	}
	out := make([][]float64, len(list))
	for i, it := range list {
		l, ok := it.([]any)
		if !ok || (size >= 0 && len(l) != size) {
			if size >= 0 {
				return nil, fmt.Errorf("%s[%d] must be a list of %d numbers", name, i, size)
			}
			return nil, fmt.Errorf("%s[%d] must be a list of numbers", name, i)
		}
		out[i] = make([]float64, len(l))
		for k, x := range l {
			f, ok := x.(float64)
			if !ok {
				return nil, fmt.Errorf("%s[%d][%d] must be a number", name, i, k)
			}
			out[i][k] = f
		}
	}
	return out, nil
}
