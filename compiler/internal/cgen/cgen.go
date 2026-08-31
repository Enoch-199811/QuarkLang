// Package cgen 将 QuarkLang 子集编译为 LLVM IR（跨系统编译器，LLVM 后端）。
// 支持：变量声明/赋值（int/String）、io.println/print、算术与取模、
// 比较、&&/||、布尔字面量、if/else、while。
package cgen

import (
	"fmt"
	"strings"
)

// Transpile 把 QuarkLang 源码编译为 LLVM IR。
func Transpile(src string) (string, error) {
	p := &parser{src: src, pos: 0}
	if err := p.parseProgram(); err != nil {
		return "", err
	}
	e := &emitter{}
	return e.emitProgram(p.funcs, p.stmts), nil
}

// ---------- LLVM IR 发射器 ----------

type strConst struct {
	name string
	size int
}

type varInfo struct {
	reg    string
	isStr  bool
	isList bool
	param  bool // 函数参数：值寄存器，直接使用不 load
	direct bool // SSA 直通：单赋值变量直接用寄存器（免 alloca/load/store）
}

type emitter struct {
	b            strings.Builder // 模块头（printf 声明 + 字符串常量）
	body         strings.Builder // 函数体（基本块 + 指令）
	cur          string          // 当前基本块名
	blockCount   int
	vars         map[string]varInfo
	regCount     int
	strs         map[string]strConst
	strCount     int
	funcReturned bool            // 函数已 ret（后续语句不可达，跳过生成）
	assigned     map[string]bool // 被赋值变量（SSA 直通判定）
}

func (e *emitter) emitInstr(f string, args ...interface{}) {
	e.body.WriteString("  " + fmt.Sprintf(f, args...) + "\n")
}

func (e *emitter) newBlock() string {
	e.blockCount++
	return fmt.Sprintf("b%d", e.blockCount)
}

// setBlock 切换到命名基本块（首次切换写出块标签）。
func (e *emitter) setBlock(name string) {
	if e.cur != name {
		e.body.WriteString("\n" + name + ":\n")
		e.cur = name
	}
}

func (e *emitter) newReg() string {
	e.regCount++
	return fmt.Sprintf("%%%d", e.regCount)
}

func (e *emitter) strConst(s string) strConst {
	if e.strs == nil {
		e.strs = map[string]strConst{}
	}
	if c, ok := e.strs[s]; ok {
		return c
	}
	e.strCount++
	c := strConst{name: fmt.Sprintf("@.str%d", e.strCount), size: len(s) + 1}
	e.strs[s] = c
	fmt.Fprintf(&e.b, "%s = private unnamed_addr constant [%d x i8] c\"%s\", align 1\n",
		c.name, c.size, llvmEscape([]byte(s)))
	return c
}

func (e *emitter) i8Ptr(c strConst) string {
	r := e.newReg()
	e.emitInstr("%s = getelementptr inbounds [%d x i8], [%d x i8]* %s, i64 0, i64 0",
		r, c.size, c.size, c.name)
	return r
}

// llvmEscape 把字节序列转义为 LLVM IR 字符串体（可打印字符原样，其余 \XX，含结尾 NUL）。
func llvmEscape(b []byte) string {
	var sb strings.Builder
	for _, x := range append(b, 0) {
		switch {
		case x >= 0x20 && x <= 0x7e && x != '"' && x != '\\':
			sb.WriteByte(x)
		default:
			fmt.Fprintf(&sb, "\\%02X", x)
		}
	}
	return sb.String()
}

func (e *emitter) emitProgram(funcs []*funcDef, mainStmts []stmt) string {
	e.b.WriteString("declare i32 @printf(i8* noundef, ...)\n")
	e.b.WriteString("declare i8* @malloc(i64)\n")
	e.b.WriteString("declare void @free(i8*)\n")
	e.b.WriteString("declare i8* @realloc(i8*, i64)\n\n")
	e.vars = map[string]varInfo{}
	// 预注册全部字符串常量
	e.strConst("true")
	e.strConst("false")
	for _, fd := range funcs {
		for _, s := range fd.body {
			e.preRegisterStmt(s)
		}
	}
	for _, s := range mainStmts {
		e.preRegisterStmt(s)
	}
	// 预扫描被赋值变量（决定 SSA 直通）
	e.assigned = map[string]bool{}
	for _, fd := range funcs {
		scanAssigned(fd.body, e.assigned)
	}
	scanAssigned(mainStmts, e.assigned)
	// 先生成全部函数体（期间动态字符串常量追加到 e.b），最后统一组装：常量全部先于函数
	var bodies strings.Builder
	for _, fd := range funcs {
		if fd.name != "main" {
			bodies.WriteString(e.emitFunc(fd))
		}
	}
	e.cur = "entry"
	e.body.Reset()
	e.funcReturned = false
	e.body.WriteString("entry:\n")
	e.emitBlock(mainStmts)
	e.emitInstr("ret i32 0")
	bodies.WriteString("define i32 @main() {\n" + e.body.String() + "}\n")
	return e.b.String() + bodies.String()
}

