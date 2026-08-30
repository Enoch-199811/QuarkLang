package lang

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"
)

func runSrc(t *testing.T, src string, args ...string) (string, error) {
	t.Helper()
	prog, err := Compile(src)
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	err = Run(prog, "test.qk", args, strings.NewReader(""), &out)
	return out.String(), err
}

// ============ v2 核心语义 ============

func TestHello(t *testing.T) {
	out, err := runSrc(t, `func main(io IOStream) {
    io.println("Hello World!");
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "Hello World!\n" {
		t.Fatalf("got %q", out)
	}
}

// 函数必须带返回类型；return 返回真实值并结束
func TestReturnValue(t *testing.T) {
	out, err := runSrc(t, `
func sq(n int) int {
    return n * n;
}

func main(io IOStream) {
    x int = sq(7);
    io.println(x);
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "49\n" {
		t.Fatalf("got %q", out)
	}
}

// log 记录并结束函数（return 不再执行）
func TestLogEndsFunction(t *testing.T) {
	out, err := runSrc(t, `
func f() int {
    log "done";
    return 999;
}

func main(io IOStream) {
    x int = f();
    io.println(x);
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "nil\n" {
		t.Fatalf("got %q", out)
	}
}

// try/catch（名字 + 类型）
func TestTryCatch(t *testing.T) {
	out, err := runSrc(t, `
func main(io IOStream) {
    try {
        y int = 1 / 0;
        io.println(y);
    } catch (e void) {
        io.println("caught: " + e);
    }
    io.println("after");
}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "caught: DivisionByZeroError") || !strings.HasSuffix(out, "after\n") {
		t.Fatalf("got %q", out)
	}
}

// .{...} 匿名结构体字面量（字段名可为关键字 in/out）
func TestStructLiteral(t *testing.T) {
	out, err := runSrc(t, `
func main(io IOStream) {
    io.println(.{in: 1, out: 2}.out);
    io.println(.{in: 1, out: 2}.in);
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "2\n1\n" {
		t.Fatalf("got %q", out)
	}
}

// 滚动 List：* 取开头、next 滚动、for 语法糖、耗尽、reset
func TestRollingList(t *testing.T) {
	out, err := runSrc(t, `
func main(io IOStream) {
    l List<int> = [10, 20, 30];
    io.println(*l);
    io.println(l.next());
    for (x : l) {
        io.println(x);
    }
    io.println(l.head() == l.tail());
    l.reset();
    io.println(*l);
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "10\n10\n20\n30\ntrue\n10\n" {
		t.Fatalf("got %q", out)
	}
}

func TestListExhausted(t *testing.T) {
	_, err := runSrc(t, `
func main(io IOStream) {
    l List<int> = [1];
    l.next();
    l.next();
}`)
	if err == nil || !strings.Contains(err.Error(), "ListExhaustedError") {
		t.Fatalf("got %v", err)
	}
}

// ============ 签名（v2）：f(args) @mb() ============

func TestMemorizeSignature(t *testing.T) {
	out, err := runSrc(t, `
func expensive(n int) int {
    return n * n;
}

func main(io IOStream) {
    mb memorize = memorize::new();
    x int = expensive(41) @mb();
    io.println(x);
    y int = expensive(41) @mb();
    io.println(y);
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "1681\n1681\n" {
		t.Fatalf("got %q", out)
	}
}

// ============ taskm 线程模型 ============

func TestTaskmThreads(t *testing.T) {
	out, err := runSrc(t, `
func add(a int, b int) int {
    return a + b;
}

func main(io IOStream) {
    pid int = taskm.spawn();
    io.println(taskm.done(pid));
    taskm.merge(pid, add, 3, 4);
    io.println(taskm.done(pid));
    taskm.block(pid);
    io.println(taskm.done(pid));
}`)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 3 || lines[0] != "true" || lines[2] != "true" {
		t.Fatalf("got %q", out)
	}
}

// ============ struct / impl / interface ============

func TestStructAndMethods(t *testing.T) {
	out, err := runSrc(t, `
struct {
    x int;
    y int;
} Point;

impl {
    func translate(self, dx int, dy int) void {
        self.x = self.x + dx;
        self.y = self.y + dy;
    }
    func sum(self) int {
        return self.x + self.y;
    }
    func new() Point {
        p Point;
        p.x = 3;
        p.y = 4;
        return p;
    }
} Point;

func main(io IOStream) {
    p Point = Point::new();
    p.translate(1, 1);
    io.println(p.x);
    io.println(p.y);
    io.println(p.sum());
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "4\n5\n9\n" {
		t.Fatalf("got %q", out)
	}
}

func TestImplConformanceError(t *testing.T) {
	src := `
interface {
    func call(prefix void, rec void) void;
} Sign;

struct {
    x int;
} Bad;

impl Sign {
    func new() Bad {
        b Bad;
        return b;
    }
} Bad;

func main(io IOStream) {
    b Bad = Bad::new();
    io.println(b.x);
}`
	_, err := runSrc(t, src)
	if err == nil || !strings.Contains(err.Error(), "missing method") {
		t.Fatalf("got %v", err)
	}
}

// ============ 泛型 / 指针 / Copyd ============

func TestGenericNode(t *testing.T) {
	out, err := runSrc(t, `
struct<T> {
    val T;
    next node<T>&;
} node;

impl<T> {
    func set(self, v T) void {
        self.val = v;
    }
    func new() node<T> {
        n node<T>;
        return n;
    }
} node;

func main(io IOStream) {
    a node<int> = node::new();
    a.set(42);
    b node<int> = node::new();
    b.set(7);
    a.next = b;
    io.println(a.next.val);
    a.next = null;
    io.println(a.next == null);
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "7\ntrue\n" {
		t.Fatalf("got %q", out)
	}
}

func TestNullPointerDeref(t *testing.T) {
	_, err := runSrc(t, `
struct<T> {
    val T;
    next node<T>&;
} node;

impl<T> {
    func new() node<T> {
        n node<T>;
        return n;
    }
} node;

func main(io IOStream) {
    a node<int> = node::new();
    a.next = null;
    io.println(a.next.val);
}`)
	if err == nil || !strings.Contains(err.Error(), "NullPointerError") {
		t.Fatalf("got %v", err)
	}
}

func TestCopydPtr(t *testing.T) {
	out, err := runSrc(t, `
func f(io IOStream, a int[Copyd]) void {
    a.append(99);
    b List<int> = a.ptr();
    io.println(b.size());
}

func main(io IOStream) {
    l List<int> = [1, 2];
    f(io, l);
    io.println(l.size());
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "3\n2\n" {
		t.Fatalf("got %q", out)
	}
}

// ============ 真实内存系统 ============

func TestMemoryCompactReclaims(t *testing.T) {
	src := `
func worker() void {
    log "w";
}

func main(io IOStream) {
    pid int = taskm.spawn();
    taskm.merge(pid, worker);
    taskm.block(pid);
    GlobalMemory::compact();
}`
	prog, err := Compile(src)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	in, err := runWithInterp(prog, "test.qk", nil, strings.NewReader(""), &out)
	if err != nil {
		t.Fatal(err)
	}
	// merge 的函数块（owner=pid）已被 compact 回收；线程自身的持久块（owner=0）保留 → 剩 1
	if n := in.mem.BlockCount(); n != 1 {
		t.Fatalf("expected 1 block (persistent thread block) after compact, got %d", n)
	}
}

// ============ 严格检查 ============

func TestTypeCheckErrors(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"badarith", `func main(io IOStream) {
    io.println("s" - 1);
}`, "arithmetic requires numbers"},
		{"star_nonlist", `func main(io IOStream) {
    n int = 5;
    io.println(*n);
}`, "requires a List"},
		{"cond_not_bool", `func main(io IOStream) {
    if (1) {
        io.println("x");
    }
}`, "condition must be bool"},
		{"arg_count", `func f(a int) int {
    return a;
}
func main(io IOStream) {
    f(1, 2, 3);
}`, "expects 1 args"},
		{"arg_type", `func f(s String) String {
    return s;
}
func main(io IOStream) {
    f(42);
}`, "cannot assign int to String"},
		{"undeclared", `func main(io IOStream) {
    x = 5;
}`, "undeclared"},
		{"duplicate", `func main(io IOStream) {
    l List<int> = [1];
    l List<int> = [2];
}`, "duplicate declaration"},
		{"use_before_init", `func main(io IOStream) {
    l List<int>;
    io.println(l.size());
}`, "used before initialization"},
		{"unknown_member", `func main(io IOStream) {
    n int = 5;
    n.append(1);
}`, "no method"},
		{"missing_ret", `func f() {
}`, "必须声明返回类型"},
	}
	for _, c := range cases {
		_, err := runSrc(t, c.src)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%s: got %v, want contains %q", c.name, err, c.want)
		}
	}
}

// ============ 边界单元测试 ============

func TestArithmeticEdges(t *testing.T) {
	out, err := runSrc(t, `
func main(io IOStream) {
    io.println(7 % 3);
    io.println(-7 % 3);
    io.println(-5 + 3);
    io.println(1 + 2 + 3);
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "1\n-1\n-2\n6\n" {
		t.Fatalf("got %q", out)
	}
}

func TestDivByZero(t *testing.T) {
	_, err := runSrc(t, `func main(io IOStream) {
    io.println(1 / 0);
}`)
	if err == nil || !strings.Contains(err.Error(), "DivisionByZeroError") {
		t.Fatalf("got %v", err)
	}
}

func TestStringConcat(t *testing.T) {
	out, err := runSrc(t, `
func main(io IOStream) {
    io.println("a" + "b" + 3);
    io.println(1 + 2 + "x");
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "ab3\n3x\n" {
		t.Fatalf("got %q", out)
	}
}

func TestBoolOps(t *testing.T) {
	out, err := runSrc(t, `
func main(io IOStream) {
    io.println(true && false);
    io.println(true || false);
    io.println(!true);
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "false\ntrue\nfalse\n" {
		t.Fatalf("got %q", out)
	}
}

func TestHashTableOps(t *testing.T) {
	out, err := runSrc(t, `
func main(io IOStream) {
    h HashTable<String, int> = HashTable::new();
    h.put("a", 10);
    h.put("b", 20);
    io.println(h.get("a"));
    io.println(h.contains("b"));
    io.println(h.size());
    h.remove("a");
    io.println(h.size());
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "10\ntrue\n2\n1\n" {
		t.Fatalf("got %q", out)
	}
}

func TestInt32Wrap(t *testing.T) {
	out, err := runSrc(t, `
func main(io IOStream) {
    io.println(2147483647 + 1);
}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "-2147483648\n" {
		t.Fatalf("got %q", out)
	}
}

func TestIORedirect(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/out.txt"
	src := fmt.Sprintf(`func main(io IOStream) {
    io.setOut(FileOutputStream("%s"));
    io.println("redirected");
}`, path)
	prog, err := Compile(src)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Run(prog, "test.qk", nil, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "redirected\n" {
		t.Fatalf("got %q", b)
	}
}
