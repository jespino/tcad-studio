// Package csg implements boolean operations (union, difference and
// intersection) on closed triangle meshes using BSP trees. The algorithm is
// a port of Evan Wallace's csg.js.
package csg

import (
	"github.com/jespino/tcad-studio/internal/geom"
)

type Vec3 = geom.Vec3

// eps is the tolerance used to classify points against planes (in mm).
const eps = 1e-5

type plane struct {
	n Vec3
	w float64
}

func planeFromPoints(a, b, c Vec3) (plane, bool) {
	n := b.Sub(a).Cross(c.Sub(a))
	l := n.Len()
	if l < 1e-14 {
		return plane{}, false
	}
	n = n.Scale(1 / l)
	return plane{n: n, w: n.Dot(a)}, true
}

func (p plane) flipped() plane { return plane{n: p.n.Neg(), w: -p.w} }

type polygon struct {
	verts []Vec3
	plane plane
	color geom.Color
}

func (p polygon) flipped() polygon {
	v := make([]Vec3, len(p.verts))
	for i := range p.verts {
		v[i] = p.verts[len(p.verts)-1-i]
	}
	return polygon{verts: v, plane: p.plane.flipped(), color: p.color}
}

const (
	coplanar = 0
	front    = 1
	back     = 2
	spanning = 3
)

// rawType classifies poly as coplanar, front, back or spanning.
func (p plane) rawType(poly polygon) int {
	ptype := coplanar
	for _, v := range poly.verts {
		t := p.n.Dot(v) - p.w
		if t < -eps {
			ptype |= back
		} else if t > eps {
			ptype |= front
		}
	}
	return ptype
}

// classify is like rawType, but coplanar polygons are reported as front or
// back depending on their facing.
func (p plane) classify(poly polygon) int {
	t := p.rawType(poly)
	if t == coplanar {
		if p.n.Dot(poly.plane.n) > 0 {
			return front
		}
		return back
	}
	return t
}

// split classifies poly against the plane and distributes it (or its pieces)
// into the output lists.
func (p plane) split(poly polygon, coFront, coBack, fr, bk *[]polygon) {
	ptype := 0
	var typesBuf [8]int
	types := typesBuf[:0]
	for _, v := range poly.verts {
		t := p.n.Dot(v) - p.w
		typ := coplanar
		if t < -eps {
			typ = back
		} else if t > eps {
			typ = front
		}
		ptype |= typ
		types = append(types, typ)
	}
	switch ptype {
	case coplanar:
		if p.n.Dot(poly.plane.n) > 0 {
			*coFront = append(*coFront, poly)
		} else {
			*coBack = append(*coBack, poly)
		}
	case front:
		*fr = append(*fr, poly)
	case back:
		*bk = append(*bk, poly)
	default:
		var f, b []Vec3
		n := len(poly.verts)
		for i := 0; i < n; i++ {
			j := (i + 1) % n
			ti, tj := types[i], types[j]
			vi, vj := poly.verts[i], poly.verts[j]
			if ti != back {
				f = append(f, vi)
			}
			if ti != front {
				b = append(b, vi)
			}
			if ti|tj == spanning {
				t := (p.w - p.n.Dot(vi)) / p.n.Dot(vj.Sub(vi))
				v := vi.Lerp(vj, t)
				f = append(f, v)
				b = append(b, v)
			}
		}
		if len(f) >= 3 {
			*fr = append(*fr, polygon{verts: f, plane: poly.plane, color: poly.color})
		}
		if len(b) >= 3 {
			*bk = append(*bk, polygon{verts: b, plane: poly.plane, color: poly.color})
		}
	}
}

type node struct {
	plane       *plane
	front, back *node
	polys       []polygon
}

func newNode(polys []polygon) *node {
	n := &node{}
	n.build(append([]polygon(nil), polys...))
	return n
}

func (n *node) invert() {
	for i := range n.polys {
		n.polys[i] = n.polys[i].flipped()
	}
	if n.plane != nil {
		f := n.plane.flipped()
		n.plane = &f
	}
	if n.front != nil {
		n.front.invert()
	}
	if n.back != nil {
		n.back.invert()
	}
	n.front, n.back = n.back, n.front
}

// clipPolygons removes the parts of polys that are inside this BSP tree.
func (n *node) clipPolygons(polys []polygon) []polygon {
	out := make([]polygon, 0, len(polys))
	for _, p := range polys {
		n.clipOne(p, &out)
	}
	return out
}

