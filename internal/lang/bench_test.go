package lang

import (
	"bytes"
	"strings"
	"testing"
)

// ============ 性能基准（高计算场景） ============

// 算术密集循环：1e6 次循环
const loopSrc = `func main(io IOStream) {
    n int = 0;
    i int = 0;
    while (i < 1000000) {
        n = n + i * 2 - 1;
        i = i + 1;
    }
    io.println(n);
}
`

// 递归计算：fib(24)
const fibSrc = `func fib(n int) int {
    if (n < 2) {
        return n;
    }
    return fib(n - 1) + fib(n - 2);
}

func main(io IOStream) {
    io.println(fib(24));
}
`

// 密集函数调用 + List 操作
const callSrc = `func sq(n int) int {
    return n * n;
}

func main(io IOStream) {
    total int = 0;
    i int = 0;
    while (i < 100000) {
        total = total + sq(i);
        i = i + 1;
    }
    io.println(total);
}
`

func benchRun(b *testing.B, src string) {
	prog, err := Compile(src)
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var out bytes.Buffer
		if err := Run(prog, "bench.qk", nil, strings.NewReader(""), &out); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCompile(b *testing.B) {
	for i := 0; i < b.N; i++ {
		if _, err := Compile(fibSrc); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEvalLoop1M(b *testing.B)    { benchRun(b, loopSrc) }
func BenchmarkFib24(b *testing.B)         { benchRun(b, fibSrc) }
func BenchmarkFuncCalls100K(b *testing.B) { benchRun(b, callSrc) }
