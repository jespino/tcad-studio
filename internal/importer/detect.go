package importer

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/jespino/tcad-studio/internal/geom"
)

type Vec2 = geom.Vec2

// detectSphere recognizes a tessellated sphere: every vertex at the same
// distance from the center and a volume close to the ideal one.
func detectSphere(verts []Vec3, tris [][3]int) (string, bool) {
	ids := usedVerts(tris)
	if len(ids) < 26 {
		return "", false
	}
	pts := make([]Vec3, len(ids))
	for i, id := range ids {
		pts[i] = verts[id]
	}
	mn, mx := bounds(pts)
	ext := mx.Sub(mn)
	if math.Abs(ext.X-ext.Y) > 0.01*ext.X || math.Abs(ext.X-ext.Z) > 0.01*ext.X {
		return "", false
	}
	c := mn.Add(mx).Scale(0.5)
	var r float64
	for _, p := range pts {
		r += p.Sub(c).Len()
	}
	r /= float64(len(pts))
	for _, p := range pts {
		if math.Abs(p.Sub(c).Len()-r) > 0.005*r {
			return "", false
		}
	}
	ratio := signedVolume(verts, tris) / (4.0 / 3 * math.Pi * r * r * r)
	if ratio < 0.9 || ratio > 1.001 {
		return "", false
	}
	// A UV sphere with s steps has about 2·s² vertices.
	steps := int(math.Round(math.Sqrt(float64(len(pts)) / 2)))
	steps = max(4, min(64, steps))
	return fmt.Sprintf("sphere(radius=%s, steps=%d)%s", num(r), steps,
		transform(identityFrame, c.Sub(geom.V(0, 0, r)))), true
}

var identityFrame = [3]Vec3{geom.V(1, 0, 0), geom.V(0, 1, 0), geom.V(0, 0, 1)}

func usedVerts(tris [][3]int) []int {
	seen := map[int]bool{}
	var out []int
	for _, t := range tris {
		for _, v := range t {
			if !seen[v] {
				seen[v] = true
				out = append(out, v)
			}
		}
	}
	return out
}

// slab is a planar region extruded from h0 to h1 along the prism axis.
type slab struct {
	h0, h1 float64
	outers [][]Vec2   // counter-clockwise outlines in (ex, ey) coordinates
	holes  [][][]Vec2 // holes of each outline, counter-clockwise too
}

// prism is a body made of stacked extrusions along d: every face is either
// perpendicular to d (at some level) or parallel to it.
type prism struct {
	d, ex, ey Vec3
	slabs     []slab
}

// maxLevels bounds the number of stacked slabs worth describing.
const maxLevels = 64

// detectPrism tries the main face directions of the body as extrusion axes
// and keeps the one that gives the simplest description.
func detectPrism(verts []Vec3, tris [][3]int) (*prism, bool) {
	var best *prism
	bestScore := math.MaxInt
	for _, d := range candidateAxes(verts, tris) {
		p, ok := analyzePrism(verts, tris, d)
		if !ok {
			continue
		}
		if s := p.score(); s < bestScore {
			best, bestScore = p, s
		}
	}
	return best, best != nil
}

// candidateAxes returns the normal directions with the largest total area,
// with a canonical sign and Z, X, Y first so that ties prefer them.
func candidateAxes(verts []Vec3, tris [][3]int) []Vec3 {
	type acc struct {
		n    Vec3
		area float64
	}
	byKey := map[[3]int64]*acc{}
	for _, t := range tris {
		c := verts[t[1]].Sub(verts[t[0]]).Cross(verts[t[2]].Sub(verts[t[0]]))
		area := c.Len() / 2
		if area < 1e-9 {
			continue
		}
		n := canonical(c.Normalize())
		k := [3]int64{int64(math.Round(n.X * 1e4)), int64(math.Round(n.Y * 1e4)), int64(math.Round(n.Z * 1e4))}
		if a, ok := byKey[k]; ok {
			a.area += area
		} else {
			byKey[k] = &acc{n: n, area: area}
		}
	}
	list := make([]*acc, 0, len(byKey))
	for _, a := range byKey {
		list = append(list, a)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].area > list[j].area })
	out := []Vec3{identityFrame[2], identityFrame[0], identityFrame[1]}
	for i := 0; i < len(list) && i < 8; i++ {
		n := list[i].n
		dup := false
		for _, o := range out {
			if n.Dot(o) > 1-1e-9 {
				dup = true
			}
		}
		if !dup {
			out = append(out, n)
		}
	}
	return out
}

