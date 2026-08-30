package lang

import "fmt"

// ParseError reports a syntax error with source position.
type ParseError struct {
	Msg  string
	Line int
	Col  int
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("ParseError: %s at line %d, col %d", e.Msg, e.Line, e.Col)
}

type parser struct {
	toks []Token
	i    int
}

// Parse builds the AST from tokens (spec §2).
func Parse(toks []Token) (*Program, error) {
	p := &parser{toks: toks}
	return p.parseProgram()
}

// Compile lexes, parses, and statically type-checks source code
// (spec §11.1: strict checks run at compile time).
func Compile(src string) (*Program, error) {
	toks, err := Lex(src)
	if err != nil {
		return nil, err
	}
	prog, err := Parse(toks)
	if err != nil {
		return nil, err
	}
	if err := Typecheck(prog); err != nil {
		return nil, err
	}
	return prog, nil
}

func (p *parser) cur() Token { return p.toks[p.i] }

func (p *parser) peek() Token {
	if p.i+1 < len(p.toks) {
		return p.toks[p.i+1]
	}
	return p.toks[len(p.toks)-1]
}

func (p *parser) advance() Token {
	t := p.toks[p.i]
	if p.i < len(p.toks)-1 {
		p.i++
	}
	return t
}

func (p *parser) curIs(k TokenKind) bool { return p.cur().Kind == k }

func (p *parser) errf(tok Token, format string, args ...interface{}) error {
	return &ParseError{Msg: fmt.Sprintf(format, args...), Line: tok.Line, Col: tok.Col}
}

func (p *parser) expect(k TokenKind, what string) (Token, error) {
	if !p.curIs(k) {
		return Token{}, p.errf(p.cur(), "expected %s, got %s", what, p.cur().Kind)
	}
	return p.advance(), nil
}

func (p *parser) expectIdent(what string) (Token, error) {
	if !p.curIs(TIdent) {
		return Token{}, p.errf(p.cur(), "expected %s, got %s", what, p.cur().Kind)
	}
	return p.advance(), nil
}

// expectMemberName accepts a member/method name; "log" is a keyword but
// also the FuncBuffer.log member name.
func (p *parser) expectMemberName(what string) (Token, error) {
	if p.curIs(TIdent) || p.curIs(TLog) {
		return p.advance(), nil
	}
	return Token{}, p.errf(p.cur(), "expected %s, got %s", what, p.cur().Kind)
}

func (p *parser) parseProgram() (*Program, error) {
	prog := &Program{}
	for !p.curIs(TEOF) {
		switch p.cur().Kind {
		case TFunc:
			fn, err := p.parseFunc()
			if err != nil {
				return nil, err
			}
			prog.Funcs = append(prog.Funcs, fn)
		case TStruct:
			sd, err := p.parseStruct()
			if err != nil {
				return nil, err
			}
			prog.Structs = append(prog.Structs, sd)
		case TInterface:
			id, err := p.parseInterface()
			if err != nil {
				return nil, err
			}
			prog.Interfaces = append(prog.Interfaces, id)
		case TImpl:
			im, err := p.parseImpl()
			if err != nil {
				return nil, err
			}
			prog.Impls = append(prog.Impls, im)
		default:
			return nil, p.errf(p.cur(), "expected a top-level declaration (func/struct/impl/interface), got %s", p.cur().Kind)
		}
	}
	return prog, nil
}

func (p *parser) parseFunc() (*FuncDecl, error) {
	kw, err := p.expect(TFunc, "'func'")
	if err != nil {
		return nil, err
	}
	name, err := p.expectIdent("function name")
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(TLParen, "'('"); err != nil {
		return nil, err
	}
	fn := &FuncDecl{Name: name.Text, Pos: Pos{Line: kw.Line, Col: kw.Col}}
	params, err := p.parseParamList()
	if err != nil {
		return nil, err
	}
	fn.Params = params
	// 可选返回类型注解：func f(...) T { ... }
	if p.curIs(TIdent) || p.curIs(TInterface) {
		typ, err := p.parseType()
		if err != nil {
			return nil, err
		}
		fn.Ret = typ
	}
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	fn.Body = body
	return fn, nil
}

