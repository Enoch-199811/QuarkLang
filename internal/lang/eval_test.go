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

// 滚动 List 语义 + for 语法糖（spec §4）
func TestRollingListAndFor(t *testing.T) {
	src := `
func main(io IOStream) {
    l List<int> = [10, 20, 30];
    io.println(*l);
    io.println(l.next());
    for (x : l) {
        io.println(x);
    }
    io.println(l.head() == l.tail());
}`
	out, err := runSrc(t, src)
	if err != nil {
		t.Fatal(err)
	}
	want := "10\n10\n20\n30\ntrue\n"
	if out != want {
		t.Fatalf("got %q want %q", out, want)
	}
}

// 耗尽后 next() 报错 + log 回放（spec §11.2）
func TestListExhausted(t *testing.T) {
	src := `
func main(io IOStream) {
    l List<int> = [1];
    log "about to consume";
    l.next();
    l.next();
}`
	_, err := runSrc(t, src)
	if err == nil {
		t.Fatal("expected ListExhaustedError")
	}
	if !strings.Contains(err.Error(), "ListExhaustedError") {
		t.Fatalf("got %v", err)
	}
	prog, err := Compile(src)
	if err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	e := Run(prog, "test.qk", nil, strings.NewReader(""), &out)
	if e == nil {
		t.Fatal("expected error")
	}
	ReportError(e, &errOut)
	s := errOut.String()
	if !strings.Contains(s, "---- execution log ----") {
		t.Fatalf("expected log replay, got %q", s)
	}
	if !strings.Contains(s, "about to consume") {
		t.Fatalf("expected manual log entry in replay, got %q", s)
	}
}

// Copyd 参数复制 + __sort__ + out 多返回值（spec §8）
func TestLocalSorted(t *testing.T) {
	src := `
func LocalSorted(n int, a int[Copyd]) {
    a.__sort__();
    out n;
    out a;
}

func main(io IOStream) {
    l List<int> = [3, 1, 2];
    fb FuncBuffer = LocalSorted(7, l);
    io.println(fb.tail.next());
    io.println(fb.tail.next());
    io.println(*l);
}`
	out, err := runSrc(t, src)
	if err != nil {
		t.Fatal(err)
	}
	want := "7\n[1, 2, 3]\n3\n"
	if out != want {
		t.Fatalf("got %q want %q", out, want)
	}
}

// @memorize 签名：缓存命中不再执行函数体；log 仅手动写入（spec §6）
func TestMemorize(t *testing.T) {
	src := `
func expensive(n int) {
    log "running expensive: " + n;
    out n * n;
}

func main(io IOStream) {
    mb memorize = memorize::new();
    fb FuncBuffer = expensive(41) @memorize(mb);
    io.println(*fb.tail);
    fb2 FuncBuffer = expensive(41) @memorize(mb);
    io.println(*fb2.tail);
    while (fb.log.head() != fb.log.tail()) {
        io.println(fb.log.next());
    }
    while (fb2.log.head() != fb2.log.tail()) {
        io.println(fb2.log.next());
    }
}`
	out, err := runSrc(t, src)
	if err != nil {
		t.Fatal(err)
	}
	want := "1681\n1681\nrunning expensive: 41\n"
	if out != want {
		t.Fatalf("got %q want %q", out, want)
	}
	if n := strings.Count(out, "running expensive"); n != 1 {
		t.Fatalf("cache hit must not re-execute the body, log count = %d, out = %q", n, out)
	}
}

// main 参数顺序 io, env, args（spec §8）
func TestMainEnvArgs(t *testing.T) {
	src := `
func main(io IOStream, env HashTable<String, String>, args List<String>) {
    io.println(env.size() > 0);
    while (args.head() != args.tail()) {
        io.println(args.next());
    }
}`
	out, err := runSrc(t, src, "a", "b")
	if err != nil {
		t.Fatal(err)
	}
	want := "true\na\nb\n"
	if out != want {
		t.Fatalf("got %q want %q", out, want)
	}
}

// 严格检查：未声明标识符
func TestUndeclaredIdentifier(t *testing.T) {
	_, err := runSrc(t, `
func main(io IOStream) {
    x = 5;
}`)
	if err == nil || !strings.Contains(err.Error(), "undeclared") {
		t.Fatalf("got %v", err)
	}
}

// 严格检查：重复声明
func TestDuplicateDecl(t *testing.T) {
	_, err := runSrc(t, `
func main(io IOStream) {
    l List<int> = [1];
    l List<int> = [2];
}`)
	if err == nil || !strings.Contains(err.Error(), "duplicate declaration") {
		t.Fatalf("got %v", err)
	}
}

