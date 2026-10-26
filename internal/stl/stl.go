// Package stl writes meshes as STL files and prepares them for export by
// welding vertices and removing T-junctions left by the CSG operations.
package stl

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"sort"

	"github.com/jespino/tcad-studio/internal/geom"
)

// WriteBinary writes a binary STL.
func WriteBinary(w io.Writer, m *geom.Mesh) error {
	return writeBinary(w, m, "tcad binary STL", func(geom.Color) uint16 { return 0 })
}

// WriteBinaryColored writes a binary STL with per-facet colors in the
// VisCAM/SolidView convention ("COLOR=" in the header, RGB555 attributes),
// which three.js understands. Slicers usually ignore the colors.
func WriteBinaryColored(w io.Writer, m *geom.Mesh) error {
	header := "tcad COLOR=\xe8\x8a\x2e\xff"
	return writeBinary(w, m, header, func(c geom.Color) uint16 {
		return uint16(c.R>>3) | uint16(c.G>>3)<<5 | uint16(c.B>>3)<<10
	})
}

func writeBinary(w io.Writer, m *geom.Mesh, header string, attr func(geom.Color) uint16) error {
	bw := bufio.NewWriter(w)
	hdr := make([]byte, 80)
	copy(hdr, header)
	if _, err := bw.Write(hdr); err != nil {
		return err
	}
	if err := binary.Write(bw, binary.LittleEndian, uint32(len(m.Tris))); err != nil {
		return err
	}
	buf := make([]byte, 50)
	put := func(off int, v float64) {
		binary.LittleEndian.PutUint32(buf[off:], math.Float32bits(float32(v)))
	}
	for _, t := range m.Tris {
		n := t.Normal()
		put(0, n.X)
		put(4, n.Y)
		put(8, n.Z)
		for k := 0; k < 3; k++ {
			put(12+k*12, t.V[k].X)
			put(16+k*12, t.V[k].Y)
			put(20+k*12, t.V[k].Z)
		}
		binary.LittleEndian.PutUint16(buf[48:], attr(t.Color))
		if _, err := bw.Write(buf); err != nil {
			return err
		}
	}
	return bw.Flush()
}

// WriteASCII writes an ASCII STL.
func WriteASCII(w io.Writer, m *geom.Mesh, name string) error {
	bw := bufio.NewWriter(w)
	fmt.Fprintf(bw, "solid %s\n", name)
	for _, t := range m.Tris {
		n := t.Normal()
		fmt.Fprintf(bw, "  facet normal %g %g %g\n    outer loop\n", n.X, n.Y, n.Z)
		for _, v := range t.V {
			fmt.Fprintf(bw, "      vertex %g %g %g\n", v.X, v.Y, v.Z)
		}
		fmt.Fprintf(bw, "    endloop\n  endfacet\n")
	}
	fmt.Fprintf(bw, "endsolid %s\n", name)
	return bw.Flush()
}

// weldTol is the distance under which vertices are merged.
const weldTol = 1e-5

type key struct{ x, y, z int64 }

func keyOf(v geom.Vec3) key {
	return key{int64(math.Round(v.X / weldTol)), int64(math.Round(v.Y / weldTol)), int64(math.Round(v.Z / weldTol))}
}

// Indexed is a welded mesh: shared vertices plus index triangles.
type Indexed struct {
	Verts []geom.Vec3
	Tris  [][3]int
	Color []geom.Color
}