// parseParamList parses "(name Type, ...)".
func (p *parser) parseParamList() ([]Param, error) {
	var params []Param
	if !p.curIs(TRParen) {
		for {
			ptok, err := p.expectIdent("parameter name")
			if err != nil {
				return nil, err
			}
			typ := ""
			if !p.curIs(TComma) && !p.curIs(TRParen) {
				t, err := p.parseType()
				if err != nil {
					return nil, err
				}
				typ = t
			}
			params = append(params, Param{
				Name: ptok.Text,
				Type: typ,
				Pos:  Pos{Line: ptok.Line, Col: ptok.Col},
			})
			if p.curIs(TComma) {
				p.advance()
				continue
			}
			break
		}
	}
	if _, err := p.expect(TRParen, "')'"); err != nil {
		return nil, err
	}
	return params, nil
}

// parseStruct parses "struct { members } [Name];".
func (p *parser) parseStruct() (*StructDecl, error) {
	kw, err := p.expect(TStruct, "'struct'")
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(TLBrace, "'{"); err != nil {
		return nil, err
	}
	sd := &StructDecl{Pos: Pos{Line: kw.Line, Col: kw.Col}}
	for !p.curIs(TRBrace) {
		if p.curIs(TEOF) {
			return nil, p.errf(p.cur(), "unterminated struct body (missing '}')")
		}
		name, err := p.expectIdent("member name")
		if err != nil {
			return nil, err
		}
		typ, err := p.parseType()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(TSemi, "';'"); err != nil {
			return nil, err
		}
		sd.Members = append(sd.Members, Member{
			Name: name.Text,
			Type: typ,
			Pos:  Pos{Line: name.Line, Col: name.Col},
		})
	}
	p.advance() // '}'
	if p.curIs(TIdent) {
		sd.Name = p.advance().Text
	}
	if _, err := p.expect(TSemi, "';'"); err != nil {
		return nil, err
	}
	return sd, nil
}

// parseMethodSig parses an interface method signature: "func name(params) Ret;".
func (p *parser) parseMethodSig() (MethodSig, error) {
	kw, err := p.expect(TFunc, "'func'")
	if err != nil {
		return MethodSig{}, err
	}
	name, err := p.expectIdent("method name")
	if err != nil {
		return MethodSig{}, err
	}
	if _, err := p.expect(TLParen, "'('"); err != nil {
		return MethodSig{}, err
	}
	params, err := p.parseParamList()
	if err != nil {
		return MethodSig{}, err
	}
	sig := MethodSig{Name: name.Text, Params: params, Pos: Pos{Line: kw.Line, Col: kw.Col}}
	if p.curIs(TIdent) || p.curIs(TInterface) {
		typ, err := p.parseType()
		if err != nil {
			return MethodSig{}, err
		}
		sig.Ret = typ
	}
	if _, err := p.expect(TSemi, "';'"); err != nil {
		return MethodSig{}, err
	}
	return sig, nil
}

// parseInterface parses "interface { sigs } [Name<...>];".
func (p *parser) parseInterface() (*InterfaceDecl, error) {
	kw, err := p.expect(TInterface, "'interface'")
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(TLBrace, "'{"); err != nil {
		return nil, err
	}
	id := &InterfaceDecl{Pos: Pos{Line: kw.Line, Col: kw.Col}}
	for !p.curIs(TRBrace) {
		if p.curIs(TEOF) {
			return nil, p.errf(p.cur(), "unterminated interface body (missing '}')")
		}
		sig, err := p.parseMethodSig()
		if err != nil {
			return nil, err
		}
		id.Methods = append(id.Methods, sig)
	}
	p.advance() // '}'
	if p.curIs(TIdent) {
		id.Name = p.advance().Text
		// 泛型接口参数：Sign<P> —— 平衡跳过
		if p.curIs(TLt) {
			p.advance()
			depth := 1
			for depth > 0 {
				if p.curIs(TEOF) {
					return nil, p.errf(p.cur(), "unterminated interface type parameters")
				}
				if p.curIs(TLt) {
					depth++
				}
				if p.curIs(TGt) {
					depth--
				}
				p.advance()
			}
		}
	}
	if _, err := p.expect(TSemi, "';'"); err != nil {
		return nil, err
	}
	return id, nil
}

