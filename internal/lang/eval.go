package lang

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

// RunError is a runtime (or strict-check) error with source position and,
// when available, the execCtx whose log explains what happened.
type RunError struct {
	Msg string
	Pos Pos
	Ctx *execCtx
}

func (e *RunError) Error() string {
	return fmt.Sprintf("%s at line %d", e.Msg, e.Pos.Line)
}

// errReturn is an internal control-flow signal, never shown to the user.
var errReturn = errors.New("__return__")

type scope struct {
	vars  map[string]Value
	outer *scope
}

func newScope(outer *scope) *scope {
	return &scope{vars: map[string]Value{}, outer: outer}
}

func (s *scope) declare(name string, v Value, pos Pos) error {
	if _, dup := s.vars[name]; dup {
		return &RunError{Msg: fmt.Sprintf("CompileError: duplicate declaration of %q", name), Pos: pos}
	}
	s.vars[name] = v
	return nil
}

func (s *scope) set(name string, v Value, pos Pos) error {
	for sc := s; sc != nil; sc = sc.outer {
		if _, ok := sc.vars[name]; ok {
			sc.vars[name] = v
			return nil
		}
	}
	return &RunError{Msg: fmt.Sprintf("CompileError: assignment to undeclared identifier %q", name), Pos: pos}
}

func (s *scope) get(name string, pos Pos) (Value, error) {
	for sc := s; sc != nil; sc = sc.outer {
		if v, ok := sc.vars[name]; ok {
			return v, nil
		}
	}
	return nil, &RunError{Msg: fmt.Sprintf("CompileError: undeclared identifier %q", name), Pos: pos}
}

// signDef is a registered signature: name -> call implementation.
type signDef struct {
	name string
	call func(prefix Value, ctx *execCtx) (Value, error)
}

type builtinFn func(args []Value, pos Pos, ctx *execCtx) (Value, error)

// MemBlock 是全局内存管理器的分配区块（spec §14.1）。
type MemBlock struct {
	ID          int
	Size        int
	Dirty       bool // 区块被更改会记录（脏标记）
	OwnerPID    int  // 所属协程（0 = 全局）
	Reclaimable bool // 无人占用，可回收
}

// MemoryManager 管理 block 分配/脏标记/回收。
type MemoryManager struct {
	mu     sync.Mutex
	nextID int
	blocks map[int]*MemBlock
}

func NewMemoryManager() *MemoryManager {
	return &MemoryManager{blocks: map[int]*MemBlock{}}
}

func (m *MemoryManager) Alloc(size, owner int) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	m.blocks[m.nextID] = &MemBlock{ID: m.nextID, Size: size, Dirty: true, OwnerPID: owner}
	return m.nextID
}

func (m *MemoryManager) MarkDirty(id int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if b, ok := m.blocks[id]; ok {
		b.Dirty = true
	}
}

// ReclaimTask 标记某协程的全部 block 为可回收（协程结束自动标记）。
func (m *MemoryManager) ReclaimTask(pid int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, b := range m.blocks {
		if b.OwnerPID == pid {
			b.Reclaimable = true
		}
	}
}

// Compact 依据记录清理无人占用的空间，返回回收的 block 数（语言层面不返回）。
func (m *MemoryManager) Compact() (reclaimed int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, b := range m.blocks {
		if b.Reclaimable {
			delete(m.blocks, id)
			reclaimed++
		}
	}
	return reclaimed
}

// BlockCount 返回当前 block 总数（测试可观测）。
func (m *MemoryManager) BlockCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.blocks)
}

// StructDef is a registered struct declaration.
type StructDef struct {
	Name       string
	TypeParams []string
	Types      map[string]string // 成员名 → 类型注解
}

// InterfaceDef is a registered interface declaration.
type InterfaceDef struct {
	Name    string
	Methods []MethodSig
}

// ImplDef is a registered impl block: Methods are static (no self), SelfMethods
// take self as first parameter.
type ImplDef struct {
	Type        string
	Iface       string
	TypeParams  []string
	Methods     map[string]*Func
	SelfMethods map[string]*Func
}

type interp struct {
	fns        map[string]*Func
	sigs       map[string]*signDef
	builtins   map[string]builtinFn
	structs    map[string]*StructDef
	interfaces map[string]*InterfaceDef
	impls      map[string]*ImplDef
	tasks      map[int]*Task
	taskMu     sync.Mutex
	nextPid    int
	mem        *MemoryManager
}

// Run executes prog. args are command-line arguments for main(); stdin/stdout
// are the default console streams for io.
// Run executes prog; see runWithInterp.
func Run(prog *Program, filename string, args []string, stdin io.Reader, stdout io.Writer) error {
	_, err := runWithInterp(prog, filename, args, stdin, stdout)
	return err
}

// runWithInterp 执行 prog 并返回解释器（测试可观测内存管理器等内部状态）。
func runWithInterp(prog *Program, filename string, args []string, stdin io.Reader, stdout io.Writer) (*interp, error) {
	if err := Typecheck(prog); err != nil {
		return nil, err
	}
	in := &interp{
		fns:        map[string]*Func{},
		sigs:       map[string]*signDef{},
		builtins:   map[string]builtinFn{},
		structs:    map[string]*StructDef{},
		interfaces: map[string]*InterfaceDef{},
		impls:      map[string]*ImplDef{},
		tasks:      map[int]*Task{},
		mem:        NewMemoryManager(),
	}
	for _, f := range prog.Funcs {
		if _, dup := in.fns[f.Name]; dup {
			return nil, fmt.Errorf("CompileError: duplicate function %q", f.Name)
		}
		in.fns[f.Name] = &Func{Name: f.Name, Params: f.Params, Ret: f.Ret, Body: f.Body, Pos: f.Pos}
	}
	for _, s := range prog.Structs {
		if _, dup := in.structs[s.Name]; dup {
			return nil, fmt.Errorf("CompileError: duplicate struct %q", s.Name)
		}
		def := &StructDef{Name: s.Name, Types: map[string]string{}}
		for _, m := range s.Members {
			def.Types[m.Name] = m.Type
		}
		in.structs[s.Name] = def
	}
	for _, i := range prog.Interfaces {
		if _, dup := in.interfaces[i.Name]; dup {
			return nil, fmt.Errorf("CompileError: duplicate interface %q", i.Name)
		}
		in.interfaces[i.Name] = &InterfaceDef{Name: i.Name, Methods: i.Methods}
	}
	for _, im := range prog.Impls {
		def, ok := in.impls[im.Type]
		if !ok {
			def = &ImplDef{Type: im.Type, Iface: im.Iface, TypeParams: im.TypeParams, Methods: map[string]*Func{}, SelfMethods: map[string]*Func{}}
			in.impls[im.Type] = def
		} else {
			if def.Iface == "" && im.Iface != "" {
				def.Iface = im.Iface
			}
			if len(def.TypeParams) == 0 {
				def.TypeParams = im.TypeParams
			}
		}
		for _, m := range im.Methods {
			if len(m.Params) > 0 && m.Params[0].Name == "self" && m.Params[0].Type == "" {
				m.Params[0].Type = im.Type
			}
			fn := &Func{Name: m.Name, Params: m.Params, Ret: m.Ret, Body: m.Body, Pos: m.Pos}
			if len(m.Params) > 0 && m.Params[0].Name == "self" {
				if _, dup := def.SelfMethods[fn.Name]; dup {
					return nil, fmt.Errorf("CompileError: duplicate method %q on %s", fn.Name, im.Type)
				}
				def.SelfMethods[fn.Name] = fn
			} else {
				if _, dup := def.Methods[fn.Name]; dup {
					return nil, fmt.Errorf("CompileError: duplicate method %q on %s", fn.Name, im.Type)
				}
				def.Methods[fn.Name] = fn
			}
		}
	}
	in.registerIOBuiltins()

	if prog.Kind == "library" {
		return nil, fmt.Errorf("RunError: #error (\"cannot run a library\"): program library; 编译为库，不可运行")
	}
	mainFn, ok := in.fns["main"]
	if !ok {
		return nil, fmt.Errorf("CompileError: no main function found (expected: func main(io IOStream, ...))")
	}
	ioObj := &IOStream{In: stdin, Out: stdout, rd: bufio.NewReader(stdin)}
	env := envTable()
	argList := NewList()
	for _, a := range args {
		argList.Append(StrV(a))
	}
	// main's injected params, in fixed order: io, env, args (spec §8).
	var mainArgs []Value
	switch len(mainFn.Params) {
	case 1:
		mainArgs = []Value{ioObj}
	case 2:
		mainArgs = []Value{ioObj, env}
	case 3:
		mainArgs = []Value{ioObj, env, argList}
	default:
		return nil, fmt.Errorf("CompileError: main must take 1-3 params in order (io IOStream, env HashTable<String,String>, args List<String>), got %d", len(mainFn.Params))
	}
	ctx := NewExecCtx(mainFn, mainArgs, mainFn.Pos)
	return in, in.execute(ctx)
}

