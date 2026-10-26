// Package importer converts triangle meshes (typically loaded from STL) into
// TCAD source. Each connected body is analysed and, when possible, rebuilt
// from primitives: spheres and prisms (2.5D extrusions along any axis) whose
// outlines become boxes, cylinders, regular polygons or custom extrusions,
// with their through holes as "hole" shapes. Bodies that cannot be
// recognized are written as exact polyhedrons.
package importer

import (
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/jespino/tcad-studio/internal/geom"
	"github.com/jespino/tcad-studio/internal/stl"
)

type Vec3 = geom.Vec3

// Mode selects how the mesh is written.
type Mode string

const (
	// ModeShapes rebuilds bodies from primitives where possible.
	ModeShapes Mode = "shapes"
	// ModeEmbed writes the whole mesh as one exact polyhedron.
	ModeEmbed Mode = "embed"
	// ModeRef references the STL file with stl("...").
	ModeRef Mode = "ref"
)

// Options configures a conversion.
type Options struct {
	Mode Mode
	// Source is the name of the original file, used in comments.
	Source string
	// RefPath is the path written in stl("...") for ModeRef.
	RefPath string
	// Center moves the model to the workplane (centered in XY, z min = 0).
	Center bool
}

// Report summarizes what the conversion did.
type Report struct {
	InputTriangles int
	OpenEdges      int
	Bodies         int
	Cavities       int
	// Kinds counts the generated statements by kind (box, cylinder...).
	Kinds map[string]int
	// Unrecognized is the number of bodies written as polyhedrons.
	Unrecognized int
}

// Convert generates TCAD source for the mesh.
func Convert(m *geom.Mesh, opts Options) (string, *Report, error) {
	if len(m.Tris) == 0 {
		return "", nil, fmt.Errorf("the mesh is empty")
	}
	rep := &Report{InputTriangles: len(m.Tris), Kinds: map[string]int{}}
	ix := stl.Weld(m)
	ix.FixTJunctions()
	rep.OpenEdges = ix.OpenEdges()
	if signedVolume(ix.Verts, ix.Tris) < 0 {
		for i := range ix.Tris {
			ix.Tris[i][1], ix.Tris[i][2] = ix.Tris[i][2], ix.Tris[i][1]
		}
	}
	if opts.Center {
		center(ix.Verts)
	}
	mn, mx := bounds(ix.Verts)

	var b strings.Builder
	src := opts.Source
	if src == "" {
		src = "mesh"
	}
	fmt.Fprintf(&b, "// Imported from %s with \"tcad import\" (mode %s).\n", filepath.Base(src), opts.Mode)
	fmt.Fprintf(&b, "// Original: %d triangles, size X %s  Y %s  Z %s mm.\n",
		len(m.Tris), num(mx.X-mn.X), num(mx.Y-mn.Y), num(mx.Z-mn.Z))
	if rep.OpenEdges > 0 {
		fmt.Fprintf(&b, "// Warning: the original mesh is not watertight (%d open edges).\n", rep.OpenEdges)
	}
	b.WriteString("\n")

	switch opts.Mode {
	case ModeRef:
		ref := opts.RefPath
		if ref == "" {
			ref = src
		}
		center := "false"
		if opts.Center {
			center = "true"
		}
		fmt.Fprintf(&b, "stl(file=%q, center=%s)\n", filepath.ToSlash(ref), center)
		rep.Bodies = 1
		rep.Kinds["stl"] = 1
	case ModeEmbed:
		writePolyhedron(&b, ix.Verts, ix.Tris, "", "")
		rep.Bodies = 1
		rep.Kinds["polyhedron"] = 1
	case ModeShapes, "":
		for _, comp := range components(ix.Tris, len(ix.Verts)) {
			rep.Bodies++
			cavity := signedVolume(ix.Verts, comp) < 0
			if cavity {
				// Inner shell of a hollow part: describe it as an outward
				// solid and subtract it.
				rep.Cavities++
				comp = flipped(comp)
			}
			body := convertBody(ix.Verts, comp, cavity, rep)
			b.WriteString(body)
		}
	default:
		return "", nil, fmt.Errorf("unknown mode %q (use shapes, embed or ref)", opts.Mode)
	}
	return b.String(), rep, nil
}