// clipOne pushes a single polygon down the tree, only allocating when it has
// to be split. Coplanar polygons follow their facing direction.
func (n *node) clipOne(p polygon, out *[]polygon) {
	for {
		if n.plane == nil {
			*out = append(*out, p)
			return
		}
		switch n.plane.classify(p) {
		case front:
			if n.front == nil {
				*out = append(*out, p)
				return
			}
			n = n.front
		case back:
			if n.back == nil {
				return
			}
			n = n.back
		default:
			var f, b []polygon
			n.plane.split(p, &f, &b, &f, &b)
			for _, fp := range f {
				if n.front == nil {
					*out = append(*out, fp)
				} else {
					n.front.clipOne(fp, out)
				}
			}
			if n.back != nil {
				for _, bp := range b {
					n.back.clipOne(bp, out)
				}
			}
			return
		}
	}
}

func (n *node) clipTo(bsp *node) {
	n.polys = bsp.clipPolygons(n.polys)
	if n.front != nil {
		n.front.clipTo(bsp)
	}
	if n.back != nil {
		n.back.clipTo(bsp)
	}
}

func (n *node) allPolygons() []polygon {
	out := append([]polygon(nil), n.polys...)
	if n.front != nil {
		out = append(out, n.front.allPolygons()...)
	}
	if n.back != nil {
		out = append(out, n.back.allPolygons()...)
	}
	return out
}

// build adds polys to the tree. The back list reuses the input slice and the
// back branch is walked iteratively, so convex meshes (whose trees are long
// chains) do not cost quadratic copying or deep recursion.
func (n *node) build(polys []polygon) {
	for len(polys) > 0 {
		if n.plane == nil {
			p := polys[0].plane
			n.plane = &p
		}
		var f, extraBack []polygon
		w := 0
		for _, p := range polys {
			switch n.plane.rawType(p) {
			case coplanar:
				n.polys = append(n.polys, p)
			case front:
				f = append(f, p)
			case back:
				polys[w] = p
				w++
			default:
				var fb, bb []polygon
				n.plane.split(p, &n.polys, &n.polys, &fb, &bb)
				f = append(f, fb...)
				extraBack = append(extraBack, bb...)
			}
		}
		back := append(polys[:w], extraBack...)
		if len(f) > 0 {
			if n.front == nil {
				n.front = &node{}
			}
			n.front.build(f)
		}
		if len(back) == 0 {
			return
		}
		if n.back == nil {
			n.back = &node{}
		}
		n, polys = n.back, back
	}
}

func toPolygons(m *geom.Mesh) []polygon {
	out := make([]polygon, 0, len(m.Tris))
	for _, t := range m.Tris {
		pl, ok := planeFromPoints(t.V[0], t.V[1], t.V[2])
		if !ok {
			continue
		}
		out = append(out, polygon{verts: []Vec3{t.V[0], t.V[1], t.V[2]}, plane: pl, color: t.Color})
	}
	return out
}

func fromPolygons(polys []polygon) *geom.Mesh {
	m := &geom.Mesh{}
	for _, p := range polys {
		for i := 1; i+1 < len(p.verts); i++ {
			a, b, c := p.verts[0], p.verts[i], p.verts[i+1]
			if b.Sub(a).Cross(c.Sub(a)).Len() < 1e-12 {
				continue
			}
			m.Tris = append(m.Tris, geom.Triangle{V: [3]Vec3{a, b, c}, Color: p.color})
		}
	}
	return m
}

type box struct{ min, max Vec3 }

func bboxOf(m *geom.Mesh) (box, bool) {
	mn, mx, ok := m.BBox()
	return box{mn, mx}, ok
}

func (a box) overlaps(b box) bool {
	const e = 1e-6
	return a.min.X <= b.max.X+e && a.max.X >= b.min.X-e &&
		a.min.Y <= b.max.Y+e && a.max.Y >= b.min.Y-e &&
		a.min.Z <= b.max.Z+e && a.max.Z >= b.min.Z-e
}

func (a box) union(b box) box { return box{a.min.Min(b.min), a.max.Max(b.max)} }

func flipAll(polys []polygon) []polygon {
	out := make([]polygon, len(polys))
	for i, p := range polys {
		out[i] = p.flipped()
	}
	return out
}

func polyBox(p polygon) box {
	b := box{p.verts[0], p.verts[0]}
	for _, v := range p.verts[1:] {
		b.min = b.min.Min(v)
		b.max = b.max.Max(v)
	}
	return b
}

// partition splits polys into those touching bb and the rest. Polygons that
// do not touch the other operand's bounding box cannot be affected by it,
// which avoids needless fragmentation.
func partition(polys []polygon, bb box) (near, far []polygon) {
	for _, p := range polys {
		if polyBox(p).overlaps(bb) {
			near = append(near, p)
		} else {
			far = append(far, p)
		}
	}
	return near, far
}