// zeroInstance 构造结构体零值实例（成员按类型注解取零值）。
func (in *interp) zeroInstance(def *StructDef) *StructValue {
	sv := &StructValue{SType: def.Name, Fields: map[string]Value{}}
	for name, typ := range def.Types {
		sv.Fields[name] = in.zeroValue(typ)
	}
	return sv
}

// baseTypeName 取泛型实例类型注解的基名（node<int> → node）。
func baseTypeName(typ string) string {
	if i := strings.Index(typ, "<"); i >= 0 {
		return typ[:i]
	}
	return typ
}

// zeroValue 按类型注解生成零值。
func (in *interp) zeroValue(typ string) Value {
	if strings.HasSuffix(typ, "&") {
		return NilV{} // 指针零值 = null
	}
	if d, ok := in.structs[baseTypeName(typ)]; ok {
		return in.zeroInstance(d)
	}
	switch baseTypeName(typ) {
	case "int", "long", "char":
		return IntV(0)
	case "float", "double":
		return FloatV(0)
	case "String":
		return StrV("")
	case "bool":
		return BoolV(false)
	}
	if strings.Contains(typ, "List") || strings.Contains(typ, "Array") {
		return NewList()
	}
	if strings.Contains(typ, "HashTable") {
		return NewHashTable()
	}
	return NilV{}
}

func envTable() *HashTable {
	h := NewHashTable()
	for _, kv := range os.Environ() {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) == 2 {
			h.m[hashKey(StrV(parts[0]))] = StrV(parts[1])
		}
	}
	return h
}

// execute runs a execCtx's function body, filling Tail and Log.
func (in *interp) execute(ctx *execCtx) error {
	if ctx.executed {
		return &RunError{Msg: fmt.Sprintf("RuntimeError: execCtx for %s already executed", ctx.Fn.Name), Pos: ctx.pos, Ctx: ctx}
	}
	ctx.executed = true

	sc := newScope(nil)
	fn := ctx.Fn
	for i, p := range fn.Params {
		v := ctx.Args[i]
		if isCopydType(p.Type) {
			v = deepCopy(v)
		}
		if err := sc.declare(p.Name, v, p.Pos); err != nil {
			return err
		}
	}
	if err := in.execBlock(fn.Body, sc, ctx); err != nil {
		if errors.Is(err, errReturn) {
			return nil
		}
		return err
	}
	return nil
}

func (in *interp) execBlock(b *Block, sc *scope, ctx *execCtx) error {
	for _, st := range b.Stmts {
		if err := in.execStmt(st, sc, ctx); err != nil {
			return err
		}
	}
	return nil
}

func (in *interp) execStmt(st Stmt, sc *scope, ctx *execCtx) error {
	switch s := st.(type) {
	case *ExprStmt:
		_, err := in.evalExpr(s.X, sc, ctx)
		return err
	case *LogStmt:
		// log 记录日志并结束函数（返回任意值，默认 nil）
		v, err := in.evalExpr(s.X, sc, ctx)
		if err != nil {
			return err
		}
		ctx.Log.Append(StrV(v.String()))
		ctx.result = NilV{}
		return errReturn
	case *TryStmt:
		// try/catch：try 块出错（非 return）时把错误装入 catch 变量（interface{}），执行 catch 块
		err := in.execBlock(s.Try, sc, ctx)
		if err != nil {
			if errors.Is(err, errReturn) {
				return err
			}
			inner := newScope(sc)
			_ = inner.declare(s.CatchVar, StrV(err.Error()), s.Pos) // 错误装入声明类型（interface{} 等），自由系统
			if cerr := in.execBlock(s.Catch, inner, ctx); cerr != nil {
				return cerr
			}
		}
		return nil
	case *ReturnStmt:
		if s.X != nil {
			v, err := in.evalExpr(s.X, sc, ctx)
			if err != nil {
				return err
			}
			ctx.result = v
		}
		return errReturn
	case *IfStmt:
		c, err := in.evalExpr(s.Cond, sc, ctx)
		if err != nil {
			return err
		}
		b, err := truthy(c)
		if err != nil {
			return err
		}
		if b {
			return in.execBlock(s.Then, sc, ctx)
		}
		if s.Else != nil {
			return in.execBlock(s.Else, sc, ctx)
		}
		return nil
	case *WhileStmt:
		for {
			c, err := in.evalExpr(s.Cond, sc, ctx)
			if err != nil {
				return err
			}
			b, err := truthy(c)
			if err != nil {
				return err
			}
			if !b {
				return nil
			}
			if err := in.execBlock(s.Body, sc, ctx); err != nil {
				return err
			}
		}
	case *ForStmt:
		v, err := in.evalExpr(s.Iter, sc, ctx)
		if err != nil {
			return err
		}
		l, ok := v.(*List)
		if !ok {
			return &RunError{Msg: fmt.Sprintf("TypeError: for-in requires a List, got %s", v.TypeName()), Pos: s.Pos, Ctx: ctx}
		}
		inner := newScope(sc)
		for l.Head() != l.Tail() {
			item, err := l.Next()
			if err != nil {
				return &RunError{Msg: err.Error(), Pos: s.Pos, Ctx: ctx}
			}
			inner.vars[s.Var] = item
			if err := in.execBlock(s.Body, inner, ctx); err != nil {
				return err
			}
		}
		return nil
	case *DeclStmt:
		var v Value = NilV{}
		if s.Init != nil {
			var err error
			v, err = in.evalExpr(s.Init, sc, ctx)
			if err != nil {
				return err
			}
		} else if def, ok := in.structs[baseTypeName(s.Type)]; ok {
			v = in.zeroInstance(def)
		}
		return sc.declare(s.Name, v, s.Pos)
	case *AssignStmt:
		v, err := in.evalExpr(s.X, sc, ctx)
		if err != nil {
			return err
		}
		switch t := s.Target.(type) {
		case *Ident:
			return sc.set(t.Name, v, t.Pos)
		case *MemberExpr:
			obj, err := in.evalExpr(t.X, sc, ctx)
			if err != nil {
				return err
			}
			if _, isNil := obj.(NilV); isNil {
				return &RunError{Msg: "NullPointerError: assignment through null pointer", Pos: s.Pos, Ctx: ctx}
			}
			if c, ok := obj.(*CopydValue); ok {
				obj = c.V
			}
			sv, ok := obj.(*StructValue)
			if !ok {
				return &RunError{Msg: fmt.Sprintf("TypeError: cannot assign member of %s", obj.TypeName()), Pos: s.Pos, Ctx: ctx}
			}
			if _, exists := sv.Fields[t.Name]; !exists {
				return &RunError{Msg: fmt.Sprintf("TypeError: no member %q on %s", t.Name, sv.SType), Pos: t.Pos, Ctx: ctx}
			}
			sv.Fields[t.Name] = v
			return nil
		}
		return &RunError{Msg: "TypeError: unsupported assignment target", Pos: s.Pos, Ctx: ctx}
	}
	return nil
}

