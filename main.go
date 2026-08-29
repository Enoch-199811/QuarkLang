package main

import (
	"fmt"
	"os"

	"quarklang/internal/lang"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: quark <file.qk> [args...]")
		os.Exit(2)
	}
	src, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	prog, err := lang.Compile(string(src))
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	if err := lang.Run(prog, os.Args[1], os.Args[2:], os.Stdin, os.Stdout); err != nil {
		lang.ReportError(err, os.Stderr)
		os.Exit(1)
	}
}
