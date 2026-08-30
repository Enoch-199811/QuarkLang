package lang

import (
	"fmt"
	"strings"
)

// ============ 静态类型（v0.1 结构子集，spec §11.1） ============

type tKind int

const (
	tInt tKind = iota
	tFloat
	tString
	tBool
	tNil
	tAny // interface{}
	tList
	tHashTable
	tFuncBuffer
	tIOStream
	tInputStream
	tOutputStream
	tChannel
	tTask
	tMemorize
	tMemory
	tFunc
	tStruct
	tTaskm
)

type Type struct {
	Kind  tKind
	Elem  *Type
	Key   *Type
	Val   *Type
	FName string // tFunc: 函数名（"" = 未知）
}

func mk(k tKind) *Type                 { return &Type{Kind: k} }
func mkList(e *Type) *Type             { return &Type{Kind: tList, Elem: e} }
func mkTable(k, v *Type) *Type         { return &Type{Kind: tHashTable, Key: k, Val: v} }
func mkFunc(name string) *Type         { return &Type{Kind: tFunc, FName: name} }

var (
	tIntV          = mk(tInt)
	tFloatV        = mk(tFloat)
	tStringV       = mk(tString)
	tBoolV         = mk(tBool)
	tNilV          = mk(tNil)
	tAnyV          = mk(tAny)
	tFuncBufferV   = mk(tFuncBuffer)
	tIOStreamV     = mk(tIOStream)
	tInputStreamV  = mk(tInputStream)
	tOutputStreamV = mk(tOutputStream)
	tChannelV      = mk(tChannel)
	tTaskV         = mk(tTask)
	tMemorizeV     = mk(tMemorize)
	tMemoryV       = mk(tMemory)
	tTaskmV        = mk(tTaskm)
)

var kindName = map[tKind]string{
	tInt: "int", tFloat: "float", tString: "String", tBool: "bool",
	tNil: "nil", tAny: "interface{}", tFuncBuffer: "FuncBuffer",
	tIOStream: "IOStream", tInputStream: "InputStream", tOutputStream: "OutputStream",
	tChannel: "Channel", tTask: "Task", tMemorize: "memorize", tMemory: "memory",
	tFunc: "func", tStruct: "struct", tTaskm: "taskm",
}

func (t *Type) String() string {
	if t == nil {
		return "<nil>"
	}
	switch t.Kind {
	case tList:
		return "List<" + t.Elem.String() + ">"
	case tHashTable:
		return "HashTable<" + t.Key.String() + ", " + t.Val.String() + ">"
	case tFunc:
		if t.FName != "" {
			return "func " + t.FName
		}
		return "func"
	case tStruct:
		return t.FName
	}
	if n, ok := kindName[t.Kind]; ok {
		return n
	}
	return "unknown"
}

// assignable 判断 from 能否赋给 to（严格；int→float 允许数值拓宽）。
func assignable(from, to *Type) bool {
	if from == nil || to == nil {
		return false
	}
	if to.Kind == tAny {
		return true
	}
	if from.Kind == tAny {
		return false
	}
	if from.Kind == tNil {
		return false
	}
	if from.Kind == tInt && to.Kind == tFloat {
		return true
	}
	if from.Kind != to.Kind {
		return false
	}
	switch to.Kind {
	case tList:
		return from.Elem.Kind == tAny || to.Elem.Kind == tAny || assignable(from.Elem, to.Elem)
	case tHashTable:
		keyOK := from.Key.Kind == tAny || to.Key.Kind == tAny || assignable(from.Key, to.Key)
		valOK := from.Val.Kind == tAny || to.Val.Kind == tAny || assignable(from.Val, to.Val)
		return keyOK && valOK
	case tFunc:
		return to.FName == "" || from.FName == to.FName
	case tStruct:
		return from.FName == to.FName
	}
	return true
}

// parseTypeStr 解析类型注解。Array<T>/T[]/T[Copyd] 在 v0.1 统一归一化为 List<T>
// （运行时只有 List；Copyd 的复制语义由运行时按参数注解处理）。
func parseTypeStr(s string) (*Type, error) {
	s = strings.TrimSpace(s)
	if s == "interface{}" {
		return tAnyV, nil
	}
	base, inner, suffix := splitType(s)
	elem := func() (*Type, error) {
		if inner == "" {
			return tAnyV, nil
		}
		return parseTypeStr(inner)
	}
	switch base {
	case "int", "long", "char": // long/char v0.1 按 int 处理（宽度见 §3.1 状态）
		if suffix != "" || strings.HasSuffix(s, "[]") {
			e, err := elem()
			if err != nil {
				return nil, err
			}
			return mkList(e), nil
		}
		return tIntV, nil
	case "float", "double":
		return tFloatV, nil
	case "bool":
		return tBoolV, nil
	case "String":
		return tStringV, nil
	case "void":
		return tNilV, nil
	case "FuncBuffer":
		return tFuncBufferV, nil
	case "IOStream":
		return tIOStreamV, nil
	case "InputStream":
		return tInputStreamV, nil
	case "OutputStream":
		return tOutputStreamV, nil
	case "Channel":
		return tChannelV, nil
	case "Task":
		return tTaskV, nil
	case "memorize":
		return tMemorizeV, nil
	case "memory":
		return tMemoryV, nil
	case "taskm":
		return tTaskmV, nil
	case "func":
		return mkFunc(""), nil
	case "List", "Array":
		e, err := elem()
		if err != nil {
			return nil, err
		}
		return mkList(e), nil
	case "Copyd":
		e, err := elem()
		if err != nil {
			return nil, err
		}
		return e, nil // Copyd<T> 语义上就是 T（复制在传递时发生）
	case "HashTable":
		k, v, err := splitTopComma(inner)
		if err != nil {
			return nil, err
		}
		kt, err := parseTypeStr(k)
		if err != nil {
			return nil, err
		}
		vt, err := parseTypeStr(v)
		if err != nil {
			return nil, err
		}
		return mkTable(kt, vt), nil
	}
	return nil, fmt.Errorf("unknown type %q", s)
}

