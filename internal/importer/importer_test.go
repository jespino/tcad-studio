package importer

import (
	"math"
	"strings"
	"testing"

	"github.com/jespino/tcad-studio/internal/csg"
	"github.com/jespino/tcad-studio/internal/dsl"
	"github.com/jespino/tcad-studio/internal/geom"
	"github.com/jespino/tcad-studio/internal/stl"
)

func build(t *testing.T, src string) *geom.Mesh {
	t.Helper()
	prog, err := dsl.Parse("test.tcad", src)
	if err != nil {
		t.Fatalf("parse: %v\n%s", err, src)
	}
	res, err := dsl.Evaluate(prog, dsl.Options{})
	if err != nil {
		t.Fatalf("eval: %v\n%s", err, src)
	}
	m, _ := stl.Prepare(res.Mesh)
	return m
}

func TestRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []string // substrings expected in the output
		deny []string // substrings that must not appear
	}{
		{"box", "box(20, 30, 10).move(5, -3, 2)", []string{"box(20, 30, 10)"}, []string{"polyhedron", "extrude"}},
		{"rotated box", "box(20, 30, 10).rotate(z=30).move(x=4)", []string{"box(", "rotate(z="}, []string{"polyhedron", "extrude"}},
		{"3d rotated box", "box(20, 30, 10).rotate(x=20, y=35, z=10)", []string{"box("}, []string{"polyhedron", "extrude"}},
		{"cylinder", "cylinder(radius=5, height=12, sides=48).rotate(x=90).move(z=5)", []string{"cylinder(radius=5, height=12, sides=48)"}, []string{"polyhedron"}},
		{"hexagon", "polygon(sides=6, radius=8, height=4).rotate(z=7)", []string{"polygon(sides=6"}, []string{"polyhedron"}},
		{"triangle", "polygon(sides=3, radius=8, height=4).move(x=3)", []string{"polygon(sides=3"}, []string{"polyhedron"}},
		{"pentagon prism", "cylinder(radius=6, height=3, sides=5).rotate(z=11).move(2, 3, 0)", []string{"polygon(sides=5"}, []string{"polyhedron"}},
		{"plate with holes", "group {\n box(40, 20, 3)\n hole cylinder(r=2, h=5, sides=32).move(x=-12, z=-1)\n hole cylinder(r=2, h=5, sides=32).move(x=12, z=-1)\n hole box(6, 6, 5).move(z=-1)\n}",
			[]string{"group {", "hole cylinder(radius=2", "hole box(6, 6, 5)"}, []string{"polyhedron"}},
		{"open box", "group {\n box(40, 30, 20)\n hole box(36, 26, 20).move(z=2)\n}", []string{"box(40, 30, 2)", "hole box(36, 26"}, []string{"polyhedron"}},
		{"stacked", "cylinder(r=10, h=5, sides=40)\ncylinder(r=5, h=10, sides=40).move(z=5)", []string{"cylinder(radius=10", "cylinder(radius=5"}, []string{"polyhedron"}},
		{"sphere", "sphere(radius=7, steps=16).move(x=3, z=1)", []string{"sphere(radius=7"}, []string{"polyhedron"}},
		{"two bodies", "box(10, 10, 10)\ncylinder(r=4, h=8, sides=24).move(x=30)", []string{"box(10, 10, 10)", "cylinder(radius=4"}, []string{"polyhedron"}},
		{"cavity", "group {\n box(20, 20, 20)\n hole box(10, 10, 10).move(z=5)\n}", []string{"hole box(10, 10, 10)", "Internal cavity"}, []string{"polyhedron"}},
		{"star", "star(points=5, radius=10, inner_radius=4, height=3)", []string{"extrude("}, []string{"polyhedron"}},
		{"wedge", "wedge(20, 10, 15)", []string{"extrude("}, []string{"polyhedron"}},
		{"torus", "torus(radius=10, tube=3, sides=24, steps=12)", []string{"polyhedron"}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			orig := build(t, c.src)
			out, rep, err := Convert(orig, Options{Mode: ModeShapes, Source: "test.stl"})
			if err != nil {
				t.Fatal(err)
			}
			for _, w := range c.want {
				if !strings.Contains(out, w) {
					t.Errorf("output lacks %q:\n%s", w, out)
				}
			}
			for _, d := range c.deny {
				if strings.Contains(out, d) {
					t.Errorf("output should not contain %q:\n%s", d, out)
				}
			}
			back := build(t, out)
			v := orig.Volume()
			if math.Abs(back.Volume()-v) > 1e-4*v {
				t.Errorf("volume %v, want %v\n%s", back.Volume(), v, out)
			}
			// Same volume is not enough: the shapes must also coincide.
			if d := csg.Subtract(orig, back).Volume() + csg.Subtract(back, orig).Volume(); d > 1e-3*v {
				t.Errorf("geometry differs (symmetric difference %v mm³)\n%s", d, out)
			}
			_ = rep
		})
	}
}

func TestModes(t *testing.T) {
	orig := build(t, "torus(radius=10, tube=3, sides=12, steps=8)")
	for _, mode := range []Mode{ModeEmbed, ModeRef} {
		out, _, err := Convert(orig, Options{Mode: mode, Source: "x.stl", RefPath: "x.stl"})
		if err != nil {
			t.Fatal(err)
		}
		if mode == ModeRef && !strings.Contains(out, `stl(file="x.stl", center=false)`) {
			t.Errorf("ref output: %s", out)
		}
		if mode == ModeEmbed {
			back := build(t, out)
			if math.Abs(back.Volume()-orig.Volume()) > 1e-6*orig.Volume() {
				t.Errorf("embed volume %v, want %v", back.Volume(), orig.Volume())
			}
		}
	}
}

func TestCenter(t *testing.T) {
	orig := build(t, "box(10, 10, 10).move(50, 60, 70)")
	out, _, err := Convert(orig, Options{Mode: ModeShapes, Center: true})
	if err != nil {
		t.Fatal(err)
	}
	mn, mx, _ := build(t, out).BBox()
	if math.Abs(mn.Z) > 1e-6 || math.Abs(mn.X+mx.X) > 1e-6 || math.Abs(mn.Y+mx.Y) > 1e-6 {
		t.Errorf("not centered: %v %v\n%s", mn, mx, out)
	}
}
