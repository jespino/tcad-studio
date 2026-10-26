// Package geom contains the basic geometric types used across the project:
// vectors, affine transforms, triangles and triangle meshes.
package geom

import (
	"math"
)

// Vec3 is a point or direction in 3D space.
type Vec3 struct{ X, Y, Z float64 }

func V(x, y, z float64) Vec3 { return Vec3{x, y, z} }

func (a Vec3) Add(b Vec3) Vec3      { return Vec3{a.X + b.X, a.Y + b.Y, a.Z + b.Z} }
func (a Vec3) Sub(b Vec3) Vec3      { return Vec3{a.X - b.X, a.Y - b.Y, a.Z - b.Z} }
func (a Vec3) Scale(s float64) Vec3 { return Vec3{a.X * s, a.Y * s, a.Z * s} }
func (a Vec3) Neg() Vec3            { return Vec3{-a.X, -a.Y, -a.Z} }
func (a Vec3) Dot(b Vec3) float64   { return a.X*b.X + a.Y*b.Y + a.Z*b.Z }
func (a Vec3) Len() float64         { return math.Sqrt(a.Dot(a)) }
func (a Vec3) Mul(b Vec3) Vec3      { return Vec3{a.X * b.X, a.Y * b.Y, a.Z * b.Z} }

func (a Vec3) Cross(b Vec3) Vec3 {
	return Vec3{a.Y*b.Z - a.Z*b.Y, a.Z*b.X - a.X*b.Z, a.X*b.Y - a.Y*b.X}
}

func (a Vec3) Normalize() Vec3 {
	l := a.Len()
	if l == 0 {
		return a
	}
	return a.Scale(1 / l)
}

func (a Vec3) Lerp(b Vec3, t float64) Vec3 { return a.Add(b.Sub(a).Scale(t)) }

func (a Vec3) Min(b Vec3) Vec3 {
	return Vec3{math.Min(a.X, b.X), math.Min(a.Y, b.Y), math.Min(a.Z, b.Z)}
}

func (a Vec3) Max(b Vec3) Vec3 {
	return Vec3{math.Max(a.X, b.X), math.Max(a.Y, b.Y), math.Max(a.Z, b.Z)}
}

// Vec2 is a point in a 2D profile.
type Vec2 struct{ X, Y float64 }

// Mat4 is a row-major affine transform. The last row is always 0 0 0 1.
type Mat4 [16]float64

func Identity() Mat4 {
	return Mat4{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1}
}

func Translate(v Vec3) Mat4 {
	return Mat4{1, 0, 0, v.X, 0, 1, 0, v.Y, 0, 0, 1, v.Z, 0, 0, 0, 1}
}

func ScaleM(v Vec3) Mat4 {
	return Mat4{v.X, 0, 0, 0, 0, v.Y, 0, 0, 0, 0, v.Z, 0, 0, 0, 0, 1}
}

func rad(deg float64) float64 { return deg * math.Pi / 180 }

func RotateX(deg float64) Mat4 {
	s, c := math.Sincos(rad(deg))
	return Mat4{1, 0, 0, 0, 0, c, -s, 0, 0, s, c, 0, 0, 0, 0, 1}
}

func RotateY(deg float64) Mat4 {
	s, c := math.Sincos(rad(deg))
	return Mat4{c, 0, s, 0, 0, 1, 0, 0, -s, 0, c, 0, 0, 0, 0, 1}
}

func RotateZ(deg float64) Mat4 {
	s, c := math.Sincos(rad(deg))
	return Mat4{c, -s, 0, 0, s, c, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1}
}

// Mul returns a*b (b is applied first, then a).
func (a Mat4) Mul(b Mat4) Mat4 {
	var r Mat4
	for i := 0; i < 4; i++ {
		for j := 0; j < 4; j++ {
			var s float64
			for k := 0; k < 4; k++ {
				s += a[i*4+k] * b[k*4+j]
			}
			r[i*4+j] = s
		}
	}
	return r
}

func (m Mat4) Apply(v Vec3) Vec3 {
	return Vec3{
		m[0]*v.X + m[1]*v.Y + m[2]*v.Z + m[3],
		m[4]*v.X + m[5]*v.Y + m[6]*v.Z + m[7],
		m[8]*v.X + m[9]*v.Y + m[10]*v.Z + m[11],
	}
}

// Det3 returns the determinant of the linear part; negative means the
// transform mirrors geometry and triangle winding has to be flipped.
func (m Mat4) Det3() float64 {
	return m[0]*(m[5]*m[10]-m[6]*m[9]) - m[1]*(m[4]*m[10]-m[6]*m[8]) + m[2]*(m[4]*m[9]-m[5]*m[8])
}

// Color is an RGB color used only for previews (STL has no color).
type Color struct{ R, G, B uint8 }

// Triangle is a single facet. Vertices are counter-clockwise when seen from
// outside the solid.
type Triangle struct {
	V     [3]Vec3
	Color Color
}

func (t Triangle) Normal() Vec3 {
	return t.V[1].Sub(t.V[0]).Cross(t.V[2].Sub(t.V[0])).Normalize()
}

// Mesh is a closed triangle soup.
type Mesh struct {
	Tris []Triangle
}

func (m *Mesh) Clone() *Mesh {
	return &Mesh{Tris: append([]Triangle(nil), m.Tris...)}
}

// Transform applies mat to every vertex in place, fixing winding for mirrors.
func (m *Mesh) Transform(mat Mat4) {
	flip := mat.Det3() < 0
	for i := range m.Tris {
		t := &m.Tris[i]
		for k := 0; k < 3; k++ {
			t.V[k] = mat.Apply(t.V[k])
		}
		if flip {
			t.V[1], t.V[2] = t.V[2], t.V[1]
		}
	}
}

func (m *Mesh) Flip() {
	for i := range m.Tris {
		m.Tris[i].V[1], m.Tris[i].V[2] = m.Tris[i].V[2], m.Tris[i].V[1]
	}
}

func (m *Mesh) SetColor(c Color) {
	for i := range m.Tris {
		m.Tris[i].Color = c
	}
}

// BBox returns the axis aligned bounding box. ok is false for empty meshes.
func (m *Mesh) BBox() (min, max Vec3, ok bool) {
	if len(m.Tris) == 0 {
		return
	}
	min = m.Tris[0].V[0]
	max = min
	for _, t := range m.Tris {
		for _, v := range t.V {
			min = min.Min(v)
			max = max.Max(v)
		}
	}
	return min, max, true
}

// Volume returns the signed volume (positive for outward facing meshes).
func (m *Mesh) Volume() float64 {
	var v float64
	for _, t := range m.Tris {
		v += t.V[0].Dot(t.V[1].Cross(t.V[2]))
	}
	return v / 6
}

// FixOrientation flips the mesh if it is inside-out.
func (m *Mesh) FixOrientation() {
	if m.Volume() < 0 {
		m.Flip()
	}
}

// AddTri appends a triangle, dropping degenerate ones.
func (m *Mesh) AddTri(a, b, c Vec3) {
	if b.Sub(a).Cross(c.Sub(a)).Len() < 1e-12 {
		return
	}
	m.Tris = append(m.Tris, Triangle{V: [3]Vec3{a, b, c}})
}

// AddQuad appends a planar quad a-b-c-d as two triangles.
func (m *Mesh) AddQuad(a, b, c, d Vec3) {
	m.AddTri(a, b, c)
	m.AddTri(a, c, d)
}

func (m *Mesh) Append(o *Mesh) {
	m.Tris = append(m.Tris, o.Tris...)
}