type operands struct {
	aNear, aFar, bNear, bFar []polygon
	aAll, bAll               []polygon
}

func prepare(a, b *geom.Mesh) (operands, bool) {
	ba, oka := bboxOf(a)
	bb, okb := bboxOf(b)
	if !oka || !okb || !ba.overlaps(bb) {
		return operands{}, false
	}
	var o operands
	o.aAll, o.bAll = toPolygons(a), toPolygons(b)
	o.aNear, o.aFar = partition(o.aAll, bb)
	o.bNear, o.bFar = partition(o.bAll, ba)
	return o, true
}

func concat(lists ...[]polygon) *geom.Mesh {
	var all []polygon
	for _, l := range lists {
		all = append(all, l...)
	}
	return fromPolygons(all)
}

// The operations below follow csg.js, but clip the original polygon lists
// instead of the polygons stored in the trees, and only the polygons near
// the other operand.

// Union returns a ∪ b.
func Union(a, b *geom.Mesh) *geom.Mesh {
	if len(a.Tris) == 0 {
		return b.Clone()
	}
	if len(b.Tris) == 0 {
		return a.Clone()
	}
	o, ok := prepare(a, b)
	if !ok {
		r := a.Clone()
		r.Append(b)
		return r
	}
	bspA, bspB := newNode(o.aAll), newNode(o.bAll)
	aKept := bspB.clipPolygons(o.aNear)
	bKept := flipAll(bspA.clipPolygons(flipAll(bspA.clipPolygons(o.bNear))))
	return concat(o.aFar, aKept, o.bFar, bKept)
}

// Subtract returns a − b.
func Subtract(a, b *geom.Mesh) *geom.Mesh {
	o, ok := prepare(a, b)
	if !ok {
		return a.Clone()
	}
	bspNotA, bspB := newNode(o.aAll), newNode(o.bAll)
	bspNotA.invert()
	aKept := flipAll(bspB.clipPolygons(flipAll(o.aNear)))
	bKept := bspNotA.clipPolygons(flipAll(bspNotA.clipPolygons(o.bNear)))
	return concat(o.aFar, aKept, bKept)
}

// Intersect returns a ∩ b.
func Intersect(a, b *geom.Mesh) *geom.Mesh {
	o, ok := prepare(a, b)
	if !ok {
		return &geom.Mesh{}
	}
	bspNotA, bspNotB := newNode(o.aAll), newNode(o.bAll)
	bspNotA.invert()
	bspNotB.invert()
	aKept := flipAll(bspNotB.clipPolygons(flipAll(o.aNear)))
	bKept := flipAll(bspNotA.clipPolygons(flipAll(bspNotA.clipPolygons(o.bNear))))
	return concat(aKept, bKept)
}

// UnionAll merges many meshes. Meshes whose bounding boxes do not touch are
// simply concatenated, which keeps the expensive BSP work to a minimum.
func UnionAll(meshes []*geom.Mesh) *geom.Mesh {
	type cluster struct {
		mesh *geom.Mesh
		bb   box
	}
	var clusters []cluster
	for _, m := range meshes {
		bb, ok := bboxOf(m)
		if !ok {
			continue
		}
		cur := cluster{mesh: m, bb: bb}
		for {
			merged := false
			for i := 0; i < len(clusters); i++ {
				if clusters[i].bb.overlaps(cur.bb) {
					cur = cluster{mesh: Union(clusters[i].mesh, cur.mesh), bb: clusters[i].bb.union(cur.bb)}
					clusters = append(clusters[:i], clusters[i+1:]...)
					merged = true
					break
				}
			}
			if !merged {
				break
			}
		}
		clusters = append(clusters, cur)
	}
	out := &geom.Mesh{}
	for _, c := range clusters {
		out.Append(c.mesh)
	}
	return out
}

// SubtractAll removes every hole from base. Holes that cannot touch base are
// skipped and the rest are merged first so a single BSP difference is needed.
func SubtractAll(base *geom.Mesh, holes []*geom.Mesh) *geom.Mesh {
	bb, ok := bboxOf(base)
	if !ok {
		return &geom.Mesh{}
	}
	var relevant []*geom.Mesh
	for _, h := range holes {
		if hb, ok := bboxOf(h); ok && hb.overlaps(bb) {
			relevant = append(relevant, h)
		}
	}
	if len(relevant) == 0 {
		return base
	}
	return Subtract(base, UnionAll(relevant))
}

// IntersectAll returns the intersection of all meshes.
func IntersectAll(meshes []*geom.Mesh) *geom.Mesh {
	if len(meshes) == 0 {
		return &geom.Mesh{}
	}
	res := meshes[0]
	for _, m := range meshes[1:] {
		res = Intersect(res, m)
	}
	return res
}