// emitFunc 生成单个非 main 函数的定义。
func (e *emitter) emitFunc(fd *funcDef) string {
	e.regCount = 0
	e.blockCount = 0
	e.body.Reset()
	e.vars = map[string]varInfo{}
	var sig strings.Builder
	sig.WriteString("define i32 @" + fd.name + "(")
	for i, p := range fd.params {
		if i > 0 {
			sig.WriteString(", ")
		}
		reg := fmt.Sprintf("%%p%d", i)
		sig.WriteString("i32 " + reg)
		e.vars[p] = varInfo{reg: reg, param: true}
	}
	sig.WriteString(")\n")
	e.cur = "entry"
	e.body.WriteString("entry:\n")
	e.funcReturned = false
	e.emitBlock(fd.body)
	if !e.funcReturned {
		e.emitInstr("ret i32 0")
	}
	return sig.String() + "{\n" + e.body.String() + "}\n"
}

func (e *emitter) preRegisterStmt(s stmt) {
	switch st := s.(type) {
	case *printlnStmt:
		for _, a := range st.args {
			e.preRegister(a)
		}
	case *exprStmt:
		e.preRegister(st.x)
	case *declStmt:
		e.preRegister(st.init)
	case *assignStmt:
		e.preRegister(st.x)
	case *ifStmt:
		e.preRegister(st.cond)
		for _, s2 := range st.then {
			e.preRegisterStmt(s2)
		}
		for _, s2 := range st.els {
			e.preRegisterStmt(s2)
		}
	case *whileStmt:
		e.preRegister(st.cond)
		for _, s2 := range st.body {
			e.preRegisterStmt(s2)
		}
	}
}

func (e *emitter) preRegister(x *expr) {
	if x == nil {
		return
	}
	if x.kind == kString {
		x.sc = e.strConst(x.s)
	}
	e.preRegister(x.l)
	e.preRegister(x.r)
}

func (e *emitter) emitBlock(stmts []stmt) {
	for _, s := range stmts {
		if e.funcReturned {
			return // ret 之后的语句不可达，跳过
		}
		e.emitStmt(s)
	}
}

func (e *emitter) emitStmt(s stmt) {
	switch st := s.(type) {
	case *exprStmt:
		e.compileExpr(st.x) // 副作用求值，结果丢弃
	case *returnStmt:
		v, _ := e.compileExpr(st.x)
		e.emitInstr("ret i32 %s", v)
		e.funcReturned = true
	case *printlnStmt:
		e.emitPrintln(st)
	case *declStmt:
		if st.typ == "List<int>" {
			// List<int> = [a, b, c]：List 编译为 {i32*, i32}（ptr + len），支持 size()/get(i)
			lit := st.init.lst
			n := len(lit.items)
			reg := e.newReg()
			e.emitInstr("%s = alloca { i32*, i32 }, align 8", reg)
			mc := e.newReg()
			e.emitInstr("%s = call i8* @malloc(i64 %d)", mc, n*4)
			p := e.newReg()
			e.emitInstr("%s = bitcast i8* %s to i32*", p, mc)
			for i, it := range lit.items {
				v, _ := e.compileExpr(it)
				if i == 0 {
					e.emitInstr("store i32 %s, i32* %s", v, p)
				} else {
					g2 := e.newReg()
					e.emitInstr("%s = getelementptr inbounds i32, i32* %s, i64 %d", g2, p, i)
					e.emitInstr("store i32 %s, i32* %s", v, g2)
				}
			}
			pf := e.newReg()
			e.emitInstr("%s = getelementptr inbounds { i32*, i32 }, { i32*, i32 }* %s, i32 0, i32 0", pf, reg)
			e.emitInstr("store i32* %s, i32** %s", p, pf)
			lf := e.newReg()
			e.emitInstr("%s = getelementptr inbounds { i32*, i32 }, { i32*, i32 }* %s, i32 0, i32 1", lf, reg)
			e.emitInstr("store i32 %d, i32* %s", n, lf)
			e.vars[st.name] = varInfo{reg: reg, isList: true}
			break
		}
		// 先分配再求值：保证 SSA 寄存器编号单调递增
		if st.typ == "int" && !e.assigned[st.name] && !e.funcReturned {
			// SSA 直通：单赋值 int 变量直接用寄存器（免 alloca/load/store）
			v, _ := e.compileExpr(st.init)
			e.vars[st.name] = varInfo{reg: v, direct: true}
			break
		}
		reg := e.newReg()
		if st.typ == "String" {
			e.emitInstr("%s = alloca i8*, align 8", reg)
			v, _ := e.compileExpr(st.init)
			e.emitInstr("store i8* %s, i8** %s", v, reg)
			e.vars[st.name] = varInfo{reg: reg, isStr: true}
		} else {
			e.emitInstr("%s = alloca i32, align 4", reg)
			v, _ := e.compileExpr(st.init)
			e.emitInstr("store i32 %s, i32* %s", v, reg)
			e.vars[st.name] = varInfo{reg: reg}
		}
	case *deleteStmt:
		// delete variable; —— 编译路径 = free（空闲队列语义由运行时内存管理器承担）
		info, ok := e.vars[st.name]
		if !ok {
			break
		}
		if info.isList {
			pf := e.newReg()
			e.emitInstr("%s = getelementptr inbounds { i32*, i32 }, { i32*, i32 }* %s, i32 0, i32 0", pf, info.reg)
			lp := e.newReg()
			e.emitInstr("%s = load i32*, i32** %s", lp, pf)
			c := e.newReg()
			e.emitInstr("%s = bitcast i32* %s to i8*", c, lp)
			e.emitInstr("call void @free(i8* %s)", c)
		} else {
			c := e.newReg()
			e.emitInstr("%s = bitcast i32* %s to i8*", c, info.reg)
			e.emitInstr("call void @free(i8* %s)", c)
		}
	case *assignStmt:
		info, ok := e.vars[st.name]
		if !ok {
			// 编译器侧严格检查：未声明变量
			e.emitInstr("; undeclared variable %s", st.name)
			return
		}
		v, _ := e.compileExpr(st.x)
		if info.isStr {
			e.emitInstr("store i8* %s, i8** %s", v, info.reg)
		} else {
			e.emitInstr("store i32 %s, i32* %s", v, info.reg)
		}
	case *ifStmt:
		thenB, endB := e.newBlock(), e.newBlock()
		var elseB string
		if len(st.els) > 0 {
			elseB = e.newBlock()
		}
		c := e.compileCond(st.cond)
		if elseB != "" {
			e.emitInstr("br i1 %s, label %%%s, label %%%s", c, thenB, elseB)
		} else {
			e.emitInstr("br i1 %s, label %%%s, label %%%s", c, thenB, endB)
		}
		saved := e.funcReturned
		e.setBlock(thenB)
		e.emitBlock(st.then)
		thenRet := e.funcReturned
		e.funcReturned = saved
		if !thenRet {
			e.emitInstr("br label %%%s", endB)
		}
		if elseB != "" {
			e.setBlock(elseB)
			e.emitBlock(st.els)
			elseRet := e.funcReturned
			e.funcReturned = saved
			if !elseRet {
				e.emitInstr("br label %%%s", endB)
			}
		}
		e.funcReturned = saved
		e.setBlock(endB)
	case *whileStmt:
		condB, bodyB, endB := e.newBlock(), e.newBlock(), e.newBlock()
		e.emitInstr("br label %%%s", condB)
		e.setBlock(condB)
		c := e.compileCond(st.cond)
		e.emitInstr("br i1 %s, label %%%s, label %%%s", c, bodyB, endB)
		saved := e.funcReturned
		e.setBlock(bodyB)
		e.emitBlock(st.body)
		bodyRet := e.funcReturned
		e.funcReturned = saved
		if !bodyRet {
			e.emitInstr("br label %%%s", condB)
		}
		e.funcReturned = saved
		e.setBlock(endB)
	}
}

