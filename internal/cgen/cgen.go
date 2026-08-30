// Package cgen 将 QuarkLang 子集转译为 C（跨系统编译器骨架）。
package cgen

import (
	"fmt"
	"strings"
)

// Transpile 把 QuarkLang 源码转译为 C 代码。
func Transpile(src string) (string, error) {
	p := &parser{src: src, pos: 0}
	if err := p.parseProgram(); err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("#include <stdio.h>\n\n")
	b.WriteString("int main(void) {\n")
	for _, s := range p.stmts {
		s.emit(&b)
	}
	b.WriteString("    return 0;\n}\n")
	return b.String(), nil
}

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
}

type stmt interface{ emit(b *strings.Builder) }

type printlnStmt struct{ args []*expr }

func (s *printlnStmt) emit(b *strings.Builder) {
	fmts := make([]string, len(s.args))
	for i, a := range s.args {
		switch a.kind {
		case kInt, kBin:
			fmts[i] = "%d"
		case kString:
			fmts[i] = "%s"
		case kIdent:
			fmts[i] = "%d"
		}
	}
	b.WriteString("    printf(\"" + strings.Join(fmts, " ") + "\\n\"")
	for _, a := range s.args {
		b.WriteString(", " + cExpr(a))
	}
	b.WriteString(");\n")
}

func cExpr(e *expr) string {
	switch e.kind {
	case kInt:
		return fmt.Sprintf("%d", e.i)
	case kString:
		return fmt.Sprintf("%q", e.s)
	case kIdent:
		return e.s
	case kBin:
		return "(" + cExpr(e.l) + " " + e.op + " " + cExpr(e.r) + ")"
	}
	return "0"
}

// parser 是极简递归下降解析器（仅编译子集）。
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
		// 语句：io.println(...);  或  io.print(...);
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
		} // print 暂忽略换行差异，v0.2 简化
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
		if p.pos >= len(p.src) || (p.src[p.pos] != '*' && p.src[p.pos] != '/') {
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
		start := p.pos
		for p.pos < len(p.src) && p.src[p.pos] != '"' {
			p.pos++
		}
		if p.pos >= len(p.src) {
			return nil, p.errf("unterminated string literal")
		}
		s := p.src[start:p.pos]
		p.pos++
		return &expr{kind: kString, s: s}, nil
	case p.isIdentStart(c):
		return &expr{kind: kIdent, s: p.lexIdent()}, nil
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