func canonical(n Vec3) Vec3 {
	ax, ay, az := math.Abs(n.X), math.Abs(n.Y), math.Abs(n.Z)
	switch {
	case az >= ax && az >= ay:
		if n.Z < 0 {
			return n.Neg()
		}
	case ax >= ay:
		if n.X < 0 {
			return n.Neg()
		}
	default:
		if n.Y < 0 {
			return n.Neg()
		}
	}
	return n
}

func analyzePrism(verts []Vec3, tris [][3]int, d Vec3) (*prism, bool) {
	a := geom.V(1, 0, 0)
	if math.Abs(d.X) > 0.9 {
		a = geom.V(0, 1, 0)
	}
	ex := a.Sub(d.Scale(a.Dot(d))).Normalize()
	ey := d.Cross(ex)
	p := &prism{d: d, ex: ex, ey: ey}

	ids := usedVerts(tris)
	mn, mx := verts[ids[0]], verts[ids[0]]
	for _, id := range ids {
		mn, mx = mn.Min(verts[id]), mx.Max(verts[id])
	}
	tol := 1e-5*mx.Sub(mn).Len() + 1e-6
	uv := func(v Vec3) Vec2 { return Vec2{X: v.Dot(ex), Y: v.Dot(ey)} }

	// Every face must be flat (perpendicular to d) or parallel to d. The
	// heights of the flat faces are the levels where the section changes;
	// vertical faces may have extra vertices anywhere.
	type side struct {
		t      [3]int
		lo, hi float64
		along  Vec3 // d × normal: the section outline runs this way
	}
	var sides []side
	var ws []float64
	for _, t := range tris {
		w0, w1, w2 := verts[t[0]].Dot(d), verts[t[1]].Dot(d), verts[t[2]].Dot(d)
		lo, hi := math.Min(w0, math.Min(w1, w2)), math.Max(w0, math.Max(w1, w2))
		if hi-lo < tol {
			ws = append(ws, (lo+hi)/2)
			continue
		}
		pa, pb, pc := uv(verts[t[0]]), uv(verts[t[1]]), uv(verts[t[2]])
		cross := (pb.X-pa.X)*(pc.Y-pa.Y) - (pb.Y-pa.Y)*(pc.X-pa.X)
		longest := math.Max(dist2(pa, pb), math.Max(dist2(pb, pc), dist2(pc, pa)))
		if longest > tol && math.Abs(cross)/longest > tol {
			return nil, false
		}
		n := verts[t[1]].Sub(verts[t[0]]).Cross(verts[t[2]].Sub(verts[t[0]]))
		sides = append(sides, side{t: t, lo: lo, hi: hi, along: d.Cross(n)})
	}
	sort.Float64s(ws)
	var levels []float64
	for _, w := range ws {
		if len(levels) == 0 || w-levels[len(levels)-1] > tol {
			levels = append(levels, w)
			if len(levels) > maxLevels {
				return nil, false
			}
		}
	}
	if len(levels) < 2 {
		return nil, false
	}

	prevKey := ""
	for i := 0; i+1 < len(levels); i++ {
		// Cut the vertical faces inside this slab. Shared edges are cut with
		// the same formula from both faces, so the segment ends match
		// exactly. The odd fraction avoids hitting vertices.
		wm := levels[i] + (levels[i+1]-levels[i])*0.4870917
		var segs [][2]Vec2
		for _, sd := range sides {
			if sd.lo >= wm || sd.hi <= wm {
				continue
			}
			var pts []Vec3
			for k := 0; k < 3; k++ {
				ia, ib := sd.t[k], sd.t[(k+1)%3]
				if ia > ib {
					ia, ib = ib, ia
				}
				wa, wb := verts[ia].Dot(d), verts[ib].Dot(d)
				if (wa-wm)*(wb-wm) < 0 {
					pts = append(pts, verts[ia].Lerp(verts[ib], (wm-wa)/(wb-wa)))
				}
			}
			if len(pts) != 2 {
				return nil, false
			}
			if pts[1].Sub(pts[0]).Dot(sd.along) < 0 {
				pts[0], pts[1] = pts[1], pts[0]
			}
			segs = append(segs, [2]Vec2{uv(pts[0]), uv(pts[1])})
		}
		sl, ok := section(segs, tol)
		if !ok {
			return nil, false
		}
		sl.h0, sl.h1 = levels[i], levels[i+1]
		if len(sl.outers) == 0 {
			prevKey = "" // gap between two separate parts
			continue
		}
		if key := sl.key(); key == prevKey {
			// Same section as the slab below: just make that one taller.
			p.slabs[len(p.slabs)-1].h1 = sl.h1
		} else {
			p.slabs = append(p.slabs, sl)
			prevKey = key
		}
	}
	return p, len(p.slabs) > 0
}

