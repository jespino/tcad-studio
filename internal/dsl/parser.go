package dsl

import (
	"fmt"
	"strings"
)

var keywords = map[string]bool{
	"let": true, "param": true, "module": true, "func": true, "for": true, "in": true,
	"if": true, "else": true, "hole": true, "solid": true, "group": true,
	"intersect": true, "true": true, "false": true, "echo": true,
}

type parser struct {
	file string
	toks []token
	i    int
}

// Parse parses a TCAD source file.
func Parse(file, src string) (*Program, error) {
	toks, err := lex(file, src)
	if err != nil {
		return nil, err
	}
	p := &parser{file: file, toks: toks}
	var stmts []Stmt
	for !p.at(tEOF, "") {
		s, err := p.statement(true)
		if err != nil {
			return nil, err
		}
		stmts = append(stmts, s)
	}
	return &Program{File: file, Stmts: stmts, Lines: strings.Split(src, "\n")}, nil
}

// ParseExpr parses a standalone expression (used for -D overrides).
func ParseExpr(src string) (Expr, error) {
	toks, err := lex("", src)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	e, err := p.expr()
	if err != nil {
		return nil, err
	}
	if !p.at(tEOF, "") {
		return nil, p.errf(p.peek().pos, "unexpected %s", p.peek())
	}
	return e, nil
}

func (p *parser) peek() token { return p.toks[p.i] }
func (p *parser) peekN(n int) token {
	if p.i+n < len(p.toks) {
		return p.toks[p.i+n]
	}
	return p.toks[len(p.toks)-1]
}
func (p *parser) next() token {
	t := p.toks[p.i]
	if t.kind != tEOF {
		p.i++
	}
	return t
}

func (p *parser) errf(pos Pos, f string, a ...any) error {
	return &Error{File: p.file, Pos: pos, Msg: fmt.Sprintf(f, a...)}
}

// at reports whether the next token has the given kind and (if not empty)
// text.
func (p *parser) at(k tokenKind, text string) bool {
	t := p.peek()
	return t.kind == k && (text == "" || t.text == text)
}

func (p *parser) atPunct(text string) bool { return p.at(tPunct, text) }
func (p *parser) atKeyword(kw string) bool { return p.at(tIdent, kw) }

func (p *parser) accept(text string) bool {
	if p.atPunct(text) {
		p.next()
		return true
	}
	return false
}

func (p *parser) expect(text string) (token, error) {
	if !p.atPunct(text) {
		return token{}, p.errf(p.peek().pos, "expected %q, found %s", text, p.peek())
	}
	return p.next(), nil
}

func (p *parser) ident() (token, error) {
	t := p.peek()
	if t.kind != tIdent {
		return t, p.errf(t.pos, "expected a name, found %s", t)
	}
	if keywords[t.text] {
		return t, p.errf(t.pos, "%q is a reserved word", t.text)
	}
	return p.next(), nil
}

func (p *parser) block() ([]Stmt, error) {
	if _, err := p.expect("{"); err != nil {
		return nil, err
	}
	var stmts []Stmt
	for !p.atPunct("}") {
		if p.at(tEOF, "") {
			return nil, p.errf(p.peek().pos, "missing closing \"}\"")
		}
		s, err := p.statement(false)
		if err != nil {
			return nil, err
		}
		stmts = append(stmts, s)
	}
	p.next()
	return stmts, nil
}

func (p *parser) statement(top bool) (Stmt, error) {
	for p.accept(";") {
	}
	t := p.peek()
	if t.kind != tIdent {
		return nil, p.errf(t.pos, "expected a statement, found %s", t)
	}
	var s Stmt
	var err error
	switch t.text {
	case "let", "param":
		if t.text == "param" && !top {
			return nil, p.errf(t.pos, "param is only allowed at the top level of the file")
		}
		s, err = p.letStmt()
	case "module", "func":
		if !top {
			return nil, p.errf(t.pos, "%s definitions are only allowed at the top level", t.text)
		}
		if t.text == "module" {
			s, err = p.moduleDef()
		} else {
			s, err = p.funcDef()
		}
	case "for":
		s, err = p.forStmt()
	case "if":
		s, err = p.ifStmt()
	case "echo":
		p.next()
		var args []Arg
		if _, err := p.expect("("); err != nil {
			return nil, err
		}
		args, err = p.args(")")
		s = &EchoStmt{P: t.pos, Args: args}
	default:
		s, err = p.shapeStmt()
	}
	if err != nil {
		return nil, err
	}
	p.accept(";")
	return s, nil
}