func splitType(s string) (base, inner, suffix string) {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, "["); i >= 0 && strings.HasSuffix(s, "]") {
		suffix = strings.TrimSpace(s[i+1 : len(s)-1])
		s = s[:i]
	}
	if i := strings.Index(s, "<"); i >= 0 && strings.HasSuffix(s, ">") {
		base = s[:i]
		inner = s[i+1 : len(s)-1]
	} else {
		base = s
	}
	return base, strings.TrimSpace(inner), suffix
}

// splitTopComma 按顶层逗号切分（忽略尖括号内的逗号）。
func splitTopComma(s string) (string, string, error) {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '<':
			depth++
		case '>':
			depth--
		case ',':
			if depth == 0 {
				return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+1:]), nil
			}
		}
	}
	return "", "", fmt.Errorf("type %q needs two arguments (K, V)", s)
}

// ============ 检查器 ============

// CheckError 是编译期（静态检查）错误。
type CheckError struct {
	Msg string
	Pos Pos
}

func (e *CheckError) Error() string {
	return fmt.Sprintf("%s at line %d", e.Msg, e.Pos.Line)
}

type cVar struct {
	typ  *Type
	init bool
}

type cScope struct {
	vars  map[string]*cVar
	outer *cScope
}

func newCScope(outer *cScope) *cScope {
	return &cScope{vars: map[string]*cVar{}, outer: outer}
}

func (s *cScope) declare(name string, v *cVar, pos Pos) error {
	if _, dup := s.vars[name]; dup {
		return &CheckError{Msg: fmt.Sprintf("CompileError: duplicate declaration of %q", name), Pos: pos}
	}
	s.vars[name] = v
	return nil
}

func (s *cScope) lookup(name string) *cVar {
	for sc := s; sc != nil; sc = sc.outer {
		if v, ok := sc.vars[name]; ok {
			return v
		}
	}
	return nil
}

type checker struct {
	fns        map[string]*Func
	sigs       map[string]int // 签名名 → <Prefix> 参数个数（memorize:1, async:0）
	structs    map[string]*StructDef
	interfaces map[string]*InterfaceDef
	impls      map[string]*ImplDef
	curRet     *Type
}

// Typecheck 执行 §11.1 的全部编译期严格检查。
func Typecheck(prog *Program) error {
	c := &checker{
		fns:        map[string]*Func{},
		sigs:       map[string]int{"memorize": 1, "async": 0},
		structs:    map[string]*StructDef{},
		interfaces: map[string]*InterfaceDef{},
		impls:      map[string]*ImplDef{},
	}
	for _, f := range prog.Funcs {
		if _, dup := c.fns[f.Name]; dup {
			return &CheckError{Msg: fmt.Sprintf("CompileError: duplicate function %q", f.Name), Pos: f.Pos}
		}
		c.fns[f.Name] = &Func{Name: f.Name, Params: f.Params, Ret: f.Ret, Body: f.Body, Pos: f.Pos}
	}
	for _, s := range prog.Structs {
		if _, dup := c.structs[s.Name]; dup {
			return &CheckError{Msg: fmt.Sprintf("CompileError: duplicate struct %q", s.Name), Pos: s.Pos}
		}
		def := &StructDef{Name: s.Name, Types: map[string]string{}}
		for _, m := range s.Members {
			def.Types[m.Name] = m.Type
		}
		c.structs[s.Name] = def
	}
	for _, i := range prog.Interfaces {
		if _, dup := c.interfaces[i.Name]; dup {
			return &CheckError{Msg: fmt.Sprintf("CompileError: duplicate interface %q", i.Name), Pos: i.Pos}
		}
		c.interfaces[i.Name] = &InterfaceDef{Name: i.Name, Methods: i.Methods}
	}
	for _, im := range prog.Impls {
		def, ok := c.impls[im.Type]
		if !ok {
			def = &ImplDef{Type: im.Type, Iface: im.Iface, Methods: map[string]*Func{}, SelfMethods: map[string]*Func{}}
			c.impls[im.Type] = def
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
					return &CheckError{Msg: fmt.Sprintf("CompileError: duplicate method %q on %s", fn.Name, im.Type), Pos: m.Pos}
				}
				def.SelfMethods[fn.Name] = fn
			} else {
				if _, dup := def.Methods[fn.Name]; dup {
					return &CheckError{Msg: fmt.Sprintf("CompileError: duplicate method %q on %s", fn.Name, im.Type), Pos: m.Pos}
				}
				def.Methods[fn.Name] = fn
			}
		}
	}
	// impl 接口一致性（§11.1.3）：接口方法必须全部实现（名称 + 参数个数）
	for _, im := range prog.Impls {
		if im.Iface == "" {
			continue
		}
		iface, ok := c.interfaces[im.Iface]
		if !ok {
			return &CheckError{Msg: fmt.Sprintf("CompileError: unknown interface %q", im.Iface), Pos: im.Pos}
		}
		def := c.impls[im.Type]
		for _, sig := range iface.Methods {
			fn, ok := def.Methods[sig.Name]
			if !ok {
				return &CheckError{Msg: fmt.Sprintf("CompileError: impl %s for %s: missing method %q", im.Type, im.Iface, sig.Name), Pos: im.Pos}
			}
			if len(fn.Params) != len(sig.Params) {
				return &CheckError{Msg: fmt.Sprintf("CompileError: impl %s for %s: method %q takes %d params, interface requires %d", im.Type, im.Iface, sig.Name, len(fn.Params), len(sig.Params)), Pos: im.Pos}
			}
		}
	}
	for _, f := range prog.Funcs {
		if f.Name == "main" && (len(f.Params) < 1 || len(f.Params) > 3) {
			return &CheckError{Msg: fmt.Sprintf("CompileError: main must take 1-3 params in order (io, env, args), got %d", len(f.Params)), Pos: f.Pos}
		}
		if err := c.checkFunc(c.fns[f.Name]); err != nil {
			return err
		}
	}
	for _, im := range prog.Impls {
		for _, m := range im.Methods {
			if err := c.checkFunc(&Func{Name: m.Name, Params: m.Params, Ret: m.Ret, Body: m.Body, Pos: m.Pos}); err != nil {
				return err
			}
		}
	}
	return nil
}

