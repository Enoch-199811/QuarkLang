package lang

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
)

// RunError is a runtime (or strict-check) error with source position and,
// when available, the FuncBuffer whose log explains what happened.
type RunError struct {
	Msg string
	Pos Pos
	FB  *FuncBuffer
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
	call func(prefix Value, fb *FuncBuffer) (Value, error)
}

type builtinFn func(args []Value, pos Pos, fb *FuncBuffer) (Value, error)

// StructDef is a registered struct declaration.
type StructDef struct {
	Name  string
	Types map[string]string // 成员名 → 类型注解
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
}

// Run executes prog. args are command-line arguments for main(); stdin/stdout
// are the default console streams for io.
func Run(prog *Program, filename string, args []string, stdin io.Reader, stdout io.Writer) error {
	if err := Typecheck(prog); err != nil {
		return err
	}
	in := &interp{
		fns:        map[string]*Func{},
		sigs:       map[string]*signDef{},
		builtins:   map[string]builtinFn{},
		structs:    map[string]*StructDef{},
		interfaces: map[string]*InterfaceDef{},
		impls:      map[string]*ImplDef{},
	}
	for _, f := range prog.Funcs {
		if _, dup := in.fns[f.Name]; dup {
			return fmt.Errorf("CompileError: duplicate function %q", f.Name)
		}
		in.fns[f.Name] = &Func{Name: f.Name, Params: f.Params, Ret: f.Ret, Body: f.Body, Pos: f.Pos}
	}
	for _, s := range prog.Structs {
		if _, dup := in.structs[s.Name]; dup {
			return fmt.Errorf("CompileError: duplicate struct %q", s.Name)
		}
		def := &StructDef{Name: s.Name, Types: map[string]string{}}
		for _, m := range s.Members {
			def.Types[m.Name] = m.Type
		}
		in.structs[s.Name] = def
	}
	for _, i := range prog.Interfaces {
		if _, dup := in.interfaces[i.Name]; dup {
			return fmt.Errorf("CompileError: duplicate interface %q", i.Name)
		}
		in.interfaces[i.Name] = &InterfaceDef{Name: i.Name, Methods: i.Methods}
	}
	for _, im := range prog.Impls {
		def, ok := in.impls[im.Type]
		if !ok {
			def = &ImplDef{Type: im.Type, Iface: im.Iface, Methods: map[string]*Func{}, SelfMethods: map[string]*Func{}}
			in.impls[im.Type] = def
		} else if def.Iface == "" && im.Iface != "" {
			def.Iface = im.Iface
		}
		for _, m := range im.Methods {
			if len(m.Params) > 0 && m.Params[0].Name == "self" && m.Params[0].Type == "" {
				m.Params[0].Type = im.Type
			}
			fn := &Func{Name: m.Name, Params: m.Params, Ret: m.Ret, Body: m.Body, Pos: m.Pos}
			if len(m.Params) > 0 && m.Params[0].Name == "self" {
				if _, dup := def.SelfMethods[fn.Name]; dup {
					return fmt.Errorf("CompileError: duplicate method %q on %s", fn.Name, im.Type)
				}
				def.SelfMethods[fn.Name] = fn
			} else {
				if _, dup := def.Methods[fn.Name]; dup {
					return fmt.Errorf("CompileError: duplicate method %q on %s", fn.Name, im.Type)
				}
				def.Methods[fn.Name] = fn
			}
		}
	}
	in.sigs["memorize"] = &signDef{name: "memorize", call: in.memorizeCall}
	in.sigs["async"] = &signDef{name: "async", call: in.asyncCall}
	in.registerIOBuiltins()

	mainFn, ok := in.fns["main"]
	if !ok {
		return fmt.Errorf("CompileError: no main function found (expected: func main(io IOStream, ...))")
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
		return fmt.Errorf("CompileError: main must take 1-3 params in order (io IOStream, env HashTable<String,String>, args List<String>), got %d", len(mainFn.Params))
	}
	fb := NewFuncBuffer(mainFn, mainArgs, mainFn.Pos)
	return in.execute(fb)
}

// zeroInstance 构造结构体零值实例（成员按类型注解取零值）。
func (in *interp) zeroInstance(def *StructDef) *StructValue {
	sv := &StructValue{SType: def.Name, Fields: map[string]Value{}}
	for name, typ := range def.Types {
		sv.Fields[name] = in.zeroValue(typ)
	}
	return sv
}