// section chains the cut segments into outlines and sorts them into outer
// outlines (counter-clockwise) and holes (clockwise) of those outlines.
func section(segs [][2]Vec2, tol float64) (slab, bool) {
	var sl slab
	next := map[Vec2]int{}
	for i, sg := range segs {
		if _, dup := next[sg[0]]; dup {
			return sl, false // pinched outline
		}
		next[sg[0]] = i
	}
	used := make([]bool, len(segs))
	var loops [][]Vec2
	for i := range segs {
		if used[i] {
			continue
		}
		var loop []Vec2
		for j := i; !used[j]; {
			used[j] = true
			loop = append(loop, segs[j][0])
			k, ok := next[segs[j][1]]
			if !ok {
				return sl, false // open outline
			}
			j = k
		}
		if loop = simplify(loop, tol); len(loop) >= 3 {
			loops = append(loops, loop)
		}
	}
	var holes [][]Vec2
	for _, l := range loops {
		if area(l) > 0 {
			sl.outers = append(sl.outers, l)
		} else {
			holes = append(holes, reversed(l))
		}
	}
	sl.holes = make([][][]Vec2, len(sl.outers))
	for _, h := range holes {
		owner := -1
		for i, o := range sl.outers {
			if inside(h[0], o) && (owner < 0 || area(o) < area(sl.outers[owner])) {
				owner = i
			}
		}
		if owner < 0 {
			return sl, false
		}
		sl.holes[owner] = append(sl.holes[owner], h)
	}
	return sl, true
}

// key identifies the section, to merge slabs that have the same one.
func (s slab) key() string {
	var parts []string
	add := func(l []Vec2, tag string) {
		var b strings.Builder
		b.WriteString(tag)
		for _, q := range l {
			fmt.Fprintf(&b, "%.4f,%.4f;", q.X, q.Y)
		}
		parts = append(parts, b.String())
	}
	for i, o := range s.outers {
		add(o, "o")
		for _, h := range s.holes[i] {
			add(h, "h")
		}
	}
	sort.Strings(parts)
	return strings.Join(parts, "|")
}

func dist2(a, b Vec2) float64 { return math.Hypot(a.X-b.X, a.Y-b.Y) }

func area(poly []Vec2) float64 {
	var a float64
	for i := range poly {
		j := (i + 1) % len(poly)
		a += poly[i].X*poly[j].Y - poly[j].X*poly[i].Y
	}
	return a / 2
}

func reversed(poly []Vec2) []Vec2 {
	out := make([]Vec2, len(poly))
	for i := range poly {
		out[i] = poly[len(poly)-1-i]
	}
	return out
}

func inside(p Vec2, poly []Vec2) bool {
	in := false
	for i, j := 0, len(poly)-1; i < len(poly); j, i = i, i+1 {
		a, b := poly[i], poly[j]
		if (a.Y > p.Y) != (b.Y > p.Y) && p.X < (b.X-a.X)*(p.Y-a.Y)/(b.Y-a.Y)+a.X {
			in = !in
		}
	}
	return in
}