func (in *interp) evalExpr(e Expr, sc *scope, ctx *execCtx) (Value, error) {
	switch x := e.(type) {
	case *IntLit:
		return IntV(x.V), nil
	case *FloatLit:
		return FloatV(x.V), nil
	case *StrLit:
		return StrV(x.V), nil
	case *BoolLit:
		return BoolV(x.V), nil
	case *NullLit:
		return NilV{}, nil
	case *StructLit:
		sv := &StructValue{SType: ".", Fields: map[string]Value{}}
		for _, f := range x.Fields {
			v, err := in.evalExpr(f.X, sc, ctx)
			if err != nil {
				return nil, err
			}
			sv.Fields[f.Name] = v
		}
		return sv, nil
	case *Ident:
		if x.Name == "true" {
			return BoolV(true), nil
		}
		if x.Name == "false" {
			return BoolV(false), nil
		}
		if x.Name == "memory" {
			return globalMemory, nil
		}
		if x.Name == "taskm" {
			return globalTaskm, nil
		}
		v, err := sc.get(x.Name, x.Pos)
		if err == nil {
			return v, nil
		}
		if fn, ok := in.fns[x.Name]; ok {
			return &FuncValue{fn: fn}, nil
		}
		return nil, err
	case *ListLit:
		l := NewList()
		for _, it := range x.Items {
			v, err := in.evalExpr(it, sc, ctx)
			if err != nil {
				return nil, err
			}
			l.Append(v)
		}
		return l, nil
	case *UnOp:
		v, err := in.evalExpr(x.X, sc, ctx)
		if err != nil {
			return nil, err
		}
		switch x.Op {
		case "*":
			l, ok := v.(*List)
			if !ok {
				return nil, &RunError{Msg: fmt.Sprintf("TypeError: '*' requires a List, got %s", v.TypeName()), Pos: x.Pos, Ctx: ctx}
			}
			item, err := l.Peek()
			if err != nil {
				return nil, &RunError{Msg: err.Error(), Pos: x.Pos, Ctx: ctx}
			}
			return item, nil
		case "-":
			switch n := v.(type) {
			case IntV:
				return wrapI32(-int64(n)), nil
			case FloatV:
				return -n, nil
			}
			return nil, &RunError{Msg: fmt.Sprintf("TypeError: unary '-' requires a number, got %s", v.TypeName()), Pos: x.Pos, Ctx: ctx}
		case "!":
			b, err := truthy(v)
			if err != nil {
				return nil, &RunError{Msg: err.Error(), Pos: x.Pos, Ctx: ctx}
			}
			return BoolV(!b), nil
		}
		return nil, &RunError{Msg: "internal: unknown unary operator " + x.Op, Pos: x.Pos, Ctx: ctx}
	case *BinOp:
		l, err := in.evalExpr(x.L, sc, ctx)
		if err != nil {
			return nil, err
		}
		if x.Op == "&&" {
			lb, err := truthy(l)
			if err != nil {
				return nil, &RunError{Msg: err.Error(), Pos: x.Pos, Ctx: ctx}
			}
			if !lb {
				return BoolV(false), nil
			}
			r, err := in.evalExpr(x.R, sc, ctx)
			if err != nil {
				return nil, err
			}
			rb, err := truthy(r)
			if err != nil {
				return nil, &RunError{Msg: err.Error(), Pos: x.Pos, Ctx: ctx}
			}
			return BoolV(rb), nil
		}
		if x.Op == "||" {
			lb, err := truthy(l)
			if err != nil {
				return nil, &RunError{Msg: err.Error(), Pos: x.Pos, Ctx: ctx}
			}
			if lb {
				return BoolV(true), nil
			}
			r, err := in.evalExpr(x.R, sc, ctx)
			if err != nil {
				return nil, err
			}
			rb, err := truthy(r)
			if err != nil {
				return nil, &RunError{Msg: err.Error(), Pos: x.Pos, Ctx: ctx}
			}
			return BoolV(rb), nil
		}
		r, err := in.evalExpr(x.R, sc, ctx)
		if err != nil {
			return nil, err
		}
		return binOp(x.Op, l, r, x.Pos, ctx)
	case *CallExpr:
		return in.evalCall(x, sc, ctx)
	case *MemberExpr:
		obj, err := in.evalExpr(x.X, sc, ctx)
		if err != nil {
			return nil, err
		}
		return evalMember(obj, x.Name, x.Pos, ctx)
	case *ScopeCall:
		return in.evalScopeCall(x, sc, ctx)
	case *IndexExpr:
		v, err := in.evalExpr(x.X, sc, ctx)
		if err != nil {
			return nil, err
		}
		i, err := in.evalExpr(x.Idx, sc, ctx)
		if err != nil {
			return nil, err
		}
		l, ok := v.(*List)
		if !ok {
			return nil, &RunError{Msg: fmt.Sprintf("TypeError: indexing requires a List, got %s", v.TypeName()), Pos: x.Pos, Ctx: ctx}
		}
		iv, ok := i.(IntV)
		if !ok {
			return nil, &RunError{Msg: "TypeError: index must be int", Pos: x.Pos, Ctx: ctx}
		}
		item, err := l.Get(int(iv))
		if err != nil {
			return nil, &RunError{Msg: err.Error(), Pos: x.Pos, Ctx: ctx}
		}
		return item, nil
	}
	return nil, &RunError{Msg: "internal: unknown expression node", Ctx: ctx}
}

