package dsl

import (
	"fmt"
	"io"
	"math"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/jespino/tcad-studio/internal/csg"
	"github.com/jespino/tcad-studio/internal/geom"
	"github.com/jespino/tcad-studio/internal/shapes"
)

// Options configures an evaluation.
type Options struct {
	// Defines overrides "param" values. Values are parsed as TCAD
	// expressions; if that fails they are used as plain strings.
	Defines map[string]string
	// Echo receives the output of echo() statements. Nil discards it.
	Echo io.Writer
	// BaseDir is where relative file names (e.g. stl("x.stl")) are looked
	// up. Empty means the current directory.
	BaseDir string
	// IgnoreUnknownDefines skips defines that match no param instead of
	// failing (useful while the file is being edited).
	IgnoreUnknownDefines bool
}

// ParamValue describes a top level "param" and its final value.
type ParamValue struct {
	Name string `json:"name"`
	// Type is number, string, bool or list.
	Type string `json:"type"`
	// Value and Default are float64, string, bool or []any.
	Value   any `json:"value"`
	Default any `json:"default"`
	// Expr is the value written as a TCAD literal, valid for -D.
	Expr       string `json:"expr"`
	Overridden bool   `json:"overridden"`
	// Options are the allowed values, from "param x = v in [...]".
	Options []any `json:"options,omitempty"`
	ParamAnnotation
}

// Result is the outcome of evaluating a program.
type Result struct {
	Mesh   *geom.Mesh
	Params []ParamValue
	// Files lists the external files the model read (e.g. imported STLs).
	Files []string
}

const maxDepth = 256

// DefaultColor is the color of solids without an explicit .color().
var DefaultColor = geom.Color{R: 0xE8, G: 0x8A, B: 0x2E}

type scope struct {
	vars   map[string]any
	parent *scope
}

func newScope(parent *scope) *scope { return &scope{vars: map[string]any{}, parent: parent} }

func (s *scope) lookup(name string) (any, bool) {
	for c := s; c != nil; c = c.parent {
		if v, ok := c.vars[name]; ok {
			return v, true
		}
	}
	return nil, false
}

// owner returns the scope where name is defined, or nil.
func (s *scope) owner(name string) *scope {
	for c := s; c != nil; c = c.parent {
		if _, ok := c.vars[name]; ok {
			return c
		}
	}
	return nil
}

func (s *scope) names() []string {
	var out []string
	for c := s; c != nil; c = c.parent {
		for n := range c.vars {
			out = append(out, n)
		}
	}
	return out
}

type object struct {
	mesh *geom.Mesh
	hole bool
}

type interp struct {
	file      string
	modules   map[string]*ModuleDef
	funcs     map[string]*FuncDef
	global    *scope
	overrides map[string]any
	used      map[string]bool
	params    []ParamValue
	files     []string
	lines     []string
	options   map[string][]any // allowed values of the params that have them
	echo      io.Writer
	baseDir   string
	depth     int
}

