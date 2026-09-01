package lang

// Pos is a source position.
type Pos struct {
	Line int
	Col  int
}

// Program is a parsed QuarkLang program.
type TypeAlias struct {
	Name string
	Type string
	Pos  Pos
}

type Program struct {
	FnList      []*FuncDecl // 函数表（eval 直取索引免 map）
	FnIndex     map[string]int
	Funcs       []*FuncDecl
	Structs     []*StructDecl
	Interfaces  []*InterfaceDecl
	Impls       []*ImplDecl
	TypeAliases []*TypeAlias
	Kind        string // "main"（默认）| "library"（program 宏）
	kindSet     bool
	Imports     []string
	Pub         []string // pub 宏：库中公开的符号名
	Src         string   // 原始源码（库导出时按函数体行区间切片）
}

// MacroDef 是 macro {模式} {主体} 定义（模式/主体均为 token 序列）。
type MacroDef struct {
	Pattern []Token
	Body    []Token
	Pos     Pos
}

// FuncDecl is a top-level function declaration. Ret is the optional return
// type annotation: functions WITHOUT Ret yield a FuncBuffer (out -> tail),
// functions WITH Ret yield the value of `return expr;` directly.
type FuncDecl struct {
	Name       string
	TypeParams []string // 泛型函数 func<T, ...>（xmind §函数）
	Params     []Param
	Ret        string
	Body       *Block
	BodyStart  Pos // 函数体源码行区间（库导出用）
	BodyEnd    Pos
	Pos        Pos
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
	Expands []string // expand interface 组合接口（xmind §接口）
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

// DeleteStmt：delete variable; —— 回收内存于 __delete__()，本质是给对应 block 的日志加消除记录。
type DeleteStmt struct {
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
	Name  string
	Type  string
	Init  Expr   // nil = uninitialized
	Decor string // "" | "const" | "copyd"（xmind §变量修饰表）
	Pos   Pos
}
type AssignStmt struct {
	Target Expr
	X      Expr
	Pos    Pos
}

func (*ExprStmt) isStmt()   {}
func (*LogStmt) isStmt()    {}
func (*DeleteStmt) isStmt() {}
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
	Name   string // 目标有名结构体（typecheck 填充），eval 用它建类型
	Pos    Pos
}

// NewExpr：new <type>[size] —— 在堆上直接申请内存（失败返回 badAlloc）。
type NewExpr struct {
	Typ  string
	Size Expr // nil = 单元素
	Pos  Pos
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
	Fn    Expr
	Args  []Expr
	Sign  *SignCall
	Pos   Pos
	FnIdx int // 编译期解析的函数索引（-1 = 未解析/变量调用）；eval 直取 FnList 免 map
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
func (*NewExpr) isExpr()    {}
func (*BinOp) isExpr()      {}
func (*UnOp) isExpr()       {}
func (*CallExpr) isExpr()   {}
func (*MemberExpr) isExpr() {}
func (*ScopeCall) isExpr()  {}
func (*IndexExpr) isExpr()  {}