// evalMember reads a plain member (no call) — e.g. execCtx.head/tail/log.
func evalMember(obj Value, name string, pos Pos, ctx *execCtx) (Value, error) {
	if _, isNil := obj.(NilV); isNil {
		return nil, &RunError{Msg: "NullPointerError: dereference of null pointer", Pos: pos, Ctx: ctx}
	}
	if c, ok := obj.(*CopydValue); ok {
		return evalMember(c.V, name, pos, ctx) // Copyd 透传
	}
	if o, ok := obj.(*StructValue); ok {
		if v, exists := o.Fields[name]; exists {
			return v, nil
		}
		return nil, &RunError{Msg: fmt.Sprintf("TypeError: no member %q on %s", name, obj.TypeName()), Pos: pos, Ctx: ctx}
	}
	return nil, &RunError{Msg: fmt.Sprintf("TypeError: no member %q on %s", name, obj.TypeName()), Pos: pos, Ctx: ctx}
}

func (in *interp) evalCall(c *CallExpr, sc *scope, ctx *execCtx) (Value, error) {
	// Signature wrapper: f(args) @sign(prefix) -> sign::call(prefix)(ctx) (spec §6).
	if c.Sign != nil {
		id, ok := c.Fn.(*Ident)
		if !ok {
			return nil, &RunError{Msg: "TypeError: a signature can only wrap a direct function call", Pos: c.Pos, Ctx: ctx}
		}
		fn, ok := in.fns[id.Name]
		if !ok {
			return nil, &RunError{Msg: fmt.Sprintf("CompileError: undeclared function %q", id.Name), Pos: id.Pos, Ctx: ctx}
		}
		argVals, err := in.evalArgs(c.Args, sc, ctx)
		if err != nil {
			return nil, err
		}
		if len(argVals) != len(fn.Params) {
			return nil, &RunError{Msg: fmt.Sprintf("CompileError: %s expects %d args, got %d", fn.Name, len(fn.Params), len(argVals)), Pos: id.Pos, Ctx: ctx}
		}
		// v2 签名：f(args) @mb(prefix) ≡ mb.call(prefix)(.{in, out})
		// 1) mb 是变量（Sign 实例）；2) prefix 中放被包装的原函数 fn；3) 记录 .{in: List(args), out: nil}
		mbv, err := in.evalExpr(&Ident{Name: c.Sign.Name, Pos: c.Pos}, sc, ctx)
		if err != nil {
			return nil, err
		}
		// 构造 prefix：原函数在 prefix 中（外加 @ 处的显式参数）
		prefix := &StructValue{SType: ".", Fields: map[string]Value{"fn": &FuncValue{fn: fn}}}
		for _, a := range c.Sign.Args {
			av, err := in.evalExpr(a, sc, ctx)
			if err != nil {
				return nil, err
			}
			prefix.Fields["prefix"] = av
		}
		// 记录 .{in, out}
		inList := NewList()
		for _, a := range argVals {
			inList.Append(a)
		}
		rec := &StructValue{SType: ".", Fields: map[string]Value{"in": inList, "out": NilV{}}}
		if _, err := in.callMethod(mbv, "call", []Value{prefix, rec}, ctx, c.Pos); err != nil {
			return nil, err
		}
		return rec.Fields["out"], nil
	}

	// Method call: obj.name(args).
	if m, ok := c.Fn.(*MemberExpr); ok {
		obj, err := in.evalExpr(m.X, sc, ctx)
		if err != nil {
			return nil, err
		}
		args, err := in.evalArgs(c.Args, sc, ctx)
		if err != nil {
			return nil, err
		}
		return in.callMethod(obj, m.Name, args, ctx, m.Pos)
	}

	id, ok := c.Fn.(*Ident)
	if !ok {
		return nil, &RunError{Msg: "TypeError: this expression is not callable", Pos: c.Pos, Ctx: ctx}
	}
	argVals, err := in.evalArgs(c.Args, sc, ctx)
	if err != nil {
		return nil, err
	}
	if fn, ok := in.fns[id.Name]; ok {
		return in.callFunc(fn, argVals, id.Pos)
	}
	if b, ok := in.builtins[id.Name]; ok {
		return b(argVals, id.Pos, ctx)
	}
	return nil, &RunError{Msg: fmt.Sprintf("CompileError: undeclared function %q", id.Name), Pos: id.Pos, Ctx: ctx}
}

// callFunc：v2 —— 函数调用返回 return 的结果（log 结束则返回 nil）。
func (in *interp) callFunc(fn *Func, args []Value, pos Pos) (Value, error) {
	if len(args) != len(fn.Params) {
		return nil, &RunError{Msg: fmt.Sprintf("CompileError: %s expects %d args, got %d", fn.Name, len(fn.Params), len(args)), Pos: pos}
	}
	for i, p := range fn.Params {
		if isCopydType(p.Type) {
			args[i] = &CopydValue{V: deepCopy(args[i])}
		}
	}
	ctx := NewExecCtx(fn, args, pos)
	if err := in.execute(ctx); err != nil {
		return nil, err
	}
	if ctx.result == nil {
		return NilV{}, nil // 未 return 的路径（log 结束等）返回 nil
	}
	return ctx.result, nil
}

func (in *interp) evalArgs(args []Expr, sc *scope, ctx *execCtx) ([]Value, error) {
	vals := make([]Value, 0, len(args))
	for _, a := range args {
		v, err := in.evalExpr(a, sc, ctx)
		if err != nil {
			return nil, err
		}
		vals = append(vals, v)
	}
	return vals, nil
}