func convertBody(verts []Vec3, tris [][3]int, cavity bool, rep *Report) string {
	prefix := ""
	comment := ""
	if cavity {
		prefix = "hole "
		comment = "// Internal cavity\n"
	}
	if !closed(tris) {
		rep.Unrecognized++
		rep.Kinds["polyhedron"]++
		var b strings.Builder
		b.WriteString(comment)
		writePolyhedron(&b, verts, tris, prefix, "// Body (not watertight, kept as an exact mesh)\n")
		return b.String()
	}
	if s, ok := detectSphere(verts, tris); ok {
		rep.Kinds["sphere"]++
		return comment + prefix + s + "\n"
	}
	if p, ok := detectPrism(verts, tris); ok {
		return comment + p.emit(prefix, rep)
	}
	rep.Unrecognized++
	rep.Kinds["polyhedron"]++
	var b strings.Builder
	b.WriteString(comment)
	writePolyhedron(&b, verts, tris, prefix, "// Body not recognized as primitives, kept as an exact mesh\n")
	return b.String()
}

// Mesh helpers.

func signedVolume(verts []Vec3, tris [][3]int) float64 {
	var v float64
	for _, t := range tris {
		v += verts[t[0]].Dot(verts[t[1]].Cross(verts[t[2]]))
	}
	return v / 6
}

func flipped(tris [][3]int) [][3]int {
	out := make([][3]int, len(tris))
	for i, t := range tris {
		out[i] = [3]int{t[0], t[2], t[1]}
	}
	return out
}

func bounds(verts []Vec3) (Vec3, Vec3) {
	mn, mx := verts[0], verts[0]
	for _, v := range verts {
		mn, mx = mn.Min(v), mx.Max(v)
	}
	return mn, mx
}

func center(verts []Vec3) {
	mn, mx := bounds(verts)
	d := geom.V(-(mn.X+mx.X)/2, -(mn.Y+mx.Y)/2, -mn.Z)
	for i := range verts {
		verts[i] = verts[i].Add(d)
	}
}

func closed(tris [][3]int) bool {
	type edge struct{ a, b int }
	count := map[edge]int{}
	for _, t := range tris {
		for k := 0; k < 3; k++ {
			count[edge{t[k], t[(k+1)%3]}]++
		}
	}
	for e, c := range count {
		if c != 1 || count[edge{e.b, e.a}] != 1 {
			return false
		}
	}
	return true
}

// components splits the triangles into connected bodies, largest first.
func components(tris [][3]int, nverts int) [][][3]int {
	parent := make([]int, nverts)
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(x int) int {
		for parent[x] != x {
			parent[x] = parent[parent[x]]
			x = parent[x]
		}
		return x
	}
	for _, t := range tris {
		a := find(t[0])
		for _, v := range t[1:] {
			if b := find(v); b != a {
				parent[b] = a
			}
		}
	}
	groups := map[int][][3]int{}
	var order []int
	for _, t := range tris {
		r := find(t[0])
		if _, ok := groups[r]; !ok {
			order = append(order, r)
		}
		groups[r] = append(groups[r], t)
	}
	out := make([][][3]int, 0, len(order))
	for _, r := range order {
		out = append(out, groups[r])
	}
	sort.SliceStable(out, func(i, j int) bool { return len(out[i]) > len(out[j]) })
	return out
}

// Output helpers.

// num formats a number compactly: with up to 4 decimals when that is within
// 2 µm of the value (STL coordinates are float32, so clean dimensions carry
// noise), otherwise with 6 decimals to keep the geometry exact.
func num(x float64) string {
	if math.Abs(x) < 5e-7 {
		return "0"
	}
	prec := 6
	if r := math.Round(x*1e4) / 1e4; math.Abs(r-x) < 2e-6 {
		x, prec = r, 4
	}
	s := strconv.FormatFloat(x, 'f', prec, 64)
	s = strings.TrimRight(s, "0")
	s = strings.TrimSuffix(s, ".")
	if s == "-0" {
		return "0"
	}
	return s
}