// parseImpl parses "impl [Iface] { funcs } Type;".
func (p *parser) parseImpl() (*ImplDecl, error) {
	kw, err := p.expect(TImpl, "'impl'")
	if err != nil {
		return nil, err
	}
	im := &ImplDecl{Pos: Pos{Line: kw.Line, Col: kw.Col}}
	if p.curIs(TIdent) {
		im.Iface = p.advance().Text
	}
	if _, err := p.expect(TLBrace, "'{"); err != nil {
		return nil, err
	}
	for !p.curIs(TRBrace) {
		if p.curIs(TEOF) {
			return nil, p.errf(p.cur(), "unterminated impl body (missing '}')")
		}
		fn, err := p.parseFunc()
		if err != nil {
			return nil, err
		}
		im.Methods = append(im.Methods, fn)
	}
	p.advance() // '}'
	typ, err := p.expectIdent("type name")
	if err != nil {
		return nil, err
	}
	im.Type = typ.Text
	if _, err := p.expect(TSemi, "';'"); err != nil {
		return nil, err
	}
	return im, nil
}

// parseType reads a type annotation: ident ( "<" ... ">" )* ( "[" ... "]" )?.
func (p *parser) parseType() (string, error) {
	if p.curIs(TInterface) {
		p.advance()
		if _, err := p.expect(TLBrace, "'{'"); err != nil {
			return "", err
		}
		if _, err := p.expect(TRBrace, "'}'"); err != nil {
			return "", err
		}
		return "interface{}", nil
	}
	tok, err := p.expectIdent("type name")
	if err != nil {
		return "", err
	}
	name := tok.Text
	for p.curIs(TLt) {
		name += "<"
		p.advance()
		depth := 1
		for depth > 0 {
			if p.curIs(TEOF) {
				return "", p.errf(p.cur(), "unterminated type arguments in %q", name)
			}
			if p.curIs(TLt) {
				depth++
			}
			if p.curIs(TGt) {
				depth--
			}
			name += p.cur().Text
			p.advance()
		}
	}
	if p.curIs(TLBracket) {
		p.advance()
		if p.curIs(TRBracket) {
			p.advance()
			name += "[]"
		} else {
			inner, err := p.expectIdent("type suffix (e.g. Copyd)")
			if err != nil {
				return "", err
			}
			if _, err := p.expect(TRBracket, "']'"); err != nil {
				return "", err
			}
			name += "[" + inner.Text + "]"
		}
	}
	return name, nil
}

func (p *parser) parseBlock() (*Block, error) {
	if _, err := p.expect(TLBrace, "'{'"); err != nil {
		return nil, err
	}
	b := &Block{}
	for !p.curIs(TRBrace) {
		if p.curIs(TEOF) {
			return nil, p.errf(p.cur(), "unterminated block (missing '}')")
		}
		st, err := p.parseStmt()
		if err != nil {
			return nil, err
		}
		b.Stmts = append(b.Stmts, st)
	}
	p.advance() // '}'
	return b, nil
}

