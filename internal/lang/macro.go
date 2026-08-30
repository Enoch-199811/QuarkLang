package lang

import (
	"fmt"
	"strings"
)

// SplitMacroDefs 从 token 流中切分出所有 macro {模式} {主体} 定义，
// 返回宏列表与剩余 token。模式内不允许嵌套大括号（用户约定）。
func SplitMacroDefs(toks []Token) ([]*MacroDef, []Token, error) {
	var macros []*MacroDef
	var rest []Token
	i := 0
	for i < len(toks) {
		t := toks[i]
		if t.Kind != TMacro {
			rest = append(rest, t)
			i++
			continue
		}
		pos := Pos{Line: t.Line, Col: t.Col}
		if i+1 >= len(toks) || toks[i+1].Kind != TLBrace {
			return nil, nil, fmt.Errorf("ParseError: macro 定义需要 {模式} {主体}，第 %d 行", t.Line)
		}
		i += 2
		var pat []Token
		for i < len(toks) && toks[i].Kind != TRBrace {
			if toks[i].Kind == TLBrace {
				return nil, nil, fmt.Errorf("ParseError: macro 模式内不能再嵌套大括号，第 %d 行", toks[i].Line)
			}
			pat = append(pat, toks[i])
			i++
		}
		if i >= len(toks) {
			return nil, nil, fmt.Errorf("ParseError: macro 模式缺少 }，第 %d 行", t.Line)
		}
		i++
		if i >= len(toks) || toks[i].Kind != TLBrace {
			return nil, nil, fmt.Errorf("ParseError: macro 主体需要 { ... }，第 %d 行", t.Line)
		}
		body, ni, err := takeBalanced(toks, i)
		if err != nil {
			return nil, nil, err
		}
		macros = append(macros, &MacroDef{Pattern: pat, Body: body, Pos: pos})
		i = ni
	}
	return macros, rest, nil
}

// takeBalanced 从 toks[i]（必须是 TLBrace）开始取平衡大括号块，返回块内 token 与结束位置。
func takeBalanced(toks []Token, i int) ([]Token, int, error) {
	depth := 0
	j := i
	for ; j < len(toks); j++ {
		switch toks[j].Kind {
		case TLBrace:
			depth++
		case TRBrace:
			depth--
			if depth == 0 {
				return toks[i+1 : j], j + 1, nil
			}
		}
	}
	return nil, 0, fmt.Errorf("ParseError: 大括号不配对，第 %d 行", toks[i].Line)
}

// isEllipsis 判断三连 '.'（...，宏模式中的通配符）。
func isEllipsis(toks []Token, i int) bool {
	return i+2 < len(toks) && toks[i].Kind == TDot && toks[i+1].Kind == TDot && toks[i+2].Kind == TDot
}

// matchPattern 尝试把模式匹配到 toks[i:] 开头；... 只允许出现在模式末尾（捕获剩余）。
func matchPattern(toks []Token, i int, pat []Token) (captures map[string][]Token, consumed int, ok bool) {
	j := i
	p := 0
	for p < len(pat) {
		if isEllipsis(pat, p) {
			// v1：... 在模式末尾；捕获到本语句结束（括号深度 0 处的 ';' 或右大括号）
			depth := 0
			k := j
			for ; k < len(toks); k++ {
				switch toks[k].Kind {
				case TLBrace:
					depth++
				case TRBrace:
					if depth == 0 {
						k++ // 含右大括号
						goto capDone
					}
					depth--
				case TSemi:
					if depth == 0 {
						k++ // 含分号
						goto capDone
					}
				}
			}
		capDone:
			captures = map[string][]Token{"...": toks[j:k]}
			return captures, k - i, true
		}
		if j >= len(toks) || toks[j].Kind != pat[p].Kind {
			return nil, 0, false
		}
		if pat[p].Kind == TIdent && toks[j].Text != pat[p].Text {
			return nil, 0, false
		}
		j++
		p++
	}
	return captures, j - i, true
}

// ExpandMacros 在 token 流上做单遍宏展开。mode 为动态预处理态："run" 或 "compile"。
func ExpandMacros(toks []Token, macros []*MacroDef, mode string) ([]Token, error) {
	var out []Token
	i := 0
	for i < len(toks) {
		matched := false
		for _, m := range macros {
			caps, consumed, ok := matchPattern(toks, i, m.Pattern)
			if !ok {
				continue
			}
			body, err := expandBody(m.Body, caps, mode)
			if err != nil {
				return nil, fmt.Errorf("宏 %s 展开失败：%v", m.PatternString(), err)
			}
			out = append(out, body...)
			i += consumed
			matched = true
			break
		}
		if !matched {
			out = append(out, toks[i])
			i++
		}
	}
	return out, nil
}