// IO 重定向：io.setOut(FileOutputStream(path))（spec §10）
func TestIORedirectOut(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/out.txt"
	src := fmt.Sprintf(`
func main(io IOStream) {
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
	if out.String() != "" {
		t.Fatalf("console should be empty after redirect, got %q", out.String())
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "redirected\n" {
		t.Fatalf("file content got %q", b)
	}
}

// IO 重定向：IO::setIn(io, path) + readln（spec §10）
func TestIOInputRedirect(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/in.txt"
	if err := os.WriteFile(path, []byte("line1\nline2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := fmt.Sprintf(`
func main(io IOStream) {
    IO::setIn(io, "%s");
    io.println(io.readln());
    io.println(io.readln());
}`, path)
	out, err := runSrc(t, src)
	if err != nil {
		t.Fatal(err)
	}
	want := "line1\nline2\n"
	if out != want {
		t.Fatalf("got %q want %q", out, want)
	}
}

// int 为 32 位（与 C 一致）：溢出环绕
func TestInt32Wrap(t *testing.T) {
	out, err := runSrc(t, `
func main(io IOStream) {
    io.println(2147483647 + 1);
    io.println(-2147483647 - 1);
}`)
	if err != nil {
		t.Fatal(err)
	}
	want := "-2147483648\n-2147483648\n"
	if out != want {
		t.Fatalf("got %q want %q", out, want)
	}
}

// 严格检查：int 字面量超出 32 位范围是编译错误
func TestIntLiteralRange(t *testing.T) {
	_, err := runSrc(t, `
func main(io IOStream) {
    io.println(2147483648);
}`)
	if err == nil || !strings.Contains(err.Error(), "32-bit") {
		t.Fatalf("got %v", err)
	}
}

// reset()：head 直接移回 0，全部历史重新可见（spec §4，已确认）
func TestListReset(t *testing.T) {
	out, err := runSrc(t, `
func main(io IOStream) {
    l List<int> = [1, 2, 3];
    l.next();
    l.next();
    l.reset();
    io.println(*l);
    while (l.head() != l.tail()) {
        io.println(l.next());
    }
}`)
	if err != nil {
		t.Fatal(err)
	}
	want := "1\n1\n2\n3\n"
	if out != want {
		t.Fatalf("got %q want %q", out, want)
	}
}

// GlobalMemory.compact() / memory.compact()：v0.1 宿主 GC 兼容空操作（spec §14）
func TestMemoryCompactStub(t *testing.T) {
	out, err := runSrc(t, `
func main(io IOStream) {
    io.println(GlobalMemory::compact());
    io.println(memory.compact());
}`)
	if err != nil {
		t.Fatal(err)
	}
	want := "0\n0\n"
	if out != want {
		t.Fatalf("got %q want %q", out, want)
	}
}

// @async() 签名：返回 Task；taskm::block 取回完整 FuncBuffer；调度事件自动日志
func TestAsyncBlock(t *testing.T) {
	out, err := runSrc(t, `
func expensive(n int) {
    out n * n;
}

func main(io IOStream) {
    t Task = expensive(41) @async();
    fb FuncBuffer = taskm::block(t);
    io.println(*fb.tail);
    while (fb.log.head() != fb.log.tail()) {
        io.println(fb.log.next());
    }
}`)
	if err != nil {
		t.Fatal(err)
	}
	want := "1681\nasync: spawned expensive(41)\nasync: done\n"
	if out != want {
		t.Fatalf("got %q want %q", out, want)
	}
}

// Task.done() + channel send/recv（worker 阻塞在 recv 时 done 必为 false）
func TestTaskDoneAndChannel(t *testing.T) {
	out, err := runSrc(t, `
func worker(c Channel) {
    x interface{} = c.recv();
    out x;
}

func main(io IOStream) {
    c Channel = taskm::channel();
    t Task = worker(c) @async();
    io.println(t.done());
    c.send(42);
    fb FuncBuffer = taskm::block(t);
    io.println(*fb.tail);
}`)
	if err != nil {
		t.Fatal(err)
	}
	want := "false\n42\n"
	if out != want {
		t.Fatalf("got %q want %q", out, want)
	}
}

// taskm::spawn(fn, args...) 显式启动（函数引用）
func TestTaskSpawn(t *testing.T) {
	out, err := runSrc(t, `
func add(a int, b int) {
    out a + b;
}

func main(io IOStream) {
    t Task = taskm::spawn(add, 3, 4);
    fb FuncBuffer = taskm::block(t);
    io.println(*fb.tail);
}`)
	if err != nil {
		t.Fatal(err)
	}
	want := "7\n"
	if out != want {
		t.Fatalf("got %q want %q", out, want)
	}
}

// yield; 协作式切换点 + 自动日志
func TestYield(t *testing.T) {
	out, err := runSrc(t, `
func worker() {
    yield;
    out 1;
}

func main(io IOStream) {
    t Task = worker() @async();
    fb FuncBuffer = taskm::block(t);
    io.println(*fb.tail);
    while (fb.log.head() != fb.log.tail()) {
        io.println(fb.log.next());
    }
}`)
	if err != nil {
		t.Fatal(err)
	}
	want := "1\nasync: spawned worker()\nyield\nasync: done\n"
	if out != want {
		t.Fatalf("got %q want %q", out, want)
	}
}

// 协程内错误通过 taskm::block 传播（带 FB 供 log 回放）
func TestAsyncErrorPropagates(t *testing.T) {
	_, err := runSrc(t, `
func bad() {
    l List<int> = [1];
    l.next();
    l.next();
}

func main(io IOStream) {
    t Task = bad() @async();
    fb FuncBuffer = taskm::block(t);
}`)
	if err == nil || !strings.Contains(err.Error(), "ListExhaustedError") {
		t.Fatalf("got %v", err)
	}
}

// IOStream 执行表：并发 println 串行化，每行完整
func TestIOExecutionTable(t *testing.T) {
	out, err := runSrc(t, `
func writer(io IOStream, tag String) {
    io.println(tag + " 1111111111");
    io.println(tag + " 2222222222");
}

func main(io IOStream) {
    t1 Task = writer(io, "A") @async();
    t2 Task = writer(io, "B") @async();
    taskm::block(t1);
    taskm::block(t2);
}`)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 intact lines, got %q", out)
	}
	seen := map[string]bool{}
	for _, ln := range lines {
		seen[ln] = true
	}
	for _, wantLine := range []string{"A 1111111111", "A 2222222222", "B 1111111111", "B 2222222222"} {
		if !seen[wantLine] {
			t.Fatalf("missing intact line %q in %q", wantLine, out)
		}
	}
}

// ============ 编译期静态检查（spec §11.1） ============

func TestTypeCheckBadArith(t *testing.T) {
	_, err := runSrc(t, `
func main(io IOStream) {
    io.println("s" - 1);
}`)
	if err == nil || !strings.Contains(err.Error(), "arithmetic requires numbers") {
		t.Fatalf("got %v", err)
	}
}

func TestTypeCheckStarOnNonList(t *testing.T) {
	_, err := runSrc(t, `
func main(io IOStream) {
    n int = 5;
    io.println(*n);
}`)
	if err == nil || !strings.Contains(err.Error(), "requires a List") {
		t.Fatalf("got %v", err)
	}
}

func TestTypeCheckCondNotBool(t *testing.T) {
	_, err := runSrc(t, `
func main(io IOStream) {
    if (1) {
        io.println("x");
    }
}`)
	if err == nil || !strings.Contains(err.Error(), "condition must be bool") {
		t.Fatalf("got %v", err)
	}
}

func TestTypeCheckArgCount(t *testing.T) {
	_, err := runSrc(t, `
func f(a int) {
    out a;
}

func main(io IOStream) {
    f(1, 2, 3);
}`)
	if err == nil || !strings.Contains(err.Error(), "expects 1 args") {
		t.Fatalf("got %v", err)
	}
}

func TestTypeCheckArgType(t *testing.T) {
	_, err := runSrc(t, `
func f(s String) {
    out s;
}

func main(io IOStream) {
    f(42);
}`)
	if err == nil || !strings.Contains(err.Error(), "cannot assign int to String") {
		t.Fatalf("got %v", err)
	}
}

func TestTypeCheckUnknownSign(t *testing.T) {
	_, err := runSrc(t, `
func f() {
}

func main(io IOStream) {
    f() @foo();
}`)
	if err == nil || !strings.Contains(err.Error(), "not a registered signature") {
		t.Fatalf("got %v", err)
	}
}

func TestTypeCheckUseBeforeInit(t *testing.T) {
	_, err := runSrc(t, `
func main(io IOStream) {
    l List<int>;
    io.println(l.size());
}`)
	if err == nil || !strings.Contains(err.Error(), "used before initialization") {
		t.Fatalf("got %v", err)
	}
}

func TestTypeCheckUnknownMember(t *testing.T) {
	_, err := runSrc(t, `
func main(io IOStream) {
    n int = 5;
    n.append(1);
}`)
	if err == nil || !strings.Contains(err.Error(), "no method") {
		t.Fatalf("got %v", err)
	}
}

// 编译期错误必须带行号（对开发者友好）
func TestTypeCheckErrorHasPosition(t *testing.T) {
	_, err := runSrc(t, `
func main(io IOStream) {
    io.println("s" - 1);
}`)
	if err == nil || !strings.Contains(err.Error(), "at line 3") {
		t.Fatalf("got %v", err)
	}
}
