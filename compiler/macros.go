package main

import (
	"strings"

	"quarklang/internal/lang"
)

// 宏展开工程化：编译器复用解释器的 token 级宏系统（一套逻辑，两处使用）。
// 流程：Lex → SplitMacroDefs → ExpandMacros(mode) → token 重组为源码 → cgen 解析。

// joinTokens 把 token 流重组为源码文本（标识符/数字/字符串间留空格，标点紧贴）。
func joinTokens(toks []lang.Token) string {
	var sb strings.Builder
	prevSpace := true
	for _, t := range toks {
		if t.Kind == lang.TEOF {
			break
		}
		text := t.Text
		if text == "" {
			continue
		}
		// 是否需要前导空格：前一个 token 结束不是符号、当前不是符号时
		curSym := isSymStart(text)
		if !prevSpace && !curSym && !strings.HasSuffix(sb.String(), " ") {
			sb.WriteByte(' ')
		}
		sb.WriteString(text)
		prevSpace = curSym
	}
	return sb.String()
}

func isSymStart(s string) bool {
	if s == "" {
		return false
	}
	c := s[0]
	return !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '"')
}

// expandMacros 对源码做 token 级宏展开（compile 模式；无宏则原样返回）。
func expandMacros(src string, mode string) (string, error) {
	toks, err := lang.Lex(src)
	if err != nil {
		return src, nil // 词法失败回退原样（让 cgen 报错）
	}
	macros, rest, err := lang.SplitMacroDefs(toks)
	if err != nil {
		return src, nil
	}
	if len(macros) == 0 {
		return src, nil
	}
	exp, err := lang.ExpandMacros(rest, macros, mode)
	if err != nil {
		return src, nil
	}
	return joinTokens(exp), nil
}
