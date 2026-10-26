package dsl

import (
	"math"
	"strings"
	"testing"

	"github.com/jespino/tcad-studio/internal/stl"
)

func eval(t *testing.T, src string, defs map[string]string, ignoreUnknown ...bool) *Result {
	t.Helper()
	prog, err := Parse("test.tcad", src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	res, err := Evaluate(prog, Options{Defines: defs, IgnoreUnknownDefines: len(ignoreUnknown) > 0})
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	return res
}

func TestVolumes(t *testing.T) {
	cases := []struct {
		name string
		src  string
		vol  float64
		tol  float64
	}{
		{"box", "box(10, 20, 30)", 6000, 1e-6},
		{"size", "box(size=[10, 20, 30])", 6000, 1e-6},
		{"union overlap", "box(20,20,10)\nbox(20,20,10).move(x=10)", 6000, 1e-6},
		{"union disjoint", "box(10,10,10)\nbox(10,10,10).move(x=30)", 2000, 1e-6},
		{"hole", "group {\n box(20,20,20)\n hole box(10,10,30).move(z=-5)\n}", 8000 - 2000, 1e-6},
		{"hole flush", "group {\n box(20,20,20)\n hole box(10,10,20)\n}", 6000, 1e-6},
		{"hole order independent", "group {\n hole box(10,10,30).move(z=-5)\n box(20,20,20)\n}", 6000, 1e-6},
		{"intersect", "intersect {\n box(20,20,20)\n box(20,20,20).move(x=10, y=10)\n}", 2000, 1e-6},
		{"top level hole", "box(20,20,20)\nhole box(20,20,10).move(z=10)", 4000, 1e-6},
		{"nested hole group", "group {\n box(30,30,10)\n hole group {\n  box(10,10,20)\n  box(10,10,20).move(x=10)\n }.move(z=-5)\n}", 9000 - 2000, 1e-6},
		{"module", "module cube(s) { box(s, s, s) }\ncube(3)", 27, 1e-9},
		{"loop", "for i in range(3) { box(1,1,1).move(x=i*2) }", 3, 1e-9},
		{"scale", "box(1,2,3).scale(2)", 48, 1e-9},
		{"mirror", "wedge(10,10,10).mirror(\"x\")", 500, 1e-9},
		{"cylinder", "cylinder(radius=10, height=10, sides=128)", math.Pi * 1000, 5},
		{"sphere", "sphere(radius=10, steps=48)", 4.0 / 3 * math.Pi * 1000, 30},
		{"tube", "tube(radius=10, wall=2, height=10, sides=128)", math.Pi * (100 - 64) * 10, 5},
		{"cone", "cone(base_radius=10, top_radius=0, height=30, sides=128)", math.Pi * 1000, 5},
		{"pyramid", "pyramid(10, 10, 30)", 1000, 1e-6},
		{"roof", "roof(10, 10, 10)", 500, 1e-6},
		{"rounded box", "box(20, 20, 20, radius=5, steps=24)", 1000 + 6*100*5 + 3*math.Pi*10*25 + 4.0/3*math.Pi*125, 10},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := eval(t, c.src, nil)
			if v := res.Mesh.Volume(); math.Abs(v-c.vol) > c.tol {
				t.Errorf("volume = %v, want %v", v, c.vol)
			}
			if _, open := stl.Prepare(res.Mesh); open != 0 {
				t.Errorf("mesh has %d open edges", open)
			}
		})
	}
}

func TestWorkplaneConvention(t *testing.T) {
	for _, src := range []string{"box()", "sphere()", "cylinder()", "torus()", "wedge()", "star()", "text(\"Hi\")", "pyramid(sides=3)"} {
		res := eval(t, src, nil)
		mn, mx, _ := res.Mesh.BBox()
		if math.Abs(mn.Z) > 1e-9 || math.Abs(mn.X+mx.X) > 1e-9 || math.Abs(mn.Y+mx.Y) > 1e-9 {
			t.Errorf("%s: not centered on the workplane: %v %v", src, mn, mx)
		}
	}
}

func TestRotate(t *testing.T) {
	res := eval(t, "box(10, 2, 2).rotate(z=90)", nil)
	mn, mx, _ := res.Mesh.BBox()
	if d := mx.Sub(mn); math.Abs(d.X-2) > 1e-9 || math.Abs(d.Y-10) > 1e-9 {
		t.Errorf("unexpected size after rotation: %v", d)
	}
}

func TestDropCenter(t *testing.T) {
	res := eval(t, "box(10, 10, 10).move(5, 7, 3).rotate(x=45).drop().center()", nil)
	mn, mx, _ := res.Mesh.BBox()
	if math.Abs(mn.Z) > 1e-9 || math.Abs(mn.X+mx.X) > 1e-9 || math.Abs(mn.Y+mx.Y) > 1e-9 {
		t.Errorf("drop/center failed: %v %v", mn, mx)
	}
}

func TestDefines(t *testing.T) {
	res := eval(t, "param s = 1\nbox(s, s, s)", map[string]string{"s": "2"})
	if v := res.Mesh.Volume(); math.Abs(v-8) > 1e-9 {
		t.Errorf("volume = %v", v)
	}
}