// resolveType 解析类型注解；未知名称回退到已注册的 struct/interface。
func (c *checker) resolveType(s string, pos Pos) (*Type, error) {
	t, err := parseTypeStr(s)
	if err == nil {
		return t, nil
	}
	if _, ok := c.structs[s]; ok {
		return &Type{Kind: tStruct, FName: s}, nil
	}
	if _, ok := c.interfaces[s]; ok {
		return tAnyV, nil
	}
	return nil, &CheckError{Msg: fmt.Sprintf("CompileError: unknown type %q", s), Pos: pos}
}

func (c *checker) errf(pos Pos, format string, args ...interface{}) error {
	return &CheckError{Msg: fmt.Sprintf(format, args...), Pos: pos}
}

func (c *checker) paramType(p Param, pos Pos) (*Type, error) {
	return c.resolveType(p.Type, p.Pos)
}

func (c *checker) checkFunc(f *Func) error {
	var retType *Type
	if f.Ret != "" {
		t, err := c.resolveType(f.Ret, f.Pos)
		if err != nil {
			return err
		}
		retType = t
	}
	prev := c.curRet
	c.curRet = retType
	defer func() { c.curRet = prev }()

	sc := newCScope(nil)
	for _, p := range f.Params {
		if p.Type == "" {
			return &CheckError{Msg: fmt.Sprintf("CompileError: parameter %q of %s is missing a type annotation", p.Name, f.Name), Pos: p.Pos}
		}
		t, err := c.paramType(p, f.Pos)
		if err != nil {
			return err
		}
		if err := sc.declare(p.Name, &cVar{typ: t, init: true}, p.Pos); err != nil {
			return err
		}
	}
	return c.checkBlock(f.Body, sc)
}

func (c *checker) checkBlock(b *Block, sc *cScope) error {
	for _, st := range b.Stmts {
		if err := c.checkStmt(st, sc); err != nil {
			return err
		}
	}
	return nil
}

func (c *checker) checkStmt(st Stmt, sc *cScope) error {
	switch s := st.(type) {
	case *ExprStmt:
		_, err := c.infer(s.X, sc)
		return err
	case *OutStmt:
		_, err := c.infer(s.X, sc)
		return err
	case *LogStmt:
		_, err := c.infer(s.X, sc)
		return err
	case *ReturnStmt:
		if s.X != nil {
			t, err := c.infer(s.X, sc)
			if err != nil {
				return err
			}
			if c.curRet != nil && !assignable(t, c.curRet) {
				return c.errf(s.Pos, "TypeError: return type is %s, got %s", c.curRet, t)
			}
		}
		return nil
	case *IfStmt:
		if err := c.requireBool(s.Cond, sc); err != nil {
			return err
		}
		if err := c.checkBlock(s.Then, sc); err != nil {
			return err
		}
		if s.Else != nil {
			return c.checkBlock(s.Else, sc)
		}
		return nil
	case *WhileStmt:
		if err := c.requireBool(s.Cond, sc); err != nil {
			return err
		}
		return c.checkBlock(s.Body, sc)
	case *ForStmt:
		it, err := c.infer(s.Iter, sc)
		if err != nil {
			return err
		}
		if it.Kind != tList {
			return c.errf(s.Pos, "TypeError: for-in requires a List, got %s", it)
		}
		inner := newCScope(sc)
		if err := inner.declare(s.Var, &cVar{typ: it.Elem, init: true}, s.Pos); err != nil {
			return err
		}
		return c.checkBlock(s.Body, inner)
	case *DeclStmt:
		typ, err := c.resolveType(s.Type, s.Pos)
		if err != nil {
			return err
		}
		v := &cVar{typ: typ, init: false}
		if s.Init != nil {
			it, err := c.infer(s.Init, sc)
			if err != nil {
				return err
			}
			if !assignable(it, typ) {
				return c.errf(s.Pos, "TypeError: cannot assign %s to %s", it, typ)
			}
			v.init = true
		} else if typ.Kind == tStruct {
			v.init = true // 结构体零值实例可用
		}
		return sc.declare(s.Name, v, s.Pos)
	case *AssignStmt:
		t, err := c.infer(s.X, sc)
		if err != nil {
			return err
		}
		switch target := s.Target.(type) {
		case *Ident:
			v := sc.lookup(target.Name)
			if v == nil {
				return c.errf(target.Pos, "CompileError: assignment to undeclared identifier %q", target.Name)
			}
			if !assignable(t, v.typ) {
				return c.errf(s.Pos, "TypeError: cannot assign %s to %s", t, v.typ)
			}
			v.init = true
			return nil
		case *MemberExpr:
			objT, err := c.infer(target.X, sc)
			if err != nil {
				return err
			}
			if objT.Kind != tStruct {
				return c.errf(s.Pos, "TypeError: cannot assign member of %s", objT)
			}
			def, ok := c.structs[objT.FName]
			if !ok {
				return c.errf(s.Pos, "TypeError: unknown struct %s", objT.FName)
			}
			fieldT, ok := def.Types[target.Name]
			if !ok {
				return c.errf(target.Pos, "TypeError: no member %q on %s", target.Name, objT.FName)
			}
			ft, err := c.resolveType(fieldT, target.Pos)
			if err != nil {
				return err
			}
			if !assignable(t, ft) {
				return c.errf(s.Pos, "TypeError: cannot assign %s to %s.%s (%s)", t, objT.FName, target.Name, ft)
			}
			return nil
		}
		return c.errf(s.Pos, "TypeError: unsupported assignment target")
	}
	return nil
}