func (p *parser) parseStmt() (Stmt, error) {
	switch p.cur().Kind {
	case TOut:
		p.advance()
		x, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(TSemi, "';'"); err != nil {
			return nil, err
		}
		return &OutStmt{X: x}, nil
	case TLog:
		kw := p.advance()
		x, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(TSemi, "';'"); err != nil {
			return nil, err
		}
		return &LogStmt{X: x, Pos: Pos{Line: kw.Line, Col: kw.Col}}, nil
	case TReturn:
		kw := p.advance()
		pos := Pos{Line: kw.Line, Col: kw.Col}
		if p.curIs(TSemi) {
			p.advance()
			return &ReturnStmt{Pos: pos}, nil
		}
		x, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(TSemi, "';'"); err != nil {
			return nil, err
		}
		return &ReturnStmt{X: x, Pos: pos}, nil
	case TIf:
		p.advance()
		if _, err := p.expect(TLParen, "'('"); err != nil {
			return nil, err
		}
		cond, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(TRParen, "')'"); err != nil {
			return nil, err
		}
		thenB, err := p.parseBlock()
		if err != nil {
			return nil, err
		}
		st := &IfStmt{Cond: cond, Then: thenB}
		if p.curIs(TElse) {
			p.advance()
			elseB, err := p.parseBlock()
			if err != nil {
				return nil, err
			}
			st.Else = elseB
		}
		return st, nil
	case TWhile:
		p.advance()
		if _, err := p.expect(TLParen, "'('"); err != nil {
			return nil, err
		}
		cond, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(TRParen, "')'"); err != nil {
			return nil, err
		}
		body, err := p.parseBlock()
		if err != nil {
			return nil, err
		}
		return &WhileStmt{Cond: cond, Body: body}, nil
	case TFor:
		kw := p.advance()
		if _, err := p.expect(TLParen, "'('"); err != nil {
			return nil, err
		}
		v, err := p.expectIdent("loop variable")
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(TColon, "':'"); err != nil {
			return nil, err
		}
		iter, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(TRParen, "')'"); err != nil {
			return nil, err
		}
		body, err := p.parseBlock()
		if err != nil {
			return nil, err
		}
		return &ForStmt{Var: v.Text, Iter: iter, Body: body, Pos: Pos{Line: kw.Line, Col: kw.Col}}, nil
	}
	// name-first declaration: Ident followed by a type (Ident or 'interface')
	if p.curIs(TIdent) && (p.peek().Kind == TIdent || p.peek().Kind == TInterface) {
		return p.parseDecl()
	}
	x, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if p.curIs(TAssign) {
		eq := p.advance()
		v, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(TSemi, "';'"); err != nil {
			return nil, err
		}
		return &AssignStmt{Target: x, X: v, Pos: Pos{Line: eq.Line, Col: eq.Col}}, nil
	}
	if _, err := p.expect(TSemi, "';'"); err != nil {
		return nil, err
	}
	return &ExprStmt{X: x}, nil
}

// parseDecl parses "name Type [= init];".
func (p *parser) parseDecl() (Stmt, error) {
	name, err := p.expectIdent("variable name")
	if err != nil {
		return nil, err
	}
	typ, err := p.parseType()
	if err != nil {
		return nil, err
	}
	st := &DeclStmt{Name: name.Text, Type: typ, Pos: Pos{Line: name.Line, Col: name.Col}}
	if p.curIs(TAssign) {
		p.advance()
		init, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		st.Init = init
	}
	if _, err := p.expect(TSemi, "';'"); err != nil {
		return nil, err
	}
	return st, nil
}

// ---- expressions ----

func (p *parser) parseExpr() (Expr, error) { return p.parseOr() }

func (p *parser) parseOr() (Expr, error)  { return p.parseBin(p.parseAnd, TOr) }
func (p *parser) parseAnd() (Expr, error) { return p.parseBin(p.parseCmp, TAnd) }
func (p *parser) parseCmp() (Expr, error) {
	return p.parseBin(p.parseAdd, TEq, TNe, TLt, TLe, TGt, TGe)
}
func (p *parser) parseAdd() (Expr, error) { return p.parseBin(p.parseMul, TPlus, TMinus) }
func (p *parser) parseMul() (Expr, error) { return p.parseBin(p.parseUnary, TStar, TSlash, TPercent) }

func (p *parser) parseBin(left func() (Expr, error), kinds ...TokenKind) (Expr, error) {
	x, err := left()
	if err != nil {
		return nil, err
	}
	for {
		matched := false
		for _, k := range kinds {
			if p.curIs(k) {
				matched = true
				break
			}
		}
		if !matched {
			return x, nil
		}
		op := p.advance()
		r, err := left()
		if err != nil {
			return nil, err
		}
		x = &BinOp{Op: op.Text, L: x, R: r, Pos: Pos{Line: op.Line, Col: op.Col}}
	}
}

func (p *parser) parseUnary() (Expr, error) {
	if p.curIs(TBang) || p.curIs(TMinus) || p.curIs(TStar) {
		tok := p.advance()
		x, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &UnOp{Op: tok.Text, X: x, Pos: Pos{Line: tok.Line, Col: tok.Col}}, nil
	}
	return p.parsePostfix()
}