func TestExpressions(t *testing.T) {
	src := `
func sq(x) = x * x
let l = [1, 2, 3] + [4]
let s = "a" + 1
let ok = len(l) == 4 && s == "a1" && sq(3) == 9 && max(l) == 4 && (2 > 1 ? 10 : 20) == 10 && l[-1] == 4
if ok { box(1, 1, 1) } else { box(2, 2, 2) }
`
	res := eval(t, src, nil)
	if v := res.Mesh.Volume(); math.Abs(v-1) > 1e-9 {
		t.Errorf("expressions evaluated wrong, volume %v", v)
	}
}

func TestErrors(t *testing.T) {
	cases := map[string]string{
		"box(lenght=3)":              `did you mean "length"`,
		"cylindr()":                  `did you mean "cylinder"`,
		"box().mvoe(x=1)":            `did you mean "move"`,
		"box(undefinedvar)":          "undefined variable",
		"group { box()":              "missing closing",
		"module m(a) { box() }\nm()": "missing required parameter",
		"torus(radius=2, tube=3)":    "must be smaller",
	}
	for src, want := range cases {
		prog, err := Parse("t", src)
		if err == nil {
			_, err = Evaluate(prog, Options{})
		}
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: got error %v, want %q", src, err, want)
		}
	}
}

func TestParamInfo(t *testing.T) {
	src := `param ancho = 60      // [20:200] Ancho exterior
param pared = 2       // [1:0.5:5]
param fuente = "sans" in ["sans", "mono"] // Tipo de letra
param tapa = true     # Imprimir la tapa
param color = "#ff0000"
param pts = [1, 2]
box(ancho, 10, pared)
`
	res := eval(t, src, map[string]string{"pared": "3", "nombre_viejo": "1"}, true)
	got := map[string]ParamValue{}
	for _, p := range res.Params {
		got[p.Name] = p
	}
	a := got["ancho"]
	if a.Type != "number" || *a.Min != 20 || *a.Max != 200 || a.Step != nil || a.Doc != "Ancho exterior" {
		t.Errorf("ancho: %+v", a)
	}
	p := got["pared"]
	if !p.Overridden || p.Value != 3.0 || p.Default != 2.0 || *p.Step != 0.5 || *p.Max != 5 {
		t.Errorf("pared: %+v", p)
	}
	f := got["fuente"]
	if len(f.Options) != 2 || f.Options[1] != "mono" || f.Doc != "Tipo de letra" || f.Expr != `"sans"` {
		t.Errorf("fuente: %+v", f)
	}
	if tp := got["tapa"]; tp.Type != "bool" || tp.Doc != "Imprimir la tapa" {
		t.Errorf("tapa: %+v", tp)
	}
	if c := got["color"]; c.Doc != "" {
		t.Errorf("a # inside a string is not a comment: %+v", c)
	}
	if l := got["pts"]; l.Type != "list" || l.Expr != "[1, 2]" {
		t.Errorf("pts: %+v", l)
	}
}

func TestFormatLiteral(t *testing.T) {
	for _, v := range []any{3.5, "a \"b\" \\ c\nd", true, []any{1.0, "x", []any{2.0}}} {
		if got := EvalDefine(FormatLiteral(v)); !equal(got, v) {
			t.Errorf("round trip of %#v gave %#v (literal %s)", v, got, FormatLiteral(v))
		}
	}
	if EvalDefine("Ana") != "Ana" || EvalDefine("2*3") != 6.0 {
		t.Error("EvalDefine fallback")
	}
}

func TestParamOptions(t *testing.T) {
	src := `param modo = "montado" in ["montado", "impresion"] // Vista o piezas
param dientes = 12 in [8, 12, 16]
if modo == "impresion" { box(dientes, 1, 1) } else { box(1, 1, 1) }
`
	res := eval(t, src, map[string]string{"modo": "impresion", "dientes": "16"})
	if v := res.Mesh.Volume(); math.Abs(v-16) > 1e-9 {
		t.Errorf("volume %v", v)
	}
	if p := res.Params[0]; len(p.Options) != 2 || p.Doc != "Vista o piezas" {
		t.Errorf("modo: %+v", p)
	}

	errs := []struct {
		src  string
		defs map[string]string
		want string
	}{
		{src, map[string]string{"modo": "impresión"}, `must be one of ["montado", "impresion"]`},
		{src, map[string]string{"dientes": "10"}, "must be one of [8, 12, 16]"},
		{`param modo = "otro" in ["a", "b"]` + "\nbox()", nil, "not one of the allowed values"},
		{`param modo = "a" in []` + "\nbox()", nil, "non-empty list"},
		{`let modo = "a" in ["a"]` + "\nbox()", nil, "only valid for param"},
		{`param modo = "montado" in ["montado", "impresion"]` + "\nif modo == \"impresión\" { box() }", nil,
			`modo is never "impresión"`},
		{`param modo = "montado" in ["montado", "impresion"]` + "\nif \"montaje\" != modo { box() }", nil,
			`did you mean "montado"`},
	}
	for _, c := range errs {
		prog, err := Parse("t", c.src)
		if err == nil {
			_, err = Evaluate(prog, Options{Defines: c.defs})
		}
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q %v: got %v, want %q", c.src, c.defs, err, c.want)
		}
	}

	// A local variable with the same name is not the param.
	ok := `param modo = "a" in ["a", "b"]
module m(modo) { if modo == "zzz" { box() } else { box(2, 2, 2) } }
m("zzz")
`
	if v := eval(t, ok, nil).Mesh.Volume(); math.Abs(v-20*20*20) > 1e-6 {
		t.Errorf("shadowed param: volume %v", v)
	}
}
