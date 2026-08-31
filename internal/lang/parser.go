package lang

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"sync"
)

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
// compileCache：进程内增量编译缓存（源码 sha256 → 已编译 Program）。
var compileCache sync.Map

func Compile(src string) (*Program, error) {
	h := sha256.Sum256([]byte(src))
	key := string(h[:])
	if p, ok := compileCache.Load(key); ok {
		return p.(*Program), nil
	}
	prog, err := compileSlow(src)
	if err != nil {
		return nil, err
	}
	compileCache.Store(key, prog)
	return prog, nil
}

func compileSlow(src string) (*Program, error) {
	toks, err := Lex(src)
	if err != nil {
		return nil, err
	}
	// 宏系统：切出 macro 定义，token 级展开（解释器为 run 态），再解析
	macros, rest, err := SplitMacroDefs(toks)
	if err != nil {
		return nil, err
	}
	if len(macros) > 0 {
		rest, err = ExpandMacros(rest, macros, "explain") // 解释器 = explain 操作时（xmind §操作时）
		if err != nil {
			return nil, err
		}
	}
	prog, err := Parse(rest)
	if err != nil {
		return nil, err
	}
	prog.Src = src
	if err := Typecheck(prog); err != nil {
		return nil, err
	}
	return prog, nil
}

func (p *parser) cur() Token              { return p.toks[p.i] }
func (p *parser) peekIs(k TokenKind) bool { return p.i+1 < len(p.toks) && p.toks[p.i+1].Kind == k }

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

// expectMemberName accepts a member/field name; keywords (like in/out/log)
// are valid names in member position.
func (p *parser) expectMemberName(what string) (Token, error) {
	if isWordToken(p.cur().Kind) {
		return p.advance(), nil
	}
	return Token{}, p.errf(p.cur(), "expected %s, got %s", what, p.cur().Kind)
}

// isBuiltinTypeName 判断是否为内建类型名（type-first 声明用）。
func isBuiltinTypeName(s string) bool {
	switch s {
	case "int", "float", "bool", "String", "void", "List", "HashTable", "Channel", "thread", "Task", "memorize", "memory", "IOStream", "Copyd",
		"istream", "ostream", "ifstream", "ofstream", "iofstream", "InputStream", "OutputStream":
		return true
	}
	return false
}

// isWordToken 判断是否为标识符或关键字（非标点/字面量）。
func isWordToken(k TokenKind) bool {
	return k == TIdent || (k > TStr && k < TLParen)
}