func (c *checker) requireBool(e Expr, sc *cScope) error {
	t, err := c.infer(e, sc)
	if err != nil {
		return err
	}
	if t.Kind != tBool {
		return c.errf(posOf(e), "TypeError: condition must be bool, got %s", t)
	}
	return nil
}

// posOf 返回表达式近似位置。
func posOf(e Expr) Pos {
	switch x := e.(type) {
	case *IntLit:
		return x.Pos
	case *FloatLit:
		return x.Pos
	case *StrLit:
		return x.Pos
	case *BoolLit:
		return x.Pos
	case *Ident:
		return x.Pos
	case *BinOp:
		return x.Pos
	case *UnOp:
		return x.Pos
	case *CallExpr:
		return x.Pos
	case *MemberExpr:
		return x.Pos
	case *ScopeCall:
		return x.Pos
	case *IndexExpr:
		return x.Pos
	}
	return Pos{}
}

// join 求列表字面量元素的公共类型。
func join(a, b *Type) *Type {
	if a == nil {
		return b
	}
	if a.Kind == tAny || b.Kind == tAny {
		return tAnyV
	}
	if a.Kind == b.Kind {
		if a.Kind == tList && a.Elem.Kind == b.Elem.Kind {
			return mkList(join(a.Elem, b.Elem))
		}
		return a
	}
	if (a.Kind == tInt && b.Kind == tFloat) || (a.Kind == tFloat && b.Kind == tInt) {
		return tFloatV
	}
	return tAnyV
}

func (c *checker) infer(e Expr, sc *cScope) (*Type, error) {
	switch x := e.(type) {
	case *IntLit:
		return tIntV, nil
	case *FloatLit:
		return tFloatV, nil
	case *StrLit:
		return tStringV, nil
	case *BoolLit:
		return tBoolV, nil
	case *Ident:
		if x.Name == "true" || x.Name == "false" {
			return tBoolV, nil
		}
		if x.Name == "memory" {
			return tMemoryV, nil
		}
		if x.Name == "taskm" {
			return tTaskmV, nil
		}
		if v := sc.lookup(x.Name); v != nil {
			if !v.init {
				return nil, c.errf(x.Pos, "CompileError: variable %q used before initialization", x.Name)
			}
			return v.typ, nil
		}
		if _, ok := c.fns[x.Name]; ok {
			return mkFunc(x.Name), nil
		}
		return nil, c.errf(x.Pos, "CompileError: undeclared identifier %q", x.Name)
	case *ListLit:
		var elem *Type
		for _, it := range x.Items {
			t, err := c.infer(it, sc)
			if err != nil {
				return nil, err
			}
			elem = join(elem, t)
		}
		if elem == nil {
			elem = tAnyV
		}
		return mkList(elem), nil
	case *UnOp:
		t, err := c.infer(x.X, sc)
		if err != nil {
			return nil, err
		}
		switch x.Op {
		case "*":
			if t.Kind != tList {
				return nil, c.errf(x.Pos, "TypeError: '*' requires a List, got %s", t)
			}
			return t.Elem, nil
		case "-":
			if t.Kind != tInt && t.Kind != tFloat {
				return nil, c.errf(x.Pos, "TypeError: unary '-' requires a number, got %s", t)
			}
			return t, nil
		case "!":
			if t.Kind != tBool {
				return nil, c.errf(x.Pos, "TypeError: '!' requires bool, got %s", t)
			}
			return tBoolV, nil
		}
		return nil, c.errf(x.Pos, "internal: unknown unary operator %s", x.Op)
	case *BinOp:
		return c.inferBin(x, sc)
	case *MemberExpr:
		recv, err := c.infer(x.X, sc)
		if err != nil {
			return nil, err
		}
		return c.memberType(recv, x.Name, x.Pos)
	case *CallExpr:
		return c.inferCall(x, sc)
	case *ScopeCall:
		return c.inferScope(x, sc)
	case *IndexExpr:
		t, err := c.infer(x.X, sc)
		if err != nil {
			return nil, err
		}
		if t.Kind != tList {
			return nil, c.errf(x.Pos, "TypeError: indexing requires a List, got %s", t)
		}
		it, err := c.infer(x.Idx, sc)
		if err != nil {
			return nil, err
		}
		if it.Kind != tInt {
			return nil, c.errf(x.Pos, "TypeError: index must be int, got %s", it)
		}
		return t.Elem, nil
	}
	return nil, c.errf(Pos{}, "internal: unknown expression node")
}