// simplify drops points that lie on the segment between their neighbours,
// which CSG and T-junction repair leave along straight edges. It always
// removes the point that deviates least first, so a stray point next to a
// real corner goes away instead of the corner.
func simplify(poly []Vec2, tol float64) []Vec2 {
	out := append([]Vec2(nil), poly...)
	dev := func(i int) float64 {
		n := len(out)
		a, b, c := out[(i+n-1)%n], out[i], out[(i+1)%n]
		l := dist2(a, c)
		if l < tol {
			return 0
		}
		return math.Abs((b.X-a.X)*(c.Y-a.Y)-(b.Y-a.Y)*(c.X-a.X)) / l
	}
	devs := make([]float64, len(out))
	for i := range out {
		devs[i] = dev(i)
	}
	for len(out) > 3 {
		best := 0
		for i := range devs {
			if devs[i] < devs[best] {
				best = i
			}
		}
		if devs[best] >= tol {
			break
		}
		out = append(out[:best], out[best+1:]...)
		devs = append(devs[:best], devs[best+1:]...)
		n := len(out)
		devs[(best+n-1)%n] = dev((best + n - 1) % n)
		devs[best%n] = dev(best % n)
	}
	return out
}

// outline describes how a single contour is rebuilt in the local frame.
type outline struct {
	kind   string // box, cylinder, polygon or extrude
	call   string // primitive call without transforms
	phi    float64
	mu, mv float64
	points int
}

func (p *prism) score() int {
	s := 0
	for _, sl := range p.slabs {
		for i, o := range sl.outers {
			s += classify(o, 1, false).points
			for _, h := range sl.holes[i] {
				s += classify(h, 1, true).points
			}
		}
	}
	// Prefer the cardinal axes on ties.
	if math.Abs(p.d.Z) < 1-1e-9 && math.Abs(p.d.X) < 1-1e-9 && math.Abs(p.d.Y) < 1-1e-9 {
		s++
	}
	return s
}

// classify recognizes rectangles and regular polygons; anything else is a
// custom extrusion.
func classify(poly []Vec2, h float64, hole bool) outline {
	if hole {
		h += 2 // holes go 1 mm past both faces
	}
	n := len(poly)
	var c Vec2
	for _, q := range poly {
		c.X += q.X / float64(n)
		c.Y += q.Y / float64(n)
	}
	if n == 4 {
		e := make([]Vec2, 4)
		for i := range e {
			e[i] = Vec2{X: poly[(i+1)%4].X - poly[i].X, Y: poly[(i+1)%4].Y - poly[i].Y}
		}
		ortho := true
		for i := range e {
			a, b := e[i], e[(i+1)%4]
			if math.Abs(a.X*b.X+a.Y*b.Y) > 1e-6*math.Hypot(a.X, a.Y)*math.Hypot(b.X, b.Y) {
				ortho = false
			}
		}
		if ortho {
			l, w := math.Hypot(e[0].X, e[0].Y), math.Hypot(e[1].X, e[1].Y)
			phi := math.Atan2(e[0].Y, e[0].X) * 180 / math.Pi
			// A rectangle looks the same every 180°; keep |phi| <= 45.
			for phi > 90 {
				phi -= 180
			}
			for phi <= -90 {
				phi += 180
			}
			if phi > 45 {
				phi -= 90
				l, w = w, l
			} else if phi <= -45 {
				phi += 90
				l, w = w, l
			}
			return outline{kind: "box", call: fmt.Sprintf("box(%s, %s, %s)", num(l), num(w), num(h)),
				phi: phi, mu: c.X, mv: c.Y, points: 1}
		}
	}
	if n >= 3 && regular(poly, c) {
		r := dist2(poly[0], c)
		kind, alpha0 := "cylinder", 0.0
		call := fmt.Sprintf("cylinder(radius=%s, height=%s, sides=%d)", num(r), num(h), n)
		if n < 12 {
			kind, alpha0 = "polygon", 90.0
			call = fmt.Sprintf("polygon(sides=%d, radius=%s, height=%s)", n, num(r), num(h))
		}
		// The primitive has a vertex at alpha0 and is re-centered on its
		// bounding box, which for odd n is not its center.
		step := 360 / float64(n)
		beta := math.Atan2(poly[0].Y-c.Y, poly[0].X-c.X) * 180 / math.Pi
		phi := math.Mod(beta-alpha0, step)
		if phi >= step/2 {
			phi -= step
		} else if phi < -step/2 {
			phi += step
		}
		var omn, omx Vec2 = Vec2{X: math.Inf(1), Y: math.Inf(1)}, Vec2{X: math.Inf(-1), Y: math.Inf(-1)}
		for k := 0; k < n; k++ {
			a := (alpha0 + step*float64(k)) * math.Pi / 180
			x, y := r*math.Cos(a), r*math.Sin(a)
			omn.X, omn.Y = math.Min(omn.X, x), math.Min(omn.Y, y)
			omx.X, omx.Y = math.Max(omx.X, x), math.Max(omx.Y, y)
		}
		ox, oy := -(omn.X+omx.X)/2, -(omn.Y+omx.Y)/2
		s, co := math.Sincos(phi * math.Pi / 180)
		return outline{kind: kind, call: call, phi: phi,
			mu: c.X - (co*ox - s*oy), mv: c.Y - (s*ox + co*oy), points: 1}
	}
	var b strings.Builder
	b.WriteString("extrude(points=[")
	for i, q := range poly {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "[%s, %s]", num(q.X), num(q.Y))
	}
	fmt.Fprintf(&b, "], height=%s)", num(h))
	return outline{kind: "extrude", call: b.String(), points: n}
}

