// Package render draws meshes to PNG images with a small software
// rasterizer, to preview models without a 3D viewer.
package render

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"sort"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"

	"github.com/jespino/tcad-studio/internal/geom"
)

// View is a camera direction.
type View struct {
	Name  string
	Label string
	Dir   geom.Vec3 // looking direction (from the eye to the model)
	Up    geom.Vec3 // up hint
}

var views = map[string]View{
	"iso":       {"iso", "ISO (front-right-top)", geom.V(-1, 1, -1), geom.V(0, 0, 1)},
	"iso-back":  {"iso-back", "ISO (back-left-top)", geom.V(1, -1, -1), geom.V(0, 0, 1)},
	"iso-below": {"iso-below", "ISO (front-right-below)", geom.V(-1, 1, 1), geom.V(0, 0, 1)},
	"front":     {"front", "FRONT (from -Y)", geom.V(0, 1, 0), geom.V(0, 0, 1)},
	"back":      {"back", "BACK (from +Y)", geom.V(0, -1, 0), geom.V(0, 0, 1)},
	"right":     {"right", "RIGHT (from +X)", geom.V(-1, 0, 0), geom.V(0, 0, 1)},
	"left":      {"left", "LEFT (from -X)", geom.V(1, 0, 0), geom.V(0, 0, 1)},
	"top":       {"top", "TOP (from +Z)", geom.V(0, 0, -1), geom.V(0, 1, 0)},
	"bottom":    {"bottom", "BOTTOM (from -Z)", geom.V(0, 0, 1), geom.V(0, -1, 0)},
}

// ViewNames lists the accepted view names, plus "all".
func ViewNames() []string {
	out := []string{"all"}
	for n := range views {
		out = append(out, n)
	}
	sort.Strings(out[1:])
	return out
}

var (
	bgColor   = color.RGBA{0xF7, 0xF8, 0xFA, 0xFF}
	textColor = color.RGBA{0x33, 0x33, 0x33, 0xFF}
	gridColor = color.RGBA{0xD0, 0xD4, 0xDA, 0xFF}
)

// Render draws the requested view ("all" gives a 2x2 sheet with iso, front,
// right and top) with each view cell being size x size pixels.
func Render(m *geom.Mesh, view string, size int) (image.Image, error) {
	mn, mx, ok := m.BBox()
	if !ok {
		return nil, fmt.Errorf("the model is empty")
	}
	var list []View
	if view == "all" {
		list = []View{views["iso"], views["front"], views["right"], views["top"]}
	} else {
		v, ok := views[view]
		if !ok {
			return nil, fmt.Errorf("unknown view %q (use one of: %s)", view, strings.Join(ViewNames(), ", "))
		}
		list = []View{v}
	}
	cols := 1
	if len(list) > 1 {
		cols = 2
	}
	rows := (len(list) + cols - 1) / cols
	const header = 22
	img := image.NewRGBA(image.Rect(0, 0, cols*size, rows*size+header))
	draw.Draw(img, img.Bounds(), &image.Uniform{bgColor}, image.Point{}, draw.Src)

	dim := mx.Sub(mn)
	drawText(img, 6, 15, fmt.Sprintf("X %.2f  Y %.2f  Z %.2f mm   triangles %d", dim.X, dim.Y, dim.Z, len(m.Tris)))

	for i, v := range list {
		cell := renderView(m, v, size, mn, mx)
		x0, y0 := (i%cols)*size, header+(i/cols)*size
		draw.Draw(img, image.Rect(x0, y0, x0+size, y0+size), cell, image.Point{}, draw.Src)
		if len(list) > 1 {
			for x := x0; x < x0+size; x++ {
				img.Set(x, y0, gridColor)
			}
			for y := y0; y < y0+size; y++ {
				img.Set(x0, y, gridColor)
			}
		}
	}
	return img, nil
}

func drawText(img draw.Image, x, y int, s string) {
	d := &font.Drawer{Dst: img, Src: image.NewUniform(textColor), Face: basicfont.Face7x13, Dot: fixed.P(x, y)}
	d.DrawString(s)
}

type camera struct {
	right, up, dir geom.Vec3
	center         geom.Vec3
	scale          float64
	half           float64
}