func (e *emitter) emitPrintln(s *printlnStmt) {
	type av struct {
		val string
		typ byte // 'i' i32, 's' i8*, 'b' i1
	}
	var args []av
	for _, a := range s.args {
		v, t := e.compileExpr(a)
		args = append(args, av{v, t})
	}
	// 按编译后实际类型生成格式串（字符串与布尔都是 %s）
	fmts := make([]string, len(args))
	for i, a := range args {
		if a.typ == 's' || a.typ == 'b' {
			fmts[i] = "%s"
		} else {
			fmts[i] = "%d"
		}
	}
	fp := e.i8Ptr(e.strConst(strings.Join(fmts, " ") + "\n"))
	// 参数指令（含 select/gep）全部生成完毕后，再取 call 的寄存器号（SSA 单调）
	var lineArgs []string
	for _, a := range args {
		switch a.typ {
		case 's':
			lineArgs = append(lineArgs, "i8* "+a.val)
		case 'b':
			// 布尔 → select "true"/"false" 字符串（先算 gep 再取号，保持 SSA 单调）
			tp := e.i8Ptr(e.strs["true"])
			fp2 := e.i8Ptr(e.strs["false"])
			r := e.newReg()
			e.emitInstr("%s = select i1 %s, i8* %s, i8* %s", r, a.val, tp, fp2)
			lineArgs = append(lineArgs, "i8* "+r)
		default:
			lineArgs = append(lineArgs, "i32 "+a.val)
		}
	}
	line := "  " + e.newReg() + " = call i32 (i8*, ...) @printf(i8* " + fp
	for _, a := range lineArgs {
		line += ", " + a
	}
	line += ")\n"
	e.body.WriteString(line)
}

// compileCond 编译条件表达式为 i1 值。
func (e *emitter) compileCond(x *expr) string {
	switch x.kind {
	case kBool:
		if x.b {
			return "true"
		}
		return "false"
	case kCmp:
		l, _ := e.compileExpr(x.l)
		r, _ := e.compileExpr(x.r)
		reg := e.newReg()
		e.emitInstr("%s = icmp %s i32 %s, %s", reg, x.op, l, r)
		return reg
	case kAndOr:
		if x.op == "!" {
			r := e.compileCond(x.l)
			reg := e.newReg()
			e.emitInstr("%s = xor i1 %s, true", reg, r)
			return reg
		}
		l := e.compileCond(x.l)
		r := e.compileCond(x.r)
		reg := e.newReg()
		op := "and"
		if x.op == "||" {
			op = "or"
		}
		e.emitInstr("%s = %s i1 %s, %s", reg, op, l, r)
		return reg
	default:
		v, _ := e.compileExpr(x)
		reg := e.newReg()
		e.emitInstr("%s = icmp ne i32 %s, 0", reg, v)
		return reg
	}
}

