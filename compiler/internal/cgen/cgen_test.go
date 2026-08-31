package cgen

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestHelloIR(t *testing.T) {
	ir, err := Transpile("func main(io IOStream) {\n    io.println(\"Hello World!\");\n}\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ir, "define i32 @main()") {
		t.Fatalf("missing main:\n%s", ir)
	}
	if !strings.Contains(ir, "declare i32 @printf") {
		t.Fatalf("missing printf decl:\n%s", ir)
	}
	if !strings.Contains(ir, "call i32 (i8*, ...) @printf") {
		t.Fatalf("missing printf call:\n%s", ir)
	}
	if !strings.Contains(ir, "c\"Hello World!\\00\"") {
		t.Fatalf("missing string constant:\n%s", ir)
	}
}

func TestArithmeticIR(t *testing.T) {
	ir, err := Transpile("func main(io IOStream) {\n    io.println(1 + 2 * 3, \"=\");\n}\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ir, "mul i32 2, 3") {
		t.Fatalf("missing mul:\n%s", ir)
	}
	if !strings.Contains(ir, "add i32 1, %") {
		t.Fatalf("missing add:\n%s", ir)
	}
}

// 全链路：QuarkLang → LLVM IR → lli 执行（本机有 LLVM 工具链时）
func TestLLIPipeline(t *testing.T) {
	lli, err := exec.LookPath("lli")
	if err != nil {
		t.Skip("lli not available")
	}
	cases := []struct {
		src  string
		want string
	}{
		{"func main(io IOStream) {\n    io.println(\"Hello World!\");\n}\n", "Hello World!\n"},
		{"func main(io IOStream) {\n    io.println(1 + 2 * 3, \"=\");\n}\n", "7 =\n"},
	}
	for _, c := range cases {
		ir, err := Transpile(c.src)
		if err != nil {
			t.Fatal(err)
		}
		f, err := os.CreateTemp("", "quark-test-*.ll")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.WriteString(ir); err != nil {
			t.Fatal(err)
		}
		name := f.Name()
		f.Close()
		defer os.Remove(name)
		out, err := exec.Command(lli, name).CombinedOutput()
		if err != nil {
			t.Fatalf("lli: %v\nIR:\n%s", err, ir)
		}
		if string(out) != c.want {
			t.Fatalf("got %q want %q\nIR:\n%s", out, c.want, ir)
		}
	}
}

// llvm-as 语法校验：生成的 IR 必须通过 LLVM 官方语法检查
func TestIRSyntax(t *testing.T) {
	llvmAs, err := exec.LookPath("llvm-as")
	if err != nil {
		t.Skip("llvm-as not available")
	}
	progs := []string{
		"func main(io IOStream) {\n    io.println(\"Hello World!\");\n}\n",
		"func main(io IOStream) {\n    io.println(1 + 2 * 3, \"=\");\n}\n",
		"func main(io IOStream) {\n    io.println(-5 + 3, 7 % 3);\n}\n",
		"func main(io IOStream) {\n    io.println(\"a\\nb\", \"q\\\"q\");\n}\n",
	}
	for _, src := range progs {
		ir, err := Transpile(src)
		if err != nil {
			t.Fatal(err)
		}
		f, err := os.CreateTemp("", "quark-syn-*.ll")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.WriteString(ir); err != nil {
			t.Fatal(err)
		}
		name := f.Name()
		f.Close()
		defer os.Remove(name)
		if out, err := exec.Command(llvmAs, name, "-o", os.DevNull).CombinedOutput(); err != nil {
			t.Fatalf("llvm-as rejected IR: %v\n%s\nIR:\n%s", err, out, ir)
		}
	}
}

