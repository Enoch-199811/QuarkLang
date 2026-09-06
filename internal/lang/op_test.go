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
