// qkc：QuarkLang → C 转译器（compiler 分支，v0.2 骨架）。
// 当前支持子集：func main(io IOStream) { io.println(...) }，
// 参数支持 int/String 字面量与 + - * / 算术。
package main

import (
	"fmt"
	"os"

	"quarklang/compiler/internal/cgen"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: qkc <file.qk>   # 输出 C 到 stdout")
		os.Exit(2)
	}
	src, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	c, err := cgen.Transpile(string(src))
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	fmt.Print(c)
}