// compileExpr 编译表达式，返回 (值引用, 类型 'i' i32 / 's' i8* / 'b' i1)。
func (e *emitter) compileExpr(x *expr) (string, byte) {
	switch x.kind {
	case kInt:
		return fmt.Sprintf("%d", x.i), 'i'
	case kString:
		return e.i8Ptr(x.sc), 's'
	case kBool:
		if x.b {
			return "true", 'b'
		}
		return "false", 'b'
	case kIdent:
		info, ok := e.vars[x.s]
		if !ok {
			return x.s, 'i' // 未声明变量：让生成的 IR 报错（llvm-as 校验会拦截）
		}
		if info.param || info.direct {
			return info.reg, 'i' // 参数/SSA 直通变量：值寄存器直接使用
		}
		reg := e.newReg()
		if info.isStr {
			e.emitInstr("%s = load i8*, i8** %s", reg, info.reg)
			return reg, 's'
		}
		e.emitInstr("%s = load i32, i32* %s", reg, info.reg)
		return reg, 'i'
	case kCmp, kAndOr:
		c := e.compileCond(x)
		return c, 'b'
	case kMethod:
		// List 方法：size() / get(i)
		info, ok := e.vars[x.method.name]
		if !ok {
			return "0", 'i'
		}
		switch x.method.method {
		case "size":
			lf := e.newReg()
			e.emitInstr("%s = getelementptr inbounds { i32*, i32 }, { i32*, i32 }* %s, i32 0, i32 1", lf, info.reg)
			v := e.newReg()
			e.emitInstr("%s = load i32, i32* %s", v, lf)
			return v, 'i'
		case "get":
			pf := e.newReg()
			e.emitInstr("%s = getelementptr inbounds { i32*, i32 }, { i32*, i32 }* %s, i32 0, i32 0", pf, info.reg)
			lp := e.newReg()
			e.emitInstr("%s = load i32*, i32** %s", lp, pf)
			i, _ := e.compileExpr(x.method.args[0])
			g2 := e.newReg()
			e.emitInstr("%s = getelementptr inbounds i32, i32* %s, i64 %s", g2, lp, i)
			v := e.newReg()
			e.emitInstr("%s = load i32, i32* %s", v, g2)
			return v, 'i'
		case "append":
			// l.append(v)：realloc 扩容 + 写入 + len++
			pf := e.newReg()
			e.emitInstr("%s = getelementptr inbounds { i32*, i32 }, { i32*, i32 }* %s, i32 0, i32 0", pf, info.reg)
			lp := e.newReg()
			e.emitInstr("%s = load i32*, i32** %s", lp, pf)
			lf := e.newReg()
			e.emitInstr("%s = getelementptr inbounds { i32*, i32 }, { i32*, i32 }* %s, i32 0, i32 1", lf, info.reg)
			l1 := e.newReg()
			e.emitInstr("%s = load i32, i32* %s", l1, lf)
			// realloc(lp, (l1+1)*4)
			n1 := e.newReg()
			e.emitInstr("%s = add i32 %s, 1", n1, l1)
			n64 := e.newReg()
			e.emitInstr("%s = sext i32 %s to i64", n64, n1)
			sz := e.newReg()
			e.emitInstr("%s = mul i64 %s, 4", sz, n64)
			bc := e.newReg()
			e.emitInstr("%s = bitcast i32* %s to i8*", bc, lp)
			rc := e.newReg()
			e.emitInstr("%s = call i8* @realloc(i8* %s, i64 %s)", rc, bc, sz)
			np := e.newReg()
			e.emitInstr("%s = bitcast i8* %s to i32*", np, rc)
			e.emitInstr("store i32* %s, i32** %s", np, pf)
			l1i := e.newReg()
			e.emitInstr("%s = sext i32 %s to i64", l1i, l1)
			g2 := e.newReg()
			e.emitInstr("%s = getelementptr inbounds i32, i32* %s, i64 %s", g2, np, l1i)
			v, _ := e.compileExpr(x.method.args[0])
			e.emitInstr("store i32 %s, i32* %s", v, g2)
			l2 := e.newReg()
			e.emitInstr("%s = add i32 %s, 1", l2, l1)
			e.emitInstr("store i32 %s, i32* %s", l2, lf)
			return "0", 'i'
		}
		return "0", 'i'
	case kIndex:
		// list[i]：取结构体 ptr 字段再下标（{i32*, i32}）
		info, ok := e.vars[x.idx.name]
		if !ok {
			return "0", 'i'
		}
		pf := e.newReg()
		e.emitInstr("%s = getelementptr inbounds { i32*, i32 }, { i32*, i32 }* %s, i32 0, i32 0", pf, info.reg)
		lp := e.newReg()
		e.emitInstr("%s = load i32*, i32** %s", lp, pf)
		i, _ := e.compileExpr(x.idx.i)
		g2 := e.newReg()
		e.emitInstr("%s = getelementptr inbounds i32, i32* %s, i64 %s", g2, lp, i)
		v := e.newReg()
		e.emitInstr("%s = load i32, i32* %s", v, g2)
		return v, 'i'
	case kCall:
		// 函数调用：call i32 @name(i32 %a, ...)
		argRegs := make([]string, 0, len(x.call.args))
		for _, a := range x.call.args {
			v, _ := e.compileExpr(a)
			argRegs = append(argRegs, v)
		}
		line := "  " + e.newReg() + " = call i32 @" + x.call.name
		if len(argRegs) > 0 {
			line += "(i32 " + strings.Join(argRegs, ", i32 ") + ")"
		}
		line += "\n"
		e.body.WriteString(line)
		return strings.Fields(line)[0], 'i' // 返回 %N（call 结果寄存器）
	case kBin:
		// 简单优化：递归常量折叠（1 + 2*3 → 7 编译期算掉，IR 更小编译更快）
		if v, ok := constEval(x); ok {
			return fmt.Sprintf("%d", int32(v)), 'i'
		}
		l, _ := e.compileExpr(x.l)
		r, _ := e.compileExpr(x.r)
		op := map[string]string{"+": "add", "-": "sub", "*": "mul", "/": "sdiv", "%": "srem", "<<": "shl", ">>": "ashr"}[x.op]
		reg := e.newReg()
		e.emitInstr("%s = %s i32 %s, %s", reg, op, l, r)
		return reg, 'i'
	}
	return "0", 'i'
}