func (p *parser) parseProgram() (*Program, error) {
	prog := &Program{}
	for !p.curIs(TEOF) {
		switch p.cur().Kind {
		case TFunc:
			if prog.kindSet {
				return nil, p.errf(p.cur(), "program 预制宏必须写在程序末尾（节点接入先后顺序），%s 声明不得在其后", "func")
			}
			fn, err := p.parseFunc()
			if err != nil {
				return nil, err
			}
			prog.Funcs = append(prog.Funcs, fn)
		case TStruct:
			if prog.kindSet {
				return nil, p.errf(p.cur(), "program 预制宏必须写在程序末尾（节点接入先后顺序），struct 声明不得在其后")
			}
			sd, err := p.parseStruct()
			if err != nil {
				return nil, err
			}
			prog.Structs = append(prog.Structs, sd)
		case TInterface:
			if prog.kindSet {
				return nil, p.errf(p.cur(), "program 预制宏必须写在程序末尾（节点接入先后顺序），interface 声明不得在其后")
			}
			id, err := p.parseInterface()
			if err != nil {
				return nil, err
			}
			prog.Interfaces = append(prog.Interfaces, id)
		case TImpl:
			if prog.kindSet {
				return nil, p.errf(p.cur(), "program 预制宏必须写在程序末尾（节点接入先后顺序），impl 声明不得在其后")
			}
			im, err := p.parseImpl()
			if err != nil {
				return nil, err
			}
			prog.Impls = append(prog.Impls, im)
		default:
			if p.cur().Kind == TIdent {
				switch p.cur().Text {
				case "type":
					// xmind：type interface<T>{...}(Name) / type struct<T>{...}(Name)
					p.advance()
					switch p.cur().Kind {
					case TInterface:
						id, err := p.parseInterface()
						if err != nil {
							return nil, err
						}
						prog.Interfaces = append(prog.Interfaces, id)
					case TStruct:
						sd, err := p.parseStruct()
						if err != nil {
							return nil, err
						}
						prog.Structs = append(prog.Structs, sd)
					default:
						// 通用类型别名：type <类型表达式> 名字;
						typ, err := p.parseType()
						if err != nil {
							return nil, err
						}
						n, err := p.expectIdent("type alias name")
						if err != nil {
							return nil, err
						}
						if _, err := p.expect(TSemi, "';'"); err != nil {
							return nil, err
						}
						prog.TypeAliases = append(prog.TypeAliases, &TypeAlias{Name: n.Text, Type: typ, Pos: Pos{Line: n.Line, Col: n.Col}})
					}
					continue
				case "space":
					// xmind：space {...} (name) 自我实现空间（匿名实现）
					p.advance()
					if _, err := p.expect(TLBrace, "'{'"); err != nil {
						return nil, err
					}
					im := &ImplDecl{Pos: Pos{Line: p.cur().Line, Col: p.cur().Col}}
					for !p.curIs(TRBrace) {
						if p.curIs(TEOF) {
							return nil, p.errf(p.cur(), "unterminated space body")
						}
						m, err := p.parseFunc()
						if err != nil {
							return nil, err
						}
						im.Methods = append(im.Methods, m)
					}
					p.advance() // '}'
					if _, err := p.expect(TLParen, "'('"); err != nil {
						return nil, err
					}
					tt, err := p.parseType()
					if err != nil {
						return nil, err
					}
					if _, err := p.expect(TRParen, "')'"); err != nil {
						return nil, err
					}
					if _, err := p.expect(TSemi, "';'"); err != nil {
						return nil, err
					}
					im.Type = tt
					if i := strings.IndexByte(im.Type, '<'); i >= 0 {
						inner := im.Type[i+1 : len(im.Type)-1]
						im.Type = im.Type[:i] // 取基名
						im.TypeParams = splitTopCommas(inner)
					}
					prog.Impls = append(prog.Impls, im)
					continue
				case "program":
					// 预制宏：program main; / program library;
					p.advance()
					kind, err := p.expectIdent("program kind (main|library)")
					if err != nil {
						return nil, err
					}
					if kind.Text != "main" && kind.Text != "library" && kind.Text != "lib" {
						return nil, p.errf(kind, "program 声明只支持 main / library（lib）")
					}
					if kind.Text == "lib" {
						kind.Text = "library"
					}
					if _, err := p.expect(TSemi, "';'"); err != nil {
						return nil, err
					}
					if prog.Kind != "" {
						return nil, p.errf(kind, "重复的 program 预制宏")
					}
					prog.Kind = kind.Text
					prog.kindSet = true
					continue
				case "import":
					// 预制宏：import path;（同目录默认在搜索范围）
					p.advance()
					var path string
					if p.cur().Kind == TStr {
						path = p.cur().Text
						p.advance()
					} else {
						n, err := p.expectIdent("import path")
						if err != nil {
							return nil, err
						}
						path = n.Text
					}
					if _, err := p.expect(TSemi, "';'"); err != nil {
						return nil, err
					}
					prog.Imports = append(prog.Imports, path)
					continue
				case "pub":
					// 预制宏：pub 前缀，公开下一个顶层符号
					p.advance()
					if p.cur().Kind != TFunc && p.cur().Kind != TStruct {
						return nil, p.errf(p.cur(), "pub 只能前缀于 func/struct")
					}
					if p.cur().Kind == TFunc {
						fn, err := p.parseFunc()
						if err != nil {
							return nil, err
						}
						prog.Pub = append(prog.Pub, fn.Name)
						prog.Funcs = append(prog.Funcs, fn)
					} else {
						sd, err := p.parseStruct()
						if err != nil {
							return nil, err
						}
						prog.Pub = append(prog.Pub, sd.Name)
						prog.Structs = append(prog.Structs, sd)
					}
					continue
				}
			}
			return nil, p.errf(p.cur(), "expected a top-level declaration (func/struct/impl/interface/program/import/pub), got %s", p.cur().Kind)
		}
	}
	return prog, nil
}

