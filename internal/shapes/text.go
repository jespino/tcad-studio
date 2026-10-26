package shapes

import (
	"fmt"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gobolditalic"
	"golang.org/x/image/font/gofont/goitalic"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/gomonobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"

	"github.com/jespino/tcad-studio/internal/csg"
	"github.com/jespino/tcad-studio/internal/geom"
)

var fontData = map[string][]byte{
	"sans":             goregular.TTF,
	"sans-bold":        gobold.TTF,
	"sans-italic":      goitalic.TTF,
	"sans-bold-italic": gobolditalic.TTF,
	"mono":             gomono.TTF,
	"mono-bold":        gomonobold.TTF,
}

var parsedFonts = map[string]*sfnt.Font{}

func loadFont(name string) (*sfnt.Font, error) {
	if f, ok := parsedFonts[name]; ok {
		return f, nil
	}
	data, ok := fontData[name]
	if !ok {
		names := make([]string, 0, len(fontData))
		for n := range fontData {
			names = append(names, n)
		}
		return nil, fmt.Errorf("unknown font %q (available: %s)", name, strings.Join(names, ", "))
	}
	f, err := sfnt.Parse(data)
	if err != nil {
		return nil, err
	}
	parsedFonts[name] = f
	return f, nil
}

// Glyphs are loaded at a large size and scaled down to keep precision.
const textPPEM = 1000

func textMesh(text, fontName string, size, height, spacing float64, align string) (*geom.Mesh, error) {
	f, err := loadFont(fontName)
	if err != nil {
		return nil, err
	}
	if align != "left" && align != "center" && align != "right" {
		return nil, fmt.Errorf("align must be left, center or right")
	}
	var buf sfnt.Buffer
	ppem := fixed.I(textPPEM)

	hIdx, _ := f.GlyphIndex(&buf, 'H')
	hb, _, err := f.GlyphBounds(&buf, hIdx, ppem, font.HintingNone)
	if err != nil {
		return nil, err
	}
	capHeight := -float64(hb.Min.Y) / 64
	scale := size / capHeight
	lineHeight := size * 1.6

	out := &geom.Mesh{}
	lines := strings.Split(text, "\n")
	for li, line := range lines {
		lineMesh := &geom.Mesh{}
		x := 0.0
		var prev sfnt.GlyphIndex
		for i, r := range line {
			idx, err := f.GlyphIndex(&buf, r)
			if err != nil {
				return nil, err
			}
			if i > 0 {
				if k, err := f.Kern(&buf, prev, idx, ppem, font.HintingNone); err == nil {
					x += float64(k) / 64 * scale
				}
			}
			segs, err := f.LoadGlyph(&buf, idx, ppem, nil)
			if err != nil {
				return nil, err
			}
			contours := flattenSegments(segs, scale, x)
			lineMesh.Append(glyphMesh(contours, height))
			adv, err := f.GlyphAdvance(&buf, idx, ppem, font.HintingNone)
			if err != nil {
				return nil, err
			}
			x += float64(adv)/64*scale + spacing
			prev = idx
		}
		// Align the line horizontally around x = 0 and stack lines downwards.
		dx := 0.0
		switch align {
		case "center":
			dx = -x / 2
		case "right":
			dx = -x
		}
		lineMesh.Transform(geom.Translate(Vec3{dx, -float64(li) * lineHeight, 0}))
		out.Append(lineMesh)
	}
	if len(out.Tris) == 0 {
		return nil, fmt.Errorf("text produces no geometry")
	}
	return out, nil
}

func flattenSegments(segs sfnt.Segments, scale, dx float64) [][]Vec2 {
	pt := func(p fixed.Point26_6) Vec2 {
		return Vec2{float64(p.X)/64*scale + dx, -float64(p.Y) / 64 * scale}
	}
	var contours [][]Vec2
	var cur []Vec2
	for _, s := range segs {
		switch s.Op {
		case sfnt.SegmentOpMoveTo:
			if len(cur) >= 3 {
				contours = append(contours, cur)
			}
			cur = []Vec2{pt(s.Args[0])}
		case sfnt.SegmentOpLineTo:
			cur = append(cur, pt(s.Args[0]))
		case sfnt.SegmentOpQuadTo:
			p0, p1, p2 := cur[len(cur)-1], pt(s.Args[0]), pt(s.Args[1])
			const n = 6
			for i := 1; i <= n; i++ {
				t := float64(i) / n
				u := 1 - t
				cur = append(cur, Vec2{
					u*u*p0.X + 2*u*t*p1.X + t*t*p2.X,
					u*u*p0.Y + 2*u*t*p1.Y + t*t*p2.Y,
				})
			}
		case sfnt.SegmentOpCubeTo:
			p0, p1, p2, p3 := cur[len(cur)-1], pt(s.Args[0]), pt(s.Args[1]), pt(s.Args[2])
			const n = 8
			for i := 1; i <= n; i++ {
				t := float64(i) / n
				u := 1 - t
				cur = append(cur, Vec2{
					u*u*u*p0.X + 3*u*u*t*p1.X + 3*u*t*t*p2.X + t*t*t*p3.X,
					u*u*u*p0.Y + 3*u*u*t*p1.Y + 3*u*t*t*p2.Y + t*t*t*p3.Y,
				})
			}
		}
	}
	if len(cur) >= 3 {
		contours = append(contours, cur)
	}
	for i := range contours {
		contours[i] = cleanPolygon(contours[i])
	}
	return contours
}

func pointInPolygon(p Vec2, poly []Vec2) bool {
	in := false
	for i, j := 0, len(poly)-1; i < len(poly); j, i = i, i+1 {
		a, b := poly[i], poly[j]
		if (a.Y > p.Y) != (b.Y > p.Y) && p.X < (b.X-a.X)*(p.Y-a.Y)/(b.Y-a.Y)+a.X {
			in = !in
		}
	}
	return in
}

// glyphMesh extrudes the contours of a glyph. Contours nested an odd number
// of times are holes (the inside of an "o").
func glyphMesh(contours [][]Vec2, height float64) *geom.Mesh {
	var solids, holes []*geom.Mesh
	for i, c := range contours {
		if len(c) < 3 {
			continue
		}
		tris := triangulate(c)
		if len(tris) == 0 {
			continue
		}
		t := tris[0]
		probe := Vec2{(c[t[0]].X + c[t[1]].X + c[t[2]].X) / 3, (c[t[0]].Y + c[t[1]].Y + c[t[2]].Y) / 3}
		depth := 0
		for j, o := range contours {
			if i != j && len(o) >= 3 && pointInPolygon(probe, o) {
				depth++
			}
		}
		if depth%2 == 0 {
			solids = append(solids, prism(c, 0, height))
		} else {
			holes = append(holes, prism(c, -1, height+1))
		}
	}
	if len(solids) == 0 {
		return &geom.Mesh{}
	}
	m := csg.UnionAll(solids)
	if len(holes) > 0 {
		m = csg.SubtractAll(m, holes)
	}
	return m
}
