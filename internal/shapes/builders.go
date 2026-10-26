// Package shapes generates the triangle meshes of the basic primitives.
//
// Every primitive follows the same convention: it is centered on the X
// and Y axes and rests on the workplane (its lowest point is at z = 0).
// X is length, Y is width and Z is height.
package shapes

import (
	"math"

	"github.com/jespino/tcad-studio/internal/geom"
)

type (
	Vec2 = geom.Vec2
	Vec3 = geom.Vec3
)

// revolve spins a closed profile, given in the (r, z) half plane, around the
// Z axis. Points with r == 0 lie on the axis and produce triangle fans.
func revolve(profile []Vec2, sides int) *geom.Mesh {
	m := &geom.Mesh{}
	n := len(profile)
	cs := make([]float64, sides+1)
	sn := make([]float64, sides+1)
	for k := 0; k <= sides; k++ {
		a := 2 * math.Pi * float64(k%sides) / float64(sides)
		sn[k], cs[k] = math.Sincos(a)
	}
	pt := func(p Vec2, k int) Vec3 {
		if math.Abs(p.X) < 1e-12 {
			return Vec3{0, 0, p.Y}
		}
		return Vec3{p.X * cs[k], p.X * sn[k], p.Y}
	}
	for i := 0; i < n; i++ {
		p, q := profile[i], profile[(i+1)%n]
		if math.Abs(p.X) < 1e-12 && math.Abs(q.X) < 1e-12 {
			continue
		}
		for k := 0; k < sides; k++ {
			m.AddQuad(pt(p, k), pt(p, k+1), pt(q, k+1), pt(q, k))
		}
	}
	m.FixOrientation()
	return m
}

func signedArea(poly []Vec2) float64 {
	var a float64
	for i := range poly {
		j := (i + 1) % len(poly)
		a += poly[i].X*poly[j].Y - poly[j].X*poly[i].Y
	}
	return a / 2
}

// cleanPolygon removes duplicated and collinear consecutive points and makes
// the polygon counter-clockwise.
func cleanPolygon(poly []Vec2) []Vec2 {
	var out []Vec2
	for _, p := range poly {
		if len(out) > 0 {
			l := out[len(out)-1]
			if math.Abs(l.X-p.X) < 1e-9 && math.Abs(l.Y-p.Y) < 1e-9 {
				continue
			}
		}
		out = append(out, p)
	}
	for len(out) > 1 {
		f, l := out[0], out[len(out)-1]
		if math.Abs(l.X-f.X) < 1e-9 && math.Abs(l.Y-f.Y) < 1e-9 {
			out = out[:len(out)-1]
		} else {
			break
		}
	}
	changed := true
	for changed && len(out) >= 3 {
		changed = false
		for i := 0; i < len(out); i++ {
			a := out[(i+len(out)-1)%len(out)]
			b := out[i]
			c := out[(i+1)%len(out)]
			cross := (b.X-a.X)*(c.Y-a.Y) - (b.Y-a.Y)*(c.X-a.X)
			if math.Abs(cross) < 1e-12 {
				out = append(out[:i], out[i+1:]...)
				changed = true
				break
			}
		}
	}
	if signedArea(out) < 0 {
		for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
			out[i], out[j] = out[j], out[i]
		}
	}
	return out
}

func pointInTri(p, a, b, c Vec2) bool {
	d1 := (p.X-b.X)*(a.Y-b.Y) - (a.X-b.X)*(p.Y-b.Y)
	d2 := (p.X-c.X)*(b.Y-c.Y) - (b.X-c.X)*(p.Y-c.Y)
	d3 := (p.X-a.X)*(c.Y-a.Y) - (c.X-a.X)*(p.Y-a.Y)
	neg := d1 < 0 || d2 < 0 || d3 < 0
	pos := d1 > 0 || d2 > 0 || d3 > 0
	return !(neg && pos)
}