func (in *interp) callMethod(obj Value, name string, args []Value, ctx *execCtx, pos Pos) (Value, error) {
	switch o := obj.(type) {
	case *List:
		switch name {
		case "head":
			if err := wantArity(name, 0, len(args), pos, ctx); err != nil {
				return nil, err
			}
			return IntV(o.Head()), nil
		case "tail":
			if err := wantArity(name, 0, len(args), pos, ctx); err != nil {
				return nil, err
			}
			return IntV(o.Tail()), nil
		case "size":
			if err := wantArity(name, 0, len(args), pos, ctx); err != nil {
				return nil, err
			}
			return IntV(o.Size()), nil
		case "next":
			if err := wantArity(name, 0, len(args), pos, ctx); err != nil {
				return nil, err
			}
			v, err := o.Next()
			if err != nil {
				return nil, &RunError{Msg: err.Error(), Pos: pos, Ctx: ctx}
			}
			return v, nil
		case "reset":
			if err := wantArity(name, 0, len(args), pos, ctx); err != nil {
				return nil, err
			}
			o.Reset()
			return o, nil
		case "append":
			if err := wantArity(name, 1, len(args), pos, ctx); err != nil {
				return nil, err
			}
			o.Append(args[0])
			return NilV{}, nil
		case "appendAll":
			if err := wantArity(name, 1, len(args), pos, ctx); err != nil {
				return nil, err
			}
			l, ok := args[0].(*List)
			if !ok {
				return nil, &RunError{Msg: fmt.Sprintf("TypeError: appendAll requires a List, got %s", args[0].TypeName()), Pos: pos, Ctx: ctx}
			}
			o.AppendAll(l)
			return NilV{}, nil
		case "toString":
			if err := wantArity(name, 0, len(args), pos, ctx); err != nil {
				return nil, err
			}
			return StrV(o.String()), nil
		case "__sort__":
			if err := wantArity(name, 0, len(args), pos, ctx); err != nil {
				return nil, err
			}
			if err := o.sortInPlace(); err != nil {
				return nil, &RunError{Msg: err.Error(), Pos: pos, Ctx: ctx}
			}
			return o, nil
		}
	case *HashTable:
		switch name {
		case "put":
			if err := wantArity(name, 2, len(args), pos, ctx); err != nil {
				return nil, err
			}
			o.Put(args[0], args[1])
			return NilV{}, nil
		case "get":
			if err := wantArity(name, 1, len(args), pos, ctx); err != nil {
				return nil, err
			}
			v, ok := o.Get(args[0])
			if !ok || v == nil {
				return NilV{}, nil
			}
			return v, nil
		case "contains":
			if err := wantArity(name, 1, len(args), pos, ctx); err != nil {
				return nil, err
			}
			return BoolV(o.Contains(args[0])), nil
		case "remove":
			if err := wantArity(name, 1, len(args), pos, ctx); err != nil {
				return nil, err
			}
			o.Remove(args[0])
			return NilV{}, nil
		case "size":
			if err := wantArity(name, 0, len(args), pos, ctx); err != nil {
				return nil, err
			}
			return IntV(o.Size()), nil
		}
	case *CopydValue:
		if name == "ptr" {
			if err := wantArity(name, 0, len(args), pos, ctx); err != nil {
				return nil, err
			}
			return o.V, nil // .ptr() 取出 Copyd 包装的地址
		}
		return in.callMethod(o.V, name, args, ctx, pos) // Copyd 透传
	case *TaskManager:
		switch name {
		case "spawn":
			// v2：taskm.spawn() 参数为空，创建线程并直接返回 pid
			if len(args) != 0 {
				return nil, wantArity("taskm.spawn", 0, len(args), pos, ctx)
			}
			pid := in.newThread()
			return IntV(pid), nil
		case "merge":
			// v2：taskm.merge(pid, fn, args...) 把函数并入线程 pid 执行
			if len(args) < 2 {
				return nil, wantArity("taskm.merge", 2, len(args), pos, ctx)
			}
			pid, ok := args[0].(IntV)
			if !ok {
				return nil, &RunError{Msg: "TypeError: taskm.merge first arg must be a pid (int)", Pos: pos, Ctx: ctx}
			}
			t, ok := in.lookupTask(int(pid))
			if !ok {
				return nil, &RunError{Msg: fmt.Sprintf("RuntimeError: unknown task pid %d", int(pid)), Pos: pos, Ctx: ctx}
			}
			fn, err := lookupFunc(args[1], in)
			if err != nil {
				return nil, &RunError{Msg: err.Error(), Pos: pos, Ctx: ctx}
			}
			if len(args)-2 != len(fn.Params) {
				return nil, &RunError{Msg: fmt.Sprintf("CompileError: %s expects %d args, got %d", fn.Name, len(fn.Params), len(args)-2), Pos: pos, Ctx: ctx}
			}
			if err := in.runOnThread(t, fn, args[2:], pos); err != nil {
				return nil, err
			}
			return NilV{}, nil
		case "block":
			// v2：taskm.block(pid) 返回 void（只等待线程空闲）
			if len(args) != 1 {
				return nil, wantArity("taskm.block", 1, len(args), pos, ctx)
			}
			t, err := in.taskArg(args[0], pos, ctx)
			if err != nil {
				return nil, err
			}
			in.taskMu.Lock()
			busy := t.Busy
			ch := t.doneCh
			in.taskMu.Unlock()
			if busy {
				<-ch
			}
			if t.err != nil {
				return nil, t.err
			}
			return NilV{}, nil // block 返回 void
		case "done":
			// done(pid)：该协程的线程是否空闲（没有函数占用）；v0.1 = 协程是否结束
			if len(args) != 1 {
				return nil, wantArity("taskm.done", 1, len(args), pos, ctx)
			}
			pid, ok := args[0].(IntV)
			if !ok {
				return nil, &RunError{Msg: fmt.Sprintf("TypeError: taskm.done requires a pid (int), got %s", args[0].TypeName()), Pos: pos, Ctx: ctx}
			}
			t, ok := in.lookupTask(int(pid))
			if !ok {
				return nil, &RunError{Msg: fmt.Sprintf("RuntimeError: unknown task pid %d", int(pid)), Pos: pos, Ctx: ctx}
			}
			in.taskMu.Lock()
			idle := !t.Busy
			in.taskMu.Unlock()
			return BoolV(idle), nil // done = 线程是否空闲（没有函数占用）
		case "channel":
			cap := 1024 // 默认容量（spec §14.2）
			if len(args) == 1 {
				n, ok := args[0].(IntV)
				if !ok || n < 1 {
					return nil, &RunError{Msg: "TypeError: taskm.channel(n) requires a positive int capacity", Pos: pos, Ctx: ctx}
				}
				cap = int(n)
			} else if len(args) != 0 {
				return nil, wantArity("taskm.channel", 0, len(args), pos, ctx)
			}
			return NewChannel(cap), nil
		}
	case *MemorizeBuffer:
		if name == "call" {
			if len(args) != 2 {
				return nil, wantArity("mb.call", 2, len(args), pos, ctx)
			}
			pref, ok := args[0].(*StructValue)
			if !ok {
				return nil, &RunError{Msg: "TypeError: mb.call 第一参数必须是 prefix 记录", Pos: pos, Ctx: ctx}
			}
			recv, ok := args[1].(*StructValue)
			if !ok {
				return nil, &RunError{Msg: "TypeError: mb.call 第二参数必须是 .{in,out} 记录", Pos: pos, Ctx: ctx}
			}
			return in.memorizeBufferCall(o, pref, recv, pos, ctx)
		}
	case *Memory:
		switch name {
		case "compact":
			if err := wantArity(name, 0, len(args), pos, ctx); err != nil {
				return nil, err
			}
			// compact() 根本不返回（spec §14.1）；实际清理无人占用的 block
			in.mem.Compact()
			return NilV{}, nil
		case "setBlock":
			if err := wantArity(name, 1, len(args), pos, ctx); err != nil {
				return nil, err
			}
			n, ok := args[0].(IntV)
			if !ok || n < 1 {
				return nil, &RunError{Msg: "TypeError: setBlock(n) requires a positive int block size", Pos: pos, Ctx: ctx}
			}
			o.BlockSize = int(n) // 动态调整 block 脏标记粒度
			return NilV{}, nil
		}
	case *Task:
		if name == "done" {
			if err := wantArity(name, 0, len(args), pos, ctx); err != nil {
				return nil, err
			}
			select {
			case <-o.doneCh:
				return BoolV(true), nil
			default:
				return BoolV(false), nil
			}
		}
	case *Channel:
		switch name {
		case "send":
			if err := wantArity(name, 1, len(args), pos, ctx); err != nil {
				return nil, err
			}
			o.ch <- args[0]
			return NilV{}, nil
		case "recv":
			if err := wantArity(name, 0, len(args), pos, ctx); err != nil {
				return nil, err
			}
			return <-o.ch, nil
		}
	case *IOStream:
		switch name {
		case "println":
			return ioPrintln(o, args, true, pos, ctx)
		case "print":
			return ioPrintln(o, args, false, pos, ctx)
		case "setIn":
			if err := wantArity(name, 1, len(args), pos, ctx); err != nil {
				return nil, err
			}
			if err := setInput(o, args[0]); err != nil {
				return nil, &RunError{Msg: err.Error(), Pos: pos, Ctx: ctx}
			}
			return NilV{}, nil
		case "setOut":
			if err := wantArity(name, 1, len(args), pos, ctx); err != nil {
				return nil, err
			}
			if err := setOutput(o, args[0]); err != nil {
				return nil, &RunError{Msg: err.Error(), Pos: pos, Ctx: ctx}
			}
			return NilV{}, nil
		case "readln":
			if err := wantArity(name, 0, len(args), pos, ctx); err != nil {
				return nil, err
			}
			o.mu.RLock()
			line, err := o.rd.ReadString('\n')
			o.mu.RUnlock()
			if err != nil && line == "" {
				return nil, &RunError{Msg: "IOError: " + err.Error(), Pos: pos, Ctx: ctx}
			}
			return StrV(strings.TrimRight(line, "\r\n")), nil
		}
	case *StructValue:
		def, ok := in.impls[o.SType]
		if !ok {
			return nil, &RunError{Msg: fmt.Sprintf("TypeError: type %s has no impl", o.SType), Pos: pos, Ctx: ctx}
		}
		if fn, ok := def.SelfMethods[name]; ok {
			callArgs := append([]Value{o}, args...)
			return in.callFunc(fn, callArgs, pos)
		}
		if _, ok := def.Methods[name]; ok {
			return nil, &RunError{Msg: fmt.Sprintf("TypeError: %s.%s is a static method; call it via %s::%s(...)", o.SType, name, o.SType, name), Pos: pos, Ctx: ctx}
		}
	}
	return nil, &RunError{Msg: fmt.Sprintf("TypeError: no method %q on %s", name, obj.TypeName()), Pos: pos, Ctx: ctx}
}