// Weld merges coincident vertices.
func Weld(m *geom.Mesh) *Indexed {
	ix := &Indexed{}
	lookup := map[key]int{}
	id := func(v geom.Vec3) int {
		k := keyOf(v)
		// Check neighbouring cells too, so points that straddle a rounding
		// boundary still weld.
		for dx := int64(-1); dx <= 1; dx++ {
			for dy := int64(-1); dy <= 1; dy++ {
				for dz := int64(-1); dz <= 1; dz++ {
					if i, ok := lookup[key{k.x + dx, k.y + dy, k.z + dz}]; ok && ix.Verts[i].Sub(v).Len() < weldTol {
						return i
					}
				}
			}
		}
		lookup[k] = len(ix.Verts)
		ix.Verts = append(ix.Verts, v)
		return len(ix.Verts) - 1
	}
	for _, t := range m.Tris {
		a, b, c := id(t.V[0]), id(t.V[1]), id(t.V[2])
		if a == b || b == c || a == c || isNeedle(ix.Verts[a], ix.Verts[b], ix.Verts[c]) {
			continue
		}
		ix.Tris = append(ix.Tris, [3]int{a, b, c})
		ix.Color = append(ix.Color, t.Color)
	}
	return ix
}

// isNeedle reports triangles thinner than the weld tolerance. Dropping them
// leaves T-junctions that FixTJunctions then closes.
func isNeedle(a, b, c geom.Vec3) bool {
	area2 := b.Sub(a).Cross(c.Sub(a)).Len()
	longest := math.Max(b.Sub(a).Len(), math.Max(c.Sub(b).Len(), a.Sub(c).Len()))
	return area2/longest < weldTol
}

func (ix *Indexed) Mesh() *geom.Mesh {
	m := &geom.Mesh{Tris: make([]geom.Triangle, 0, len(ix.Tris))}
	for i, t := range ix.Tris {
		m.Tris = append(m.Tris, geom.Triangle{V: [3]geom.Vec3{ix.Verts[t[0]], ix.Verts[t[1]], ix.Verts[t[2]]}, Color: ix.Color[i]})
	}
	return m
}

type edge struct{ a, b int }

func (ix *Indexed) edgeCounts() map[edge]int {
	counts := map[edge]int{}
	for _, t := range ix.Tris {
		for k := 0; k < 3; k++ {
			a, b := t[k], t[(k+1)%3]
			if a > b {
				a, b = b, a
			}
			counts[edge{a, b}]++
		}
	}
	return counts
}

// OpenEdges returns how many edges are not shared by exactly two triangles.
// Zero means the mesh is watertight (manifold).
func (ix *Indexed) OpenEdges() int {
	n := 0
	for _, c := range ix.edgeCounts() {
		if c != 2 {
			n++
		}
	}
	return n
}

// FixTJunctions splits triangles that have other vertices lying on their
// edges, which is what BSP based CSG leaves behind. After this the mesh is
// normally watertight.
func (ix *Indexed) FixTJunctions() {
	for iter := 0; iter < 4; iter++ {
		counts := ix.edgeCounts()
		// Only edges used once can host a T-junction.
		var open []edge
		for e, c := range counts {
			if c == 1 {
				open = append(open, e)
			}
		}
		if len(open) == 0 {
			return
		}
		openVerts := map[int]bool{}
		for _, e := range open {
			openVerts[e.a] = true
			openVerts[e.b] = true
		}
		candidates := make([]int, 0, len(openVerts))
		for v := range openVerts {
			candidates = append(candidates, v)
		}
		sort.Ints(candidates)
		grid := newGrid(ix.Verts, candidates)

		// For every open edge, find vertices lying strictly inside it.
		splits := map[edge][]int{}
		for _, e := range open {
			a, b := ix.Verts[e.a], ix.Verts[e.b]
			d := b.Sub(a)
			l2 := d.Dot(d)
			if l2 == 0 {
				continue
			}
			var on []int
			grid.query(a.Min(b), a.Max(b), func(v int) {
				if v == e.a || v == e.b {
					return
				}
				p := ix.Verts[v]
				t := p.Sub(a).Dot(d) / l2
				if t <= 1e-9 || t >= 1-1e-9 {
					return
				}
				if a.Add(d.Scale(t)).Sub(p).Len() < weldTol {
					on = append(on, v)
				}
			})
			if len(on) > 0 {
				sort.Slice(on, func(i, j int) bool {
					return ix.Verts[on[i]].Sub(a).Dot(d) < ix.Verts[on[j]].Sub(a).Dot(d)
				})
				splits[e] = on
			}
		}
		if len(splits) == 0 {
			return
		}
		var tris [][3]int
		var colors []geom.Color
		for i, t := range ix.Tris {
			// Build the polygon with the extra vertices inserted on each edge.
			var poly []int
			changed := false
			for k := 0; k < 3; k++ {
				a, b := t[k], t[(k+1)%3]
				poly = append(poly, a)
				e := edge{min(a, b), max(a, b)}
				if s, ok := splits[e]; ok {
					changed = true
					if a == e.a {
						poly = append(poly, s...)
					} else {
						for j := len(s) - 1; j >= 0; j-- {
							poly = append(poly, s[j])
						}
					}
				}
			}
			if !changed {
				tris = append(tris, t)
				colors = append(colors, ix.Color[i])
				continue
			}
			for _, nt := range ix.triangulateCollinear(poly, t) {
				tris = append(tris, nt)
				colors = append(colors, ix.Color[i])
			}
		}
		ix.Tris, ix.Color = tris, colors
	}
}