// 一元负号 + 取模（lli 全链路）
func TestUnaryMinusAndModulo(t *testing.T) {
	lli, err := exec.LookPath("lli")
	if err != nil {
		t.Skip("lli not available")
	}
	ir, err := Transpile("func main(io IOStream) {\n    io.println(-5 + 3, 7 % 3);\n}\n")
	if err != nil {
		t.Fatal(err)
	}
	f, _ := os.CreateTemp("", "quark-neg-*.ll")
	f.WriteString(ir)
	name := f.Name()
	f.Close()
	defer os.Remove(name)
	out, err := exec.Command(lli, name).CombinedOutput()
	if err != nil {
		t.Fatalf("lli: %v\nIR:\n%s", err, ir)
	}
	if string(out) != "-2 1\n" {
		t.Fatalf("got %q, IR:\n%s", out, ir)
	}
}

// 字符串转义（\n、\"）→ IR 转义 → 运行时还原
func TestStringEscapes(t *testing.T) {
	lli, err := exec.LookPath("lli")
	if err != nil {
		t.Skip("lli not available")
	}
	ir, err := Transpile("func main(io IOStream) {\n    io.println(\"a\\nb\", \"q\\\"q\");\n}\n")
	if err != nil {
		t.Fatal(err)
	}
	f, _ := os.CreateTemp("", "quark-esc-*.ll")
	f.WriteString(ir)
	name := f.Name()
	f.Close()
	defer os.Remove(name)
	out, err := exec.Command(lli, name).CombinedOutput()
	if err != nil {
		t.Fatalf("lli: %v\nIR:\n%s", err, ir)
	}
	if string(out) != "a\nb q\"q\n" {
		t.Fatalf("got %q, IR:\n%s", out, ir)
	}
}

// 变量 + if/else + while + 比较 + 布尔（lli 全链路）
func TestVariablesAndControlFlow(t *testing.T) {
	lli, err := exec.LookPath("lli")
	if err != nil {
		t.Skip("lli not available")
	}
	src := "func main(io IOStream) {\n" +
		"    x int = 5;\n" +
		"    y int = x * 2 + 1;\n" +
		"    io.println(y);\n" +
		"    if (y > 10) {\n" +
		"        io.println(\"big\");\n" +
		"    } else {\n" +
		"        io.println(\"small\");\n" +
		"    }\n" +
		"    n int = 0;\n" +
		"    i int = 1;\n" +
		"    while (i <= 5) {\n" +
		"        n = n + i;\n" +
		"        i = i + 1;\n" +
		"    }\n" +
		"    io.println(n);\n" +
		"    io.println(3 == 3, 3 != 4, 2 < 1);\n" +
		"    io.println(true && false, true || false, !true);\n" +
		"    s String = \"hi\";\n" +
		"    io.println(s);\n" +
		"}\n"
	ir, err := Transpile(src)
	if err != nil {
		t.Fatal(err)
	}
	f, _ := os.CreateTemp("", "quark-cf-*.ll")
	f.WriteString(ir)
	name := f.Name()
	f.Close()
	defer os.Remove(name)
	out, err := exec.Command(lli, name).CombinedOutput()
	if err != nil {
		t.Fatalf("lli: %v\nIR:\n%s", err, ir)
	}
	want := "11\nbig\n15\ntrue true false\nfalse true false\nhi\n"
	if string(out) != want {
		t.Fatalf("got %q want %q\nIR:\n%s", out, want, ir)
	}
}

// 多函数 + 递归调用（lli 全链路：编译路径性能对标 C）
func TestFunctionCallAndRecursion(t *testing.T) {
	lli, err := exec.LookPath("lli")
	if err != nil {
		t.Skip("lli not available")
	}
	src := `func fib(n int) int {
    if (n < 2) {
        return n;
    }
    return fib(n - 1) + fib(n - 2);
}

func main(io IOStream) {
    io.println(fib(10));
}`
	ir, err := Transpile(src)
	if err != nil {
		t.Fatal(err)
	}
	f, _ := os.CreateTemp("", "quark-fib-*.ll")
	f.WriteString(ir)
	name := f.Name()
	f.Close()
	defer os.Remove(name)
	out, err := exec.Command(lli, name).CombinedOutput()
	if err != nil {
		t.Fatalf("lli: %v\nIR:\n%s", err, ir)
	}
	if string(out) != "55\n" {
		t.Fatalf("got %q", out)
	}
}