func wantArity(name string, want, got int, pos Pos, ctx *execCtx) error {
	if want != got {
		return &RunError{Msg: fmt.Sprintf("CompileError: %s() expects %d args, got %d", name, want, got), Pos: pos, Ctx: ctx}
	}
	return nil
}

func ioPrintln(s *IOStream, args []Value, newline bool, pos Pos, ctx *execCtx) (Value, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	parts := make([]string, len(args))
	for i, a := range args {
		parts[i] = a.String()
	}
	out := strings.Join(parts, " ")
	if newline {
		out += "\n"
	}
	if _, err := io.WriteString(s.Out, out); err != nil {
		return nil, &RunError{Msg: "IOError: " + err.Error(), Pos: pos, Ctx: ctx}
	}
	return NilV{}, nil
}

func setInput(s *IOStream, v Value) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch t := v.(type) {
	case StrV:
		f, err := os.Open(string(t))
		if err != nil {
			return fmt.Errorf("IOError: cannot open %q: %v", string(t), err)
		}
		s.In = f
		s.rd = bufio.NewReader(f)
		return nil
	case *InputStream:
		s.In = t.R
		s.rd = bufio.NewReader(t.R)
		return nil
	}
	return fmt.Errorf("TypeError: setIn requires a path string or an InputStream, got %s", v.TypeName())
}

func setOutput(s *IOStream, v Value) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch t := v.(type) {
	case StrV:
		f, err := os.Create(string(t))
		if err != nil {
			return fmt.Errorf("IOError: cannot create %q: %v", string(t), err)
		}
		s.Out = f
		return nil
	case *OutputStream:
		s.Out = t.W
		return nil
	}
	return fmt.Errorf("TypeError: setOut requires a path string or an OutputStream, got %s", v.TypeName())
}

func (in *interp) evalScopeCall(x *ScopeCall, sc *scope, ctx *execCtx) (Value, error) {
	args, err := in.evalArgs(x.Args, sc, ctx)
	if err != nil {
		return nil, err
	}
	switch x.Scope {
	case "memorize":
		if x.Name == "new" && len(args) == 0 {
			return &MemorizeBuffer{Table: NewHashTable()}, nil
		}
		return nil, &RunError{Msg: fmt.Sprintf("TypeError: memorize has no static method %q", x.Name), Pos: x.Pos, Ctx: ctx}
	case "HashTable":
		if x.Name == "new" && len(args) == 0 {
			return NewHashTable(), nil
		}
		return nil, &RunError{Msg: fmt.Sprintf("TypeError: HashTable has no static method %q", x.Name), Pos: x.Pos, Ctx: ctx}
	case "List":
		if x.Name == "new" && len(args) == 0 {
			return NewList(), nil
		}
		return nil, &RunError{Msg: fmt.Sprintf("TypeError: List has no static method %q", x.Name), Pos: x.Pos, Ctx: ctx}
	case "GlobalMemory":
		switch x.Name {
		case "compact":
			if len(args) != 0 {
				return nil, wantArity("GlobalMemory::compact", 0, len(args), x.Pos, ctx)
			}
			in.mem.Compact()
			return NilV{}, nil
		case "setBlock":
			if len(args) != 1 {
				return nil, wantArity("GlobalMemory::setBlock", 1, len(args), x.Pos, ctx)
			}
			n, ok := args[0].(IntV)
			if !ok || n < 1 {
				return nil, &RunError{Msg: "TypeError: GlobalMemory::setBlock(n) requires a positive int", Pos: x.Pos, Ctx: ctx}
			}
			globalMemory.BlockSize = int(n)
			return NilV{}, nil
		}
		return nil, &RunError{Msg: fmt.Sprintf("TypeError: GlobalMemory has no static method %q", x.Name), Pos: x.Pos, Ctx: ctx}
	case "taskm":
		// taskm 是全局变量：正确语法是 taskm.spawn(...) / taskm.block(...) 等
		return nil, &RunError{Msg: "TypeError: taskm is a global variable — use taskm.spawn(...) / taskm.block(pid) / taskm.done(pid) / taskm.merge(pid) / taskm.channel([n])", Pos: x.Pos, Ctx: ctx}
	case "IO":
		switch x.Name {
		case "setIn":
			if len(args) != 2 {
				return nil, wantArity("IO::setIn", 2, len(args), x.Pos, ctx)
			}
			ioObj, ok := args[0].(*IOStream)
			if !ok {
				return nil, &RunError{Msg: fmt.Sprintf("TypeError: IO::setIn's first arg must be an IOStream, got %s", args[0].TypeName()), Pos: x.Pos, Ctx: ctx}
			}
			if err := setInput(ioObj, args[1]); err != nil {
				return nil, &RunError{Msg: err.Error(), Pos: x.Pos, Ctx: ctx}
			}
			return NilV{}, nil
		case "setOut":
			if len(args) != 2 {
				return nil, wantArity("IO::setOut", 2, len(args), x.Pos, ctx)
			}
			ioObj, ok := args[0].(*IOStream)
			if !ok {
				return nil, &RunError{Msg: fmt.Sprintf("TypeError: IO::setOut's first arg must be an IOStream, got %s", args[0].TypeName()), Pos: x.Pos, Ctx: ctx}
			}
			if err := setOutput(ioObj, args[1]); err != nil {
				return nil, &RunError{Msg: err.Error(), Pos: x.Pos, Ctx: ctx}
			}
			return NilV{}, nil
		}
		return nil, &RunError{Msg: fmt.Sprintf("TypeError: IO has no static method %q", x.Name), Pos: x.Pos, Ctx: ctx}
	}
	if def, ok := in.impls[x.Scope]; ok {
		if fn, ok := def.Methods[x.Name]; ok {
			return in.callFunc(fn, args, x.Pos)
		}
		return nil, &RunError{Msg: fmt.Sprintf("TypeError: %s has no static method %q", x.Scope, x.Name), Pos: x.Pos, Ctx: ctx}
	}
	return nil, &RunError{Msg: fmt.Sprintf("CompileError: unknown scope %q", x.Scope), Pos: x.Pos, Ctx: ctx}
}