func isNumeric(t *Type) bool { return t.Kind == tInt || t.Kind == tFloat }

func (c *checker) inferBin(x *BinOp, sc *cScope) (*Type, error) {
	l, err := c.infer(x.L, sc)
	if err != nil {
		return nil, err
	}
	switch x.Op {
	case "&&", "||":
		if l.Kind != tBool {
			return nil, c.errf(x.Pos, "TypeError: %s requires bool operands, got %s", x.Op, l)
		}
		r, err := c.infer(x.R, sc)
		if err != nil {
			return nil, err
		}
		if r.Kind != tBool {
			return nil, c.errf(x.Pos, "TypeError: %s requires bool operands, got %s", x.Op, r)
		}
		return tBoolV, nil
	}
	r, err := c.infer(x.R, sc)
	if err != nil {
		return nil, err
	}
	switch x.Op {
	case "+":
		if l.Kind == tString || r.Kind == tString {
			return tStringV, nil
		}
		if !isNumeric(l) || !isNumeric(r) {
			return nil, c.errf(x.Pos, "TypeError: '+' requires numbers or a String, got %s and %s", l, r)
		}
		if l.Kind == tFloat || r.Kind == tFloat {
			return tFloatV, nil
		}
		return tIntV, nil
	case "-", "*", "/", "%":
		if !isNumeric(l) || !isNumeric(r) {
			return nil, c.errf(x.Pos, "TypeError: arithmetic requires numbers, got %s and %s", l, r)
		}
		if l.Kind == tFloat || r.Kind == tFloat {
			return tFloatV, nil
		}
		return tIntV, nil
	case "==", "!=":
		if !(isNumeric(l) && isNumeric(r)) && !(l.Kind == tString && r.Kind == tString) &&
			!(l.Kind == tBool && r.Kind == tBool) && !(l.Kind == tAny || r.Kind == tAny) {
			return nil, c.errf(x.Pos, "TypeError: cannot compare %s and %s", l, r)
		}
		return tBoolV, nil
	case "<", "<=", ">", ">=":
		if (isNumeric(l) && isNumeric(r)) || (l.Kind == tString && r.Kind == tString) {
			return tBoolV, nil
		}
		return nil, c.errf(x.Pos, "TypeError: cannot order-compare %s and %s", l, r)
	}
	return nil, c.errf(x.Pos, "internal: unknown operator %s", x.Op)
}

// memberType 检查成员访问（未声明成员 → 编译错误）。
func (c *checker) memberType(recv *Type, name string, pos Pos) (*Type, error) {
	switch recv.Kind {
	case tFuncBuffer, tTask:
		switch name {
		case "head":
			return mkList(tAnyV), nil
		case "tail":
			return mkList(tAnyV), nil
		case "log":
			return mkList(tStringV), nil
		}
	case tMemorize:
		// v0.1 memorize 为内置签名状态对象，无公开成员
	case tStruct:
		if def, ok := c.structs[recv.FName]; ok {
			if typ, exists := def.Types[name]; exists {
				return c.resolveType(typ, pos)
			}
		}
	}
	return nil, c.errf(pos, "TypeError: no member %q on %s", name, recv)
}

func (c *checker) checkArity(name string, want, got int, pos Pos) error {
	if want != got {
		return c.errf(pos, "CompileError: %s() expects %d args, got %d", name, want, got)
	}
	return nil
}

