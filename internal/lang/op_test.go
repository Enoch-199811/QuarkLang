package lang

import (
	"strings"
	"testing"
)

// Operation 接口族：内置 dynamic 协议；多 impl（方法不重叠）+ 空自我实现；运算符重载
func TestOperationOverload(t *testing.T) {
	out, err := runSrc(t, `struct {
    x int;
    y int;
} Vec2;

impl Vec2 AddOperation {
    fn add(self, o Vec2) Vec2 {
        r Vec2; r.x = self.x + o.x; r.y = self.y + o.y; return r;
    }
} Vec2;

impl Vec2 SubOperation {
    fn sub(self, o Vec2) Vec2 {
        r Vec2; r.x = self.x - o.x; r.y = self.y - o.y; return r;
    }
} Vec2;

impl Vec2 EqOperation {
    fn eq(self, o Vec2) bool {
        return self.x == o.x && self.y == o.y;
    }
} Vec2;

impl Vec2 NegOperation {
    fn neg(self) Vec2 {
        r Vec2; r.x = -self.x; r.y = -self.y; return r;
    }
} Vec2;

impl {} Vec2;

fn main(io IOStream) {
    a Vec2; a.x = 1; a.y = 2;
    b Vec2; b.x = 3; b.y = 4;
    c Vec2 = a + b;
    io.println(c.x);
    io.println(c.y);
    d Vec2 = a - b;
    io.println(d.x);
    e Vec2 = -c;
    io.println(e.y);
    io.println(a == b);
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "4\n6\n-2\n-6\nfalse\n" {
		t.Fatalf("got %q", out)
	}
}

// dynamic expand 组合接口解析
func TestDynamicExpandParse(t *testing.T) {
	_, err := Compile(`interface {
    dynamic expand interface Operation;
} Everything;
fn main(io IOStream) { io.println(1); }`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Compile(`interface {
    width String;
} X;`); err == nil || !strings.Contains(err.Error(), "fn") {
		t.Fatalf("expected method-sig error, got %v", err)
	}
}

// 函数重载：同名多签名（参数数据/品种）按实参最优匹配
func TestFunctionOverload(t *testing.T) {
	out, err := runSrc(t, `fn add(a int, b int) int { return a + b; }
fn add(a float, b float) float { return a + b; }
fn add(a String, b String) String { return a + b; }
fn add(a int, b int, c int) int { return a + b + c; }

fn main(io IOStream) {
    io.println(add(1, 2));
    io.println(add(1.5, 2.5));
    io.println(add("A", "B"));
    io.println(add(1, 2, 3));
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "3\n4\nAB\n6\n" {
		t.Fatalf("got %q", out)
	}
}

// 反引号原始字符串（Go 语义）
func TestRawString(t *testing.T) {
	out, err := runSrc(t, `fn main(io IOStream) {
    s String = `+"`"+`line1
line2 "q" \n raw`+"`"+`;
    io.println(s.size());
    io.println(s);
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "22\nline1\nline2 \"q\" \\n raw\n" {
		t.Fatalf("got %q", out)
	}
}

// expand interface X; 是接口体内的语句（而非常规声明）；dynamic 前缀；组合递归 + 多 impl 聚合满足
func TestExpandStatementSyntax(t *testing.T) {
	out, err := runSrc(t, `interface {
    dynamic expand interface AddOperation;
    dynamic fn ping(self Self) void;
} Everything;

struct {
    x int;
} Thing;

impl Thing AddOperation {
    fn add(self, o Thing) Thing {
        r Thing; r.x = self.x + o.x; return r;
    }
} Thing;

impl Thing Everything {
    fn ping(self) void {
    }
} Thing;

fn main(io IOStream) {
    a Thing; a.x = 5;
    b Thing; b.x = 7;
    io.println((a + b).x);
    io.println(a.ping());
}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "12\n") {
		t.Fatalf("got %q", out)
	}
}