func regular(poly []Vec2, c Vec2) bool {
	r := dist2(poly[0], c)
	e := dist2(poly[0], poly[1])
	if r < 1e-6 {
		return false
	}
	for i := range poly {
		if math.Abs(dist2(poly[i], c)-r) > 1e-4*r+1e-5 {
			return false
		}
		if math.Abs(dist2(poly[i], poly[(i+1)%len(poly)])-e) > 1e-4*e+1e-5 {
			return false
		}
	}
	return true
}

// frameFor returns the world frame of an outline rotated phi degrees inside
// the prism's local frame.
func (p *prism) frameFor(phi float64) [3]Vec3 {
	s, c := math.Sincos(phi * math.Pi / 180)
	return [3]Vec3{p.ex.Scale(c).Add(p.ey.Scale(s)), p.ex.Scale(-s).Add(p.ey.Scale(c)), p.d}
}

func (p *prism) emit(prefix string, rep *Report) string {
	frame := [3]Vec3{p.ex, p.ey, p.d}
	var stmts []string
	for _, sl := range p.slabs {
		h := sl.h1 - sl.h0
		origin := p.d.Scale(sl.h0)
		for i, o := range sl.outers {
			solid := classify(o, h, false)
			rep.Kinds[solid.kind]++
			if len(sl.holes[i]) == 0 {
				// Single shape: one combined rotation and translation.
				t := p.ex.Scale(solid.mu).Add(p.ey.Scale(solid.mv)).Add(origin)
				stmts = append(stmts, solid.call+transform(p.frameFor(solid.phi), t)+"\n")
				continue
			}
			// Shape with holes: built in the local frame, then the group
			// is placed.
			var b strings.Builder
			b.WriteString("group {\n")
			b.WriteString("  " + localShape(solid, 0) + "\n")
			for _, hl := range sl.holes[i] {
				ho := classify(hl, h, true)
				rep.Kinds["hole "+ho.kind]++
				b.WriteString("  hole " + localShape(ho, -1) + "\n")
			}
			b.WriteString("}" + transform(frame, origin) + "\n")
			stmts = append(stmts, b.String())
		}
	}
	if prefix == "" || len(stmts) == 1 {
		return prefix + strings.Join(stmts, prefix)
	}
	// A cavity made of several pieces is subtracted as a whole.
	var b strings.Builder
	b.WriteString(prefix + "group {\n")
	for _, st := range stmts {
		for _, line := range strings.SplitAfter(strings.TrimSuffix(st, "\n"), "\n") {
			b.WriteString("  " + line)
		}
		b.WriteString("\n")
	}
	b.WriteString("}\n")
	return b.String()
}

func localShape(o outline, w float64) string {
	loc := prism{ex: identityFrame[0], ey: identityFrame[1], d: identityFrame[2]}
	return o.call + transform(loc.frameFor(o.phi), geom.V(o.mu, o.mv, w))
}
