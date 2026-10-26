package dsl

// Expressions.

type Expr interface{ exprPos() Pos }

type (
	NumberLit struct {
		P   Pos
		Val float64
	}
	StringLit struct {
		P   Pos
		Val string
	}
	BoolLit struct {
		P   Pos
		Val bool
	}
	ListLit struct {
		P     Pos
		Items []Expr
	}
	Ident struct {
		P    Pos
		Name string
	}
	Unary struct {
		P  Pos
		Op string
		X  Expr
	}
	Binary struct {
		P    Pos
		Op   string
		L, R Expr
	}
	Ternary struct {
		P                Pos
		Cond, Then, Else Expr
	}
	CallExpr struct {
		P    Pos
		Name string
		Args []Arg
	}
	IndexExpr struct {
		P        Pos
		X, Index Expr
	}
)

func (e *NumberLit) exprPos() Pos { return e.P }
func (e *StringLit) exprPos() Pos { return e.P }
func (e *BoolLit) exprPos() Pos   { return e.P }
func (e *ListLit) exprPos() Pos   { return e.P }
func (e *Ident) exprPos() Pos     { return e.P }
func (e *Unary) exprPos() Pos     { return e.P }
func (e *Binary) exprPos() Pos    { return e.P }
func (e *Ternary) exprPos() Pos   { return e.P }
func (e *CallExpr) exprPos() Pos  { return e.P }
func (e *IndexExpr) exprPos() Pos { return e.P }

// Arg is a positional (Name == "") or named argument.
type Arg struct {
	P     Pos
	Name  string
	Value Expr
}

// ParamDef is a module or function parameter with an optional default.
type ParamDef struct {
	Name    string
	Default Expr
}

// Statements.

type Stmt interface{ stmtPos() Pos }

type (
	LetStmt struct {
		P       Pos
		Name    string
		Value   Expr
		IsParam bool
		// Options is the list after "in" (param only): the closed set of
		// values the param accepts.
		Options Expr
	}
	ModuleDef struct {
		P      Pos
		Name   string
		Params []ParamDef
		Body   []Stmt
	}
	FuncDef struct {
		P      Pos
		Name   string
		Params []ParamDef
		Body   Expr
	}
	ForStmt struct {
		P    Pos
		Var  string
		Iter Expr
		Body []Stmt
	}
	IfStmt struct {
		P    Pos
		Cond Expr
		Then []Stmt
		Else []Stmt
	}
	EchoStmt struct {
		P    Pos
		Args []Arg
	}
	// ShapeStmt instantiates a primitive, a module call or a group.
	ShapeStmt struct {
		P    Pos
		Hole bool
		Kind string // "call", "group" or "intersect"
		Name string // primitive or module name for Kind == "call"
		Args []Arg
		Body []Stmt
		Mods []Modifier
	}
)

// Modifier is a transform applied with .name(args) after a shape.
type Modifier struct {
	P    Pos
	Name string
	Args []Arg
}

func (s *LetStmt) stmtPos() Pos   { return s.P }
func (s *ModuleDef) stmtPos() Pos { return s.P }
func (s *FuncDef) stmtPos() Pos   { return s.P }
func (s *ForStmt) stmtPos() Pos   { return s.P }
func (s *IfStmt) stmtPos() Pos    { return s.P }
func (s *EchoStmt) stmtPos() Pos  { return s.P }
func (s *ShapeStmt) stmtPos() Pos { return s.P }

// Program is a parsed TCAD file.
type Program struct {
	File  string
	Stmts []Stmt
	// Lines is the source text, used to read param annotations.
	Lines []string
}