// ---------- AST ----------

type exprKind int

const (
	kInt exprKind = iota
	kString
	kIdent
	kBin
	kBool
	kCmp
	kAndOr
	kCall
	kList
	kIndex
	kMethod
)

type expr struct {
	kind   exprKind
	i      int64
	b      bool
	s      string
	op     string
	l, r   *expr
	call   *callExpr   // kCall
	lst    *listLit    // kList
	idx    *indexExpr  // kIndex
	method *methodExpr // kMethod
	sc     strConst    // 预注册的字符串常量（kString）
}

type indexExpr struct {
	name string
	i    *expr
}

type methodExpr struct {
	name   string
	method string
	args   []*expr
}

type stmt interface{}

type printlnStmt struct {
	args []*expr
}

type declStmt struct {
	name string
	typ  string
	init *expr
}

type assignStmt struct {
	name string
	x    *expr
}

type ifStmt struct {
	cond *expr
	then []stmt
	els  []stmt
}

type whileStmt struct {
	cond *expr
	body []stmt
}

// ---------- 递归下降解析器 ----------

type funcDef struct {
	name   string
	params []string
	ret    string
	body   []stmt
}

type callExpr struct {
	name string
	args []*expr
}

type returnStmt struct {
	x *expr
}

type listLit struct {
	items []*expr
}

type deleteStmt struct {
	name string
}

type exprStmt struct {
	x *expr
}

type varInfoList struct {
	reg    string
	isList bool
}

type parser struct {
	src   string
	pos   int
	funcs []*funcDef
	stmts []stmt // main 的函数体
}

func (p *parser) errf(format string, args ...interface{}) error {
	return fmt.Errorf(format, args...)
}

func (p *parser) skipSpace() {
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			p.pos++
			continue
		}
		if c == '/' && p.pos+1 < len(p.src) && p.src[p.pos+1] == '/' {
			for p.pos < len(p.src) && p.src[p.pos] != '\n' {
				p.pos++
			}
			continue
		}
		return
	}
}

func (p *parser) isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func (p *parser) isIdentPart(c byte) bool {
	return p.isIdentStart(c) || (c >= '0' && c <= '9')
}

func (p *parser) lexIdent() string {
	start := p.pos
	for p.pos < len(p.src) && p.isIdentPart(p.src[p.pos]) {
		p.pos++
	}
	return p.src[start:p.pos]
}

func (p *parser) expect(ch byte) error {
	if p.pos >= len(p.src) || p.src[p.pos] != ch {
		return p.errf("expected %q at pos %d", string(ch), p.pos)
	}
	p.pos++
	return nil
}

func (p *parser) expectIdent() (string, error) {
	if p.pos >= len(p.src) || !p.isIdentStart(p.src[p.pos]) {
		return "", p.errf("expected identifier at pos %d", p.pos)
	}
	return p.lexIdent(), nil
}

func (p *parser) parseProgram() error {
	p.skipSpace()
	for p.pos < len(p.src) {
		name, err := p.expectIdent()
		if err != nil {
			return err
		}
		if name != "func" {
			return p.errf("compiler v0.2 supports only func declarations, got %q", name)
		}
		if err := p.parseFunc(); err != nil {
			return err
		}
		p.skipSpace()
	}
	return nil
}

// parseFunc 解析多函数声明：func name(p1 T1, ...) [ret] { body }。
func (p *parser) parseFunc() error {
	p.skipSpace()
	name, err := p.expectIdent()
	if err != nil {
		return err
	}
	fd := &funcDef{name: name}
	p.skipSpace()
	if p.pos < len(p.src) && p.src[p.pos] == '(' {
		p.pos++
		for {
			p.skipSpace()
			if p.pos < len(p.src) && p.src[p.pos] == ')' {
				p.pos++
				break
			}
			pn, err := p.expectIdent()
			if err != nil {
				return err
			}
			fd.params = append(fd.params, pn)
			p.skipSpace()
			// 参数类型（int/String/...，编译器 v0.2 只支持 int 语义）
			if p.pos < len(p.src) && p.isIdentStart(p.src[p.pos]) {
				p.lexIdent()
			}
			p.skipSpace()
			if p.pos < len(p.src) && p.src[p.pos] == ',' {
				p.pos++
				continue
			}
		}
	}
	p.skipSpace()
	// 返回类型（'{' 前的标识符；main 无返回类型 = void）
	if p.pos < len(p.src) && p.isIdentStart(p.src[p.pos]) {
		fd.ret = p.lexIdent()
		p.skipSpace()
	}
	if err := p.expect('{'); err != nil {
		return err
	}
	stmts, err := p.parseBlock()
	if err != nil {
		return err
	}
	fd.body = stmts
	p.funcs = append(p.funcs, fd)
	if name == "main" {
		p.stmts = stmts
	}
	return nil
}

