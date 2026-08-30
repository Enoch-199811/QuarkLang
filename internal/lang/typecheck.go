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
	tPtr
	tCopyd
	tNull
	tTypeVar
)

type Type struct {
	Kind   tKind
	Elem   *Type
	Key    *Type
	Val    *Type
	FName  string           // tFunc: 函数名（"" = 未知）；tStruct: 结构体名
	Args   []*Type          // tStruct: 泛型实例实参
	Fields map[string]*Type // tStruct 匿名（FName="."）：字段类型表
}

func mk(k tKind) *Type         { return &Type{Kind: k} }
func mkList(e *Type) *Type     { return &Type{Kind: tList, Elem: e} }
func mkTable(k, v *Type) *Type { return &Type{Kind: tHashTable, Key: k, Val: v} }
func mkFunc(name string) *Type { return &Type{Kind: tFunc, FName: name} }

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
	tFunc: "func", tStruct: "struct", tTaskm: "taskm", tPtr: "ptr", tCopyd: "Copyd", tNull: "null", tTypeVar: "typevar",
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
		if len(t.Args) > 0 {
			parts := make([]string, len(t.Args))
			for i, a := range t.Args {
				parts[i] = a.String()
			}
			return t.FName + "<" + strings.Join(parts, ", ") + ">"
		}
		return t.FName
	case tPtr:
		return t.Elem.String() + "&"
	case tCopyd:
		return "Copyd<" + t.Elem.String() + ">"
	case tNull:
		return "null"
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
	// 指针：值可赋给指针；null 可赋给指针
	if to.Kind == tPtr {
		if from.Kind == tNull {
			return true
		}
		if from.Kind == tPtr {
			return assignable(from.Elem, to.Elem) || to.Elem.Kind == tAny || from.Elem.Kind == tAny
		}
		return assignable(from, to.Elem) || to.Elem.Kind == tAny
	}
	// 类型变量（泛型函数）：与任何类型互相可赋
	if from.Kind == tTypeVar || to.Kind == tTypeVar {
		return true
	}
	// Copyd：与内部类型互相可赋
	if from.Kind == tCopyd {
		return assignable(from.Elem, to)
	}
	if to.Kind == tCopyd {
		return assignable(from, to.Elem)
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
		if from.FName != to.FName || len(from.Args) != len(to.Args) {
			return false
		}
		for i := range from.Args {
			a, b := from.Args[i], to.Args[i]
			if a.Kind != tAny && b.Kind != tAny && !assignable(a, b) {
				return false
			}
		}
		return true
	case tPtr:
		return assignable(from.Elem, to.Elem)
	case tCopyd:
		return assignable(from.Elem, to.Elem)
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
		return tAnyV, nil // v2：void = 空接口 interface{} 的默认名字
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
	case "Task", "thread":
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
	typ     *Type
	init    bool
	isConst bool
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
	curSubst   map[string]*Type // 泛型方法体/调用点的类型参数替换
	typeVars   map[string]bool  // 泛型函数当前作用域的类型参数（func<T,...>）
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
		c.fns[f.Name] = &Func{Name: f.Name, TypeParams: f.TypeParams, Params: f.Params, Ret: f.Ret, Body: f.Body, Pos: f.Pos}
	}
	for _, s := range prog.Structs {
		if _, dup := c.structs[s.Name]; dup {
			return &CheckError{Msg: fmt.Sprintf("CompileError: duplicate struct %q", s.Name), Pos: s.Pos}
		}
		def := &StructDef{Name: s.Name, TypeParams: s.TypeParams, Types: map[string]string{}}
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
		// 泛型规则：struct 有泛型参数时 impl 必须引入同样的参数；struct 无参数时 impl 不许有
		if sd, ok := c.structs[im.Type]; ok {
			if len(sd.TypeParams) > 0 && len(im.TypeParams) != len(sd.TypeParams) {
				return &CheckError{Msg: fmt.Sprintf("CompileError: struct %s has %d type parameter(s) — impl must introduce the same parameters (impl<T> {...} %s)", im.Type, len(sd.TypeParams), im.Type), Pos: im.Pos}
			}
			if len(sd.TypeParams) == 0 && len(im.TypeParams) > 0 {
				return &CheckError{Msg: fmt.Sprintf("CompileError: struct %s has no type parameters, but impl declares %d", im.Type, len(im.TypeParams)), Pos: im.Pos}
			}
		}
		def, ok := c.impls[im.Type]
		if !ok {
			def = &ImplDef{Type: im.Type, Iface: im.Iface, TypeParams: im.TypeParams, Methods: map[string]*Func{}, SelfMethods: map[string]*Func{}}
			c.impls[im.Type] = def
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
			fn := &Func{Name: m.Name, TypeParams: m.TypeParams, Params: m.Params, Ret: m.Ret, Body: m.Body, Pos: m.Pos}
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
		subst := map[string]*Type{}
		for _, tp := range im.TypeParams {
			subst[tp] = tAnyV // 泛型方法体：参数按 interface{} 宽松检查
		}
		prev := c.curSubst
		c.curSubst = subst
		for _, m := range im.Methods {
			if err := c.checkFunc(&Func{Name: m.Name, TypeParams: m.TypeParams, Params: m.Params, Ret: m.Ret, Body: m.Body, Pos: m.Pos}); err != nil {
				c.curSubst = prev
				return err
			}
		}
		c.curSubst = prev
	}
	return nil
}