func writePolyhedron(b *strings.Builder, verts []Vec3, tris [][3]int, prefix, comment string) {
	// Reindex so that only the vertices of these triangles are written.
	idx := map[int]int{}
	var pts []Vec3
	faces := make([][3]int, len(tris))
	for i, t := range tris {
		for k, v := range t {
			j, ok := idx[v]
			if !ok {
				j = len(pts)
				idx[v] = j
				pts = append(pts, verts[v])
			}
			faces[i][k] = j
		}
	}
	b.WriteString(comment)
	b.WriteString(prefix + "polyhedron(\n  points=[")
	for i, p := range pts {
		if i%4 == 0 {
			b.WriteString("\n    ")
		} else {
			b.WriteString(" ")
		}
		fmt.Fprintf(b, "[%s, %s, %s],", num5(p.X), num5(p.Y), num5(p.Z))
	}
	b.WriteString("\n  ],\n  faces=[")
	for i, f := range faces {
		if i%8 == 0 {
			b.WriteString("\n    ")
		} else {
			b.WriteString(" ")
		}
		fmt.Fprintf(b, "[%d, %d, %d],", f[0], f[1], f[2])
	}
	b.WriteString("\n  ]\n)\n")
}

// num5 keeps more precision for raw mesh coordinates.
func num5(x float64) string {
	if math.Abs(x) < 5e-7 {
		return "0"
	}
	s := fmt.Sprintf("%.6f", x)
	s = strings.TrimRight(s, "0")
	return strings.TrimSuffix(s, ".")
}

// transform formats .rotate(...).move(...) for a rotation matrix (columns
// are the images of the local axes) and a translation.
func transform(r [3]Vec3, t Vec3) string {
	var b strings.Builder
	ax, ay, az := euler(r)
	if ax != "0" || ay != "0" || az != "0" {
		b.WriteString(".rotate(")
		var parts []string
		for _, p := range [][2]string{{"x", ax}, {"y", ay}, {"z", az}} {
			if p[1] != "0" {
				parts = append(parts, p[0]+"="+p[1])
			}
		}
		b.WriteString(strings.Join(parts, ", ") + ")")
	}
	if num(t.X) != "0" || num(t.Y) != "0" || num(t.Z) != "0" {
		var parts []string
		for _, p := range [][2]string{{"x", num(t.X)}, {"y", num(t.Y)}, {"z", num(t.Z)}} {
			if p[1] != "0" {
				parts = append(parts, p[0]+"="+p[1])
			}
		}
		b.WriteString(".move(" + strings.Join(parts, ", ") + ")")
	}
	return b.String()
}

// euler decomposes R = Rz(c)·Ry(b)·Rx(a), which is what .rotate(a, b, c)
// applies, and returns the formatted angles in degrees.
func euler(r [3]Vec3) (string, string, string) {
	// m[row][col]
	m := [3][3]float64{
		{r[0].X, r[1].X, r[2].X},
		{r[0].Y, r[1].Y, r[2].Y},
		{r[0].Z, r[1].Z, r[2].Z},
	}
	var a, b, c float64
	sb := -m[2][0]
	sb = math.Max(-1, math.Min(1, sb))
	b = math.Asin(sb)
	if math.Abs(sb) < 1-1e-9 {
		a = math.Atan2(m[2][1], m[2][2])
		c = math.Atan2(m[1][0], m[0][0])
	} else {
		// Gimbal lock: only a + c (or a - c) is defined; put it all in c.
		a = 0
		c = math.Atan2(-m[0][1], m[1][1])
	}
	deg := func(x float64) string {
		d := x * 180 / math.Pi
		if math.Abs(d-math.Round(d)) < 1e-6 {
			d = math.Round(d)
		}
		if d == -180 {
			d = 180
		}
		return num(d)
	}
	return deg(a), deg(b), deg(c)
}