// expandBody 展开宏主体中的动态预处理命令（# 开头）。
func expandBody(body []Token, caps map[string][]Token, mode string) ([]Token, error) {
	var out []Token
	i := 0
	for i < len(body) {
		t := body[i]
		if t.Kind != TSharp {
			out = append(out, t)
			i++
			continue
		}
		if i+1 >= len(body) || body[i+1].Kind != TIdent {
			return nil, fmt.Errorf("第 %d 行：# 后必须是预处理命令（when/insert/execute/error）", t.Line)
		}
		cmd := body[i+1].Text
		i += 2
		if i >= len(body) || body[i].Kind != TLParen {
			return nil, fmt.Errorf("第 %d 行：#%s 需要 ( ... )", t.Line, cmd)
		}
		depth := 0
		j := i
		for ; j < len(body); j++ {
			switch body[j].Kind {
			case TLParen:
				depth++
			case TRParen:
				depth--
				if depth == 0 {
					goto done
				}
			}
		}
		return nil, fmt.Errorf("第 %d 行：#%s 括号不配对", t.Line, cmd)
	done:
		args := body[i+1 : j]
		i = j + 1
		switch cmd {
		case "when":
			// #when (compile|run) { ... }：块在括号之后
			if len(args) < 1 || args[0].Kind != TIdent {
				return nil, fmt.Errorf("第 %d 行：#when 需要 (compile|run)", t.Line)
			}
			if i >= len(body) || body[i].Kind != TLBrace {
				return nil, fmt.Errorf("第 %d 行：#when 需要 { ... } 块", t.Line)
			}
			blk, ni, err := takeBalanced(body, i)
			if err != nil {
				return nil, err
			}
			i = ni
			if args[0].Text == mode {
				sub, err := expandBody(blk, caps, mode)
				if err != nil {
					return nil, err
				}
				out = append(out, sub...)
			}
		case "insert":
			inner, err := parseAstArg(args)
			if err != nil {
				return nil, err
			}
			captured, ok := caps[inner]
			if !ok {
				return nil, fmt.Errorf("第 %d 行：#insert(#ast(%s))：%s 不是模式捕获名", t.Line, inner, inner)
			}
			out = append(out, captured...)
		case "execute":
			if len(args) < 1 || args[0].Kind != TIdent {
				return nil, fmt.Errorf("第 %d 行：#execute 需要 (名字)", t.Line)
			}
			out = append(out, Token{Kind: TIdent, Text: args[0].Text, Line: t.Line, Col: t.Col})
		case "error":
			if len(args) < 1 || args[0].Kind != TStr {
				return nil, fmt.Errorf("第 %d 行：#error 需要 (\"消息\")", t.Line)
			}
			return nil, fmt.Errorf("第 %d 行：预处理错误 #error(%s)", t.Line, args[0].Text)
		default:
			return nil, fmt.Errorf("第 %d 行：未知预处理命令 #%s", t.Line, cmd)
		}
	}
	return out, nil
}

// parseAstArg 解析 (#ast(名字)) 参数，返回名字；名字可为标识符或 ...（三连点通配捕获）。
func parseAstArg(args []Token) (string, error) {
	if len(args) < 4 || args[0].Kind != TSharp || args[1].Kind != TIdent || args[1].Text != "ast" ||
		args[2].Kind != TLParen || args[len(args)-1].Kind != TRParen {
		return "", fmt.Errorf("#insert 需要 (#ast(名字)) 形式")
	}
	if isEllipsis(args, 3) {
		return "...", nil
	}
	if args[3].Kind == TIdent {
		return args[3].Text, nil
	}
	return "", fmt.Errorf("#insert 需要 (#ast(名字)) 形式")
}

// PatternString 便于报错展示宏模式。
func (m *MacroDef) PatternString() string {
	var sb strings.Builder
	for _, t := range m.Pattern {
		sb.WriteString(t.Text)
		sb.WriteString(" ")
	}
	return strings.TrimSpace(sb.String())
}