// parseBlock 解析语句列表，直到 '}'（消费之）。
func (p *parser) parseBlock() ([]stmt, error) {
	var stmts []stmt
	for {
		p.skipSpace()
		if p.pos >= len(p.src) {
			return nil, p.errf("unterminated block")
		}
		if p.src[p.pos] == '}' {
			p.pos++
			return stmts, nil
		}
		stmtStart := p.pos
		first, err := p.expectIdent()
		if err != nil {
			return nil, err
		}
		p.skipSpace()
		switch first {
		case "delete":
			// delete variable; —— 加入空闲队列（编译路径 = free）
			p.skipSpace()
			target, err := p.expectIdent()
			if err != nil {
				return nil, err
			}
			p.skipSpace()
			if err := p.expect(';'); err != nil {
				return nil, err
			}
			stmts = append(stmts, &deleteStmt{name: target})
		case "return":
			x, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			p.skipSpace()
			if err := p.expect(';'); err != nil {
				return nil, err
			}
			stmts = append(stmts, &returnStmt{x: x})
		case "io":
			if err := p.expect('.'); err != nil {
				return nil, err
			}
			m, err := p.expectIdent()
			if err != nil {
				return nil, err
			}
			if m != "println" && m != "print" {
				return nil, p.errf("compiler v0.2 supports only io.println/io.print, got %q", m)
			}
			p.skipSpace()
			if err := p.expect('('); err != nil {
				return nil, err
			}
			var args []*expr
			p.skipSpace()
			if p.pos < len(p.src) && p.src[p.pos] != ')' {
				for {
					a, err := p.parseExpr()
					if err != nil {
						return nil, err
					}
					args = append(args, a)
					p.skipSpace()
					if p.pos < len(p.src) && p.src[p.pos] == ',' {
						p.pos++
						p.skipSpace()
						continue
					}
					break
				}
			}
			if err := p.expect(')'); err != nil {
				return nil, err
			}
			p.skipSpace()
			if err := p.expect(';'); err != nil {
				return nil, err
			}
			if m == "println" {
				stmts = append(stmts, &printlnStmt{args: args})
			}
		case "if":
			if err := p.expect('('); err != nil {
				return nil, err
			}
			cond, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			if err := p.expect(')'); err != nil {
				return nil, err
			}
			p.skipSpace()
			if err := p.expect('{'); err != nil {
				return nil, err
			}
			thenStmts, err := p.parseBlock()
			if err != nil {
				return nil, err
			}
			var els []stmt
			p.skipSpace()
			if p.pos+3 < len(p.src) && p.src[p.pos:p.pos+4] == "else" {
				p.pos += 4
				p.skipSpace()
				if err := p.expect('{'); err != nil {
					return nil, err
				}
				els, err = p.parseBlock()
				if err != nil {
					return nil, err
				}
			}
			stmts = append(stmts, &ifStmt{cond: cond, then: thenStmts, els: els})
		case "while":
			if err := p.expect('('); err != nil {
				return nil, err
			}
			cond, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			if err := p.expect(')'); err != nil {
				return nil, err
			}
			p.skipSpace()
			if err := p.expect('{'); err != nil {
				return nil, err
			}
			body, err := p.parseBlock()
			if err != nil {
				return nil, err
			}
			stmts = append(stmts, &whileStmt{cond: cond, body: body})
		default:
			// 声明（name Type = expr;）或赋值（name = expr;）
			if p.pos < len(p.src) && p.src[p.pos] == '=' {
				p.pos++
				p.skipSpace()
				x, err := p.parseExpr()
				if err != nil {
					return nil, err
				}
				p.skipSpace()
				if err := p.expect(';'); err != nil {
					return nil, err
				}
				stmts = append(stmts, &assignStmt{name: first, x: x})
			} else if p.pos < len(p.src) && p.isIdentStart(p.src[p.pos]) {
				typ := p.parseTypeName()
				p.skipSpace()
				if err := p.expect('='); err != nil {
					return nil, p.errf("expected '=' in declaration of %q", first)
				}
				p.skipSpace()
				init, err := p.parseExpr()
				if err != nil {
					return nil, err
				}
				p.skipSpace()
				if err := p.expect(';'); err != nil {
					return nil, err
				}
				stmts = append(stmts, &declStmt{name: first, typ: typ, init: init})
			} else {
				// 表达式语句：expr;（如 l.size();）——回溯到语句起点完整解析
				p.pos = stmtStart
				x, err := p.parseExpr()
				if err != nil {
					return nil, err
				}
				p.skipSpace()
				if err := p.expect(';'); err != nil {
					return nil, err
				}
				stmts = append(stmts, &exprStmt{x: x})
			}
		}
	}
}

