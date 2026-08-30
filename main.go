// qkc：QuarkLang → LLVM IR 编译器（compiler 分支，v0.2）。
// 用法：qkc file.qk           输出 LLVM IR 到 stdout
//
//	qkc -run file.qk      用 clang 编译 IR 为原生二进制并执行
package main

import (
	"fmt"
	"os"
	"os/exec"

	"quarklang/compiler/internal/cgen"
)

func main() {
	args := os.Args[1:]
	run := false
	if len(args) > 0 && args[0] == "-run" {
		run = true
		args = args[1:]
	}
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: qkc [-run] <file.qk>   # 输出 LLVM IR；-run 编译并执行")
		os.Exit(2)
	}
	src, err := os.ReadFile(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	ir, err := cgen.Transpile(string(src))
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	if !run {
		fmt.Print(ir)
		return
	}
	// -run：写临时 .ll，clang 编译为原生并执行
	tmp, err := os.CreateTemp("", "quark-*.ll")
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(ir); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	tmp.Close()
	bin := tmp.Name() + ".bin"
	defer os.Remove(bin)
	cmd := exec.Command("clang", tmp.Name(), "-o", bin)
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintln(os.Stderr, "clang:", string(out))
		os.Exit(1)
	}
	out, err := exec.Command(bin).CombinedOutput()
	fmt.Print(string(out))
	if err != nil {
		os.Exit(1)
	}
}
