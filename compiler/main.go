// qkc：QuarkLang → LLVM IR 编译器（compiler 分支，v0.2）。
// 用法：qkc file.qk           输出 LLVM IR 到 stdout（增量：IR 缓存命中跳过全编译）
//
//	qkc -run file.qk      用 clang 编译 IR 为原生二进制并执行（增量：二进制缓存命中跳过 clang）
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"quarklang/compiler/internal/cgen"
)

// 增量编译：按源文件内容哈希缓存 IR 与原生二进制。
// 缓存目录可用 QUARK_CACHE 覆盖，默认 <tmp>/quarklang-cache。
func cacheDir() string {
	dir := os.Getenv("QUARK_CACHE")
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "quarklang-cache")
	}
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

func srcHash(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:16]), nil
}

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
	hash, err := srcHash(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	irPath := filepath.Join(cacheDir(), hash+".ll")
	binPath := filepath.Join(cacheDir(), hash+".bin")

	// -run：二进制缓存命中 → 直接执行（跳过全编译 + clang）
	if run {
		if _, err := os.Stat(binPath); err == nil {
			out, err := exec.Command(binPath).CombinedOutput()
			fmt.Print(string(out))
			if err != nil {
				os.Exit(1)
			}
			return
		}
	}

	// IR 缓存命中 → 跳过 lex/parse/typecheck/emit
	var ir string
	if b, err := os.ReadFile(irPath); err == nil {
		ir = string(b)
	} else {
		src, err := os.ReadFile(args[0])
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		ir, err = cgen.Transpile(string(src))
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		if err := os.WriteFile(irPath, []byte(ir), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "warn: 无法写 IR 缓存:", err)
		}
	}

	if !run {
		fmt.Print(ir)
		return
	}

	// -run：编译 IR → 原生二进制并缓存
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
	cmd := exec.Command("clang", tmp.Name(), "-o", binPath, "-O2", "-Wno-override-module")
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintln(os.Stderr, "clang:", string(out))
		os.Exit(1)
	}
	out, err := exec.Command(binPath).CombinedOutput()
	fmt.Print(string(out))
	if err != nil {
		os.Exit(1)
	}
}