// methodType 检查方法调用（接收者类型 + 参数个数 + 参数类型）。
func (c *checker) methodType(recv *Type, name string, args []*Type, pos Pos) (*Type, error) {
	switch recv.Kind {
	case tList:
		switch name {
		case "head", "tail", "size":
			if err := c.checkArity(name, 0, len(args), pos); err != nil {
				return nil, err
			}
			return tIntV, nil
		case "next":
			if err := c.checkArity(name, 0, len(args), pos); err != nil {
				return nil, err
			}
			return recv.Elem, nil
		case "reset":
			if err := c.checkArity(name, 0, len(args), pos); err != nil {
				return nil, err
			}
			return recv, nil
		case "append":
			if err := c.checkArity(name, 1, len(args), pos); err != nil {
				return nil, err
			}
			if !assignable(args[0], recv.Elem) {
				return nil, c.errf(pos, "TypeError: append expects %s, got %s", recv.Elem, args[0])
			}
			return tNilV, nil
		case "appendAll":
			if err := c.checkArity(name, 1, len(args), pos); err != nil {
				return nil, err
			}
			if args[0].Kind != tList {
				return nil, c.errf(pos, "TypeError: appendAll requires a List, got %s", args[0])
			}
			return tNilV, nil
		case "toString":
			if err := c.checkArity(name, 0, len(args), pos); err != nil {
				return nil, err
			}
			return tStringV, nil
		case "__sort__":
			if err := c.checkArity(name, 0, len(args), pos); err != nil {
				return nil, err
			}
			return tNilV, nil
		}
	case tHashTable:
		switch name {
		case "put":
			if err := c.checkArity(name, 2, len(args), pos); err != nil {
				return nil, err
			}
			if !assignable(args[0], recv.Key) {
				return nil, c.errf(pos, "TypeError: put key expects %s, got %s", recv.Key, args[0])
			}
			if !assignable(args[1], recv.Val) {
				return nil, c.errf(pos, "TypeError: put value expects %s, got %s", recv.Val, args[1])
			}
			return tNilV, nil
		case "get":
			if err := c.checkArity(name, 1, len(args), pos); err != nil {
				return nil, err
			}
			return recv.Val, nil
		case "contains":
			if err := c.checkArity(name, 1, len(args), pos); err != nil {
				return nil, err
			}
			return tBoolV, nil
		case "remove":
			if err := c.checkArity(name, 1, len(args), pos); err != nil {
				return nil, err
			}
			return tNilV, nil
		case "size":
			if err := c.checkArity(name, 0, len(args), pos); err != nil {
				return nil, err
			}
			return tIntV, nil
		}
	case tFuncBuffer:
		if name == "execute" {
			if err := c.checkArity(name, 0, len(args), pos); err != nil {
				return nil, err
			}
			return tFuncBufferV, nil
		}
	case tIOStream:
		switch name {
		case "println", "print":
			return tNilV, nil
		case "setIn":
			if err := c.checkArity(name, 1, len(args), pos); err != nil {
				return nil, err
			}
			if args[0].Kind != tString && args[0].Kind != tInputStream {
				return nil, c.errf(pos, "TypeError: setIn requires a path String or an InputStream, got %s", args[0])
			}
			return tNilV, nil
		case "setOut":
			if err := c.checkArity(name, 1, len(args), pos); err != nil {
				return nil, err
			}
			if args[0].Kind != tString && args[0].Kind != tOutputStream {
				return nil, c.errf(pos, "TypeError: setOut requires a path String or an OutputStream, got %s", args[0])
			}
			return tNilV, nil
		case "readln":
			if err := c.checkArity(name, 0, len(args), pos); err != nil {
				return nil, err
			}
			return tStringV, nil
		}
	case tChannel:
		switch name {
		case "send":
			if err := c.checkArity(name, 1, len(args), pos); err != nil {
				return nil, err
			}
			return tNilV, nil
		case "recv":
			if err := c.checkArity(name, 0, len(args), pos); err != nil {
				return nil, err
			}
			return tAnyV, nil
		}
	case tTask:
		if name == "done" {
			if err := c.checkArity(name, 0, len(args), pos); err != nil {
				return nil, err
			}
			return tBoolV, nil
		}
	case tMemory:
		switch name {
		case "compact":
			if err := c.checkArity(name, 0, len(args), pos); err != nil {
				return nil, err
			}
			return tNilV, nil // compact() 根本不返回
		case "setBlock":
			if err := c.checkArity(name, 1, len(args), pos); err != nil {
				return nil, err
			}
			if args[0].Kind != tInt {
				return nil, c.errf(pos, "TypeError: setBlock(n) requires an int block size, got %s", args[0])
			}
			return tNilV, nil
		}
	case tTaskm:
		switch name {
		case "spawn":
			if len(args) < 1 {
				return nil, c.errf(pos, "CompileError: taskm.spawn requires a function reference as first arg")
			}
			if args[0].Kind != tFunc {
				return nil, c.errf(pos, "TypeError: taskm.spawn requires a function reference, got %s", args[0])
			}
			if args[0].FName != "" {
				if fn, ok := c.fns[args[0].FName]; ok {
					if len(fn.Params) != len(args)-1 {
						return nil, c.errf(pos, "CompileError: %s expects %d args, got %d", fn.Name, len(fn.Params), len(args)-1)
					}
					for i, p := range fn.Params {
						pt, err := c.paramType(p, pos)
						if err != nil {
							return nil, err
						}
						if !assignable(args[i+1], pt) {
							return nil, c.errf(pos, "TypeError: argument %d of %s: cannot assign %s to %s", i+1, fn.Name, args[i+1], pt)
						}
					}
				}
			}
			return tIntV, nil // spawn() 返回 pid
		case "block", "merge":
			if err := c.checkArity("taskm."+name, 1, len(args), pos); err != nil {
				return nil, err
			}
			if args[0].Kind != tTask && args[0].Kind != tInt {
				return nil, c.errf(pos, "TypeError: taskm.%s requires a Task or pid, got %s", name, args[0])
			}
			return tFuncBufferV, nil
		case "done":
			if err := c.checkArity("taskm.done", 1, len(args), pos); err != nil {
				return nil, err
			}
			if args[0].Kind != tInt {
				return nil, c.errf(pos, "TypeError: taskm.done requires a pid (int), got %s", args[0])
			}
			return tBoolV, nil
		case "channel":
			if len(args) == 1 {
				if args[0].Kind != tInt {
					return nil, c.errf(pos, "TypeError: taskm.channel(n) requires an int capacity, got %s", args[0])
				}
			} else if len(args) != 0 {
				return nil, c.errf(pos, "CompileError: taskm.channel() expects 0 or 1 args, got %d", len(args))
			}
			return tChannelV, nil
		}
	case tStruct:
		def, ok := c.impls[recv.FName]
		if !ok {
			return nil, c.errf(pos, "TypeError: type %s has no impl", recv.FName)
		}
		if fn, ok := def.SelfMethods[name]; ok {
			if len(fn.Params)-1 != len(args) {
				return nil, c.errf(pos, "CompileError: %s() expects %d args, got %d", name, len(fn.Params)-1, len(args))
			}
			for i, p := range fn.Params[1:] {
				pt, err := c.paramType(p, pos)
				if err != nil {
					return nil, err
				}
				if !assignable(args[i], pt) {
					return nil, c.errf(pos, "TypeError: argument %d of %s: cannot assign %s to %s", i+1, name, args[i], pt)
				}
			}
			if fn.Ret != "" {
				return c.resolveType(fn.Ret, pos)
			}
			return tFuncBufferV, nil
		}
		if _, ok := def.Methods[name]; ok {
			return nil, c.errf(pos, "TypeError: %s is a static method; call it via %s::%s(...)", name, recv.FName, name)
		}
	}
	return nil, c.errf(pos, "TypeError: no method %q on %s", name, recv)
}

