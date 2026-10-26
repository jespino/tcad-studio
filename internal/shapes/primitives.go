package shapes

import (
	"fmt"
	"math"
	"sort"

	"github.com/jespino/tcad-studio/internal/geom"
)

// Param describes a primitive parameter.
type Param struct {
	Name    string
	Default any // float64, string or nil when the parameter is required
	Aliases []string
	// Quality names the global default ("sides" or "steps") that overrides
	// Default when the user does not pass the parameter.
	Quality string
	// IsPath marks file names, resolved relative to the model file.
	IsPath bool
	Doc    string
}

// Primitive is a shape that can be instantiated from the DSL.
type Primitive struct {
	Name   string
	Doc    string
	Params []Param
	// KeepCoords disables re-centering, for shapes defined by user points.
	KeepCoords bool
	build      func(a Args) (*geom.Mesh, error)
}

// Args holds resolved arguments: float64, string, bool or []any values.
type Args map[string]any

func (a Args) Num(n string) float64 { v, _ := a[n].(float64); return v }
func (a Args) Int(n string) int     { return int(math.Round(a.Num(n))) }
func (a Args) Str(n string) string  { v, _ := a[n].(string); return v }
func (a Args) Bool(n string) bool   { v, _ := a[n].(bool); return v }

// Points interprets a parameter as a list of [x, y] pairs.
func (a Args) Points(n string) ([]Vec2, error) {
	list, ok := a[n].([]any)
	if !ok {
		return nil, fmt.Errorf("%s must be a list of [x, y] points", n)
	}
	out := make([]Vec2, 0, len(list))
	for i, it := range list {
		p, ok := it.([]any)
		if !ok || len(p) != 2 {
			return nil, fmt.Errorf("%s[%d] must be a [x, y] pair", n, i)
		}
		x, ok1 := p[0].(float64)
		y, ok2 := p[1].(float64)
		if !ok1 || !ok2 {
			return nil, fmt.Errorf("%s[%d] must contain numbers", n, i)
		}
		out = append(out, Vec2{x, y})
	}
	if len(out) < 3 {
		return nil, fmt.Errorf("%s needs at least 3 points", n)
	}
	return out, nil
}

// Build validates the arguments and creates the mesh.
func (p *Primitive) Build(a Args) (*geom.Mesh, error) {
	m, err := p.build(a)
	if err != nil {
		return nil, err
	}
	if !p.KeepCoords {
		restOnWorkplane(m)
	}
	return m, nil
}

var registry = map[string]*Primitive{}

func register(p *Primitive) { registry[p.Name] = p }

// Lookup returns the primitive with the given name.
func Lookup(name string) (*Primitive, bool) {
	p, ok := registry[name]
	return p, ok
}