func (p *parser) parsePostfix() (Expr, error) {
	x, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	for {
		switch {
		case p.curIs(TDot):
			p.advance()
			name, err := p.expectMemberName("member name")
			if err != nil {
				return nil, err
			}
			pos := Pos{Line: name.Line, Col: name.Col}
			if p.curIs(TLParen) {
				args, err := p.parseArgs()
				if err != nil {
					return nil, err
				}
				x = &CallExpr{Fn: &MemberExpr{X: x, Name: name.Text, Pos: pos}, Args: args, Pos: pos}
			} else {
				x = &MemberExpr{X: x, Name: name.Text, Pos: pos}
			}
		case p.curIs(TLParen):
			lp := p.cur()
			args, err := p.parseArgs()
			if err != nil {
				return nil, err
			}
			x = &CallExpr{Fn: x, Args: args, Pos: Pos{Line: lp.Line, Col: lp.Col}}
		case p.curIs(TLBracket):
			p.advance()
			idx, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			rt, err := p.expect(TRBracket, "']'")
			if err != nil {
				return nil, err
			}
			x = &IndexExpr{X: x, Idx: idx, Pos: Pos{Line: rt.Line, Col: rt.Col}}
		case p.curIs(TScope):
			p.advance()
			name, err := p.expectMemberName("method name")
			if err != nil {
				return nil, err
			}
			args, err := p.parseArgs()
			if err != nil {
				return nil, err
			}
			id, ok := x.(*Ident)
			if !ok {
				return nil, p.errf(name, "'::' requires an identifier on the left")
			}
			x = &ScopeCall{Scope: id.Name, Name: name.Text, Args: args, Pos: Pos{Line: name.Line, Col: name.Col}}
		case p.curIs(TAt):
			p.advance()
			sname, err := p.expectIdent("signature name")
			if err != nil {
				return nil, err
			}
			sargs, err := p.parseArgs()
			if err != nil {
				return nil, err
			}
			call, ok := x.(*CallExpr)
			if !ok {
				return nil, p.errf(sname, "@ signature must follow a function call")
			}
			if call.Sign != nil {
				return nil, p.errf(sname, "duplicate signature on one call")
			}
			call.Sign = &SignCall{Name: sname.Text, Args: sargs}
		default:
			return x, nil
		}
	}
}

func (p *parser) parseArgs() ([]Expr, error) {
	if _, err := p.expect(TLParen, "'('"); err != nil {
		return nil, err
	}
	var args []Expr
	if !p.curIs(TRParen) {
		for {
			a, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			args = append(args, a)
			if p.curIs(TComma) {
				p.advance()
				continue
			}
			break
		}
	}
	if _, err := p.expect(TRParen, "')'"); err != nil {
		return nil, err
	}
	return args, nil
}

func (p *parser) parsePrimary() (Expr, error) {
	tok := p.cur()
	pos := Pos{Line: tok.Line, Col: tok.Col}
	switch tok.Kind {
	case TInt:
		p.advance()
		if tok.Int > 2147483647 || tok.Int < -2147483648 {
			return nil, p.errf(tok, "int literal %d out of 32-bit range (int is 32-bit, like C)", tok.Int)
		}
		return &IntLit{V: tok.Int, Pos: pos}, nil
	case TFloat:
		p.advance()
		return &FloatLit{V: tok.Flt, Pos: pos}, nil
	case TStr:
		p.advance()
		return &StrLit{V: tok.Text, Pos: pos}, nil
	case TTrue:
		p.advance()
		return &BoolLit{V: true, Pos: pos}, nil
	case TFalse:
		p.advance()
		return &BoolLit{V: false, Pos: pos}, nil
	case TIdent:
		p.advance()
		return &Ident{Name: tok.Text, Pos: pos}, nil
	case TLBracket:
		p.advance()
		l := &ListLit{}
		if !p.curIs(TRBracket) {
			for {
				it, err := p.parseExpr()
				if err != nil {
					return nil, err
				}
				l.Items = append(l.Items, it)
				if p.curIs(TComma) {
					p.advance()
					continue
				}
				break
			}
		}
		if _, err := p.expect(TRBracket, "']'"); err != nil {
			return nil, err
		}
		return l, nil
	case TLParen:
		p.advance()
		x, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(TRParen, "')'"); err != nil {
			return nil, err
		}
		return x, nil
	}
	return nil, p.errf(tok, "unexpected token %s in expression", tok.Kind)
}
