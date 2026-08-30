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