// All returns every primitive sorted by name.
func All() []*Primitive {
	out := make([]*Primitive, 0, len(registry))
	for _, p := range registry {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func positive(a Args, names ...string) error {
	for _, n := range names {
		if a.Num(n) <= 0 {
			return fmt.Errorf("%s must be greater than 0", n)
		}
	}
	return nil
}

func atLeast(a Args, n string, min int) error {
	if a.Int(n) < min {
		return fmt.Errorf("%s must be at least %d", n, min)
	}
	return nil
}

var (
	pLength = Param{Name: "length", Default: 20.0, Aliases: []string{"l"}, Doc: "size along X"}
	pWidth  = Param{Name: "width", Default: 20.0, Aliases: []string{"w"}, Doc: "size along Y"}
	pHeight = Param{Name: "height", Default: 20.0, Aliases: []string{"h"}, Doc: "size along Z"}
	pRadius = Param{Name: "radius", Default: 10.0, Aliases: []string{"r"}, Doc: "radius"}
	pSides  = Param{Name: "sides", Default: 32.0, Quality: "sides", Doc: "number of segments around the circumference"}
	pSteps  = Param{Name: "steps", Default: 16.0, Quality: "steps", Doc: "number of segments of curved profiles"}
)

func withDefault(p Param, d any) Param { p.Default = d; return p }

func init() {
	register(&Primitive{
		Name: "box",
		Doc:  "Rectangular box. A radius > 0 rounds every edge like Tinkercad's Box.",
		Params: []Param{pLength, pWidth, pHeight,
			{Name: "radius", Default: 0.0, Aliases: []string{"r"}, Doc: "edge rounding radius"},
			withDefault(pSteps, 8.0)},
		build: func(a Args) (*geom.Mesh, error) {
			if err := positive(a, "length", "width", "height"); err != nil {
				return nil, err
			}
			return roundedBox(a.Num("length"), a.Num("width"), a.Num("height"), a.Num("radius"), a.Int("steps")), nil
		},
	})

	register(&Primitive{
		Name: "cylinder",
		Doc:  "Cylinder. Few sides give prisms (6 = hexagonal). Bevel rounds/chamfers top and bottom edges.",
		Params: []Param{pRadius, pHeight, pSides,
			{Name: "bevel", Default: 0.0, Doc: "bevel size of the top and bottom edges"},
			{Name: "segments", Default: 1.0, Doc: "bevel segments (1 = chamfer, more = rounded)"}},
		build: func(a Args) (*geom.Mesh, error) {
			if err := positive(a, "radius", "height"); err != nil {
				return nil, err
			}
			if err := atLeast(a, "sides", 3); err != nil {
				return nil, err
			}
			r, h := a.Num("radius"), a.Num("height")
			b := math.Max(0, math.Min(a.Num("bevel"), math.Min(r, h/2)))
			prof := roundedRect(0, 0, r, h, [4]float64{0, b, b, 0}, a.Int("segments"))
			return revolve(prof, a.Int("sides")), nil
		},
	})

	register(&Primitive{
		Name: "cone",
		Doc:  "Cone or truncated cone.",
		Params: []Param{
			{Name: "base_radius", Default: 10.0, Aliases: []string{"radius", "r", "r1"}, Doc: "bottom radius"},
			{Name: "top_radius", Default: 0.0, Aliases: []string{"r2"}, Doc: "top radius (0 = pointy)"},
			pHeight, pSides},
		build: func(a Args) (*geom.Mesh, error) {
			if err := positive(a, "height"); err != nil {
				return nil, err
			}
			if err := atLeast(a, "sides", 3); err != nil {
				return nil, err
			}
			r1, r2, h := a.Num("base_radius"), a.Num("top_radius"), a.Num("height")
			if r1 < 0 || r2 < 0 || r1+r2 == 0 {
				return nil, fmt.Errorf("cone radii must be >= 0 and not both 0")
			}
			prof := []Vec2{{0, 0}, {r1, 0}, {r2, h}, {0, h}}
			return revolve(prof, a.Int("sides")), nil
		},
	})

	register(&Primitive{
		Name:   "sphere",
		Doc:    "Sphere resting on the workplane (center at z = radius).",
		Params: []Param{pRadius, pSteps},
		build: func(a Args) (*geom.Mesh, error) {
			if err := positive(a, "radius"); err != nil {
				return nil, err
			}
			steps := max(a.Int("steps"), 2)
			r := a.Num("radius")
			prof := arc(nil, Vec2{0, r}, r, -90, 90, steps)
			return revolve(prof, steps*2), nil
		},
	})

	register(&Primitive{
		Name:   "half_sphere",
		Doc:    "Dome: upper half of a sphere, flat face on the workplane.",
		Params: []Param{pRadius, pSteps},
		build: func(a Args) (*geom.Mesh, error) {
			if err := positive(a, "radius"); err != nil {
				return nil, err
			}
			steps := max(a.Int("steps"), 1)
			r := a.Num("radius")
			prof := append([]Vec2{{0, 0}}, arc(nil, Vec2{0, 0}, r, 0, 90, steps)...)
			return revolve(prof, steps*4), nil
		},
	})

	register(&Primitive{
		Name: "torus",
		Doc:  "Torus (donut). radius goes from the center to the middle of the tube.",
		Params: []Param{pRadius,
			{Name: "tube", Default: 2.0, Doc: "radius of the tube cross-section"},
			pSides, pSteps},
		build: func(a Args) (*geom.Mesh, error) {
			if err := positive(a, "radius", "tube"); err != nil {
				return nil, err
			}
			R, t := a.Num("radius"), a.Num("tube")
			if t >= R {
				return nil, fmt.Errorf("torus tube (%g) must be smaller than radius (%g)", t, R)
			}
			steps := max(a.Int("steps"), 3)
			prof := arc(nil, Vec2{R, t}, t, 0, 360, steps)
			return revolve(prof[:len(prof)-1], max(a.Int("sides"), 3)), nil
		},
	})

	register(&Primitive{
		Name: "tube",
		Doc:  "Hollow cylinder (pipe).",
		Params: []Param{
			{Name: "radius", Default: 10.0, Aliases: []string{"r"}, Doc: "outer radius"},
			{Name: "wall", Default: 2.0, Aliases: []string{"thickness"}, Doc: "wall thickness"},
			pHeight, pSides,
			{Name: "bevel", Default: 0.0, Doc: "bevel size of the edges"},
			{Name: "segments", Default: 1.0, Doc: "bevel segments"}},
		build: func(a Args) (*geom.Mesh, error) {
			if err := positive(a, "radius", "wall", "height"); err != nil {
				return nil, err
			}
			r, w, h := a.Num("radius"), a.Num("wall"), a.Num("height")
			if w >= r {
				return nil, fmt.Errorf("tube wall (%g) must be smaller than radius (%g)", w, r)
			}
			b := math.Max(0, math.Min(a.Num("bevel"), math.Min(w/2, h/2)))
			prof := roundedRect(r-w, 0, r, h, [4]float64{b, b, b, b}, a.Int("segments"))
			return revolve(prof, max(a.Int("sides"), 3)), nil
		},
	})

	register(&Primitive{
		Name:   "paraboloid",
		Doc:    "Paraboloid with its base on the workplane and the tip at the top.",
		Params: []Param{pRadius, pHeight, pSides, pSteps},
		build: func(a Args) (*geom.Mesh, error) {
			if err := positive(a, "radius", "height"); err != nil {
				return nil, err
			}
			r, h := a.Num("radius"), a.Num("height")
			steps := max(a.Int("steps"), 1)
			prof := []Vec2{{0, 0}}
			for i := 0; i <= steps; i++ {
				x := r * (1 - float64(i)/float64(steps))
				prof = append(prof, Vec2{x, h * (1 - (x/r)*(x/r))})
			}
			return revolve(prof, max(a.Int("sides"), 3)), nil
		},
	})

	register(&Primitive{
		Name:   "wedge",
		Doc:    "Wedge: right triangle profile (XZ) extruded along Y. The vertical face is at -X.",
		Params: []Param{pLength, pWidth, pHeight},
		build: func(a Args) (*geom.Mesh, error) {
			if err := positive(a, "length", "width", "height"); err != nil {
				return nil, err
			}
			l, w, h := a.Num("length"), a.Num("width"), a.Num("height")
			return extrudeAlongY([]Vec2{{-l / 2, 0}, {l / 2, 0}, {-l / 2, h}}, w), nil
		},
	})

	register(&Primitive{
		Name:   "roof",
		Doc:    "Roof: triangular prism whose ridge runs along X.",
		Params: []Param{pLength, pWidth, pHeight},
		build: func(a Args) (*geom.Mesh, error) {
			if err := positive(a, "length", "width", "height"); err != nil {
				return nil, err
			}
			l, w, h := a.Num("length"), a.Num("width"), a.Num("height")
			return extrudeAlongX([]Vec2{{-w / 2, 0}, {w / 2, 0}, {0, h}}, l), nil
		},
	})

	register(&Primitive{
		Name:   "round_roof",
		Doc:    "Round roof: half cylinder lying along X.",
		Params: []Param{pLength, pWidth, withDefault(pHeight, 10.0), pSides},
		build: func(a Args) (*geom.Mesh, error) {
			if err := positive(a, "length", "width", "height"); err != nil {
				return nil, err
			}
			l, w, h := a.Num("length"), a.Num("width"), a.Num("height")
			n := max(a.Int("sides")/2, 2)
			var prof []Vec2
			for i := 0; i <= n; i++ {
				t := math.Pi * float64(i) / float64(n)
				prof = append(prof, Vec2{w / 2 * math.Cos(t), h * math.Sin(t)})
			}
			return extrudeAlongX(prof, l), nil
		},
	})

	register(&Primitive{
		Name: "pyramid",
		Doc:  "Pyramid. With 4 sides the base is a length x width rectangle; other values give a regular polygon base.",
		Params: []Param{pLength, pWidth, pHeight,
			{Name: "sides", Default: 4.0, Doc: "number of sides of the base"}},
		build: func(a Args) (*geom.Mesh, error) {
			if err := positive(a, "length", "width", "height"); err != nil {
				return nil, err
			}
			if err := atLeast(a, "sides", 3); err != nil {
				return nil, err
			}
			l, w, h := a.Num("length"), a.Num("width"), a.Num("height")
			var base []Vec2
			if a.Int("sides") == 4 {
				base = []Vec2{{-l / 2, -w / 2}, {l / 2, -w / 2}, {l / 2, w / 2}, {-l / 2, w / 2}}
			} else {
				base = regularPolygon(a.Int("sides"), l/2, w/2, math.Pi/2)
			}
			return pyramid(base, h), nil
		},
	})

	register(&Primitive{
		Name: "polygon",
		Doc:  "Regular polygon prism (radius measured to the vertices).",
		Params: []Param{{Name: "sides", Default: 6.0, Doc: "number of sides"},
			pRadius, pHeight},
		build: func(a Args) (*geom.Mesh, error) {
			if err := positive(a, "radius", "height"); err != nil {
				return nil, err
			}
			if err := atLeast(a, "sides", 3); err != nil {
				return nil, err
			}
			r := a.Num("radius")
			return prism(regularPolygon(a.Int("sides"), r, r, math.Pi/2), 0, a.Num("height")), nil
		},
	})

	register(&Primitive{
		Name: "star",
		Doc:  "Star prism with a point towards +Y.",
		Params: []Param{{Name: "points", Default: 5.0, Doc: "number of points"},
			pRadius,
			{Name: "inner_radius", Default: 5.0, Doc: "radius of the inner vertices"},
			withDefault(pHeight, 5.0)},
		build: func(a Args) (*geom.Mesh, error) {
			if err := positive(a, "radius", "inner_radius", "height"); err != nil {
				return nil, err
			}
			if err := atLeast(a, "points", 2); err != nil {
				return nil, err
			}
			n := a.Int("points")
			var poly []Vec2
			for i := 0; i < 2*n; i++ {
				r := a.Num("radius")
				if i%2 == 1 {
					r = a.Num("inner_radius")
				}
				t := math.Pi/2 + math.Pi*float64(i)/float64(n)
				poly = append(poly, Vec2{r * math.Cos(t), r * math.Sin(t)})
			}
			return prism(poly, 0, a.Num("height")), nil
		},
	})

	register(&Primitive{
		Name:   "heart",
		Doc:    "Heart prism fitted in a length x width footprint.",
		Params: []Param{pLength, pWidth, withDefault(pHeight, 5.0)},
		build: func(a Args) (*geom.Mesh, error) {
			if err := positive(a, "length", "width", "height"); err != nil {
				return nil, err
			}
			const n = 72
			var poly []Vec2
			for i := 0; i < n; i++ {
				t := 2 * math.Pi * float64(i) / n
				s := math.Sin(t)
				x := 16 * s * s * s
				y := 13*math.Cos(t) - 5*math.Cos(2*t) - 2*math.Cos(3*t) - math.Cos(4*t)
				poly = append(poly, Vec2{x, y})
			}
			fitPolygon(poly, a.Num("length"), a.Num("width"))
			return prism(poly, 0, a.Num("height")), nil
		},
	})

	register(&Primitive{
		Name:   "icosahedron",
		Doc:    "Regular icosahedron (radius measured to the vertices).",
		Params: []Param{pRadius},
		build: func(a Args) (*geom.Mesh, error) {
			if err := positive(a, "radius"); err != nil {
				return nil, err
			}
			return icosahedron(a.Num("radius")), nil
		},
	})

	register(&Primitive{
		Name: "text",
		Doc:  "Extruded text. Use \\n for several lines.",
		Params: []Param{
			{Name: "text", Default: "Text", Doc: "string to write"},
			{Name: "size", Default: 10.0, Doc: "height of the capital letters"},
			withDefault(pHeight, 3.0),
			{Name: "font", Default: "sans", Doc: "sans, sans-bold, sans-italic, sans-bold-italic, mono, mono-bold"},
			{Name: "spacing", Default: 0.0, Doc: "extra space between letters (mm)"},
			{Name: "align", Default: "center", Doc: "line alignment: left, center or right"}},
		build: func(a Args) (*geom.Mesh, error) {
			if err := positive(a, "size", "height"); err != nil {
				return nil, err
			}
			return textMesh(a.Str("text"), a.Str("font"), a.Num("size"), a.Num("height"), a.Num("spacing"), a.Str("align"))
		},
	})

	register(&Primitive{
		Name: "extrude",
		Doc:  "Extrudes a custom 2D polygon (XY points) along Z. Coordinates are kept as given.",
		Params: []Param{
			{Name: "points", Doc: "list of [x, y] points"},
			withDefault(pHeight, 10.0)},
		KeepCoords: true,
		build: func(a Args) (*geom.Mesh, error) {
			pts, err := a.Points("points")
			if err != nil {
				return nil, err
			}
			if err := positive(a, "height"); err != nil {
				return nil, err
			}
			return prism(pts, 0, a.Num("height")), nil
		},
	})

	register(&Primitive{
		Name: "revolve",
		Doc:  "Revolves a custom profile of [r, z] points (r >= 0) around the Z axis. Coordinates are kept as given.",
		Params: []Param{
			{Name: "points", Doc: "list of [r, z] points"},
			pSides},
		KeepCoords: true,
		build: func(a Args) (*geom.Mesh, error) {
			pts, err := a.Points("points")
			if err != nil {
				return nil, err
			}
			for _, p := range pts {
				if p.X < 0 {
					return nil, fmt.Errorf("revolve points must have r >= 0")
				}
			}
			return revolve(cleanPolygon(pts), max(a.Int("sides"), 3)), nil
		},
	})
}

func roundedBox(l, w, h, r float64, steps int) *geom.Mesh {
	r = math.Min(r, math.Min(l/2, math.Min(w/2, h/2)))
	if r <= 1e-9 {
		return prism([]Vec2{{-l / 2, -w / 2}, {l / 2, -w / 2}, {l / 2, w / 2}, {-l / 2, w / 2}}, 0, h)
	}
	steps = max(steps, 1)
	hx, hy, hz := l/2-r, w/2-r, h/2-r
	signs := [4][2]float64{{1, 1}, {-1, 1}, {-1, -1}, {1, -1}}
	var rows [][]Vec3
	for _, hemi := range []float64{-1, 1} {
		for i := 0; i <= steps; i++ {
			phi := 90 * float64(i) / float64(steps)
			if hemi < 0 {
				phi -= 90
			}
			sp, cp := math.Sincos(phi * math.Pi / 180)
			var row []Vec3
			for q := 0; q < 4; q++ {
				for j := 0; j <= steps; j++ {
					th := (float64(q)*90 + 90*float64(j)/float64(steps)) * math.Pi / 180
					st, ct := math.Sincos(th)
					row = append(row, Vec3{
						r*cp*ct + signs[q][0]*hx,
						r*cp*st + signs[q][1]*hy,
						r*sp + hemi*hz + h/2,
					})
				}
			}
			rows = append(rows, row)
		}
	}
	m := &geom.Mesh{}
	for i := 0; i+1 < len(rows); i++ {
		a, b := rows[i], rows[i+1]
		for c := range a {
			d := (c + 1) % len(a)
			m.AddQuad(a[c], a[d], b[d], b[c])
		}
	}
	// Flat caps between the four pole points.
	m.AddQuad(Vec3{hx, hy, h}, Vec3{-hx, hy, h}, Vec3{-hx, -hy, h}, Vec3{hx, -hy, h})
	m.AddQuad(Vec3{hx, hy, 0}, Vec3{hx, -hy, 0}, Vec3{-hx, -hy, 0}, Vec3{-hx, hy, 0})
	m.FixOrientation()
	return m
}

// extrudeAlongX extrudes a (y, z) profile along X, centered.
func extrudeAlongX(prof []Vec2, length float64) *geom.Mesh {
	m := prism(prof, -length/2, length/2)
	// (x', y', z') -> (z', x', y')
	m.Transform(geom.Mat4{0, 0, 1, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 1})
	return m
}

// extrudeAlongY extrudes an (x, z) profile along Y, centered.
func extrudeAlongY(prof []Vec2, width float64) *geom.Mesh {
	m := prism(prof, -width/2, width/2)
	// (x', y', z') -> (x', z', y')
	m.Transform(geom.Mat4{1, 0, 0, 0, 0, 0, 1, 0, 0, 1, 0, 0, 0, 0, 0, 1})
	m.FixOrientation()
	return m
}

func pyramid(base []Vec2, h float64) *geom.Mesh {
	base = cleanPolygon(base)
	m := &geom.Mesh{}
	apex := Vec3{0, 0, h}
	for _, t := range triangulate(base) {
		a, b, c := base[t[0]], base[t[1]], base[t[2]]
		m.AddTri(Vec3{a.X, a.Y, 0}, Vec3{c.X, c.Y, 0}, Vec3{b.X, b.Y, 0})
	}
	for i := range base {
		a, b := base[i], base[(i+1)%len(base)]
		m.AddTri(Vec3{a.X, a.Y, 0}, Vec3{b.X, b.Y, 0}, apex)
	}
	m.FixOrientation()
	return m
}

// fitPolygon scales a polygon in place to fit a w x h box.
func fitPolygon(poly []Vec2, w, h float64) {
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, p := range poly {
		minX, maxX = math.Min(minX, p.X), math.Max(maxX, p.X)
		minY, maxY = math.Min(minY, p.Y), math.Max(maxY, p.Y)
	}
	for i := range poly {
		poly[i].X = (poly[i].X-minX)/(maxX-minX)*w - w/2
		poly[i].Y = (poly[i].Y-minY)/(maxY-minY)*h - h/2
	}
}

func icosahedron(r float64) *geom.Mesh {
	phi := (1 + math.Sqrt(5)) / 2
	v := []Vec3{
		{-1, phi, 0}, {1, phi, 0}, {-1, -phi, 0}, {1, -phi, 0},
		{0, -1, phi}, {0, 1, phi}, {0, -1, -phi}, {0, 1, -phi},
		{phi, 0, -1}, {phi, 0, 1}, {-phi, 0, -1}, {-phi, 0, 1},
	}
	for i := range v {
		v[i] = v[i].Normalize().Scale(r)
	}
	faces := [][3]int{
		{0, 11, 5}, {0, 5, 1}, {0, 1, 7}, {0, 7, 10}, {0, 10, 11},
		{1, 5, 9}, {5, 11, 4}, {11, 10, 2}, {10, 7, 6}, {7, 1, 8},
		{3, 9, 4}, {3, 4, 2}, {3, 2, 6}, {3, 6, 8}, {3, 8, 9},
		{4, 9, 5}, {2, 4, 11}, {6, 2, 10}, {8, 6, 7}, {9, 8, 1},
	}
	m := &geom.Mesh{}
	for _, f := range faces {
		m.AddTri(v[f[0]], v[f[1]], v[f[2]])
	}
	m.FixOrientation()
	return m
}