func newCamera(v View, size int, mn, mx geom.Vec3) camera {
	dir := v.Dir.Normalize()
	right := dir.Cross(v.Up).Normalize()
	up := right.Cross(dir).Normalize()
	diag := mx.Sub(mn).Len()
	if diag == 0 {
		diag = 1
	}
	return camera{right: right, up: up, dir: dir, center: mn.Add(mx).Scale(0.5),
		scale: 0.82 * float64(size) / diag, half: float64(size) / 2}
}

func (c camera) project(p geom.Vec3) (x, y, depth float64) {
	q := p.Sub(c.center)
	return c.half + q.Dot(c.right)*c.scale, c.half - q.Dot(c.up)*c.scale, q.Dot(c.dir)
}

const ss = 2 // supersampling factor

func renderView(m *geom.Mesh, v View, size int, mn, mx geom.Vec3) *image.RGBA {
	n := size * ss
	cam := newCamera(v, n, mn, mx)
	depth := make([]float64, n*n)
	tri := make([]int32, n*n)
	for i := range depth {
		depth[i] = math.Inf(1)
		tri[i] = -1
	}
	diag := mx.Sub(mn).Len()
	normals := make([]geom.Vec3, len(m.Tris))
	// Sliver triangles left by CSG have noisy normals; they must not create
	// crease lines.
	sliver := make([]bool, len(m.Tris))
	planes := make([][3]float64, len(m.Tris))
	for ti, t := range m.Tris {
		normals[ti] = t.Normal()
		e0, e1, e2 := t.V[1].Sub(t.V[0]), t.V[2].Sub(t.V[1]), t.V[0].Sub(t.V[2])
		longest := math.Max(e0.Len(), math.Max(e1.Len(), e2.Len()))
		sliver[ti] = e0.Cross(t.V[2].Sub(t.V[0])).Len()/longest < diag*1e-3
		x0, y0, z0 := cam.project(t.V[0])
		x1, y1, z1 := cam.project(t.V[1])
		x2, y2, z2 := cam.project(t.V[2])
		area := (x1-x0)*(y2-y0) - (x2-x0)*(y1-y0)
		if math.Abs(area) < 1e-12 {
			continue
		}
		// Screen space depth plane: depth = a*x + b*y + c.
		a := ((z1-z0)*(y2-y0) - (z2-z0)*(y1-y0)) / area
		b := ((x1-x0)*(z2-z0) - (x2-x0)*(z1-z0)) / area
		planes[ti] = [3]float64{a, b, z0 - a*x0 - b*y0}
		minX := max(0, int(math.Floor(math.Min(x0, math.Min(x1, x2)))))
		maxX := min(n-1, int(math.Ceil(math.Max(x0, math.Max(x1, x2)))))
		minY := max(0, int(math.Floor(math.Min(y0, math.Min(y1, y2)))))
		maxY := min(n-1, int(math.Ceil(math.Max(y0, math.Max(y1, y2)))))
		for py := minY; py <= maxY; py++ {
			fy := float64(py) + 0.5
			for px := minX; px <= maxX; px++ {
				fx := float64(px) + 0.5
				w0 := ((x1-fx)*(y2-fy) - (x2-fx)*(y1-fy)) / area
				w1 := ((x2-fx)*(y0-fy) - (x0-fx)*(y2-fy)) / area
				w2 := 1 - w0 - w1
				if w0 < -1e-9 || w1 < -1e-9 || w2 < -1e-9 {
					continue
				}
				z := w0*z0 + w1*z1 + w2*z2
				i := py*n + px
				if z < depth[i] {
					depth[i] = z
					tri[i] = int32(ti)
				}
			}
		}
	}

	light := cam.dir.Neg().Add(cam.up.Scale(0.5)).Add(cam.right.Scale(-0.3)).Normalize()
	hi := image.NewRGBA(image.Rect(0, 0, n, n))
	for py := 0; py < n; py++ {
		for px := 0; px < n; px++ {
			i := py*n + px
			t := tri[i]
			if t < 0 {
				hi.SetRGBA(px, py, bgColor)
				continue
			}
			nm := normals[t]
			back := nm.Dot(cam.dir) > 0
			if back {
				nm = nm.Neg()
			}
			// Half-Lambert shading keeps faces turned away from the light readable.
			hl := (nm.Dot(light) + 1) / 2
			k := 0.3 + 0.7*hl*hl
			if back {
				k *= 0.5 // inside faces (should not be visible on a solid)
			}
			if isEdge(px, py, n, tri, depth, planes, normals, sliver, diag) {
				k *= 0.35
			}
			c := m.Tris[t].Color
			hi.SetRGBA(px, py, color.RGBA{shade(c.R, k), shade(c.G, k), shade(c.B, k), 0xFF})
		}
	}
	out := downsample(hi, size)
	drawAxes(out, cam)
	drawText(out, 8, size-10, v.Label)
	return out
}