func (c *checker) inferCall(x *CallExpr, sc *cScope) (*Type, error) {
	// 签名包装（§6）：fn(args) @sign(prefix)
	if x.Sign != nil {
		id, ok := x.Fn.(*Ident)
		if !ok {
			return nil, c.errf(x.Pos, "TypeError: a signature can only wrap a direct function call")
		}
		fn, ok := c.fns[id.Name]
		if !ok {
			return nil, c.errf(id.Pos, "CompileError: undeclared function %q", id.Name)
		}
		if want, ok := c.sigs[x.Sign.Name]; ok {
			if len(x.Sign.Args) != want {
				return nil, c.errf(x.Pos, "CompileError: signature %q takes %d <Prefix> argument(s), got %d", x.Sign.Name, want, len(x.Sign.Args))
			}
			if x.Sign.Name == "memorize" && len(x.Sign.Args) == 1 {
				pt, err := c.infer(x.Sign.Args[0], sc)
				if err != nil {
					return nil, err
				}
				if pt.Kind != tMemorize {
					return nil, c.errf(x.Pos, "TypeError: @memorize prefix must be a memorize buffer, got %s", pt)
				}
			}
		} else {
			// 用户类型实现 Sign：签名名 = 类型名，必须有静态 call(prefix, fb)
			def, ok := c.impls[x.Sign.Name]
			if !ok {
				return nil, c.errf(x.Pos, "CompileError: %q is not a registered signature (its type does not implement Sign)", x.Sign.Name)
			}
			if len(x.Sign.Args) != 1 {
				return nil, c.errf(x.Pos, "CompileError: signature %q takes 1 <Prefix> argument, got %d", x.Sign.Name, len(x.Sign.Args))
			}
			callFn, ok := def.Methods["call"]
			if !ok || len(callFn.Params) != 2 {
				return nil, c.errf(x.Pos, "CompileError: type %q does not implement Sign (no static call(prefix, fb))", x.Sign.Name)
			}
			pt, err := c.infer(x.Sign.Args[0], sc)
			if err != nil {
				return nil, err
			}
			wantT, err := c.paramType(callFn.Params[0], x.Pos)
			if err != nil {
				return nil, err
			}
			if !assignable(pt, wantT) {
				return nil, c.errf(x.Pos, "TypeError: @%s prefix must be %s, got %s", x.Sign.Name, wantT, pt)
			}
		}
		if err := c.checkCallArgs(fn, x.Args, sc, x.Pos); err != nil {
			return nil, err
		}
		if x.Sign.Name == "async" {
			return tTaskV, nil
		}
		return tFuncBufferV, nil
	}
	// 方法调用
	if m, ok := x.Fn.(*MemberExpr); ok {
		recv, err := c.infer(m.X, sc)
		if err != nil {
			return nil, err
		}
		args, err := c.inferArgs(x.Args, sc)
		if err != nil {
			return nil, err
		}
		return c.methodType(recv, m.Name, args, m.Pos)
	}
	id, ok := x.Fn.(*Ident)
	if !ok {
		return nil, c.errf(x.Pos, "TypeError: this expression is not callable")
	}
	args, err := c.inferArgs(x.Args, sc)
	if err != nil {
		return nil, err
	}
	if fn, ok := c.fns[id.Name]; ok {
		if err := c.checkCallArgs(fn, x.Args, sc, x.Pos); err != nil {
			return nil, err
		}
		if fn.Ret != "" {
			return c.resolveType(fn.Ret, x.Pos)
		}
		return tFuncBufferV, nil
	}
	switch id.Name {
	case "FileInputStream":
		if err := c.checkArity("FileInputStream", 1, len(args), id.Pos); err != nil {
			return nil, err
		}
		if args[0].Kind != tString {
			return nil, c.errf(id.Pos, "TypeError: FileInputStream requires a path String, got %s", args[0])
		}
		return tInputStreamV, nil
	case "FileOutputStream":
		if err := c.checkArity("FileOutputStream", 1, len(args), id.Pos); err != nil {
			return nil, err
		}
		if args[0].Kind != tString {
			return nil, c.errf(id.Pos, "TypeError: FileOutputStream requires a path String, got %s", args[0])
		}
		return tOutputStreamV, nil
	case "ConsoleInputStream":
		if err := c.checkArity("ConsoleInputStream", 0, len(args), id.Pos); err != nil {
			return nil, err
		}
		return tInputStreamV, nil
	case "ConsoleOutputStream":
		if err := c.checkArity("ConsoleOutputStream", 0, len(args), id.Pos); err != nil {
			return nil, err
		}
		return tOutputStreamV, nil
	}
	return nil, c.errf(id.Pos, "CompileError: undeclared function %q", id.Name)
}