// triangulateCollinear triangulates a triangle whose edges received extra
// collinear vertices. It fans from a new vertex at the centroid, which never
// produces degenerate triangles and keeps every boundary edge.
func (ix *Indexed) triangulateCollinear(poly []int, orig [3]int) [][3]int {
	a, b, c := ix.Verts[orig[0]], ix.Verts[orig[1]], ix.Verts[orig[2]]
	ix.Verts = append(ix.Verts, a.Add(b).Add(c).Scale(1.0/3))
	center := len(ix.Verts) - 1
	out := make([][3]int, 0, len(poly))
	for i := range poly {
		out = append(out, [3]int{center, poly[i], poly[(i+1)%len(poly)]})
	}
	return out
}

type grid struct {
	cell  float64
	cells map[key][]int
	verts []geom.Vec3
}

func newGrid(verts []geom.Vec3, ids []int) *grid {
	mn, mx := verts[ids[0]], verts[ids[0]]
	for _, i := range ids {
		mn, mx = mn.Min(verts[i]), mx.Max(verts[i])
	}
	size := mx.Sub(mn)
	cell := math.Max(math.Max(size.X, size.Y), size.Z) / 64
	if cell < 1e-3 {
		cell = 1e-3
	}
	g := &grid{cell: cell, cells: map[key][]int{}, verts: verts}
	for _, i := range ids {
		k := g.key(verts[i])
		g.cells[k] = append(g.cells[k], i)
	}
	return g
}

func (g *grid) key(v geom.Vec3) key {
	return key{int64(math.Floor(v.X / g.cell)), int64(math.Floor(v.Y / g.cell)), int64(math.Floor(v.Z / g.cell))}
}

func (g *grid) query(mn, mx geom.Vec3, f func(int)) {
	pad := geom.V(weldTol, weldTol, weldTol)
	a, b := g.key(mn.Sub(pad)), g.key(mx.Add(pad))
	// Very long edges may cover many cells; iterate over the smaller set.
	ncells := (b.x - a.x + 1) * (b.y - a.y + 1) * (b.z - a.z + 1)
	if ncells > int64(len(g.cells)) {
		for k, ids := range g.cells {
			if k.x >= a.x && k.x <= b.x && k.y >= a.y && k.y <= b.y && k.z >= a.z && k.z <= b.z {
				for _, i := range ids {
					f(i)
				}
			}
		}
		return
	}
	for x := a.x; x <= b.x; x++ {
		for y := a.y; y <= b.y; y++ {
			for z := a.z; z <= b.z; z++ {
				for _, i := range g.cells[key{x, y, z}] {
					f(i)
				}
			}
		}
	}
}

// Prepare welds the mesh and repairs T-junctions, returning the cleaned
// mesh and the number of remaining open edges.
func Prepare(m *geom.Mesh) (*geom.Mesh, int) {
	ix := Weld(m)
	ix.FixTJunctions()
	return ix.Mesh(), ix.OpenEdges()
}
