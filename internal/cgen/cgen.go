// Package cgen 将 QuarkLang 子集编译为 LLVM IR（文本形式，跨平台交给 LLVM 后端）。
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
	e.emitProgram(p.stmts)
	return e.b.String(), nil
}

type strConst struct {
	name string
	size int
}

type emitter struct {
	b        strings.Builder
	strs     map[string]strConst
	strCount int
	regCount int
}

func (e *emitter) newReg() string {
	e.regCount++
	return fmt.Sprintf("%%%d", e.regCount)
}

// strConst 注册字符串常量（含结尾 NUL），返回全局名与字节长度。
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

// i8Ptr 生成指向字符串常量的 i8* 值。
func (e *emitter) i8Ptr(c strConst) string {
	r := e.newReg()
	fmt.Fprintf(&e.b, "  %s = getelementptr inbounds [%d x i8], [%d x i8]* %s, i64 0, i64 0\n",
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

// preRegister 递归预注册表达式中的字符串常量（保证声明先于函数体）。
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

func (e *emitter) emitProgram(stmts []stmt) {
	e.b.WriteString("declare i32 @printf(i8* noundef, ...)\n\n")
	// 先注册全部字符串常量（含 println 格式串），保证声明先于函数体
	for _, s := range stmts {
		if ps, ok := s.(*printlnStmt); ok {
			ps.fmt = e.strConst(ps.format())
			for _, a := range ps.args {
				e.preRegister(a)
			}
		}
	}
	e.b.WriteString("define i32 @main() {\nentry:\n")
	for _, s := range stmts {
		if ps, ok := s.(*printlnStmt); ok {
			e.emitPrintln(ps)
		}
	}
	e.b.WriteString("  ret i32 0\n}\n")
}

type argVal struct {
	val string
	ptr bool
}

func (e *emitter) emitPrintln(s *printlnStmt) {
	fp := e.i8Ptr(s.fmt)
	var args []argVal
	for _, a := range s.args {
		v, p := e.compileExpr(a)
		args = append(args, argVal{v, p})
	}
	line := fmt.Sprintf("  %s = call i32 (i8*, ...) @printf(i8* %s", e.newReg(), fp)
	for _, a := range args {
		if a.ptr {
			line += ", i8* " + a.val
		} else {
			line += ", i32 " + a.val
		}
	}
	line += ")\n"
	e.b.WriteString(line)
}

// compileExpr 编译表达式，返回 (值引用, 是否为 i8* 字符串指针)。
func (e *emitter) compileExpr(x *expr) (string, bool) {
	switch x.kind {
	case kInt:
		return fmt.Sprintf("%d", x.i), false
	case kString:
		return e.i8Ptr(x.sc), true
	case kIdent:
		return x.s, false // v0.2：变量名直通（i32）
	case kBin:
		l, _ := e.compileExpr(x.l)
		r, _ := e.compileExpr(x.r)
		op := map[string]string{"+": "add", "-": "sub", "*": "mul", "/": "sdiv", "%": "srem"}[x.op]
		reg := e.newReg()
		fmt.Fprintf(&e.b, "  %s = %s i32 %s, %s\n", reg, op, l, r)
		return reg, false
	}
	return "0", false
}

// ---------- 极简递归下降解析器（编译子集） ----------

type exprKind int

const (
	kInt exprKind = iota
	kString
	kIdent
	kBin
)

type expr struct {
	kind exprKind
	i    int64
	s    string
	op   string
	l, r *expr
	sc   strConst // 预注册的字符串常量（kString）
}

type stmt interface{}

type printlnStmt struct {
	args []*expr
	fmt  strConst
}

func (s *printlnStmt) format() string {
	fmts := make([]string, len(s.args))
	for i, a := range s.args {
		switch a.kind {
		case kString:
			fmts[i] = "%s"
		default:
			fmts[i] = "%d"
		}
	}
	return strings.Join(fmts, " ") + "\n"
}

type parser struct {
	src   string
	pos   int
	stmts []stmt
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

func (p *parser) parseFunc() error {
	p.skipSpace()
	name, err := p.expectIdent()
	if err != nil {
		return err
	}
	p.skipSpace()
	if err := p.expect('('); err != nil {
		return err
	}
	// 跳过参数列表（v0.2 仅 main(io IOStream)）
	depth := 1
	for depth > 0 && p.pos < len(p.src) {
		if p.src[p.pos] == '(' {
			depth++
		}
		if p.src[p.pos] == ')' {
			depth--
		}
		p.pos++
	}
	if name != "main" {
		return p.errf("compiler v0.2 supports only func main, got func %s", name)
	}
	p.skipSpace()
	if err := p.expect('{'); err != nil {
		return err
	}
	for {
		p.skipSpace()
		if p.pos >= len(p.src) {
			return p.errf("unterminated function body")
		}
		if p.src[p.pos] == '}' {
			p.pos++
			return nil
		}
		obj, err := p.expectIdent()
		if err != nil {
			return err
		}
		if obj != "io" {
			return p.errf("compiler v0.2 supports only io.println in main, got %q", obj)
		}
		p.skipSpace()
		if err := p.expect('.'); err != nil {
			return err
		}
		m, err := p.expectIdent()
		if err != nil {
			return err
		}
		if m != "println" && m != "print" {
			return p.errf("compiler v0.2 supports only io.println/io.print, got %q", m)
		}
		p.skipSpace()
		if err := p.expect('('); err != nil {
			return err
		}
		var args []*expr
		p.skipSpace()
		if p.pos < len(p.src) && p.src[p.pos] != ')' {
			for {
				a, err := p.parseExpr()
				if err != nil {
					return err
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
			return err
		}
		p.skipSpace()
		if err := p.expect(';'); err != nil {
			return err
		}
		if m == "println" {
			p.stmts = append(p.stmts, &printlnStmt{args: args})
		}
	}
}

func (p *parser) parseExpr() (*expr, error) {
	p.skipSpace()
	return p.parseAdd()
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
		return &expr{kind: kIdent, s: p.lexIdent()}, nil
	case c == '-':
		// 一元负号：等价于 0 - x
		p.pos++
		inner, err := p.parsePrimary()
		if err != nil {
			return nil, err
		}
		return &expr{kind: kBin, op: "-", l: &expr{kind: kInt}, r: inner}, nil
	case c == '(':
		p.pos++
		e, err := p.parseAdd()
		if err != nil {
			return nil, err
		}
		p.skipSpace()
		if err := p.expect(')'); err != nil {
			return nil, err
		}
		return e, nil
	}
	return nil, p.errf("unexpected character %q in expression", string(c))
}