// zeroValue 按类型注解生成零值。
func (in *interp) zeroValue(typ string) Value {
	if d, ok := in.structs[typ]; ok {
		return in.zeroInstance(d)
	}
	switch typ {
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

// execute runs a FuncBuffer's function body, filling Tail and Log.
func (in *interp) execute(fb *FuncBuffer) error {
	if fb.executed {
		return &RunError{Msg: fmt.Sprintf("RuntimeError: FuncBuffer for %s already executed", fb.Fn.Name), Pos: fb.pos, FB: fb}
	}
	fb.executed = true

	sc := newScope(nil)
	fn := fb.Fn
	for i, p := range fn.Params {
		v, err := fb.Head.Get(i)
		if err != nil {
			return &RunError{Msg: err.Error(), Pos: p.Pos, FB: fb}
		}
		if isCopydType(p.Type) {
			v = deepCopy(v)
		}
		if err := sc.declare(p.Name, v, p.Pos); err != nil {
			return err
		}
	}
	if err := in.execBlock(fn.Body, sc, fb); err != nil {
		if errors.Is(err, errReturn) {
			return nil
		}
		return err
	}
	return nil
}

func (in *interp) execBlock(b *Block, sc *scope, fb *FuncBuffer) error {
	for _, st := range b.Stmts {
		if err := in.execStmt(st, sc, fb); err != nil {
			return err
		}
	}
	return nil
}

func (in *interp) execStmt(st Stmt, sc *scope, fb *FuncBuffer) error {
	switch s := st.(type) {
	case *ExprStmt:
		_, err := in.evalExpr(s.X, sc, fb)
		return err
	case *OutStmt:
		v, err := in.evalExpr(s.X, sc, fb)
		if err != nil {
			return err
		}
		fb.Tail.Append(v)
		return nil
	case *LogStmt:
		v, err := in.evalExpr(s.X, sc, fb)
		if err != nil {
			return err
		}
		fb.Log.Append(StrV(v.String()))
		return nil
	case *YieldStmt:
		// 协作式切换点；调度事件自动日志（spec §14）
		fb.Log.Append(StrV("yield"))
		runtime.Gosched()
		return nil
	case *ReturnStmt:
		if s.X != nil {
			v, err := in.evalExpr(s.X, sc, fb)
			if err != nil {
				return err
			}
			fb.Tail.Append(v)
		}
		return errReturn
	case *IfStmt:
		c, err := in.evalExpr(s.Cond, sc, fb)
		if err != nil {
			return err
		}
		b, err := truthy(c)
		if err != nil {
			return err
		}
		if b {
			return in.execBlock(s.Then, sc, fb)
		}
		if s.Else != nil {
			return in.execBlock(s.Else, sc, fb)
		}
		return nil
	case *WhileStmt:
		for {
			c, err := in.evalExpr(s.Cond, sc, fb)
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
			if err := in.execBlock(s.Body, sc, fb); err != nil {
				return err
			}
		}
	case *ForStmt:
		v, err := in.evalExpr(s.Iter, sc, fb)
		if err != nil {
			return err
		}
		l, ok := v.(*List)
		if !ok {
			return &RunError{Msg: fmt.Sprintf("TypeError: for-in requires a List, got %s", v.TypeName()), Pos: s.Pos, FB: fb}
		}
		inner := newScope(sc)
		for l.Head() != l.Tail() {
			item, err := l.Next()
			if err != nil {
				return &RunError{Msg: err.Error(), Pos: s.Pos, FB: fb}
			}
			inner.vars[s.Var] = item
			if err := in.execBlock(s.Body, inner, fb); err != nil {
				return err
			}
		}
		return nil
	case *DeclStmt:
		var v Value = NilV{}
		if s.Init != nil {
			var err error
			v, err = in.evalExpr(s.Init, sc, fb)
			if err != nil {
				return err
			}
		} else if def, ok := in.structs[s.Type]; ok {
			v = in.zeroInstance(def)
		}
		return sc.declare(s.Name, v, s.Pos)
	case *AssignStmt:
		v, err := in.evalExpr(s.X, sc, fb)
		if err != nil {
			return err
		}
		switch t := s.Target.(type) {
		case *Ident:
			return sc.set(t.Name, v, t.Pos)
		case *MemberExpr:
			obj, err := in.evalExpr(t.X, sc, fb)
			if err != nil {
				return err
			}
			sv, ok := obj.(*StructValue)
			if !ok {
				return &RunError{Msg: fmt.Sprintf("TypeError: cannot assign member of %s", obj.TypeName()), Pos: s.Pos, FB: fb}
			}
			if _, exists := sv.Fields[t.Name]; !exists {
				return &RunError{Msg: fmt.Sprintf("TypeError: no member %q on %s", t.Name, sv.SType), Pos: t.Pos, FB: fb}
			}
			sv.Fields[t.Name] = v
			return nil
		}
		return &RunError{Msg: "TypeError: unsupported assignment target", Pos: s.Pos, FB: fb}
	}
	return nil
}

func (in *interp) evalExpr(e Expr, sc *scope, fb *FuncBuffer) (Value, error) {
	switch x := e.(type) {
	case *IntLit:
		return IntV(x.V), nil
	case *FloatLit:
		return FloatV(x.V), nil
	case *StrLit:
		return StrV(x.V), nil
	case *BoolLit:
		return BoolV(x.V), nil
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
			v, err := in.evalExpr(it, sc, fb)
			if err != nil {
				return nil, err
			}
			l.Append(v)
		}
		return l, nil
	case *UnOp:
		v, err := in.evalExpr(x.X, sc, fb)
		if err != nil {
			return nil, err
		}
		switch x.Op {
		case "*":
			l, ok := v.(*List)
			if !ok {
				return nil, &RunError{Msg: fmt.Sprintf("TypeError: '*' requires a List, got %s", v.TypeName()), Pos: x.Pos, FB: fb}
			}
			item, err := l.Peek()
			if err != nil {
				return nil, &RunError{Msg: err.Error(), Pos: x.Pos, FB: fb}
			}
			return item, nil
		case "-":
			switch n := v.(type) {
			case IntV:
				return wrapI32(-int64(n)), nil
			case FloatV:
				return -n, nil
			}
			return nil, &RunError{Msg: fmt.Sprintf("TypeError: unary '-' requires a number, got %s", v.TypeName()), Pos: x.Pos, FB: fb}
		case "!":
			b, err := truthy(v)
			if err != nil {
				return nil, &RunError{Msg: err.Error(), Pos: x.Pos, FB: fb}
			}
			return BoolV(!b), nil
		}
		return nil, &RunError{Msg: "internal: unknown unary operator " + x.Op, Pos: x.Pos, FB: fb}
	case *BinOp:
		l, err := in.evalExpr(x.L, sc, fb)
		if err != nil {
			return nil, err
		}
		if x.Op == "&&" {
			lb, err := truthy(l)
			if err != nil {
				return nil, &RunError{Msg: err.Error(), Pos: x.Pos, FB: fb}
			}
			if !lb {
				return BoolV(false), nil
			}
			r, err := in.evalExpr(x.R, sc, fb)
			if err != nil {
				return nil, err
			}
			rb, err := truthy(r)
			if err != nil {
				return nil, &RunError{Msg: err.Error(), Pos: x.Pos, FB: fb}
			}
			return BoolV(rb), nil
		}
		if x.Op == "||" {
			lb, err := truthy(l)
			if err != nil {
				return nil, &RunError{Msg: err.Error(), Pos: x.Pos, FB: fb}
			}
			if lb {
				return BoolV(true), nil
			}
			r, err := in.evalExpr(x.R, sc, fb)
			if err != nil {
				return nil, err
			}
			rb, err := truthy(r)
			if err != nil {
				return nil, &RunError{Msg: err.Error(), Pos: x.Pos, FB: fb}
			}
			return BoolV(rb), nil
		}
		r, err := in.evalExpr(x.R, sc, fb)
		if err != nil {
			return nil, err
		}
		return binOp(x.Op, l, r, x.Pos, fb)
	case *CallExpr:
		return in.evalCall(x, sc, fb)
	case *MemberExpr:
		obj, err := in.evalExpr(x.X, sc, fb)
		if err != nil {
			return nil, err
		}
		return evalMember(obj, x.Name, x.Pos, fb)
	case *ScopeCall:
		return in.evalScopeCall(x, sc, fb)
	case *IndexExpr:
		v, err := in.evalExpr(x.X, sc, fb)
		if err != nil {
			return nil, err
		}
		i, err := in.evalExpr(x.Idx, sc, fb)
		if err != nil {
			return nil, err
		}
		l, ok := v.(*List)
		if !ok {
			return nil, &RunError{Msg: fmt.Sprintf("TypeError: indexing requires a List, got %s", v.TypeName()), Pos: x.Pos, FB: fb}
		}
		iv, ok := i.(IntV)
		if !ok {
			return nil, &RunError{Msg: "TypeError: index must be int", Pos: x.Pos, FB: fb}
		}
		item, err := l.Get(int(iv))
		if err != nil {
			return nil, &RunError{Msg: err.Error(), Pos: x.Pos, FB: fb}
		}
		return item, nil
	}
	return nil, &RunError{Msg: "internal: unknown expression node", FB: fb}
}

// evalMember reads a plain member (no call) — e.g. FuncBuffer.head/tail/log.
func evalMember(obj Value, name string, pos Pos, fb *FuncBuffer) (Value, error) {
	if o, ok := obj.(*Task); ok {
		switch name {
		case "head":
			return o.Head, nil
		case "tail":
			return o.Tail, nil
		case "log":
			return o.Log, nil
		}
	}
	if o, ok := obj.(*FuncBuffer); ok {
		switch name {
		case "head":
			return o.Head, nil
		case "tail":
			return o.Tail, nil
		case "log":
			return o.Log, nil
		}
	}
	if o, ok := obj.(*StructValue); ok {
		if v, exists := o.Fields[name]; exists {
			return v, nil
		}
		return nil, &RunError{Msg: fmt.Sprintf("TypeError: no member %q on %s", name, obj.TypeName()), Pos: pos, FB: fb}
	}
	return nil, &RunError{Msg: fmt.Sprintf("TypeError: no member %q on %s", name, obj.TypeName()), Pos: pos, FB: fb}
}

func (in *interp) evalCall(c *CallExpr, sc *scope, fb *FuncBuffer) (Value, error) {
	// Signature wrapper: f(args) @sign(prefix) -> sign::call(prefix)(fb) (spec §6).
	if c.Sign != nil {
		id, ok := c.Fn.(*Ident)
		if !ok {
			return nil, &RunError{Msg: "TypeError: a signature can only wrap a direct function call", Pos: c.Pos, FB: fb}
		}
		fn, ok := in.fns[id.Name]
		if !ok {
			return nil, &RunError{Msg: fmt.Sprintf("CompileError: undeclared function %q", id.Name), Pos: id.Pos, FB: fb}
		}
		var prefix Value = NilV{}
		switch len(c.Sign.Args) {
		case 0:
		case 1:
			p, err := in.evalExpr(c.Sign.Args[0], sc, fb)
			if err != nil {
				return nil, err
			}
			prefix = p
		default:
			return nil, &RunError{Msg: "CompileError: a signature takes zero or one <Prefix> argument", Pos: c.Pos, FB: fb}
		}
		argVals, err := in.evalArgs(c.Args, sc, fb)
		if err != nil {
			return nil, err
		}
		if len(argVals) != len(fn.Params) {
			return nil, &RunError{Msg: fmt.Sprintf("CompileError: %s expects %d args, got %d", fn.Name, len(fn.Params), len(argVals)), Pos: id.Pos, FB: fb}
		}
		nfb := NewFuncBuffer(fn, argVals, id.Pos)
		if sd, ok := in.sigs[c.Sign.Name]; ok {
			return sd.call(prefix, nfb)
		}
		// 用户类型实现 Sign：签名名 = 类型名，必须有静态 call(prefix, fb)
		if def, ok := in.impls[c.Sign.Name]; ok {
			callFn, ok := def.Methods["call"]
			if !ok {
				return nil, &RunError{Msg: fmt.Sprintf("CompileError: type %q implements no static call — it does not implement Sign", c.Sign.Name), Pos: c.Pos, FB: fb}
			}
			cfb, err := in.callFunc(callFn, []Value{prefix, nfb}, c.Pos)
			if err != nil {
				return nil, err
			}
			nextFB, ok := cfb.(*FuncBuffer)
			if !ok {
				return nil, &RunError{Msg: "TypeError: Sign::call must return the next FuncBuffer", Pos: c.Pos, FB: fb}
			}
			return nextFB, nil
		}
		return nil, &RunError{Msg: fmt.Sprintf("CompileError: %q is not a registered signature (its type does not implement Sign)", c.Sign.Name), Pos: c.Pos, FB: fb}
	}

	// Method call: obj.name(args).
	if m, ok := c.Fn.(*MemberExpr); ok {
		obj, err := in.evalExpr(m.X, sc, fb)
		if err != nil {
			return nil, err
		}
		args, err := in.evalArgs(c.Args, sc, fb)
		if err != nil {
			return nil, err
		}
		return in.callMethod(obj, m.Name, args, fb, m.Pos)
	}

	id, ok := c.Fn.(*Ident)
	if !ok {
		return nil, &RunError{Msg: "TypeError: this expression is not callable", Pos: c.Pos, FB: fb}
	}
	argVals, err := in.evalArgs(c.Args, sc, fb)
	if err != nil {
		return nil, err
	}
	if fn, ok := in.fns[id.Name]; ok {
		return in.callFunc(fn, argVals, id.Pos)
	}
	if b, ok := in.builtins[id.Name]; ok {
		return b(argVals, id.Pos, fb)
	}
	return nil, &RunError{Msg: fmt.Sprintf("CompileError: undeclared function %q", id.Name), Pos: id.Pos, FB: fb}
}

// callFunc 调用一个函数/方法。fn.Ret != "" 时返回 return 的值（tail 首元素），
// 否则返回整个 FuncBuffer（out 收集在 tail）。
func (in *interp) callFunc(fn *Func, args []Value, pos Pos) (Value, error) {
	if len(args) != len(fn.Params) {
		return nil, &RunError{Msg: fmt.Sprintf("CompileError: %s expects %d args, got %d", fn.Name, len(fn.Params), len(args)), Pos: pos}
	}
	for i, p := range fn.Params {
		if isCopydType(p.Type) {
			args[i] = deepCopy(args[i])
		}
	}
	nfb := NewFuncBuffer(fn, args, pos)
	if err := in.execute(nfb); err != nil {
		return nil, err
	}
	if fn.Ret == "" {
		return nfb, nil
	}
	v, err := nfb.Tail.Next()
	if err != nil {
		return NilV{}, nil // 带返回类型但裸 return：视为 nil
	}
	return v, nil
}

func (in *interp) evalArgs(args []Expr, sc *scope, fb *FuncBuffer) ([]Value, error) {
	vals := make([]Value, 0, len(args))
	for _, a := range args {
		v, err := in.evalExpr(a, sc, fb)
		if err != nil {
			return nil, err
		}
		vals = append(vals, v)
	}
	return vals, nil
}

func (in *interp) callMethod(obj Value, name string, args []Value, fb *FuncBuffer, pos Pos) (Value, error) {
	switch o := obj.(type) {
	case *List:
		switch name {
		case "head":
			if err := wantArity(name, 0, len(args), pos, fb); err != nil {
				return nil, err
			}
			return IntV(o.Head()), nil
		case "tail":
			if err := wantArity(name, 0, len(args), pos, fb); err != nil {
				return nil, err
			}
			return IntV(o.Tail()), nil
		case "size":
			if err := wantArity(name, 0, len(args), pos, fb); err != nil {
				return nil, err
			}
			return IntV(o.Size()), nil
		case "next":
			if err := wantArity(name, 0, len(args), pos, fb); err != nil {
				return nil, err
			}
			v, err := o.Next()
			if err != nil {
				return nil, &RunError{Msg: err.Error(), Pos: pos, FB: fb}
			}
			return v, nil
		case "reset":
			if err := wantArity(name, 0, len(args), pos, fb); err != nil {
				return nil, err
			}
			o.Reset()
			return o, nil
		case "append":
			if err := wantArity(name, 1, len(args), pos, fb); err != nil {
				return nil, err
			}
			o.Append(args[0])
			return NilV{}, nil
		case "appendAll":
			if err := wantArity(name, 1, len(args), pos, fb); err != nil {
				return nil, err
			}
			l, ok := args[0].(*List)
			if !ok {
				return nil, &RunError{Msg: fmt.Sprintf("TypeError: appendAll requires a List, got %s", args[0].TypeName()), Pos: pos, FB: fb}
			}
			o.AppendAll(l)
			return NilV{}, nil
		case "toString":
			if err := wantArity(name, 0, len(args), pos, fb); err != nil {
				return nil, err
			}
			return StrV(o.String()), nil
		case "__sort__":
			if err := wantArity(name, 0, len(args), pos, fb); err != nil {
				return nil, err
			}
			if err := o.sortInPlace(); err != nil {
				return nil, &RunError{Msg: err.Error(), Pos: pos, FB: fb}
			}
			return o, nil
		}
	case *HashTable:
		switch name {
		case "put":
			if err := wantArity(name, 2, len(args), pos, fb); err != nil {
				return nil, err
			}
			o.Put(args[0], args[1])
			return NilV{}, nil
		case "get":
			if err := wantArity(name, 1, len(args), pos, fb); err != nil {
				return nil, err
			}
			v, ok := o.Get(args[0])
			if !ok || v == nil {
				return NilV{}, nil
			}
			return v, nil
		case "contains":
			if err := wantArity(name, 1, len(args), pos, fb); err != nil {
				return nil, err
			}
			return BoolV(o.Contains(args[0])), nil
		case "remove":
			if err := wantArity(name, 1, len(args), pos, fb); err != nil {
				return nil, err
			}
			o.Remove(args[0])
			return NilV{}, nil
		case "size":
			if err := wantArity(name, 0, len(args), pos, fb); err != nil {
				return nil, err
			}
			return IntV(o.Size()), nil
		}
	case *FuncBuffer:
		if name == "execute" {
			if err := wantArity(name, 0, len(args), pos, fb); err != nil {
				return nil, err
			}
			if err := in.execute(o); err != nil {
				return nil, err
			}
			return o, nil
		}
	case *Memory:
		if name == "compact" {
			if err := wantArity(name, 0, len(args), pos, fb); err != nil {
				return nil, err
			}
			// v0.1: 内存由宿主 Go GC 管理；兼容性入口，无实际回收
			return IntV(0), nil
		}
	case *Task:
		if name == "done" {
			if err := wantArity(name, 0, len(args), pos, fb); err != nil {
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
			if err := wantArity(name, 1, len(args), pos, fb); err != nil {
				return nil, err
			}
			o.ch <- args[0]
			return NilV{}, nil
		case "recv":
			if err := wantArity(name, 0, len(args), pos, fb); err != nil {
				return nil, err
			}
			return <-o.ch, nil
		}
	case *IOStream:
		switch name {
		case "println":
			return ioPrintln(o, args, true, pos, fb)
		case "print":
			return ioPrintln(o, args, false, pos, fb)
		case "setIn":
			if err := wantArity(name, 1, len(args), pos, fb); err != nil {
				return nil, err
			}
			if err := setInput(o, args[0]); err != nil {
				return nil, &RunError{Msg: err.Error(), Pos: pos, FB: fb}
			}
			return NilV{}, nil
		case "setOut":
			if err := wantArity(name, 1, len(args), pos, fb); err != nil {
				return nil, err
			}
			if err := setOutput(o, args[0]); err != nil {
				return nil, &RunError{Msg: err.Error(), Pos: pos, FB: fb}
			}
			return NilV{}, nil
		case "readln":
			if err := wantArity(name, 0, len(args), pos, fb); err != nil {
				return nil, err
			}
			o.mu.Lock()
			line, err := o.rd.ReadString('\n')
			o.mu.Unlock()
			if err != nil && line == "" {
				return nil, &RunError{Msg: "IOError: " + err.Error(), Pos: pos, FB: fb}
			}
			return StrV(strings.TrimRight(line, "\r\n")), nil
		}
	case *StructValue:
		def, ok := in.impls[o.SType]
		if !ok {
			return nil, &RunError{Msg: fmt.Sprintf("TypeError: type %s has no impl", o.SType), Pos: pos, FB: fb}
		}
		if fn, ok := def.SelfMethods[name]; ok {
			callArgs := append([]Value{o}, args...)
			return in.callFunc(fn, callArgs, pos)
		}
		if _, ok := def.Methods[name]; ok {
			return nil, &RunError{Msg: fmt.Sprintf("TypeError: %s.%s is a static method; call it via %s::%s(...)", o.SType, name, o.SType, name), Pos: pos, FB: fb}
		}
	}
	return nil, &RunError{Msg: fmt.Sprintf("TypeError: no method %q on %s", name, obj.TypeName()), Pos: pos, FB: fb}
}

func wantArity(name string, want, got int, pos Pos, fb *FuncBuffer) error {
	if want != got {
		return &RunError{Msg: fmt.Sprintf("CompileError: %s() expects %d args, got %d", name, want, got), Pos: pos, FB: fb}
	}
	return nil
}

func ioPrintln(s *IOStream, args []Value, newline bool, pos Pos, fb *FuncBuffer) (Value, error) {
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
		return nil, &RunError{Msg: "IOError: " + err.Error(), Pos: pos, FB: fb}
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

func (in *interp) evalScopeCall(x *ScopeCall, sc *scope, fb *FuncBuffer) (Value, error) {
	args, err := in.evalArgs(x.Args, sc, fb)
	if err != nil {
		return nil, err
	}
	switch x.Scope {
	case "memorize":
		if x.Name == "new" && len(args) == 0 {
			return &MemorizeBuffer{Table: NewHashTable()}, nil
		}
		return nil, &RunError{Msg: fmt.Sprintf("TypeError: memorize has no static method %q", x.Name), Pos: x.Pos, FB: fb}
	case "HashTable":
		if x.Name == "new" && len(args) == 0 {
			return NewHashTable(), nil
		}
		return nil, &RunError{Msg: fmt.Sprintf("TypeError: HashTable has no static method %q", x.Name), Pos: x.Pos, FB: fb}
	case "List":
		if x.Name == "new" && len(args) == 0 {
			return NewList(), nil
		}
		return nil, &RunError{Msg: fmt.Sprintf("TypeError: List has no static method %q", x.Name), Pos: x.Pos, FB: fb}
	case "GlobalMemory":
		if x.Name == "compact" && len(args) == 0 {
			// v0.1: 内存由宿主 Go GC 管理；兼容性入口
			return IntV(0), nil
		}
		return nil, &RunError{Msg: fmt.Sprintf("TypeError: GlobalMemory has no static method %q", x.Name), Pos: x.Pos, FB: fb}
	case "taskm":
		switch x.Name {
		case "spawn":
			if len(args) < 1 {
				return nil, wantArity("taskm::spawn", 1, len(args), x.Pos, fb)
			}
			fn, err := lookupFunc(args[0], in)
			if err != nil {
				return nil, &RunError{Msg: err.Error(), Pos: x.Pos, FB: fb}
			}
			argVals := args[1:]
			if len(argVals) != len(fn.Params) {
				return nil, &RunError{Msg: fmt.Sprintf("CompileError: %s expects %d args, got %d", fn.Name, len(fn.Params), len(argVals)), Pos: x.Pos, FB: fb}
			}
			nfb := NewFuncBuffer(fn, argVals, x.Pos)
			t, err := in.spawnTask(nfb)
			if err != nil {
				return nil, err
			}
			return t, nil
		case "block":
			if len(args) != 1 {
				return nil, wantArity("taskm::block", 1, len(args), x.Pos, fb)
			}
			t, ok := args[0].(*Task)
			if !ok {
				return nil, &RunError{Msg: fmt.Sprintf("TypeError: taskm::block requires a Task, got %s", args[0].TypeName()), Pos: x.Pos, FB: fb}
			}
			<-t.doneCh
			if t.err != nil {
				return nil, t.err
			}
			return t.FuncBuffer, nil
		case "channel":
			if len(args) != 0 {
				return nil, wantArity("taskm::channel", 0, len(args), x.Pos, fb)
			}
			return NewChannel(), nil
		}
		return nil, &RunError{Msg: fmt.Sprintf("TypeError: taskm has no static method %q", x.Name), Pos: x.Pos, FB: fb}
	case "IO":
		switch x.Name {
		case "setIn":
			if len(args) != 2 {
				return nil, wantArity("IO::setIn", 2, len(args), x.Pos, fb)
			}
			ioObj, ok := args[0].(*IOStream)
			if !ok {
				return nil, &RunError{Msg: fmt.Sprintf("TypeError: IO::setIn's first arg must be an IOStream, got %s", args[0].TypeName()), Pos: x.Pos, FB: fb}
			}
			if err := setInput(ioObj, args[1]); err != nil {
				return nil, &RunError{Msg: err.Error(), Pos: x.Pos, FB: fb}
			}
			return NilV{}, nil
		case "setOut":
			if len(args) != 2 {
				return nil, wantArity("IO::setOut", 2, len(args), x.Pos, fb)
			}
			ioObj, ok := args[0].(*IOStream)
			if !ok {
				return nil, &RunError{Msg: fmt.Sprintf("TypeError: IO::setOut's first arg must be an IOStream, got %s", args[0].TypeName()), Pos: x.Pos, FB: fb}
			}
			if err := setOutput(ioObj, args[1]); err != nil {
				return nil, &RunError{Msg: err.Error(), Pos: x.Pos, FB: fb}
			}
			return NilV{}, nil
		}
		return nil, &RunError{Msg: fmt.Sprintf("TypeError: IO has no static method %q", x.Name), Pos: x.Pos, FB: fb}
	}
	if def, ok := in.impls[x.Scope]; ok {
		if fn, ok := def.Methods[x.Name]; ok {
			return in.callFunc(fn, args, x.Pos)
		}
		return nil, &RunError{Msg: fmt.Sprintf("TypeError: %s has no static method %q", x.Scope, x.Name), Pos: x.Pos, FB: fb}
	}
	return nil, &RunError{Msg: fmt.Sprintf("CompileError: unknown scope %q", x.Scope), Pos: x.Pos, FB: fb}
}

func (in *interp) registerIOBuiltins() {
	in.builtins["FileInputStream"] = func(args []Value, pos Pos, fb *FuncBuffer) (Value, error) {
		if len(args) != 1 {
			return nil, wantArity("FileInputStream", 1, len(args), pos, fb)
		}
		p, ok := args[0].(StrV)
		if !ok {
			return nil, &RunError{Msg: fmt.Sprintf("TypeError: FileInputStream requires a path string, got %s", args[0].TypeName()), Pos: pos, FB: fb}
		}
		f, err := os.Open(string(p))
		if err != nil {
			return nil, &RunError{Msg: fmt.Sprintf("IOError: cannot open %q: %v", string(p), err), Pos: pos, FB: fb}
		}
		return &InputStream{R: f}, nil
	}
	in.builtins["FileOutputStream"] = func(args []Value, pos Pos, fb *FuncBuffer) (Value, error) {
		if len(args) != 1 {
			return nil, wantArity("FileOutputStream", 1, len(args), pos, fb)
		}
		p, ok := args[0].(StrV)
		if !ok {
			return nil, &RunError{Msg: fmt.Sprintf("TypeError: FileOutputStream requires a path string, got %s", args[0].TypeName()), Pos: pos, FB: fb}
		}
		f, err := os.Create(string(p))
		if err != nil {
			return nil, &RunError{Msg: fmt.Sprintf("IOError: cannot create %q: %v", string(p), err), Pos: pos, FB: fb}
		}
		return &OutputStream{W: f}, nil
	}
	in.builtins["ConsoleInputStream"] = func(args []Value, pos Pos, fb *FuncBuffer) (Value, error) {
		if len(args) != 0 {
			return nil, wantArity("ConsoleInputStream", 0, len(args), pos, fb)
		}
		return &InputStream{R: os.Stdin}, nil
	}
	in.builtins["ConsoleOutputStream"] = func(args []Value, pos Pos, fb *FuncBuffer) (Value, error) {
		if len(args) != 0 {
			return nil, wantArity("ConsoleOutputStream", 0, len(args), pos, fb)
		}
		return &OutputStream{W: os.Stdout}, nil
	}
}

// asyncCall implements the built-in @async() signature: run the wrapped call
// as a coroutine and return a Task (done flag + the full FuncBuffer, §14).
func (in *interp) asyncCall(prefix Value, fb *FuncBuffer) (Value, error) {
	t, err := in.spawnTask(fb)
	if err != nil {
		return nil, err
	}
	return t, nil
}

// spawnTask starts a coroutine that executes fb. Scheduling events are
// auto-logged (cooperative functions auto-log, spec §14).
func (in *interp) spawnTask(fb *FuncBuffer) (*Task, error) {
	t := &Task{FuncBuffer: fb, doneCh: make(chan struct{})}
	argStrs := make([]string, 0, fb.Head.Size())
	for i := 0; i < fb.Head.Size(); i++ {
		if v, err := fb.Head.Get(i); err == nil {
			argStrs = append(argStrs, v.String())
		}
	}
	fb.Log.Append(StrV("async: spawned " + fb.Fn.Name + "(" + strings.Join(argStrs, ", ") + ")"))
	go func() {
		t.err = in.execute(fb)
		fb.Log.Append(StrV("async: done"))
		close(t.doneCh)
	}()
	return t, nil
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

// memorizeCall implements Sign::call for the built-in memorize signature:
// the cache is a head -> tail map held by the prefix (a memorize buffer).
func (in *interp) memorizeCall(prefix Value, fb *FuncBuffer) (Value, error) {
	mb, ok := prefix.(*MemorizeBuffer)
	if !ok {
		return nil, &RunError{Msg: fmt.Sprintf("TypeError: @memorize prefix must be a memorize buffer, got %s", prefix.TypeName()), Pos: fb.pos, FB: fb}
	}
	if cached, hit := mb.Table.Get(fb.Head); hit {
		l, ok := cached.(*List)
		if !ok {
			return nil, &RunError{Msg: "internal: cached value is not a List", Pos: fb.pos, FB: fb}
		}
		fb.Tail.AppendAll(l)
		return fb, nil
	}
	if err := in.execute(fb); err != nil {
		return nil, err
	}
	mb.Table.Put(fb.Head, fb.Tail)
	return fb, nil
}

func binOp(op string, l, r Value, pos Pos, fb *FuncBuffer) (Value, error) {
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
		return arith(op, l, r, pos, fb)
	case "==", "!=", "<", "<=", ">", ">=":
		return cmp(op, l, r, pos, fb)
	}
	return nil, &RunError{Msg: "internal: unknown operator " + op, Pos: pos, FB: fb}
}

func arith(op string, l, r Value, pos Pos, fb *FuncBuffer) (Value, error) {
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
				return nil, &RunError{Msg: "DivisionByZeroError: integer division by zero", Pos: pos, FB: fb}
			}
			return wrapI32(int64(li) / int64(ri)), nil
		case "%":
			if ri == 0 {
				return nil, &RunError{Msg: "DivisionByZeroError: modulo by zero", Pos: pos, FB: fb}
			}
			return wrapI32(int64(li) % int64(ri)), nil
		}
	}
	lf, err := toFloat(l)
	if err != nil {
		return nil, &RunError{Msg: err.Error(), Pos: pos, FB: fb}
	}
	rf, err := toFloat(r)
	if err != nil {
		return nil, &RunError{Msg: err.Error(), Pos: pos, FB: fb}
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
			return nil, &RunError{Msg: "DivisionByZeroError: float division by zero", Pos: pos, FB: fb}
		}
		return FloatV(lf / rf), nil
	case "%":
		return nil, &RunError{Msg: "TypeError: '%' requires int operands", Pos: pos, FB: fb}
	}
	return nil, &RunError{Msg: "internal: unknown arithmetic operator " + op, Pos: pos, FB: fb}
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

func cmp(op string, l, r Value, pos Pos, fb *FuncBuffer) (Value, error) {
	if op == "==" || op == "!=" {
		eq, err := equalValues(l, r)
		if err != nil {
			return nil, &RunError{Msg: err.Error(), Pos: pos, FB: fb}
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
	return nil, &RunError{Msg: fmt.Sprintf("TypeError: cannot order-compare %s and %s", l.TypeName(), r.TypeName()), Pos: pos, FB: fb}
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

// ReportError prints an error plus the replay of the FuncBuffer log that
// explains what went wrong (spec §11.2).
func ReportError(err error, w io.Writer) {
	fmt.Fprintf(w, "error: %s\n", err.Error())
	if re, ok := err.(*RunError); ok && re.FB != nil && re.FB.Log.Size() > 0 {
		fmt.Fprintln(w, "---- execution log ----")
		l := re.FB.Log.copyVisible()
		for l.Head() != l.Tail() {
			v, _ := l.Next()
			fmt.Fprintf(w, "  %s\n", v.String())
		}
	}
}