// triangulate performs ear clipping on a simple counter-clockwise polygon and
// returns index triples.
func triangulate(poly []Vec2) [][3]int {
	n := len(poly)
	idx := make([]int, n)
	for i := range idx {
		idx[i] = i
	}
	var tris [][3]int
	guard := 0
	for len(idx) > 3 && guard < n*n {
		guard++
		found := false
		for i := 0; i < len(idx); i++ {
			ia, ib, ic := idx[(i+len(idx)-1)%len(idx)], idx[i], idx[(i+1)%len(idx)]
			a, b, c := poly[ia], poly[ib], poly[ic]
			cross := (b.X-a.X)*(c.Y-a.Y) - (b.Y-a.Y)*(c.X-a.X)
			if cross <= 1e-14 {
				continue
			}
			ear := true
			for _, j := range idx {
				if j == ia || j == ib || j == ic {
					continue
				}
				if pointInTri(poly[j], a, b, c) {
					ear = false
					break
				}
			}
			if !ear {
				continue
			}
			tris = append(tris, [3]int{ia, ib, ic})
			idx = append(idx[:i], idx[i+1:]...)
			found = true
			break
		}
		if !found {
			break
		}
	}
	// Whatever is left (normally a single triangle) is closed with a fan.
	for i := 1; i+1 < len(idx); i++ {
		tris = append(tris, [3]int{idx[0], idx[i], idx[i+1]})
	}
	return tris
}

// prism extrudes a 2D polygon (in the XY plane) from z0 to z1.
func prism(poly []Vec2, z0, z1 float64) *geom.Mesh {
	poly = cleanPolygon(poly)
	m := &geom.Mesh{}
	if len(poly) < 3 {
		return m
	}
	for _, t := range triangulate(poly) {
		a, b, c := poly[t[0]], poly[t[1]], poly[t[2]]
		m.AddTri(Vec3{a.X, a.Y, z1}, Vec3{b.X, b.Y, z1}, Vec3{c.X, c.Y, z1})
		m.AddTri(Vec3{a.X, a.Y, z0}, Vec3{c.X, c.Y, z0}, Vec3{b.X, b.Y, z0})
	}
	for i := range poly {
		a, b := poly[i], poly[(i+1)%len(poly)]
		m.AddQuad(Vec3{a.X, a.Y, z0}, Vec3{b.X, b.Y, z0}, Vec3{b.X, b.Y, z1}, Vec3{a.X, a.Y, z1})
	}
	m.FixOrientation()
	return m
}

// arc appends points of a circular arc around c from angle a0 to a1 (deg).
func arc(out []Vec2, c Vec2, r, a0, a1 float64, segs int) []Vec2 {
	if segs < 1 {
		segs = 1
	}
	for i := 0; i <= segs; i++ {
		a := (a0 + (a1-a0)*float64(i)/float64(segs)) * math.Pi / 180
		out = append(out, Vec2{c.X + r*math.Cos(a), c.Y + r*math.Sin(a)})
	}
	return out
}

// roundedRect returns a counter-clockwise rectangle profile whose corners
// (bottom-left, bottom-right, top-right, top-left) are rounded with the
// given radii.
func roundedRect(x0, y0, x1, y1 float64, r [4]float64, segs int) []Vec2 {
	var out []Vec2
	// sx, sy point from the corner towards the arc center.
	corner := func(cx, cy, sx, sy, rad, a0 float64) {
		if rad <= 0 {
			out = append(out, Vec2{cx, cy})
			return
		}
		out = arc(out, Vec2{cx + sx*rad, cy + sy*rad}, rad, a0, a0+90, segs)
	}
	corner(x0, y0, 1, 1, r[0], 180)
	corner(x1, y0, -1, 1, r[1], 270)
	corner(x1, y1, -1, -1, r[2], 0)
	corner(x0, y1, 1, -1, r[3], 90)
	return out
}

func regularPolygon(n int, rx, ry, phase float64) []Vec2 {
	out := make([]Vec2, n)
	for i := 0; i < n; i++ {
		a := phase + 2*math.Pi*float64(i)/float64(n)
		out[i] = Vec2{rx * math.Cos(a), ry * math.Sin(a)}
	}
	return out
}

// restOnWorkplane moves the mesh so that it is centered in XY with its
// lowest point at z = 0.
func restOnWorkplane(m *geom.Mesh) {
	mn, mx, ok := m.BBox()
	if !ok {
		return
	}
	m.Transform(geom.Translate(Vec3{-(mn.X + mx.X) / 2, -(mn.Y + mx.Y) / 2, -mn.Z}))
}
