package lang

// Pos is a source position.
type Pos struct {
	Line int
	Col  int
}

// Program is a parsed QuarkLang program.
type Program struct {
	Funcs []*FuncDecl
}

// FuncDecl is a top-level function declaration.
type FuncDecl struct {
	Name   string
	Params []Param
	Body   *Block
	Pos    Pos
}

// Param is a function parameter ("name Type").
type Param struct {
	Name string
	Type string
	Pos  Pos
}

// Block is a brace-delimited statement list.
type Block struct {
	Stmts []Stmt
}

// Stmt is any statement.
type Stmt interface{ isStmt() }

// Expr is any expression.
type Expr interface{ isExpr() }

// ---- statements ----

type ExprStmt struct{ X Expr }
type OutStmt struct{ X Expr }
type LogStmt struct {
	X   Expr
	Pos Pos
}
type YieldStmt struct{ Pos Pos }
type ReturnStmt struct{ X Expr } // nil X = bare return
type IfStmt struct {
	Cond Expr
	Then *Block
	Else *Block // nil when absent
}
type WhileStmt struct {
	Cond Expr
	Body *Block
}
type ForStmt struct {
	Var  string
	Iter Expr
	Body *Block
	Pos  Pos
}
type DeclStmt struct {
	Name string
	Type string
	Init Expr // nil = uninitialized
	Pos  Pos
}
type AssignStmt struct {
	Target Expr
	X      Expr
	Pos    Pos
}

func (*ExprStmt) isStmt()   {}
func (*OutStmt) isStmt()    {}
func (*LogStmt) isStmt()    {}
func (*YieldStmt) isStmt()  {}
func (*ReturnStmt) isStmt() {}
func (*IfStmt) isStmt()     {}
func (*WhileStmt) isStmt()  {}
func (*ForStmt) isStmt()    {}
func (*DeclStmt) isStmt()   {}
func (*AssignStmt) isStmt() {}

// ---- expressions ----

type IntLit struct {
	V   int64
	Pos Pos
}
type FloatLit struct {
	V   float64
	Pos Pos
}
type StrLit struct {
	V   string
	Pos Pos
}
type BoolLit struct {
	V   bool
	Pos Pos
}
type Ident struct {
	Name string
	Pos  Pos
}
type ListLit struct {
	Items []Expr
}
type BinOp struct {
	Op   string
	L, R Expr
	Pos  Pos
}
type UnOp struct {
	Op  string
	X   Expr
	Pos Pos
}
type CallExpr struct {
	Fn   Expr
	Args []Expr
	Sign *SignCall
	Pos  Pos
}
type SignCall struct {
	Name string
	Args []Expr
}
type MemberExpr struct {
	X    Expr
	Name string
	Pos  Pos
}
type ScopeCall struct {
	Scope string
	Name  string
	Args  []Expr
	Pos   Pos
}
type IndexExpr struct {
	X   Expr
	Idx Expr
	Pos Pos
}

func (*IntLit) isExpr()     {}
func (*FloatLit) isExpr()   {}
func (*StrLit) isExpr()     {}
func (*BoolLit) isExpr()    {}
func (*Ident) isExpr()      {}
func (*ListLit) isExpr()    {}
func (*BinOp) isExpr()      {}
func (*UnOp) isExpr()       {}
func (*CallExpr) isExpr()   {}
func (*MemberExpr) isExpr() {}
func (*ScopeCall) isExpr()  {}
func (*IndexExpr) isExpr()  {}