// resolveType 解析类型注解（无替换上下文）。
func (c *checker) resolveType(s string, pos Pos) (*Type, error) {
	return c.substType(s, nil, pos)
}

// substType 解析类型注解，并对泛型类型参数做替换（subst: 参数名 → 具体类型）。
// 支持：T（参数名）、T&（指针）、node<T>（泛型实例）、List<T>/HashTable<K,V>、
// Copyd<T>、int[Copyd]/int[]（≈Copyd<Array>/Array）、null。
func (c *checker) substType(s string, subst map[string]*Type, pos Pos) (*Type, error) {
	s = strings.TrimSpace(s)
	if s == "null" {
		return &Type{Kind: tNull}, nil
	}
	if s == "interface{}" {
		return tAnyV, nil
	}
	if s == "" {
		return nil, &CheckError{Msg: "CompileError: empty type annotation", Pos: pos}
	}
	// 指针后缀：T&
	if strings.HasSuffix(s, "&") {
		e, err := c.substType(strings.TrimSuffix(s, "&"), subst, pos)
		if err != nil {
			return nil, err
		}
		return &Type{Kind: tPtr, Elem: e}, nil
	}
	base, inner, suffix := splitType(s)
	// 裸类型参数替换（无内层、无后缀）
	if subst != nil && inner == "" && suffix == "" {
		if t, ok := subst[base]; ok {
			return t, nil
		}
	}
	// 数值基元 + 数组/Copyd 后缀
	switch base {
	case "int", "long", "char":
		if suffix == "[]" {
			return mkList(tIntV), nil
		}
		if suffix == "Copyd" {
			return &Type{Kind: tCopyd, Elem: mkList(tIntV)}, nil // int[Copyd] = Copyd<Array<int>>
		}
		return tIntV, nil
	case "float", "double":
		if suffix == "[]" {
			return mkList(tFloatV), nil
		}
		if suffix == "Copyd" {
			return &Type{Kind: tCopyd, Elem: mkList(tFloatV)}, nil
		}
		return tFloatV, nil
	case "String":
		return tStringV, nil
	case "bool":
		return tBoolV, nil
	case "void":
		return tAnyV, nil // v2：void = 空接口 interface{} 的默认名字
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
	case "Task", "thread":
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
		e := tAnyV
		if inner != "" {
			var err error
			e, err = c.substType(inner, subst, pos)
			if err != nil {
				return nil, err
			}
		}
		return mkList(e), nil
	case "Copyd":
		if inner == "" {
			return &Type{Kind: tCopyd, Elem: tAnyV}, nil
		}
		e, err := c.substType(inner, subst, pos)
		if err != nil {
			return nil, err
		}
		return &Type{Kind: tCopyd, Elem: e}, nil
	case "HashTable":
		k, v, err := splitTopComma(inner)
		if err != nil {
			return nil, &CheckError{Msg: "CompileError: " + err.Error(), Pos: pos}
		}
		kt, err := c.substType(k, subst, pos)
		if err != nil {
			return nil, err
		}
		vt, err := c.substType(v, subst, pos)
		if err != nil {
			return nil, err
		}
		return mkTable(kt, vt), nil
	}
	// 泛型结构体实例：node / node<T> / node<T, U>
	if def, ok := c.structs[base]; ok {
		var args []*Type
		if inner != "" {
			for _, a := range splitTopCommas(inner) {
				at, err := c.substType(a, subst, pos)
				if err != nil {
					return nil, err
				}
				args = append(args, at)
			}
			if len(def.TypeParams) > 0 && len(args) != len(def.TypeParams) {
				return nil, &CheckError{Msg: fmt.Sprintf("CompileError: %s takes %d type argument(s), got %d", base, len(def.TypeParams), len(args)), Pos: pos}
			}
		}
		return &Type{Kind: tStruct, FName: base, Args: args}, nil
	}
	if _, ok := c.interfaces[base]; ok {
		return tAnyV, nil
	}
	// 泛型函数的类型变量（func<T,...>）：T 在作用域内即为类型变量
	if c.typeVars[base] {
		return &Type{Kind: tTypeVar, FName: base}, nil
	}
	return nil, &CheckError{Msg: fmt.Sprintf("CompileError: unknown type %q", s), Pos: pos}
}