func (p *parser) parseFunc() (*FuncDecl, error) {
	kw, err := p.expect(TFunc, "'func'")
	if err != nil {
		return nil, err
	}
	var typeParams []string
	if p.curIs(TLt) {
		// 泛型参数 <T, ...>（xmind §函数：泛型可有可无）
		p.advance()
		for {
			tp, err := p.expectIdent("type parameter")
			if err != nil {
				return nil, err
			}
			typeParams = append(typeParams, tp.Text)
			if p.curIs(TComma) {
				p.advance()
				continue
			}
			break
		}
		if _, err := p.expect(TGt, "'>'"); err != nil {
			return nil, err
		}
	}
	name, err := p.expectIdent("function name")
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(TLParen, "'('"); err != nil {
		return nil, err
	}
	fn := &FuncDecl{Name: name.Text, TypeParams: typeParams, Pos: Pos{Line: kw.Line, Col: kw.Col}}
	params, err := p.parseParamList()
	if err != nil {
		return nil, err
	}
	fn.Params = params
	// 返回类型注解必填（新模型：函数必须声明返回类型；main 入口可省略，视为 void）
	if p.curIs(TIdent) || p.curIs(TInterface) {
		typ, err := p.parseType()
		if err != nil {
			return nil, err
		}
		fn.Ret = typ
	} else if name.Text == "main" {
		fn.Ret = "void"
	} else {
		return nil, p.errf(p.cur(), "函数必须声明返回类型：func %s(...) 返回类型 { ... }", name.Text)
	}
	startTok := p.cur() // parseBlock 前：'{' 之后第一个 token（起始行）
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	fn.Body = body
	fn.BodyStart = Pos{Line: startTok.Line, Col: startTok.Col}
	if p.i > 0 {
		fn.BodyEnd = Pos{Line: p.toks[p.i-1].Line, Col: p.toks[p.i-1].Col} // 结束 '}'
	}
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

// parseTypeParams parses "<T, U, ...>".
func (p *parser) parseTypeParams() ([]string, error) {
	if _, err := p.expect(TLt, "'<'"); err != nil {
		return nil, err
	}
	var params []string
	for {
		tok, err := p.expectIdent("type parameter")
		if err != nil {
			return nil, err
		}
		params = append(params, tok.Text)
		if p.curIs(TComma) {
			p.advance()
			continue
		}
		break
	}
	if _, err := p.expect(TGt, "'>'"); err != nil {
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
	sd := &StructDecl{Pos: Pos{Line: kw.Line, Col: kw.Col}}
	// 可选泛型参数：struct<T, U> { ... }
	if p.curIs(TLt) {
		params, err := p.parseTypeParams()
		if err != nil {
			return nil, err
		}
		sd.TypeParams = params
	}
	if _, err := p.expect(TLBrace, "'{"); err != nil {
		return nil, err
	}
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
	if err := p.parseStructName(sd); err != nil {
		return nil, err
	}
	if _, err := p.expect(TSemi, "';'"); err != nil {
		return nil, err
	}
	return sd, nil
}

// parseStructName 取结构体名字：xmind 用 } (name); 括号形式，兼容 } name; 旧形式。
func (p *parser) parseStructName(sd *StructDecl) error {
	if p.curIs(TLParen) {
		p.advance()
		n, err := p.expectIdent("struct name")
		if err != nil {
			return err
		}
		if _, err := p.expect(TRParen, "')'"); err != nil {
			return err
		}
		sd.Name = n.Text
	} else if p.curIs(TIdent) {
		sd.Name = p.advance().Text
	}
	return nil
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
		// expand interface Name; 组合接口行（xmind §接口）
		if p.curIs(TIdent) && p.cur().Text == "expand" {
			p.advance()
			if _, err := p.expect(TInterface, "'interface'"); err != nil {
				return nil, err
			}
			n, err := p.expectIdent("expanded interface name")
			if err != nil {
				return nil, err
			}
			if _, err := p.expect(TSemi, "';'"); err != nil {
				return nil, err
			}
			id.Expands = append(id.Expands, n.Text)
			continue
		}
		sig, err := p.parseMethodSig()
		if err != nil {
			return nil, err
		}
		id.Methods = append(id.Methods, sig)
	}
	p.advance() // '}'
	if p.curIs(TLParen) {
		// xmind：type interface {...} (name);
		p.advance()
		n, err := p.expectIdent("interface name")
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(TRParen, "')'"); err != nil {
			return nil, err
		}
		id.Name = n.Text
	} else if p.curIs(TIdent) {
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
	// 可选泛型参数：impl<T> [Iface] { ... }
	if p.curIs(TLt) {
		params, err := p.parseTypeParams()
		if err != nil {
			return nil, err
		}
		im.TypeParams = params
	}
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
	if name == "function" && p.curIs(TLt) {
		// 函数类型：function<ret, p1, p2, ...>（函数引用）
		name += "<"
		p.advance()
		depth := 1
		for depth > 0 {
			if p.curIs(TEOF) {
				return "", p.errf(p.cur(), "unterminated function type")
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
		return name, nil
	}
	if name == "pointer" {
		// pointer 修饰：pointer <type>（等价 T&，指向堆上数据）
		rest, err := p.parseType()
		if err != nil {
			return "", err
		}
		return "pointer " + rest, nil
	}
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
	if p.curIs(TLBracket) && !p.peekIs(TInt) && !p.peekIs(TMinus) {
		// 类型后缀 [Copyd]/[]：数字开头的 [ 是 new <type>[size] 的 size，不消费
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
	// & 后缀：指针类型（如 node<T>&）
	if p.curIs(TAmper) {
		p.advance()
		name += "&"
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
	// delete variable; 语句（xmind 内存：回收内存于 __delete__()）
	if p.curIs(TIdent) && p.cur().Text == "delete" {
		kw := p.advance()
		x, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(TSemi, "';'"); err != nil {
			return nil, err
		}
		return &DeleteStmt{X: x, Pos: Pos{Line: kw.Line, Col: kw.Col}}, nil
	}
	switch p.cur().Kind {
	case TOut:
		return nil, p.errf(p.cur(), "out 已在新模型中移除：请用 return expr; 返回结果")
	case TTry:
		kw := p.advance()
		tryB, err := p.parseBlock()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(TCatch, "'catch'"); err != nil {
			return nil, err
		}
		if _, err := p.expect(TLParen, "'('"); err != nil {
			return nil, err
		}
		cv, err := p.expectIdent("catch variable")
		if err != nil {
			return nil, err
		}
		ct, err := p.parseType()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(TRParen, "')'"); err != nil {
			return nil, err
		}
		catchB, err := p.parseBlock()
		if err != nil {
			return nil, err
		}
		return &TryStmt{Try: tryB, CatchVar: cv.Text, CatchVarType: ct, Catch: catchB, Pos: Pos{Line: kw.Line, Col: kw.Col}}, nil
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
	// 变量修饰：const / copyd 前缀，类型在前（<decor> <type> <name>，xmind §变量）
	if p.curIs(TIdent) && (p.cur().Text == "const" || p.cur().Text == "copyd") {
		decor := p.advance().Text
		typ, err := p.parseType()
		if err != nil {
			return nil, err
		}
		name, err := p.expectIdent("variable name")
		if err != nil {
			return nil, err
		}
		st := &DeclStmt{Name: name.Text, Type: typ, Decor: decor, Pos: Pos{Line: name.Line, Col: name.Col}}
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
	// type-first declaration（xmind §变量：<type> <name>）：内建类型名开头
	if p.curIs(TIdent) && isBuiltinTypeName(p.cur().Text) {
		typ, err := p.parseType()
		if err != nil {
			return nil, err
		}
		name, err := p.expectIdent("variable name")
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
func (p *parser) parseAdd() (Expr, error)   { return p.parseBin(p.parseShift, TPlus, TMinus) }
func (p *parser) parseMul() (Expr, error)   { return p.parseBin(p.parseUnary, TStar, TSlash, TPercent) }
func (p *parser) parseShift() (Expr, error) { return p.parseBin(p.parseMul, TShl, TShr) }

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
	case TNull:
		p.advance()
		return &NullLit{Pos: pos}, nil
	case TDot:
		// 结构体字面量/展开式：.{ field: expr, ... }
		p.advance()
		if _, err := p.expect(TLBrace, "'{'"); err != nil {
			return nil, err
		}
		lit := &StructLit{Pos: pos}
		if !p.curIs(TRBrace) {
			for {
				name, err := p.expectMemberName("field name")
				if err != nil {
					return nil, err
				}
				if _, err := p.expect(TColon, "':'"); err != nil {
					return nil, err
				}
				v, err := p.parseExpr()
				if err != nil {
					return nil, err
				}
				lit.Fields = append(lit.Fields, StructLitField{Name: name.Text, X: v})
				if p.curIs(TComma) {
					p.advance()
					continue
				}
				break
			}
		}
		if _, err := p.expect(TRBrace, "'}'"); err != nil {
			return nil, err
		}
		return lit, nil
	case TIdent:
		if tok.Text == "new" {
			// new <type>[size] —— 堆上直接申请内存（失败 badAlloc）
			p.advance()
			typ, err := p.parseType()
			if err != nil {
				return nil, err
			}
			ne := &NewExpr{Typ: typ, Pos: pos}
			if p.curIs(TLBracket) {
				p.advance()
				sz, err := p.parseExpr()
				if err != nil {
					return nil, err
				}
				if _, err := p.expect(TRBracket, "']'"); err != nil {
					return nil, err
				}
				ne.Size = sz
			}
			return ne, nil
		}
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
