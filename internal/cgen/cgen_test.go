package cgen

import (
	"strings"
	"testing"
)

func TestHello(t *testing.T) {
	c, err := Transpile("func main(io IOStream) {\n    io.println(\"Hello World!\");\n}\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c, "printf(\"%s\\n\", \"Hello World!\")") {
		t.Fatalf("got:\n%s", c)
	}
	if !strings.Contains(c, "int main(void)") {
		t.Fatalf("missing main:\n%s", c)
	}
}

func TestArithmetic(t *testing.T) {
	c, err := Transpile("func main(io IOStream) {\n    io.println(1 + 2 * 3, \"=\");\n}\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c, "printf(\"%d %s\\n\", (1 + (2 * 3)), \"=\")") {
		t.Fatalf("got:\n%s", c)
	}
}
