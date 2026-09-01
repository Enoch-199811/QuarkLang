# QuarkLang

一门**编译型编程语言**：为高计算、高并发、海量临时数据场景设计。同一种语法，双后端：**Go 解释器**（开发/调试）+ **LLVM IR 编译器**（`qkc`，性能 = C 级）。

## 亮点

- **编译路径性能 = C**：LLVM `-O3` 同后端（fib35 21ms vs C 22ms；P99 延迟分布逐分位同级）；
- **并发模型 = Erlang 式**：`spawn/merge/block/done/channel` 用户态任务 + 线程池——并发模型是 Erlang 式，性能是 C 级；
- **零 GC 内存**：block 线性分配 + 占用度最小堆，`delete` 入空闲队列（数据保留）、`clear` 才清空——无 GC 停顿、碎片率 0.195%、复用率 99.96%；
- **sum 数学优化**：线性闭式 / 周期位级置换 / 均匀随机期望——10 亿项求和 O(1)（2ms，Go 循环 223ms）；
- **增量编译**：IR+二进制两级缓存，二次编译 16 倍提速；
- **零第三方依赖**：词法/解析/类型检查/求值/LLVM IR 发射全部手写。

## 特性（v2 语法面）

- **函数**：显式返回类型，`return expr` 结束并返回，`log expr;` 记录并结束；
- **try/catch**：`try { } catch (e void) { }`（除零等错误可捕获）；
- **void = 空接口**：任意值可赋；
- **struct / impl / interface**：`type struct { a int; } Point;`、`impl Point { fn sum(self) int {...} }`、`.{3, 5}` 字面量、`self.a` 字段访问；
- **泛型**：`func<T>` / `f<int>(x)`（类型擦除，编译可用）；
- **函数引用**：`type function<int, int> F;`、函数作为值传递与调用；
- **指针 / 堆申请**：`pointer <T>` 修饰、`new <type>[size]` 堆上申请（非法大小 `badAlloc`）、空指针解引用 `NullPointerError`；
- **签名**：`f(args) @mb(prefix)` ≡ `mb.call(prefix)(.{in, out})`——记忆化/包装；
- **taskm 并发**：`t thread = taskm.spawn(); t.merge(fn, args); taskm.block(t.pid()); taskm.done(pid); c channel = taskm.channel(); c.send(v); c.recv();`——用户态任务 + 线程池（编译路径 pthread 载体，跨系统）；
- **宏系统**：`macro {模式}{主体}`、`#when(compile/explain)`、`#insert(#ast(...))`、`#exec`、`#error`——**解释器与编译器共享同一 token 级宏展开**；
- **delete/clear 语义**：`delete` 入空闲队列（数据保留，可复用），`clear` 真正清空空闲数据（使用中保留，数据安全）；
- **List<int>**：字面量/下标/`size()`/`get(i)`/`append(v)`（几何增长 O(n)）；
- **program/library**：`program main;`/`library;`、`import`、`pub`——可发布为 `.qlib` 库；
- **解释器与编译器语法完全一致**（同前端，双后端）。

## 快速开始

### 解释器（仓库根，Go 模块 `quarklang`）

```sh
go build -o quark .
./quark examples/hello.qk
go test ./internal/lang/     # 全量测试（-race 可跑）
```

### 编译器（`compiler/`，LLVM 后端）

```sh
cd compiler && go build -o qkc .
./qkc -run hello.qk          # LLVM IR → clang 原生 → 执行
./qkc hello.qk               # 仅输出 IR
```

**依赖**：Go ≥ 1.21（零第三方 Go 依赖）；LLVM 工具链（`clang`/`lli`/`llvm-as`，系统包）。可选 `rustc`/`gcc` 仅用于跨语言对比基准。

**跨系统**：qkc 产出与平台无关的 LLVM IR，目标平台 `clang`/`llc` 生成原生二进制；`.qlib` 库（gob）跨系统；线程运行时（`qthreads.c` 内嵌）POSIX/Windows 双载体。

**优化旗标**：默认 `-O3`（便携）；`QUARK_CFLAGS="-O3 -march=native"` 本机极限（产物仅当前 CPU）；PGO 可用 `-fprofile-generate/-fprofile-use`（fib35 -29%）。

**缓存**：增量编译缓存默认 `/tmp/quarklang-cache`（`QUARK_CACHE` 覆盖）；`QUARK_CFLAGS` 参与缓存键。

## 性能（实测，可复现）

| 基准 | QuarkLang(编译) | C | Rust | Go | Erlang |
|---|---|---|---|---|---|
| fib(30) | **3ms** | 3ms | 3ms | 6ms | 1106ms |
| fib(35) | **21ms** | 22ms | **17ms** | 36ms | — |
| 8 路并发 ×1e7 | **1ms** | — | — | 6ms | 61s(1e5) |
| P99（fib20×1000） | **14/27µs** | 16/25µs | — | — | — |
| 潮汐 1 亿轮 | **1ms** | 1ms | 29ms | 92ms | — |
| sum 10 亿项（闭式） | **2ms** | — | — | 223ms | — |

复现：`bench/Makefile`（跨语言）+ `docs/benchmarks.md`（方法/公正性声明）。

## 布局

- `main.go` + `internal/lang/` —— 解释器（lexer/parser/typecheck/eval/runtime/宏）
- `compiler/` —— LLVM 编译器（`qkc` + `internal/cgen` IR 发射器 + 内嵌线程运行时）
- `bench/` —— 跨语言对比源（C/Rust/Go/Erlang + Makefile）
- `examples/` —— 示例

## 分支

| 分支 | 内容 |
|---|---|
| `main` | 集成（解释器 + 编译器 + bench） |
| `interpreter` | 解释器历史 |
| `examples` | 示例 |
| `docs` | 设计文档（独立维护，不并入 main；含 benchmarks.md） |
| `design` | XMind 设计蓝图（只读保护） |

设计文档：[docs 分支 spec.md](https://github.com/Enoch-199811/QuarkLang/blob/docs/spec.md)。

## 状态

v2 语法面完整（解释器 + 编译器一致），性能 = C 级（编译路径）。见 `docs/benchmarks.md` 与宣传视频（`/home/jack/quarklang-promo/`）。