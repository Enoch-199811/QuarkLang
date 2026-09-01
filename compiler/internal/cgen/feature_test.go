package cgen

import (
	"testing"
)

func runTP(t *testing.T, src string) string {
	ir, err := Transpile(src)
	if err != nil {
		t.Fatalf("%v", err)
	}
	return ir
}

func TestStructImplCompile(t *testing.T) {
	ir := runTP(t, "type struct { a int; b int; } Point;\nimpl Point { fn sum(self Point) int { return self.a + self.b; } }\nfn main(io IOStream) {\n    p Point = .{3, 5};\n    io.println(p.sum());\n}\n")
	if ir == "" {
		t.Fatal("empty ir")
	}
}

func TestGenericCompile(t *testing.T) {
	ir := runTP(t, "fn id<T>(n int) int {\n    return n * 2;\n}\nfn main(io IOStream) {\n    io.println(id<int>(21));\n}\n")
	if ir == "" {
		t.Fatal("empty ir")
	}
}

func TestTryCatchCompile(t *testing.T) {
	ir := runTP(t, "fn main(io IOStream) {\n    try {\n        a int = 10 / 0;\n    } catch (e void) {\n        io.println(1);\n    }\n}\n")
	if ir == "" {
		t.Fatal("empty ir")
	}
}

func TestStringConcatCompile(t *testing.T) {
	ir := runTP(t, "fn main(io IOStream) {\n    s String = \"a\";\n    io.println(s + \"b\");\n}\n")
	if ir == "" {
		t.Fatal("empty ir")
	}
}