// constEval 递归求值常量表达式（字面量算术折叠）。
func constEval(x *expr) (int64, bool) {
	if x == nil {
		return 0, false
	}
	if x.kind == kInt {
		return x.i, true
	}
	if x.kind == kBin {
		l, ok1 := constEval(x.l)
		r, ok2 := constEval(x.r)
		if !ok1 || !ok2 {
			return 0, false
		}
		switch x.op {
		case "+":
			return l + r, true
		case "-":
			return l - r, true
		case "*":
			return l * r, true
		case "/":
			if r != 0 {
				return l / r, true
			}
		case "%":
			if r != 0 {
				return l % r, true
			}
		case "<<":
			return int64(int32(l) << uint(r&31)), true
		case ">>":
			return int64(int32(l) >> uint(r&31)), true
		}
	}
	return 0, false
}

// scanAssigned 收集语句中所有被赋值（assignStmt）的变量名 —— 决定哪些 decl 可 SSA 直通。
func scanAssigned(stmts []stmt, out map[string]bool) {
	for _, s := range stmts {
		switch st := s.(type) {
		case *assignStmt:
			out[st.name] = true
		case *ifStmt:
			scanAssigned(st.then, out)
			scanAssigned(st.els, out)
		case *whileStmt:
			scanAssigned(st.body, out)
		}
	}
}

// parseTypeName 解析类型注解：int / String / List<int>（编译器支持的子集）。
func (p *parser) parseTypeName() string {
	t := p.lexIdent()
	if t == "List" && p.pos < len(p.src) && p.src[p.pos] == '<' {
		p.pos++
		for p.pos < len(p.src) && p.src[p.pos] != '>' {
			p.pos++
		}
		if p.pos < len(p.src) {
			p.pos++
		}
		return "List<int>"
	}
	return t
}

func (p *parser) peekChar() byte {
	if p.pos < len(p.src) {
		return p.src[p.pos]
	}
	return 0
}

func (p *parser) parseExpr() (*expr, error) {
	p.skipSpace()
	return p.parseOr()
}

func (p *parser) parseOr() (*expr, error) {
	l, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for {
		p.skipSpace()
		if p.pos+1 < len(p.src) && p.src[p.pos] == '|' && p.src[p.pos+1] == '|' {
			p.pos += 2
			r, err := p.parseAnd()
			if err != nil {
				return nil, err
			}
			l = &expr{kind: kAndOr, op: "||", l: l, r: r}
			continue
		}
		return l, nil
	}
}

func (p *parser) parseAnd() (*expr, error) {
	l, err := p.parseCmp()
	if err != nil {
		return nil, err
	}
	for {
		p.skipSpace()
		if p.pos+1 < len(p.src) && p.src[p.pos] == '&' && p.src[p.pos+1] == '&' {
			p.pos += 2
			r, err := p.parseCmp()
			if err != nil {
				return nil, err
			}
			l = &expr{kind: kAndOr, op: "&&", l: l, r: r}
			continue
		}
		return l, nil
	}
}

func (p *parser) parseCmp() (*expr, error) {
	l, err := p.parseAdd()
	if err != nil {
		return nil, err
	}
	for {
		p.skipSpace()
		if p.pos >= len(p.src) {
			return l, nil
		}
		op := ""
		switch {
		case p.src[p.pos] == '=' && p.pos+1 < len(p.src) && p.src[p.pos+1] == '=':
			op = "eq"
		case p.src[p.pos] == '!' && p.pos+1 < len(p.src) && p.src[p.pos+1] == '=':
			op = "ne"
		case p.src[p.pos] == '<' && p.pos+1 < len(p.src) && p.src[p.pos+1] == '=':
			op = "sle"
		case p.src[p.pos] == '>' && p.pos+1 < len(p.src) && p.src[p.pos+1] == '=':
			op = "sge"
		case p.src[p.pos] == '<':
			op = "slt"
		case p.src[p.pos] == '>':
			op = "sgt"
		}
		if op == "" {
			return l, nil
		}
		if op == "eq" || op == "ne" || op == "sle" || op == "sge" {
			p.pos += 2
		} else {
			p.pos++
		}
		r, err := p.parseAdd()
		if err != nil {
			return nil, err
		}
		l = &expr{kind: kCmp, op: op, l: l, r: r}
	}
}

func (p *parser) parseAdd() (*expr, error) {
	l, err := p.parseMul()
	if err != nil {
		return nil, err
	}
	for {
		p.skipSpace()
		if p.pos >= len(p.src) || (p.src[p.pos] != '+' && p.src[p.pos] != '-') {
			return l, nil
		}
		op := string(p.src[p.pos])
		p.pos++
		r, err := p.parseMul()
		if err != nil {
			return nil, err
		}
		l = &expr{kind: kBin, op: op, l: l, r: r}
	}
}

func (p *parser) parseMul() (*expr, error) {
	l, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	for {
		p.skipSpace()
		// 位移 << >>
		if p.pos+1 < len(p.src) && ((p.src[p.pos] == '<' && p.src[p.pos+1] == '<') || (p.src[p.pos] == '>' && p.src[p.pos+1] == '>')) {
			op := string(p.src[p.pos]) + string(p.src[p.pos+1])
			p.pos += 2
			r, err := p.parsePrimary()
			if err != nil {
				return nil, err
			}
			l = &expr{kind: kBin, op: op, l: l, r: r}
			continue
		}
		if p.pos >= len(p.src) || (p.src[p.pos] != '*' && p.src[p.pos] != '/' && p.src[p.pos] != '%') {
			return l, nil
		}
		op := string(p.src[p.pos])
		p.pos++
		r, err := p.parsePrimary()
		if err != nil {
			return nil, err
		}
		l = &expr{kind: kBin, op: op, l: l, r: r}
	}
}