// Evaluate runs a parsed program and returns the resulting mesh.
func Evaluate(prog *Program, opts Options) (*Result, error) {
	in := &interp{
		file:      prog.File,
		modules:   map[string]*ModuleDef{},
		funcs:     map[string]*FuncDef{},
		global:    newScope(nil),
		overrides: map[string]any{},
		used:      map[string]bool{},
		echo:      opts.Echo,
		baseDir:   opts.BaseDir,
	}
	if in.echo == nil {
		in.echo = io.Discard
	}
	in.global.vars["PI"] = math.Pi

	for name, src := range opts.Defines {
		in.overrides[name] = EvalDefine(src)
	}

	// Modules and functions can be used before their definition.
	for _, s := range prog.Stmts {
		switch d := s.(type) {
		case *ModuleDef:
			if _, ok := shapes.Lookup(d.Name); ok {
				return nil, in.errf(d.P, "module %q has the same name as a primitive", d.Name)
			}
			if _, ok := in.modules[d.Name]; ok {
				return nil, in.errf(d.P, "module %q is defined twice", d.Name)
			}
			in.modules[d.Name] = d
		case *FuncDef:
			if _, ok := builtins[d.Name]; ok {
				return nil, in.errf(d.P, "function %q has the same name as a builtin function", d.Name)
			}
			if _, ok := in.funcs[d.Name]; ok {
				return nil, in.errf(d.P, "function %q is defined twice", d.Name)
			}
			in.funcs[d.Name] = d
		}
	}

	in.lines = prog.Lines
	objs, err := in.execBlock(prog.Stmts, in.global)
	if err != nil {
		return nil, err
	}
	var unknown []string
	for name := range in.overrides {
		if !in.used[name] {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 && !opts.IgnoreUnknownDefines {
		sort.Strings(unknown)
		return nil, fmt.Errorf("unknown param(s) in defines: %s", strings.Join(unknown, ", "))
	}
	mesh := combine(objs, false)
	return &Result{Mesh: mesh, Params: in.params, Files: in.files}, nil
}

// EvalDefine converts the text of a define (-D name=value) into a value:
// it is evaluated as a TCAD expression (3, "text", [1, 2], 2*PI...) and,
// if that fails, used as a plain string.
func EvalDefine(src string) any {
	if e, err := ParseExpr(src); err == nil {
		in := &interp{global: newScope(nil), echo: io.Discard}
		in.global.vars["PI"] = math.Pi
		if v, err := in.eval(e, newScope(in.global)); err == nil {
			return v
		}
	}
	return src
}

// TypeName returns number, string, bool or list.
func TypeName(v any) string { return typeName(v) }

func (in *interp) errf(p Pos, f string, a ...any) error {
	return &Error{File: in.file, Pos: p, Msg: fmt.Sprintf(f, a...)}
}

func (in *interp) execBlock(stmts []Stmt, sc *scope) ([]object, error) {
	var objs []object
	for _, st := range stmts {
		switch s := st.(type) {
		case *LetStmt:
			v, err := in.eval(s.Value, sc)
			if err != nil {
				return nil, err
			}
			if s.IsParam {
				pv := ParamValue{Name: s.Name, Type: typeName(v), Default: v}
				if s.P.Line-1 < len(in.lines) {
					pv.ParamAnnotation = parseAnnotation(in.lines[s.P.Line-1])
				}
				if s.Options != nil {
					opts, err := in.eval(s.Options, sc)
					if err != nil {
						return nil, err
					}
					list, ok := opts.([]any)
					if !ok || len(list) == 0 {
						return nil, in.errf(s.Options.exprPos(), "param %s: \"in\" needs a non-empty list of allowed values", s.Name)
					}
					if !slices.ContainsFunc(list, func(o any) bool { return equal(o, v) }) {
						return nil, in.errf(s.P, "param %s: the default %s is not one of the allowed values %s", s.Name, FormatLiteral(v), FormatLiteral(list))
					}
					pv.Options = list
					if in.options == nil {
						in.options = map[string][]any{}
					}
					in.options[s.Name] = list
				}
				if o, ok := in.overrides[s.Name]; ok {
					if typeName(o) != typeName(v) {
						return nil, in.errf(s.P, "param %s expects a %s, the define gives a %s", s.Name, typeName(v), typeName(o))
					}
					if pv.Options != nil && !slices.ContainsFunc(pv.Options, func(x any) bool { return equal(x, o) }) {
						return nil, in.errf(s.P, "param %s must be one of %s, got %s", s.Name, FormatLiteral(pv.Options), FormatLiteral(o))
					}
					v = o
					in.used[s.Name] = true
					pv.Overridden = true
				}
				pv.Value = v
				pv.Expr = FormatLiteral(v)
				in.params = append(in.params, pv)
			}
			sc.vars[s.Name] = v
		case *ModuleDef, *FuncDef:
			// Hoisted in Evaluate.
		case *ForStmt:
			it, err := in.eval(s.Iter, sc)
			if err != nil {
				return nil, err
			}
			list, ok := it.([]any)
			if !ok {
				return nil, in.errf(s.P, "for expects a list (e.g. range(5)), got %s", typeName(it))
			}
			for _, item := range list {
				child := newScope(sc)
				child.vars[s.Var] = item
				o, err := in.execBlock(s.Body, child)
				if err != nil {
					return nil, err
				}
				objs = append(objs, o...)
			}
		case *IfStmt:
			c, err := in.eval(s.Cond, sc)
			if err != nil {
				return nil, err
			}
			body := s.Else
			if truthy(c) {
				body = s.Then
			}
			o, err := in.execBlock(body, newScope(sc))
			if err != nil {
				return nil, err
			}
			objs = append(objs, o...)
		case *EchoStmt:
			var parts []string
			for _, a := range s.Args {
				v, err := in.eval(a.Value, sc)
				if err != nil {
					return nil, err
				}
				if a.Name != "" {
					parts = append(parts, a.Name+" = "+formatValue(v))
				} else {
					parts = append(parts, formatValue(v))
				}
			}
			fmt.Fprintf(in.echo, "echo %s: %s\n", s.P, strings.Join(parts, " "))
		case *ShapeStmt:
			o, err := in.shape(s, sc)
			if err != nil {
				return nil, err
			}
			objs = append(objs, o)
		}
	}
	return objs, nil
}

// combine applies group semantics: every solid is merged and then every
// hole is removed. With intersect the solids are intersected instead.
func combine(objs []object, intersect bool) *geom.Mesh {
	var solids, holes []*geom.Mesh
	for _, o := range objs {
		if len(o.mesh.Tris) == 0 {
			continue
		}
		if o.hole {
			holes = append(holes, o.mesh)
		} else {
			solids = append(solids, o.mesh)
		}
	}
	if len(solids) == 0 {
		return &geom.Mesh{}
	}
	var base *geom.Mesh
	if intersect {
		base = csg.IntersectAll(solids)
	} else {
		base = csg.UnionAll(solids)
	}
	if len(holes) > 0 && len(base.Tris) > 0 {
		// Faces carved by a hole take the color of the solid.
		c := solids[0].Tris[0].Color
		for _, h := range holes {
			h.SetColor(c)
		}
		base = csg.SubtractAll(base, holes)
	}
	return base
}

func (in *interp) shape(s *ShapeStmt, sc *scope) (object, error) {
	var mesh *geom.Mesh
	switch s.Kind {
	case "group", "intersect":
		objs, err := in.execBlock(s.Body, newScope(sc))
		if err != nil {
			return object{}, err
		}
		mesh = combine(objs, s.Kind == "intersect")
	case "call":
		var err error
		if m, ok := in.modules[s.Name]; ok {
			mesh, err = in.callModule(m, s, sc)
		} else if p, ok := shapes.Lookup(s.Name); ok {
			mesh, err = in.primitive(p, s, sc)
		} else {
			var cands []string
			for _, p := range shapes.All() {
				cands = append(cands, p.Name)
			}
			for n := range in.modules {
				cands = append(cands, n)
			}
			err = in.errf(s.P, "unknown shape %q%s", s.Name, suggest(s.Name, cands))
		}
		if err != nil {
			return object{}, err
		}
	}
	for _, m := range s.Mods {
		if err := in.modifier(mesh, m, sc); err != nil {
			return object{}, err
		}
	}
	return object{mesh: mesh, hole: s.Hole}, nil
}

func (in *interp) bindParams(p Pos, kind, name string, defs []ParamDef, args []Arg, caller, sc *scope) error {
	set := map[string]bool{}
	for i, a := range args {
		v, err := in.eval(a.Value, caller)
		if err != nil {
			return err
		}
		pname := a.Name
		if pname == "" {
			if i >= len(defs) {
				return in.errf(a.P, "too many arguments for %s %s (it takes %d)", kind, name, len(defs))
			}
			pname = defs[i].Name
		} else {
			found := false
			var names []string
			for _, d := range defs {
				names = append(names, d.Name)
				if d.Name == pname {
					found = true
				}
			}
			if !found {
				return in.errf(a.P, "%s %s has no parameter %q (parameters: %s)", kind, name, pname, strings.Join(names, ", "))
			}
		}
		if set[pname] {
			return in.errf(a.P, "parameter %q given twice", pname)
		}
		set[pname] = true
		sc.vars[pname] = v
	}
	for _, d := range defs {
		if set[d.Name] {
			continue
		}
		if d.Default == nil {
			return in.errf(p, "%s %s: missing required parameter %q", kind, name, d.Name)
		}
		v, err := in.eval(d.Default, sc)
		if err != nil {
			return err
		}
		sc.vars[d.Name] = v
	}
	return nil
}

func (in *interp) enter(p Pos) error {
	in.depth++
	if in.depth > maxDepth {
		return in.errf(p, "too much recursion")
	}
	return nil
}

func (in *interp) callModule(m *ModuleDef, s *ShapeStmt, caller *scope) (*geom.Mesh, error) {
	if err := in.enter(s.P); err != nil {
		return nil, err
	}
	defer func() { in.depth-- }()
	sc := newScope(in.global)
	if err := in.bindParams(s.P, "module", m.Name, m.Params, s.Args, caller, sc); err != nil {
		return nil, err
	}
	objs, err := in.execBlock(m.Body, sc)
	if err != nil {
		return nil, err
	}
	return combine(objs, false), nil
}

func (in *interp) primitive(p *shapes.Primitive, s *ShapeStmt, sc *scope) (*geom.Mesh, error) {
	vals := shapes.Args{}
	paramNames := make([]string, len(p.Params))
	for i, pr := range p.Params {
		paramNames[i] = pr.Name
	}
	resolve := func(name string) (shapes.Param, bool) {
		for _, pr := range p.Params {
			if pr.Name == name {
				return pr, true
			}
			for _, al := range pr.Aliases {
				if al == name {
					return pr, true
				}
			}
		}
		return shapes.Param{}, false
	}
	setVal := func(a Arg, pr shapes.Param, v any) error {
		if _, dup := vals[pr.Name]; dup {
			return in.errf(a.P, "%s: parameter %q given twice", p.Name, pr.Name)
		}
		if pr.Default != nil && typeName(v) != typeName(pr.Default) {
			return in.errf(a.P, "%s: parameter %q expects a %s, got %s", p.Name, pr.Name, typeName(pr.Default), typeName(v))
		}
		vals[pr.Name] = v
		return nil
	}
	pos := 0
	for _, a := range s.Args {
		v, err := in.eval(a.Value, sc)
		if err != nil {
			return nil, err
		}
		if a.Name == "" {
			if pos >= len(p.Params) {
				return nil, in.errf(a.P, "too many arguments for %s (parameters: %s)", p.Name, strings.Join(paramNames, ", "))
			}
			if err := setVal(a, p.Params[pos], v); err != nil {
				return nil, err
			}
			pos++
			continue
		}
		if a.Name == "size" {
			if _, ok := resolve("length"); ok {
				l, ok := v.([]any)
				if !ok || len(l) != 3 {
					return nil, in.errf(a.P, "%s: size must be a list [length, width, height]", p.Name)
				}
				for i, n := range []string{"length", "width", "height"} {
					pr, _ := resolve(n)
					if err := setVal(a, pr, l[i]); err != nil {
						return nil, err
					}
				}
				continue
			}
		}
		pr, ok := resolve(a.Name)
		if !ok {
			return nil, in.errf(a.P, "%s has no parameter %q%s (parameters: %s)", p.Name, a.Name, suggest(a.Name, paramNames), strings.Join(paramNames, ", "))
		}
		if err := setVal(a, pr, v); err != nil {
			return nil, err
		}
	}
	for _, pr := range p.Params {
		if _, ok := vals[pr.Name]; ok {
			continue
		}
		if pr.Quality != "" {
			if v, ok := sc.lookup("$" + pr.Quality); ok {
				if _, isNum := v.(float64); !isNum {
					return nil, in.errf(s.P, "$%s must be a number", pr.Quality)
				}
				vals[pr.Name] = v
				continue
			}
		}
		if pr.Default == nil {
			return nil, in.errf(s.P, "%s: missing required parameter %q", p.Name, pr.Name)
		}
		vals[pr.Name] = pr.Default
	}
	for _, pr := range p.Params {
		if f, ok := vals[pr.Name].(string); pr.IsPath && ok && f != "" && !filepath.IsAbs(f) {
			vals[pr.Name] = filepath.Join(in.baseDir, f)
		}
		if f, ok := vals[pr.Name].(string); pr.IsPath && ok && f != "" && !slices.Contains(in.files, f) {
			in.files = append(in.files, f)
		}
	}
	m, err := p.Build(vals)
	if err != nil {
		return nil, in.errf(s.P, "%s: %v", p.Name, err)
	}
	m.SetColor(DefaultColor)
	return m, nil
}

// modArgs resolves the x, y, z style arguments of a modifier.
func (in *interp) modArgs(m Modifier, sc *scope, names []string, defaults []float64) ([]float64, int, error) {
	out := append([]float64(nil), defaults...)
	given := 0
	for i, a := range m.Args {
		v, err := in.eval(a.Value, sc)
		if err != nil {
			return nil, 0, err
		}
		x, ok := v.(float64)
		if !ok {
			return nil, 0, in.errf(a.P, ".%s expects numbers, got %s", m.Name, typeName(v))
		}
		idx := i
		if a.Name != "" {
			idx = -1
			for j, n := range names {
				if n == a.Name {
					idx = j
				}
			}
			if idx < 0 {
				return nil, 0, in.errf(a.P, ".%s has no parameter %q (parameters: %s)", m.Name, a.Name, strings.Join(names, ", "))
			}
		} else if idx >= len(names) {
			return nil, 0, in.errf(a.P, "too many arguments for .%s", m.Name)
		}
		out[idx] = x
		given++
	}
	return out, given, nil
}

func (in *interp) modifier(mesh *geom.Mesh, m Modifier, sc *scope) error {
	xyz := []string{"x", "y", "z"}
	switch m.Name {
	case "move", "translate":
		v, _, err := in.modArgs(m, sc, xyz, []float64{0, 0, 0})
		if err != nil {
			return err
		}
		mesh.Transform(geom.Translate(geom.V(v[0], v[1], v[2])))
	case "rotate":
		v, _, err := in.modArgs(m, sc, xyz, []float64{0, 0, 0})
		if err != nil {
			return err
		}
		mesh.Transform(geom.RotateZ(v[2]).Mul(geom.RotateY(v[1])).Mul(geom.RotateX(v[0])))
	case "scale":
		v, given, err := in.modArgs(m, sc, xyz, []float64{1, 1, 1})
		if err != nil {
			return err
		}
		if given == 1 && len(m.Args) == 1 && m.Args[0].Name == "" {
			v[1], v[2] = v[0], v[0]
		}
		if v[0] == 0 || v[1] == 0 || v[2] == 0 {
			return in.errf(m.P, ".scale factors cannot be 0")
		}
		mesh.Transform(geom.ScaleM(geom.V(v[0], v[1], v[2])))
	case "mirror":
		s := geom.V(1, 1, 1)
		for _, a := range m.Args {
			v, err := in.eval(a.Value, sc)
			if err != nil {
				return err
			}
			axes := ""
			if a.Name != "" {
				if truthy(v) {
					axes = a.Name
				}
			} else if str, ok := v.(string); ok {
				axes = str
			} else {
				return in.errf(a.P, `.mirror expects axis names, e.g. .mirror("x")`)
			}
			for _, c := range axes {
				switch c {
				case 'x':
					s.X = -s.X
				case 'y':
					s.Y = -s.Y
				case 'z':
					s.Z = -s.Z
				default:
					return in.errf(a.P, ".mirror: unknown axis %q", c)
				}
			}
		}
		mesh.Transform(geom.ScaleM(s))
	case "drop":
		// "Drop to the workplane": lowest point to z = 0.
		if len(m.Args) != 0 {
			return in.errf(m.P, ".drop takes no arguments")
		}
		if mn, _, ok := mesh.BBox(); ok {
			mesh.Transform(geom.Translate(geom.V(0, 0, -mn.Z)))
		}
	case "center":
		axes := "xy"
		if len(m.Args) > 1 {
			return in.errf(m.P, `.center takes at most one argument, e.g. .center("xyz")`)
		}
		if len(m.Args) == 1 {
			v, err := in.eval(m.Args[0].Value, sc)
			if err != nil {
				return err
			}
			str, ok := v.(string)
			if !ok || strings.Trim(str, "xyz") != "" || str == "" {
				return in.errf(m.P, `.center expects axis names, e.g. .center("xyz")`)
			}
			axes = str
		}
		if mn, mx, ok := mesh.BBox(); ok {
			c := mn.Add(mx).Scale(-0.5)
			var d geom.Vec3
			if strings.Contains(axes, "x") {
				d.X = c.X
			}
			if strings.Contains(axes, "y") {
				d.Y = c.Y
			}
			if strings.Contains(axes, "z") {
				d.Z = c.Z
			}
			mesh.Transform(geom.Translate(d))
		}
	case "color", "colour":
		c, err := in.color(m, sc)
		if err != nil {
			return err
		}
		mesh.SetColor(c)
	default:
		return in.errf(m.P, "unknown modifier .%s%s", m.Name,
			suggest(m.Name, []string{"move", "rotate", "scale", "mirror", "drop", "center", "color"}))
	}
	return nil
}

var colorNames = map[string]geom.Color{
	"red": {0xD3, 0x2F, 0x2F}, "orange": {0xE8, 0x8A, 0x2E}, "yellow": {0xF5, 0xC5, 0x18},
	"green": {0x43, 0xA0, 0x47}, "blue": {0x1E, 0x88, 0xE5}, "purple": {0x8E, 0x44, 0xAD},
	"pink": {0xEC, 0x6E, 0xAD}, "white": {0xF2, 0xF2, 0xF2}, "black": {0x30, 0x30, 0x30},
	"gray": {0x9E, 0x9E, 0x9E}, "grey": {0x9E, 0x9E, 0x9E}, "brown": {0x8D, 0x5B, 0x3A},
	"cyan": {0x26, 0xC6, 0xDA}, "magenta": {0xD8, 0x1B, 0x60}, "teal": {0x00, 0x89, 0x7B},
	"navy": {0x1A, 0x23, 0x7E}, "lime": {0x9C, 0xCC, 0x3C},
}

func (in *interp) color(m Modifier, sc *scope) (geom.Color, error) {
	if len(m.Args) == 1 {
		v, err := in.eval(m.Args[0].Value, sc)
		if err != nil {
			return geom.Color{}, err
		}
		s, ok := v.(string)
		if !ok {
			return geom.Color{}, in.errf(m.P, `.color expects a name ("red"), a hex value ("#ff8800") or r, g, b`)
		}
		if c, ok := colorNames[strings.ToLower(s)]; ok {
			return c, nil
		}
		if h := strings.TrimPrefix(s, "#"); len(h) == 6 {
			if n, err := strconv.ParseUint(h, 16, 32); err == nil {
				return geom.Color{R: uint8(n >> 16), G: uint8(n >> 8), B: uint8(n)}, nil
			}
		}
		names := make([]string, 0, len(colorNames))
		for n := range colorNames {
			names = append(names, n)
		}
		sort.Strings(names)
		return geom.Color{}, in.errf(m.P, "unknown color %q (use #rrggbb or one of: %s)", s, strings.Join(names, ", "))
	}
	v, given, err := in.modArgs(m, sc, []string{"r", "g", "b"}, []float64{0, 0, 0})
	if err != nil {
		return geom.Color{}, err
	}
	if given != 3 {
		return geom.Color{}, in.errf(m.P, ".color expects a name, a hex string or r, g, b values (0-255)")
	}
	cl := func(x float64) uint8 { return uint8(math.Max(0, math.Min(255, math.Round(x)))) }
	return geom.Color{R: cl(v[0]), G: cl(v[1]), B: cl(v[2])}, nil
}

func (in *interp) eval(e Expr, sc *scope) (any, error) {
	switch x := e.(type) {
	case *NumberLit:
		return x.Val, nil
	case *StringLit:
		return x.Val, nil
	case *BoolLit:
		return x.Val, nil
	case *ListLit:
		out := make([]any, len(x.Items))
		for i, it := range x.Items {
			v, err := in.eval(it, sc)
			if err != nil {
				return nil, err
			}
			out[i] = v
		}
		return out, nil
	case *Ident:
		if v, ok := sc.lookup(x.Name); ok {
			return v, nil
		}
		return nil, in.errf(x.P, "undefined variable %q%s", x.Name, suggest(x.Name, sc.names()))
	case *Unary:
		v, err := in.eval(x.X, sc)
		if err != nil {
			return nil, err
		}
		switch x.Op {
		case "!":
			return !truthy(v), nil
		case "-", "+":
			n, ok := v.(float64)
			if !ok {
				return nil, in.errf(x.P, "unary %s expects a number, got %s", x.Op, typeName(v))
			}
			if x.Op == "-" {
				return -n, nil
			}
			return n, nil
		}
	case *Binary:
		return in.binary(x, sc)
	case *Ternary:
		c, err := in.eval(x.Cond, sc)
		if err != nil {
			return nil, err
		}
		if truthy(c) {
			return in.eval(x.Then, sc)
		}
		return in.eval(x.Else, sc)
	case *IndexExpr:
		v, err := in.eval(x.X, sc)
		if err != nil {
			return nil, err
		}
		iv, err := in.eval(x.Index, sc)
		if err != nil {
			return nil, err
		}
		idx, ok := iv.(float64)
		if !ok {
			return nil, in.errf(x.P, "index must be a number")
		}
		i := int(idx)
		switch l := v.(type) {
		case []any:
			if i < 0 {
				i += len(l)
			}
			if i < 0 || i >= len(l) {
				return nil, in.errf(x.P, "index %d out of range (length %d)", int(idx), len(l))
			}
			return l[i], nil
		case string:
			r := []rune(l)
			if i < 0 {
				i += len(r)
			}
			if i < 0 || i >= len(r) {
				return nil, in.errf(x.P, "index %d out of range (length %d)", int(idx), len(r))
			}
			return string(r[i]), nil
		}
		return nil, in.errf(x.P, "cannot index a %s", typeName(v))
	case *CallExpr:
		return in.call(x, sc)
	}
	return nil, in.errf(e.exprPos(), "invalid expression")
}

func (in *interp) call(x *CallExpr, sc *scope) (any, error) {
	if b, ok := builtins[x.Name]; ok {
		args := make([]any, len(x.Args))
		for i, a := range x.Args {
			if a.Name != "" {
				return nil, in.errf(a.P, "builtin %s does not take named arguments", x.Name)
			}
			v, err := in.eval(a.Value, sc)
			if err != nil {
				return nil, err
			}
			args[i] = v
		}
		if len(args) < b.minArgs || (b.maxArgs >= 0 && len(args) > b.maxArgs) {
			return nil, in.errf(x.P, "wrong number of arguments for %s", x.Name)
		}
		v, err := b.fn(args)
		if err != nil {
			return nil, in.errf(x.P, "%s: %v", x.Name, err)
		}
		return v, nil
	}
	f, ok := in.funcs[x.Name]
	if !ok {
		if _, isShape := shapes.Lookup(x.Name); isShape {
			return nil, in.errf(x.P, "%s is a shape and cannot be used inside an expression", x.Name)
		}
		if _, isMod := in.modules[x.Name]; isMod {
			return nil, in.errf(x.P, "%s is a module and cannot be used inside an expression", x.Name)
		}
		var cands []string
		for n := range builtins {
			cands = append(cands, n)
		}
		for n := range in.funcs {
			cands = append(cands, n)
		}
		return nil, in.errf(x.P, "unknown function %q%s", x.Name, suggest(x.Name, cands))
	}
	if err := in.enter(x.P); err != nil {
		return nil, err
	}
	defer func() { in.depth-- }()
	fsc := newScope(in.global)
	if err := in.bindParams(x.P, "function", f.Name, f.Params, x.Args, sc, fsc); err != nil {
		return nil, err
	}
	return in.eval(f.Body, fsc)
}

func (in *interp) binary(x *Binary, sc *scope) (any, error) {
	l, err := in.eval(x.L, sc)
	if err != nil {
		return nil, err
	}
	// Short circuit logical operators.
	switch x.Op {
	case "&&":
		if !truthy(l) {
			return false, nil
		}
		r, err := in.eval(x.R, sc)
		if err != nil {
			return nil, err
		}
		return truthy(r), nil
	case "||":
		if truthy(l) {
			return true, nil
		}
		r, err := in.eval(x.R, sc)
		if err != nil {
			return nil, err
		}
		return truthy(r), nil
	}
	r, err := in.eval(x.R, sc)
	if err != nil {
		return nil, err
	}
	switch x.Op {
	case "==", "!=":
		if err := in.checkOptionCompare(x, sc); err != nil {
			return nil, err
		}
		return equal(l, r) == (x.Op == "=="), nil
	}
	if x.Op == "+" {
		if ls, ok := l.(string); ok {
			return ls + formatValue(r), nil
		}
		if ll, ok := l.([]any); ok {
			if rl, ok := r.([]any); ok {
				return append(append([]any{}, ll...), rl...), nil
			}
		}
	}
	a, ok1 := l.(float64)
	b, ok2 := r.(float64)
	if !ok1 || !ok2 {
		return nil, in.errf(x.P, "operator %s cannot be applied to %s and %s", x.Op, typeName(l), typeName(r))
	}
	switch x.Op {
	case "+":
		return a + b, nil
	case "-":
		return a - b, nil
	case "*":
		return a * b, nil
	case "/":
		if b == 0 {
			return nil, in.errf(x.P, "division by zero")
		}
		return a / b, nil
	case "%":
		if b == 0 {
			return nil, in.errf(x.P, "modulo by zero")
		}
		return math.Mod(a, b), nil
	case "<":
		return a < b, nil
	case "<=":
		return a <= b, nil
	case ">":
		return a > b, nil
	case ">=":
		return a >= b, nil
	}
	return nil, in.errf(x.P, "unknown operator %s", x.Op)
}

// checkOptionCompare catches comparisons that can never be true, such as
// mode == "printing" when mode only accepts "assembled" and "print".
func (in *interp) checkOptionCompare(x *Binary, sc *scope) error {
	check := func(id, other Expr) error {
		ident, ok := id.(*Ident)
		if !ok {
			return nil
		}
		opts, ok := in.options[ident.Name]
		if !ok || sc.owner(ident.Name) != in.global {
			return nil // not a param with options, or shadowed by a local
		}
		var lit any
		switch o := other.(type) {
		case *StringLit:
			lit = o.Val
		case *NumberLit:
			lit = o.Val
		case *BoolLit:
			lit = o.Val
		default:
			return nil
		}
		if slices.ContainsFunc(opts, func(v any) bool { return equal(v, lit) }) {
			return nil
		}
		var names []string
		for _, o := range opts {
			if s, ok := o.(string); ok {
				names = append(names, s)
			}
		}
		hint := ""
		if s, ok := lit.(string); ok {
			hint = suggest(s, names)
		}
		return in.errf(x.P, "%s is never %s: its allowed values are %s%s", ident.Name, FormatLiteral(lit), FormatLiteral(opts), hint)
	}
	if err := check(x.L, x.R); err != nil {
		return err
	}
	return check(x.R, x.L)
}

// suggest returns a " (did you mean ...?)" hint for typos.
func suggest(name string, candidates []string) string {
	best, bestD := "", 3
	for _, c := range candidates {
		if d := levenshtein(name, c); d < bestD {
			best, bestD = c, d
		}
	}
	if best == "" {
		return ""
	}
	return fmt.Sprintf(" (did you mean %q?)", best)
}

func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur := make([]int, len(rb)+1)
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(rb)]
}