func shade(c uint8, k float64) uint8 {
	return uint8(math.Min(255, float64(c)*k))
}

func isEdge(x, y, n int, tri []int32, depth []float64, planes [][3]float64, normals []geom.Vec3, sliver []bool, diag float64) bool {
	i := y*n + x
	t := tri[i]
	for _, d := range [][2]int{{1, 0}, {0, 1}, {-1, 0}, {0, -1}} {
		nx, ny := x+d[0], y+d[1]
		if nx < 0 || ny < 0 || nx >= n || ny >= n {
			continue
		}
		j := ny*n + nx
		u := tri[j]
		if u < 0 {
			return true
		}
		if u == t {
			continue
		}
		// A depth jump: the neighbour is not on the extension of our plane.
		p := planes[t]
		expected := p[0]*(float64(nx)+0.5) + p[1]*(float64(ny)+0.5) + p[2]
		if math.Abs(depth[j]-expected) > diag*0.003 {
			return true
		}
		if !sliver[t] && !sliver[u] && math.Abs(normals[t].Dot(normals[u])) < 0.85 {
			return true
		}
	}
	return false
}

func downsample(src *image.RGBA, size int) *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			var r, g, b int
			for dy := 0; dy < ss; dy++ {
				for dx := 0; dx < ss; dx++ {
					c := src.RGBAAt(x*ss+dx, y*ss+dy)
					r += int(c.R)
					g += int(c.G)
					b += int(c.B)
				}
			}
			k := ss * ss
			out.SetRGBA(x, y, color.RGBA{uint8(r / k), uint8(g / k), uint8(b / k), 0xFF})
		}
	}
	return out
}

// drawAxes draws a small XYZ triad in the top-left corner of a view.
func drawAxes(img *image.RGBA, cam camera) {
	const ox, oy, l = 30.0, 34.0, 20.0
	axes := []struct {
		d     geom.Vec3
		c     color.RGBA
		label string
	}{
		{geom.V(1, 0, 0), color.RGBA{0xD3, 0x2F, 0x2F, 0xFF}, "X"},
		{geom.V(0, 1, 0), color.RGBA{0x2E, 0x9D, 0x3A, 0xFF}, "Y"},
		{geom.V(0, 0, 1), color.RGBA{0x1E, 0x60, 0xD5, 0xFF}, "Z"},
	}
	for _, a := range axes {
		dx, dy := a.d.Dot(cam.right), -a.d.Dot(cam.up)
		if math.Hypot(dx, dy) < 0.2 {
			// Axis points at the viewer: just draw a dot with its label.
			img.SetRGBA(int(ox), int(oy), a.c)
			drawLabel(img, int(ox)+4, int(oy)+4, a.label, a.c)
			continue
		}
		ex, ey := ox+dx*l, oy+dy*l
		line(img, ox, oy, ex, ey, a.c)
		drawLabel(img, int(ex+dx*6)-3, int(ey+dy*6)+4, a.label, a.c)
	}
}

func drawLabel(img draw.Image, x, y int, s string, c color.RGBA) {
	d := &font.Drawer{Dst: img, Src: image.NewUniform(c), Face: basicfont.Face7x13, Dot: fixed.P(x, y)}
	d.DrawString(s)
}

func line(img *image.RGBA, x0, y0, x1, y1 float64, c color.RGBA) {
	steps := int(math.Max(math.Abs(x1-x0), math.Abs(y1-y0))) + 1
	for i := 0; i <= steps; i++ {
		t := float64(i) / float64(steps)
		x, y := int(x0+(x1-x0)*t), int(y0+(y1-y0)*t)
		img.SetRGBA(x, y, c)
		img.SetRGBA(x+1, y, c)
	}
}