// splitTopCommas 按顶层逗号切分（忽略尖括号内逗号）。
func splitTopCommas(s string) []string {
	var parts []string
	depth, start := 0, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '<':
			depth++
		case '>':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	if rest := strings.TrimSpace(s[start:]); rest != "" {
		parts = append(parts, rest)
	}
	return parts
}

func (c *checker) errf(pos Pos, format string, args ...interface{}) error {
	return &CheckError{Msg: fmt.Sprintf(format, args...), Pos: pos}
}

func (c *checker) paramType(p Param, pos Pos) (*Type, error) {
	return c.substType(p.Type, c.curSubst, p.Pos)
}

func (c *checker) checkFunc(f *Func) error {
	// 泛型函数：类型参数先进入作用域（返回类型/参数都可用 T）
	prevVars := c.typeVars
	if len(f.TypeParams) > 0 {
		c.typeVars = map[string]bool{}
		for _, tp := range f.TypeParams {
			c.typeVars[tp] = true
		}
	}
	defer func() { c.typeVars = prevVars }()

	var retType *Type
	if f.Ret != "" {
		t, err := c.substType(f.Ret, c.curSubst, f.Pos)
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
	case *LogStmt:
		_, err := c.infer(s.X, sc)
		return err
	case *TryStmt:
		if err := c.checkBlock(s.Try, sc); err != nil {
			return err
		}
		ct, err := c.resolveType(s.CatchVarType, s.Pos)
		if err != nil {
			return err
		}
		inner := newCScope(sc)
		if err := inner.declare(s.CatchVar, &cVar{typ: ct, init: true}, s.Pos); err != nil {
			return err
		}
		return c.checkBlock(s.Catch, inner)
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
		// 变量修饰：copyd = 传时复制（类型标注追加 [Copyd]）；const = 常量
		typStr := s.Type
		if s.Decor == "copyd" {
			typStr = typStr + "[Copyd]"
		}
		typ, err := c.substType(typStr, c.curSubst, s.Pos)
		if err != nil {
			return err
		}
		v := &cVar{typ: typ, init: false, isConst: s.Decor == "const"}
		if s.Init != nil {
			it, err := c.infer(s.Init, sc)
			if err != nil {
				return err
			}
			if !assignable(it, typ) {
				return c.errf(s.Pos, "TypeError: cannot assign %s to %s", it, typ)
			}
			v.init = true
		} else if typ.Kind == tStruct || typ.Kind == tPtr {
			v.init = true // 结构体零值实例 / 指针零值（null）可用
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
			if v.isConst {
				return c.errf(target.Pos, "CompileError: const 变量 %q 不可重新赋值", target.Name)
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
			ft, err := c.substType(fieldT, c.instanceSubst(def, objT), target.Pos)
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
	case *NullLit:
		return x.Pos
	case *StructLit:
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
	case *NullLit:
		return &Type{Kind: tNull}, nil
	case *StructLit:
		// 匿名结构体字面量（.{in,out} 等）：带字段类型表的匿名结构体
		fields := map[string]*Type{}
		for _, f := range x.Fields {
			ft, err := c.infer(f.X, sc)
			if err != nil {
				return nil, err
			}
			fields[f.Name] = ft
		}
		return &Type{Kind: tStruct, FName: ".", Fields: fields}, nil
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
		ok := (isNumeric(l) && isNumeric(r)) || (l.Kind == tString && r.Kind == tString) ||
			(l.Kind == tBool && r.Kind == tBool) || (l.Kind == tAny || r.Kind == tAny) ||
			(l.Kind == tNull || r.Kind == tNull) || (l.Kind == tPtr || r.Kind == tPtr) ||
			(l.Kind == tStruct && r.Kind == tStruct) || (l.Kind == tCopyd || r.Kind == tCopyd)
		if !ok {
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
		if recv.Fields != nil {
			if ft, ok := recv.Fields[name]; ok {
				return ft, nil
			}
			return nil, c.errf(pos, "TypeError: no member %q on 匿名结构体", name)
		}
		if def, ok := c.structs[recv.FName]; ok {
			if typ, exists := def.Types[name]; exists {
				subst := c.instanceSubst(def, recv)
				return c.substType(typ, subst, pos)
			}
		}
	case tPtr:
		// 指针成员访问自动解引用
		return c.memberType(recv.Elem, name, pos)
	case tCopyd:
		return c.memberType(recv.Elem, name, pos)
	}
	return nil, c.errf(pos, "TypeError: no member %q on %s", name, recv)
}

// instanceSubst 构造实例的类型参数替换表（泛型方法体内的 curSubst 优先）。
func (c *checker) instanceSubst(def *StructDef, recv *Type) map[string]*Type {
	subst := map[string]*Type{}
	for i, tp := range def.TypeParams {
		if i < len(recv.Args) {
			subst[tp] = recv.Args[i]
		} else {
			subst[tp] = tAnyV
		}
	}
	for k, v := range c.curSubst {
		subst[k] = v
	}
	return subst
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
		// thread 类方法（xmind：merge / pid / talk）
		switch name {
		case "merge":
			if len(args) < 1 {
				return nil, c.errf(pos, "CompileError: thread.merge(fn, args...) requires at least 1 arg")
			}
			if args[0].Kind != tFunc {
				return nil, c.errf(pos, "TypeError: thread.merge second arg must be a function reference, got %s", args[0])
			}
			if args[0].FName != "" {
				if fn, ok := c.fns[args[0].FName]; ok && len(fn.Params) != len(args)-1 {
					return nil, c.errf(pos, "CompileError: %s expects %d args, got %d", fn.Name, len(fn.Params), len(args)-1)
				}
			}
			return tNilV, nil
		case "pid":
			if err := c.checkArity(name, 0, len(args), pos); err != nil {
				return nil, err
			}
			return tIntV, nil
		case "talk":
			if err := c.checkArity(name, 1, len(args), pos); err != nil {
				return nil, err
			}
			if args[0].Kind != tChannel {
				return nil, c.errf(pos, "TypeError: thread.talk requires a channel, got %s", args[0])
			}
			return tNilV, nil
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
			// xmind：taskm.spawn() 无参，返回 thread 类
			if len(args) != 0 {
				return nil, c.errf(pos, "CompileError: taskm.spawn() takes no args, got %d", len(args))
			}
			return tTaskV, nil
		case "merge":
			// v2：taskm.merge(pid, fn, args...)
			if len(args) < 2 {
				return nil, c.errf(pos, "CompileError: taskm.merge(pid, fn, args...) requires at least 2 args, got %d", len(args))
			}
			if args[0].Kind != tInt {
				return nil, c.errf(pos, "TypeError: taskm.merge first arg must be a pid (int), got %s", args[0])
			}
			if args[1].Kind != tFunc {
				return nil, c.errf(pos, "TypeError: taskm.merge second arg must be a function reference, got %s", args[1])
			}
			if args[1].FName != "" {
				if fn, ok := c.fns[args[1].FName]; ok {
					if len(fn.Params) != len(args)-2 {
						return nil, c.errf(pos, "CompileError: %s expects %d args, got %d", fn.Name, len(fn.Params), len(args)-2)
					}
					for i, p := range fn.Params {
						pt, err := c.paramType(p, pos)
						if err != nil {
							return nil, err
						}
						if !assignable(args[i+2], pt) {
							return nil, c.errf(pos, "TypeError: argument %d of %s: cannot assign %s to %s", i+1, fn.Name, args[i+2], pt)
						}
					}
				}
			}
			return tNilV, nil
		case "block":
			// v2：taskm.block(pid) 返回 void
			if err := c.checkArity("taskm.block", 1, len(args), pos); err != nil {
				return nil, err
			}
			if args[0].Kind != tTask && args[0].Kind != tInt {
				return nil, c.errf(pos, "TypeError: taskm.block requires a pid, got %s", args[0])
			}
			return tNilV, nil
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
		if sd, ok := c.structs[recv.FName]; ok {
			prev := c.curSubst
			c.curSubst = c.instanceSubst(sd, recv)
			defer func() { c.curSubst = prev }()
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
				return c.substType(fn.Ret, c.curSubst, pos)
			}
			return tFuncBufferV, nil
		}
		if _, ok := def.Methods[name]; ok {
			return nil, c.errf(pos, "TypeError: %s is a static method; call it via %s::%s(...)", name, recv.FName, name)
		}
	case tPtr:
		// 指针方法调用自动解引用
		return c.methodType(recv.Elem, name, args, pos)
	case tCopyd:
		if name == "ptr" {
			if err := c.checkArity(name, 0, len(args), pos); err != nil {
				return nil, err
			}
			return recv.Elem, nil // .ptr() 取出 Copyd 包装的地址
		}
		return c.methodType(recv.Elem, name, args, pos)
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
		// v2 签名：@mb(prefix) —— mb 是变量（Sign 实例），结果类型 = 被包装函数返回类型
		if err := c.checkCallArgs(fn, x.Args, sc, x.Pos); err != nil {
			return nil, err
		}
		// mb 必须在作用域中（Sign 实例变量）
		if v := sc.lookup(x.Sign.Name); v == nil {
			return nil, c.errf(x.Pos, "CompileError: @%s —— 签名名必须是作用域中的 Sign 实例变量（mb）", x.Sign.Name)
		}
		// 结果类型：被包装函数 fn 的返回类型
		if fn.Ret != "" {
			return c.substType(fn.Ret, c.curSubst, x.Pos)
		}
		return tNilV, nil
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
			if len(fn.TypeParams) > 0 {
				gs, err := c.inferGenSubst(fn, x.Args, sc)
				if err != nil {
					return nil, err
				}
				return c.substType(fn.Ret, gs, x.Pos)
			}
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

// inferGenSubst 从实参推断泛型函数的类型参数（func<T,...>）。
func (c *checker) inferGenSubst(fn *Func, args []Expr, sc *cScope) (map[string]*Type, error) {
	argTys, err := c.inferArgs(args, sc)
	if err != nil {
		return nil, err
	}
	genSubst := map[string]*Type{}
	for i, p := range fn.Params {
		for _, tp := range fn.TypeParams {
			if genSubst[tp] == nil && strings.Contains(p.Type, tp) && i < len(argTys) {
				genSubst[tp] = argTys[i]
			}
		}
	}
	return genSubst, nil
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
	genSubst := map[string]*Type{}
	if len(fn.TypeParams) > 0 {
		genSubst, err = c.inferGenSubst(fn, args, sc)
		if err != nil {
			return err
		}
	}
	for i, p := range fn.Params {
		pt, err := c.substType(p.Type, genSubst, pos)
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
		// 泛型静态方法：类型参数按 interface{} 宽松替换
		prev := c.curSubst
		subst := map[string]*Type{}
		for _, tp := range def.TypeParams {
			subst[tp] = tAnyV
		}
		c.curSubst = subst
		defer func() { c.curSubst = prev }()
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
			return c.substType(fn.Ret, c.curSubst, x.Pos)
		}
		return tFuncBufferV, nil
	}
	return nil, c.errf(x.Pos, "CompileError: unknown scope %q", x.Scope)
}
