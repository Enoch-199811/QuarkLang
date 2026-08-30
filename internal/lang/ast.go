package lang

// Pos is a source position.
type Pos struct {
	Line int
	Col  int
}

// Program is a parsed QuarkLang program.
type Program struct {
	Funcs      []*FuncDecl
	Structs    []*StructDecl
	Interfaces []*InterfaceDecl
	Impls      []*ImplDecl
}

// FuncDecl is a top-level function declaration. Ret is the optional return
// type annotation: functions WITHOUT Ret yield a FuncBuffer (out -> tail),
// functions WITH Ret yield the value of `return expr;` directly.
type FuncDecl struct {
	Name   string
	Params []Param
	Ret    string
	Body   *Block
	Pos    Pos
}

// Param is a function parameter ("name Type").
type Param struct {
	Name string
	Type string
	Pos  Pos
}

// Member is a struct member declaration ("name Type;").
type Member struct {
	Name string
	Type string
	Pos  Pos
}

// StructDecl is a struct declaration: "struct<T> { ... } Name;".
type StructDecl struct {
	Name       string
	TypeParams []string
	Members    []Member
	Pos        Pos
}

// MethodSig is an interface method signature (no body).
type MethodSig struct {
	Name   string
	Params []Param
	Ret    string
	Pos    Pos
}

// InterfaceDecl is an interface declaration.
type InterfaceDecl struct {
	Name    string
	Methods []MethodSig
	Pos     Pos
}

// ImplDecl is an impl declaration: "impl<T> [Iface] { funcs } Type;".
// 规则：struct 有泛型参数时，impl 必须引入同样的参数。
type ImplDecl struct {
	Iface      string
	Type       string
	TypeParams []string
	Methods    []*FuncDecl
	Pos        Pos
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
type LogStmt struct {
	X   Expr
	Pos Pos
}
type TryStmt struct {
	Try          *Block
	CatchVar     string
	CatchVarType string
	Catch        *Block
	Pos          Pos
}
type ReturnStmt struct {
	X   Expr // nil X = bare return
	Pos Pos
}
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
func (*LogStmt) isStmt()    {}
func (*TryStmt) isStmt()    {}
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
type NullLit struct{ Pos Pos }
type Ident struct {
	Name string
	Pos  Pos
}
type ListLit struct {
	Items []Expr
}
type StructLit struct {
	Fields []StructLitField
	Pos    Pos
}
type StructLitField struct {
	Name string
	X    Expr
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
func (*NullLit) isExpr()    {}
func (*Ident) isExpr()      {}
func (*ListLit) isExpr()    {}
func (*StructLit) isExpr()  {}
func (*BinOp) isExpr()      {}
func (*UnOp) isExpr()       {}
func (*CallExpr) isExpr()   {}
func (*MemberExpr) isExpr() {}
func (*ScopeCall) isExpr()  {}
func (*IndexExpr) isExpr()  {}