func (c *checker) inferArgs(args []Expr, sc *cScope) ([]*Type, error) {
	out := make([]*Type, 0, len(args))
	for _, a := range args {
		t, err := c.infer(a, sc)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

// checkCallArgs 检查实参与形参个数、类型。
func (c *checker) checkCallArgs(fn *Func, args []Expr, sc *cScope, pos Pos) error {
	argTys, err := c.inferArgs(args, sc)
	if err != nil {
		return err
	}
	if len(argTys) != len(fn.Params) {
		return c.errf(pos, "CompileError: %s expects %d args, got %d", fn.Name, len(fn.Params), len(argTys))
	}
	for i, p := range fn.Params {
		pt, err := c.paramType(p, pos)
		if err != nil {
			return err
		}
		if !assignable(argTys[i], pt) {
			return c.errf(pos, "TypeError: argument %d of %s: cannot assign %s to %s", i+1, fn.Name, argTys[i], pt)
		}
	}
	return nil
}

func (c *checker) inferScope(x *ScopeCall, sc *cScope) (*Type, error) {
	args, err := c.inferArgs(x.Args, sc)
	if err != nil {
		return nil, err
	}
	switch x.Scope {
	case "memorize":
		if x.Name == "new" && len(args) == 0 {
			return tMemorizeV, nil
		}
		return nil, c.errf(x.Pos, "TypeError: memorize has no static method %q", x.Name)
	case "HashTable":
		if x.Name == "new" && len(args) == 0 {
			return mkTable(tAnyV, tAnyV), nil
		}
		return nil, c.errf(x.Pos, "TypeError: HashTable has no static method %q", x.Name)
	case "List":
		if x.Name == "new" && len(args) == 0 {
			return mkList(tAnyV), nil
		}
		return nil, c.errf(x.Pos, "TypeError: List has no static method %q", x.Name)
	case "IO":
		switch x.Name {
		case "setIn":
			if err := c.checkArity("IO::setIn", 2, len(args), x.Pos); err != nil {
				return nil, err
			}
			if args[0].Kind != tIOStream {
				return nil, c.errf(x.Pos, "TypeError: IO::setIn's first arg must be an IOStream, got %s", args[0])
			}
			if args[1].Kind != tString && args[1].Kind != tInputStream {
				return nil, c.errf(x.Pos, "TypeError: IO::setIn requires a path String or an InputStream, got %s", args[1])
			}
			return tNilV, nil
		case "setOut":
			if err := c.checkArity("IO::setOut", 2, len(args), x.Pos); err != nil {
				return nil, err
			}
			if args[0].Kind != tIOStream {
				return nil, c.errf(x.Pos, "TypeError: IO::setOut's first arg must be an IOStream, got %s", args[0])
			}
			if args[1].Kind != tString && args[1].Kind != tOutputStream {
				return nil, c.errf(x.Pos, "TypeError: IO::setOut requires a path String or an OutputStream, got %s", args[1])
			}
			return tNilV, nil
		}
		return nil, c.errf(x.Pos, "TypeError: IO has no static method %q", x.Name)
	case "taskm":
		// taskm 是全局变量：正确语法是 taskm.spawn(...) 等
		return nil, c.errf(x.Pos, "TypeError: taskm is a global variable — use taskm.spawn(...) / taskm.block(pid) / taskm.done(pid) / taskm.merge(pid) / taskm.channel([n])")
	case "GlobalMemory":
		switch x.Name {
		case "compact":
			if err := c.checkArity("GlobalMemory::compact", 0, len(args), x.Pos); err != nil {
				return nil, err
			}
			return tNilV, nil // compact() 根本不返回
		case "setBlock":
			if err := c.checkArity("GlobalMemory::setBlock", 1, len(args), x.Pos); err != nil {
				return nil, err
			}
			if args[0].Kind != tInt {
				return nil, c.errf(x.Pos, "TypeError: GlobalMemory::setBlock(n) requires an int, got %s", args[0])
			}
			return tNilV, nil
		}
		return nil, c.errf(x.Pos, "TypeError: GlobalMemory has no static method %q", x.Name)
	}
	if def, ok := c.impls[x.Scope]; ok {
		fn, ok := def.Methods[x.Name]
		if !ok {
			return nil, c.errf(x.Pos, "TypeError: %s has no static method %q", x.Scope, x.Name)
		}
		if len(fn.Params) != len(args) {
			return nil, c.errf(x.Pos, "CompileError: %s::%s expects %d args, got %d", x.Scope, x.Name, len(fn.Params), len(args))
		}
		for i, p := range fn.Params {
			pt, err := c.paramType(p, x.Pos)
			if err != nil {
				return nil, err
			}
			if !assignable(args[i], pt) {
				return nil, c.errf(x.Pos, "TypeError: argument %d of %s::%s: cannot assign %s to %s", i+1, x.Scope, x.Name, args[i], pt)
			}
		}
		if fn.Ret != "" {
			return c.resolveType(fn.Ret, x.Pos)
		}
		return tFuncBufferV, nil
	}
	return nil, c.errf(x.Pos, "CompileError: unknown scope %q", x.Scope)
}