func (p *parser) letStmt() (Stmt, error) {
	kw := p.next()
	name, err := p.ident()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect("="); err != nil {
		return nil, err
	}
	val, err := p.expr()
	if err != nil {
		return nil, err
	}
	s := &LetStmt{P: kw.pos, Name: name.text, Value: val, IsParam: kw.text == "param"}
	// param modo = "a" in ["a", "b"]
	if p.atKeyword("in") {
		in := p.next()
		if !s.IsParam {
			return nil, p.errf(in.pos, "\"in [...]\" (allowed values) is only valid for param")
		}
		if s.Options, err = p.expr(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (p *parser) paramDefs() ([]ParamDef, error) {
	if _, err := p.expect("("); err != nil {
		return nil, err
	}
	var params []ParamDef
	for !p.atPunct(")") {
		name, err := p.ident()
		if err != nil {
			return nil, err
		}
		pd := ParamDef{Name: name.text}
		if p.accept("=") {
			if pd.Default, err = p.expr(); err != nil {
				return nil, err
			}
		}
		params = append(params, pd)
		if !p.accept(",") {
			break
		}
	}
	if _, err := p.expect(")"); err != nil {
		return nil, err
	}
	return params, nil
}

func (p *parser) moduleDef() (Stmt, error) {
	kw := p.next()
	name, err := p.ident()
	if err != nil {
		return nil, err
	}
	params, err := p.paramDefs()
	if err != nil {
		return nil, err
	}
	body, err := p.block()
	if err != nil {
		return nil, err
	}
	return &ModuleDef{P: kw.pos, Name: name.text, Params: params, Body: body}, nil
}

func (p *parser) funcDef() (Stmt, error) {
	kw := p.next()
	name, err := p.ident()
	if err != nil {
		return nil, err
	}
	params, err := p.paramDefs()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect("="); err != nil {
		return nil, err
	}
	body, err := p.expr()
	if err != nil {
		return nil, err
	}
	return &FuncDef{P: kw.pos, Name: name.text, Params: params, Body: body}, nil
}

func (p *parser) forStmt() (Stmt, error) {
	kw := p.next()
	v, err := p.ident()
	if err != nil {
		return nil, err
	}
	if !p.atKeyword("in") {
		return nil, p.errf(p.peek().pos, "expected \"in\" after the loop variable")
	}
	p.next()
	iter, err := p.expr()
	if err != nil {
		return nil, err
	}
	body, err := p.block()
	if err != nil {
		return nil, err
	}
	return &ForStmt{P: kw.pos, Var: v.text, Iter: iter, Body: body}, nil
}

func (p *parser) ifStmt() (Stmt, error) {
	kw := p.next()
	cond, err := p.expr()
	if err != nil {
		return nil, err
	}
	then, err := p.block()
	if err != nil {
		return nil, err
	}
	s := &IfStmt{P: kw.pos, Cond: cond, Then: then}
	if p.atKeyword("else") {
		p.next()
		if p.atKeyword("if") {
			elif, err := p.ifStmt()
			if err != nil {
				return nil, err
			}
			s.Else = []Stmt{elif}
		} else if s.Else, err = p.block(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (p *parser) shapeStmt() (Stmt, error) {
	start := p.peek()
	s := &ShapeStmt{P: start.pos}
	if p.atKeyword("hole") || p.atKeyword("solid") {
		s.Hole = p.next().text == "hole"
	}
	t := p.peek()
	switch {
	case p.atKeyword("group") || p.atKeyword("intersect"):
		p.next()
		s.Kind = t.text
		body, err := p.block()
		if err != nil {
			return nil, err
		}
		s.Body = body
	case t.kind == tIdent && p.peekN(1).kind == tPunct && p.peekN(1).text == "(":
		name, err := p.ident()
		if err != nil {
			return nil, err
		}
		p.next()
		s.Kind = "call"
		s.Name = name.text
		if s.Args, err = p.args(")"); err != nil {
			return nil, err
		}
	case t.kind == tIdent && p.peekN(1).kind == tPunct && p.peekN(1).text == "=":
		return nil, p.errf(t.pos, "use \"let %s = ...\" to define a variable", t.text)
	default:
		return nil, p.errf(t.pos, "expected a shape (e.g. box(...), group { ... }), found %s", t)
	}
	for p.atPunct(".") {
		p.next()
		name, err := p.ident()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect("("); err != nil {
			return nil, err
		}
		args, err := p.args(")")
		if err != nil {
			return nil, err
		}
		s.Mods = append(s.Mods, Modifier{P: name.pos, Name: name.text, Args: args})
	}
	return s, nil
}

// args parses a comma separated argument list up to (and including) the
// closing token.
func (p *parser) args(closing string) ([]Arg, error) {
	var args []Arg
	for !p.atPunct(closing) {
		a := Arg{P: p.peek().pos}
		if p.peek().kind == tIdent && p.peekN(1).kind == tPunct && p.peekN(1).text == "=" {
			a.Name = p.next().text
			p.next()
		}
		v, err := p.expr()
		if err != nil {
			return nil, err
		}
		a.Value = v
		args = append(args, a)
		if !p.accept(",") {
			break
		}
	}
	if _, err := p.expect(closing); err != nil {
		return nil, err
	}
	return args, nil
}

// Expression parsing by precedence climbing.

func (p *parser) expr() (Expr, error) { return p.ternary() }

func (p *parser) ternary() (Expr, error) {
	cond, err := p.binary(0)
	if err != nil {
		return nil, err
	}
	if !p.atPunct("?") {
		return cond, nil
	}
	q := p.next()
	then, err := p.ternary()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(":"); err != nil {
		return nil, err
	}
	els, err := p.ternary()
	if err != nil {
		return nil, err
	}
	return &Ternary{P: q.pos, Cond: cond, Then: then, Else: els}, nil
}

var precedence = [][]string{
	{"||"},
	{"&&"},
	{"==", "!="},
	{"<", "<=", ">", ">="},
	{"+", "-"},
	{"*", "/", "%"},
}

func (p *parser) binary(level int) (Expr, error) {
	if level >= len(precedence) {
		return p.unary()
	}
	l, err := p.binary(level + 1)
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		found := false
		if t.kind == tPunct {
			for _, op := range precedence[level] {
				if t.text == op {
					found = true
				}
			}
		}
		if !found {
			return l, nil
		}
		p.next()
		r, err := p.binary(level + 1)
		if err != nil {
			return nil, err
		}
		l = &Binary{P: t.pos, Op: t.text, L: l, R: r}
	}
}

func (p *parser) unary() (Expr, error) {
	if p.atPunct("-") || p.atPunct("!") || p.atPunct("+") {
		t := p.next()
		x, err := p.unary()
		if err != nil {
			return nil, err
		}
		return &Unary{P: t.pos, Op: t.text, X: x}, nil
	}
	return p.postfix()
}

func (p *parser) postfix() (Expr, error) {
	x, err := p.primary()
	if err != nil {
		return nil, err
	}
	for p.atPunct("[") {
		t := p.next()
		idx, err := p.expr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect("]"); err != nil {
			return nil, err
		}
		x = &IndexExpr{P: t.pos, X: x, Index: idx}
	}
	return x, nil
}

func (p *parser) primary() (Expr, error) {
	t := p.peek()
	switch t.kind {
	case tNumber:
		p.next()
		return &NumberLit{P: t.pos, Val: t.num}, nil
	case tString:
		p.next()
		return &StringLit{P: t.pos, Val: t.text}, nil
	case tIdent:
		switch t.text {
		case "true", "false":
			p.next()
			return &BoolLit{P: t.pos, Val: t.text == "true"}, nil
		}
		if keywords[t.text] {
			return nil, p.errf(t.pos, "unexpected %q in expression", t.text)
		}
		p.next()
		if p.atPunct("(") {
			p.next()
			args, err := p.args(")")
			if err != nil {
				return nil, err
			}
			return &CallExpr{P: t.pos, Name: t.text, Args: args}, nil
		}
		return &Ident{P: t.pos, Name: t.text}, nil
	case tPunct:
		switch t.text {
		case "(":
			p.next()
			e, err := p.expr()
			if err != nil {
				return nil, err
			}
			if _, err := p.expect(")"); err != nil {
				return nil, err
			}
			return e, nil
		case "[":
			p.next()
			var items []Expr
			for !p.atPunct("]") {
				e, err := p.expr()
				if err != nil {
					return nil, err
				}
				items = append(items, e)
				if !p.accept(",") {
					break
				}
			}
			if _, err := p.expect("]"); err != nil {
				return nil, err
			}
			return &ListLit{P: t.pos, Items: items}, nil
		}
	}
	return nil, p.errf(t.pos, "unexpected %s in expression", t)
}