func (p *parser) parsePrimary() (*expr, error) {
	p.skipSpace()
	if p.pos >= len(p.src) {
		return nil, p.errf("unexpected end of expression")
	}
	c := p.src[p.pos]
	switch {
	case c >= '0' && c <= '9':
		start := p.pos
		for p.pos < len(p.src) && p.src[p.pos] >= '0' && p.src[p.pos] <= '9' {
			p.pos++
		}
		var v int64
		for _, d := range p.src[start:p.pos] {
			v = v*10 + int64(d-'0')
		}
		return &expr{kind: kInt, i: v}, nil
	case c == '"':
		p.pos++
		var sb strings.Builder
		for {
			if p.pos >= len(p.src) {
				return nil, p.errf("unterminated string literal")
			}
			ch := p.src[p.pos]
			if ch == '"' {
				p.pos++
				break
			}
			if ch == '\\' && p.pos+1 < len(p.src) {
				n := p.src[p.pos+1]
				switch n {
				case 'n':
					sb.WriteByte('\n')
				case 't':
					sb.WriteByte('\t')
				case 'r':
					sb.WriteByte('\r')
				case '"':
					sb.WriteByte('"')
				case '\\':
					sb.WriteByte('\\')
				default:
					sb.WriteByte(n)
				}
				p.pos += 2
				continue
			}
			sb.WriteByte(ch)
			p.pos++
		}
		return &expr{kind: kString, s: sb.String()}, nil
	case p.isIdentStart(c):
		name := p.lexIdent()
		if name == "true" {
			return &expr{kind: kBool, b: true}, nil
		}
		if name == "false" {
			return &expr{kind: kBool, b: false}, nil
		}
		// 函数调用：name(args)
		save := p.pos
		p.skipSpace()
		if p.pos < len(p.src) && p.src[p.pos] == '(' {
			p.pos++
			call := &callExpr{name: name}
			for {
				p.skipSpace()
				if p.pos < len(p.src) && p.src[p.pos] == ')' {
					p.pos++
					break
				}
				a, err := p.parseExpr()
				if err != nil {
					return nil, err
				}
				call.args = append(call.args, a)
				p.skipSpace()
				if p.pos < len(p.src) && p.src[p.pos] == ',' {
					p.pos++
					continue
				}
			}
			return &expr{kind: kCall, call: call}, nil
		}
		p.pos = save
		// 下标访问：name[expr]
		p.skipSpace()
		if p.pos < len(p.src) && p.src[p.pos] == '[' {
			p.pos++
			ie, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			p.skipSpace()
			if err := p.expect(']'); err != nil {
				return nil, err
			}
			return &expr{kind: kIndex, idx: &indexExpr{name: name, i: ie}}, nil
		}
		// 方法调用：name.method(args)（如 l.size() / l.get(i)）
		p.skipSpace()
		if p.pos < len(p.src) && p.src[p.pos] == '.' {
			p.pos++
			m, err := p.expectIdent()
			if err != nil {
				return nil, err
			}
			me := &methodExpr{name: name, method: m}
			p.skipSpace()
			if p.pos < len(p.src) && p.src[p.pos] == '(' {
				p.pos++
				for {
					p.skipSpace()
					if p.pos < len(p.src) && p.src[p.pos] == ')' {
						p.pos++
						break
					}
					a, err := p.parseExpr()
					if err != nil {
						return nil, err
					}
					me.args = append(me.args, a)
					p.skipSpace()
					if p.pos < len(p.src) && p.src[p.pos] == ',' {
						p.pos++
						continue
					}
				}
			}
			return &expr{kind: kMethod, method: me}, nil
		}
		return &expr{kind: kIdent, s: name}, nil
	case c == '-':
		p.pos++
		inner, err := p.parsePrimary()
		if err != nil {
			return nil, err
		}
		return &expr{kind: kBin, op: "-", l: &expr{kind: kInt}, r: inner}, nil
	case c == '!':
		p.pos++
		inner, err := p.parsePrimary()
		if err != nil {
			return nil, err
		}
		return &expr{kind: kAndOr, op: "!", l: inner}, nil
	case c == '(':
		p.pos++
		e, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		p.skipSpace()
		if err := p.expect(')'); err != nil {
			return nil, err
		}
		return e, nil
	case c == '[':
		p.pos++
		lit := &listLit{}
		for {
			p.skipSpace()
			if p.pos < len(p.src) && p.src[p.pos] == ']' {
				p.pos++
				break
			}
			it, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			lit.items = append(lit.items, it)
			p.skipSpace()
			if p.pos < len(p.src) && p.src[p.pos] == ',' {
				p.pos++
				continue
			}
		}
		return &expr{kind: kList, lst: lit}, nil
	case c == '[':
		// 列表字面量 [a, b, c]
		p.pos++
		lit := &listLit{}
		for {
			p.skipSpace()
			if p.pos < len(p.src) && p.src[p.pos] == ']' {
				p.pos++
				break
			}
			it, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			lit.items = append(lit.items, it)
			p.skipSpace()
			if p.pos < len(p.src) && p.src[p.pos] == ',' {
				p.pos++
				continue
			}
		}
		return &expr{kind: kList, lst: lit}, nil
	}
	return nil, p.errf("unexpected character %q in expression", string(c))
}