func (in *interp) registerIOBuiltins() {
	in.builtins["FileInputStream"] = func(args []Value, pos Pos, ctx *execCtx) (Value, error) {
		if len(args) != 1 {
			return nil, wantArity("FileInputStream", 1, len(args), pos, ctx)
		}
		p, ok := args[0].(StrV)
		if !ok {
			return nil, &RunError{Msg: fmt.Sprintf("TypeError: FileInputStream requires a path string, got %s", args[0].TypeName()), Pos: pos, Ctx: ctx}
		}
		f, err := os.Open(string(p))
		if err != nil {
			return nil, &RunError{Msg: fmt.Sprintf("IOError: cannot open %q: %v", string(p), err), Pos: pos, Ctx: ctx}
		}
		return &InputStream{R: f}, nil
	}
	in.builtins["FileOutputStream"] = func(args []Value, pos Pos, ctx *execCtx) (Value, error) {
		if len(args) != 1 {
			return nil, wantArity("FileOutputStream", 1, len(args), pos, ctx)
		}
		p, ok := args[0].(StrV)
		if !ok {
			return nil, &RunError{Msg: fmt.Sprintf("TypeError: FileOutputStream requires a path string, got %s", args[0].TypeName()), Pos: pos, Ctx: ctx}
		}
		f, err := os.Create(string(p))
		if err != nil {
			return nil, &RunError{Msg: fmt.Sprintf("IOError: cannot create %q: %v", string(p), err), Pos: pos, Ctx: ctx}
		}
		return &OutputStream{W: f}, nil
	}
	in.builtins["ConsoleInputStream"] = func(args []Value, pos Pos, ctx *execCtx) (Value, error) {
		if len(args) != 0 {
			return nil, wantArity("ConsoleInputStream", 0, len(args), pos, ctx)
		}
		return &InputStream{R: os.Stdin}, nil
	}
	in.builtins["ConsoleOutputStream"] = func(args []Value, pos Pos, ctx *execCtx) (Value, error) {
		if len(args) != 0 {
			return nil, wantArity("ConsoleOutputStream", 0, len(args), pos, ctx)
		}
		return &OutputStream{W: os.Stdout}, nil
	}
}

// registerTask 登记协程，返回其 pid。
func (in *interp) registerTask(t *Task) int {
	in.taskMu.Lock()
	defer in.taskMu.Unlock()
	in.nextPid++
	in.tasks[in.nextPid] = t
	return in.nextPid
}

// lookupTask 按 pid 查找协程。
func (in *interp) lookupTask(pid int) (*Task, bool) {
	in.taskMu.Lock()
	defer in.taskMu.Unlock()
	t, ok := in.tasks[pid]
	return t, ok
}

// newThread 创建线程（taskm.spawn()），返回 pid。
func (in *interp) newThread() int {
	t := &Task{doneCh: make(chan struct{}), BlockID: in.mem.Alloc(globalMemory.BlockSize, 0)}
	pid := in.registerTask(t)
	t.Pid = pid
	return pid
}

// runOnThread 把函数并入线程 pid 执行（taskm.merge）。
func (in *interp) runOnThread(t *Task, fn *Func, args []Value, pos Pos) error {
	in.taskMu.Lock()
	if t.Busy {
		in.taskMu.Unlock()
		return &RunError{Msg: "RuntimeError: thread busy — merge only when idle (taskm.done)", Pos: pos}
	}
	t.Busy = true
	t.doneCh = make(chan struct{})
	in.taskMu.Unlock()

	// 内存 block 归属该线程；结束后自动标记可回收
	blockID := in.mem.Alloc(globalMemory.BlockSize, t.Pid)
	t.BlockID = blockID
	ctx := NewExecCtx(fn, args, pos)
	ctx.Log.mem = in.mem
	ctx.Log.blockID = blockID
	ctx.Log.Append(StrV(fmt.Sprintf("taskm: merged %s(%d) pid=%d", fn.Name, len(args), t.Pid)))
	go func() {
		t.err = in.execute(ctx)
		ctx.Log.Append(StrV("taskm: done"))
		in.mem.ReclaimTask(t.Pid)
		in.taskMu.Lock()
		t.Busy = false
		in.taskMu.Unlock()
		close(t.doneCh)
	}()
	return nil
}

// taskArg 接受 Task 或 pid，返回对应协程。
func (in *interp) taskArg(v Value, pos Pos, ctx *execCtx) (*Task, error) {
	switch a := v.(type) {
	case *Task:
		return a, nil
	case IntV:
		t, ok := in.lookupTask(int(a))
		if !ok {
			return nil, &RunError{Msg: fmt.Sprintf("RuntimeError: unknown task pid %d", int(a)), Pos: pos, Ctx: ctx}
		}
		return t, nil
	}
	return nil, &RunError{Msg: fmt.Sprintf("TypeError: taskm requires a Task or pid, got %s", v.TypeName()), Pos: pos, Ctx: ctx}
}

// lookupFunc resolves a function reference (FuncValue or a name string).
func lookupFunc(v Value, in *interp) (*Func, error) {
	switch t := v.(type) {
	case *FuncValue:
		return t.fn, nil
	case StrV:
		if fn, ok := in.fns[string(t)]; ok {
			return fn, nil
		}
		return nil, fmt.Errorf("CompileError: undeclared function %q", string(t))
	}
	return nil, fmt.Errorf("TypeError: taskm::spawn requires a function reference, got %s", v.TypeName())
}

// memorizeBufferCall 实现内置 memorize 实例的 call(prefix, rec)：
// 按 rec.in 记忆化，执行 prefix.fn，结果写回 rec.out。
func (in *interp) memorizeBufferCall(mb *MemorizeBuffer, prefix *StructValue, rec *StructValue, pos Pos, ctx *execCtx) (Value, error) {
	nv, ok := prefix.Fields["fn"]
	if !ok {
		return nil, &RunError{Msg: "TypeError: memorize prefix 缺少被包装函数 fn", Pos: pos, Ctx: ctx}
	}
	f, ok := nv.(*FuncValue)
	if !ok {
		return nil, &RunError{Msg: "TypeError: prefix.fn 不是函数", Pos: pos, Ctx: ctx}
	}
	inList, ok := rec.Fields["in"].(*List)
	if !ok {
		return nil, &RunError{Msg: "TypeError: rec.in 必须是 List", Pos: pos, Ctx: ctx}
	}
	// 记忆化键：in 参数列表
	if cached, hit := mb.Table.Get(inList); hit {
		rec.Fields["out"] = cached
		return cached, nil
	}
	argVals := make([]Value, 0, inList.Size())
	for i := 0; i < inList.Size(); i++ {
		v, _ := inList.Get(i)
		argVals = append(argVals, v)
	}
	res, err := in.callFunc(f.fn, argVals, pos)
	if err != nil {
		return nil, err
	}
	rec.Fields["out"] = res
	mb.Table.Put(inList, res)
	return res, nil
}

func binOp(op string, l, r Value, pos Pos, ctx *execCtx) (Value, error) {
	if op == "+" {
		if _, ok := l.(StrV); ok {
			return StrV(l.String() + r.String()), nil
		}
		if _, ok := r.(StrV); ok {
			return StrV(l.String() + r.String()), nil
		}
	}
	switch op {
	case "+", "-", "*", "/", "%":
		return arith(op, l, r, pos, ctx)
	case "==", "!=", "<", "<=", ">", ">=":
		return cmp(op, l, r, pos, ctx)
	}
	return nil, &RunError{Msg: "internal: unknown operator " + op, Pos: pos, Ctx: ctx}
}

func arith(op string, l, r Value, pos Pos, ctx *execCtx) (Value, error) {
	li, lInt := l.(IntV)
	ri, rInt := r.(IntV)
	if lInt && rInt {
		switch op {
		case "+":
			return wrapI32(int64(li) + int64(ri)), nil
		case "-":
			return wrapI32(int64(li) - int64(ri)), nil
		case "*":
			return wrapI32(int64(li) * int64(ri)), nil
		case "/":
			if ri == 0 {
				return nil, &RunError{Msg: "DivisionByZeroError: integer division by zero", Pos: pos, Ctx: ctx}
			}
			return wrapI32(int64(li) / int64(ri)), nil
		case "%":
			if ri == 0 {
				return nil, &RunError{Msg: "DivisionByZeroError: modulo by zero", Pos: pos, Ctx: ctx}
			}
			return wrapI32(int64(li) % int64(ri)), nil
		}
	}
	lf, err := toFloat(l)
	if err != nil {
		return nil, &RunError{Msg: err.Error(), Pos: pos, Ctx: ctx}
	}
	rf, err := toFloat(r)
	if err != nil {
		return nil, &RunError{Msg: err.Error(), Pos: pos, Ctx: ctx}
	}
	switch op {
	case "+":
		return FloatV(lf + rf), nil
	case "-":
		return FloatV(lf - rf), nil
	case "*":
		return FloatV(lf * rf), nil
	case "/":
		if rf == 0 {
			return nil, &RunError{Msg: "DivisionByZeroError: float division by zero", Pos: pos, Ctx: ctx}
		}
		return FloatV(lf / rf), nil
	case "%":
		return nil, &RunError{Msg: "TypeError: '%' requires int operands", Pos: pos, Ctx: ctx}
	}
	return nil, &RunError{Msg: "internal: unknown arithmetic operator " + op, Pos: pos, Ctx: ctx}
}

// wrapI32 truncates to 32-bit two's complement, matching C int overflow.
func wrapI32(x int64) IntV { return IntV(int32(x)) }

func toFloat(v Value) (float64, error) {
	switch t := v.(type) {
	case IntV:
		return float64(t), nil
	case FloatV:
		return float64(t), nil
	}
	return 0, fmt.Errorf("TypeError: arithmetic requires numbers, got %s", v.TypeName())
}

func cmp(op string, l, r Value, pos Pos, ctx *execCtx) (Value, error) {
	if op == "==" || op == "!=" {
		eq, err := equalValues(l, r)
		if err != nil {
			return nil, &RunError{Msg: err.Error(), Pos: pos, Ctx: ctx}
		}
		if op == "==" {
			return BoolV(eq), nil
		}
		return BoolV(!eq), nil
	}
	switch a := l.(type) {
	case IntV:
		switch b := r.(type) {
		case IntV:
			return BoolV(ordCmp(float64(a), float64(b), op)), nil
		case FloatV:
			return BoolV(ordCmp(float64(a), float64(b), op)), nil
		}
	case FloatV:
		switch b := r.(type) {
		case FloatV:
			return BoolV(ordCmp(float64(a), float64(b), op)), nil
		case IntV:
			return BoolV(ordCmp(float64(a), float64(b), op)), nil
		}
	case StrV:
		if b, ok := r.(StrV); ok {
			return BoolV(ordCmpStr(string(a), string(b), op)), nil
		}
	}
	return nil, &RunError{Msg: fmt.Sprintf("TypeError: cannot order-compare %s and %s", l.TypeName(), r.TypeName()), Pos: pos, Ctx: ctx}
}

func ordCmp(a, b float64, op string) bool {
	switch op {
	case "<":
		return a < b
	case "<=":
		return a <= b
	case ">":
		return a > b
	case ">=":
		return a >= b
	}
	return false
}

func ordCmpStr(a, b, op string) bool {
	switch op {
	case "<":
		return a < b
	case "<=":
		return a <= b
	case ">":
		return a > b
	case ">=":
		return a >= b
	}
	return false
}

// equalValues implements == / != with strict type discipline.
func equalValues(l, r Value) (bool, error) {
	switch a := l.(type) {
	case IntV:
		switch b := r.(type) {
		case IntV:
			return a == b, nil
		case FloatV:
			return float64(a) == float64(b), nil
		}
		return false, nil
	case FloatV:
		switch b := r.(type) {
		case FloatV:
			return float64(a) == float64(b), nil
		case IntV:
			return float64(a) == float64(b), nil
		}
		return false, nil
	case StrV:
		if b, ok := r.(StrV); ok {
			return a == b, nil
		}
		return false, nil
	case BoolV:
		if b, ok := r.(BoolV); ok {
			return a == b, nil
		}
		return false, nil
	}
	if l == nil || r == nil {
		return l == nil && r == nil, nil
	}
	return l == r, nil
}

func truthy(v Value) (bool, error) {
	if b, ok := v.(BoolV); ok {
		return bool(b), nil
	}
	return false, fmt.Errorf("TypeError: condition must be bool, got %s", v.TypeName())
}

func isCopydType(t string) bool { return strings.Contains(t, "Copyd") }

// ReportError prints an error plus the replay of the execCtx log that
// explains what went wrong (spec §11.2).
func ReportError(err error, w io.Writer) {
	fmt.Fprintf(w, "error: %s\n", err.Error())
	if re, ok := err.(*RunError); ok && re.Ctx != nil && re.Ctx.Log.Size() > 0 {
		fmt.Fprintln(w, "---- execution log ----")
		l := re.Ctx.Log.copyVisible()
		for l.Head() != l.Tail() {
			v, _ := l.Next()
			fmt.Fprintf(w, "  %s\n", v.String())
		}
	}
}
