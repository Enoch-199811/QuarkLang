# QuarkLang

一门以「函数调用可观测」为核心的语言：每次函数调用产出 **FuncBuffer**（head 参数 / tail 结果 / log 日志），`@` **签名**（Sign 接口）对调用做第二层包装；**List 是双指针滚动缓冲**（`*` 取开头、`next()` 滚动、耗尽报错）；语言默认**严格检查**，出错时回放 log 定位「为什么运行错了」。定位：**CLI 工具的语言**。

## 特性

- **FuncBuffer**：head（输入参数）/ tail（out 结果）/ log（执行日志）三件套，调用值默认是整个缓冲区；
- **@ 签名**：`f(args) @memorize(mb)`、`f(args) @async()`——对调用做第二层包装（Sign 接口，`call` 为默认要求）；
- **滚动 List**：`*` 取开头、`next()` 滚到头、`head()==tail()` 耗尽报错、`reset()` 回卷、`for (x : l)` 语法糖；
- **严格检查**：编译期静态类型检查（未声明标识符/成员、类型不匹配、使用前未初始化、签名注册等，错误带行号）+ 运行期硬错误 + log 回放诊断；
- **协程系统**：`@async()`、全局变量 `taskm`（`taskm.spawn(...)`→pid / `taskm.block(pid)` / `taskm.merge(pid)` / `taskm.done(pid)` / `taskm.channel([n])` 默认 1024）、Task（是否完成 + 完整函数）、IOStream 执行表（FIFO、读优先）、调度事件自动日志（spawn 带 pid / done）；
- **IO 体系**：`main(io IOStream, env, args)` 数据流注入、`IO::setIn/setOut` 与 `io.setIn/setOut` 重定向、File/Console 流；
- **C 风格数值**：int 32 位环绕、越界字面量编译错误；`Copyd<T>` 传递复制语义。
- **struct / impl / interface**：用户自定义结构体与接口；`self` 实例方法（`.` 调用）与静态方法（`::` 调用）；`impl Sign` 接口一致性检查；用户类型可直接实现 Sign 作为自定义签名（如 `@LocalMemorize(mb)`）。
- **泛型 / 指针 / Copyd**：`struct<T>` / `impl<T>`（struct 有泛型参数时 impl 必须引入，实例化替换检查）；指针类型 `T&` 与 `null`（自动解引用、NullPointerError）；`Copyd<T>` 运行时包装 + `.ptr()`。
- **真实内存系统**：block 分配（`memory.setBlock(n)` 调粒度）、写入标脏、协程结束自动标记可回收、`compact()` 实际清理。

## 快速开始（Get Started）

### 解释器（仓库根目录，Go 模块 `quarklang`）

~~~sh
go build -o quark .          # 构建 CLI
./quark examples/hello.qk    # 运行 .qk 程序（Hello World!）
go test ./internal/lang/     # 47 项测试（go test -race 同样可跑）
~~~

### 编译器（`compiler/` 目录，独立 Go 模块 `quarklang/compiler`，LLVM 后端）

需要本机 LLVM 工具链（`clang`/`lli`/`llvm-as`；仅生成 IR 可只需要 `llvm-as`）。

**依赖说明**：
- **Go ≥ 1.21**：解释器与编译器均为纯 Go 实现，**零第三方 Go 依赖**（词法/解析/类型检查/求值/LLVM IR 发射全部手写）；
- **LLVM 工具链**（系统包，如 `clang`/`llvm`）：`qkc -run` 用 `clang` 编译 IR 为原生二进制；测试用 `lli` 做全链路执行、`llvm-as` 做 IR 语法校验；
- 可选：`rustc`/`gcc` 仅用于跨语言性能对比基准，不影响构建；
- 缓存目录：qkc 增量编译缓存默认 `/tmp/quarklang-cache`（可用环境变量 `QUARK_CACHE` 覆盖）。

~~~sh
cd compiler
go run . testdata/hello.qk          # 输出 LLVM IR 到 stdout
go run . -run testdata/hello.qk     # clang 编译为原生二进制并执行
go test ./...                       # 7 项测试（llvm-as 语法校验 + lli 全链路）
~~~

## 示例

| 示例 | 说明 |
|---|---|
| `examples/hello.qk` | Hello World |
| `examples/list.qk` | 滚动 List：`*` / `next()` / `for` / `reset()` |
| `examples/localsorted.qk` | Copyd 副本排序 + out 多返回值 |
| `examples/memo.qk` | @memorize 缓存（head→tail 映射）+ 手动 log |
| `examples/io.qk` | IO 重定向与 Console/File 流 |
| `examples/async.qk` | 协程：@async() / channel / taskm.spawn→pid · block · done / 自动日志 |
| `examples/error.qk` | ListExhaustedError + log 回放演示 |
| `examples/struct.qk` | 用户 struct/interface/impl + 用户自定义 Sign |

## 布局

- `main.go` —— 解释器 CLI 入口：`quark <file.qk> [args...]`
- `internal/lang/` —— lexer / parser / typecheck / eval / runtime
- `examples/` —— 示例程序
- `compiler/` —— LLVM 编译器（`qkc` CLI + `internal/cgen` IR 发射器 + 测试）

## 分支

| 分支 | 内容 |
|---|---|
| `main` | 集成分支（interpreter + examples 的合并结果） |
| `interpreter` | 解释器实现（lexer/parser/typecheck/eval/runtime） |
| `compiler` | 编译器（**已并入 main**，位于 `compiler/` 目录；分支保留历史） |
| `examples` | 示例程序 |
| `docs` | 语言设计文档（独立维护，**不并入 main**） |

语言设计文档见 [docs 分支的 spec.md](https://github.com/Enoch-199811/QuarkLang/blob/docs/spec.md)（权威来源，随设计迭代；本地可直接在 `docs/` 目录编辑提交，它跟随 docs 分支）。

## 状态

v0.2：解释器（滚动 List、FuncBuffer、@memorize/@async 签名、struct/impl/interface、泛型/指针/Copyd、真实内存系统、协程、静态类型检查，**47 项测试全绿**）；编译器（`compiler/`，**LLVM 后端**：变量/控制流/算术/比较/布尔，`-run` 一键编译执行，**7 项测试全绿**）。待实现见 docs 分支 spec §15。

## 许可证

[MIT](LICENSE) © 2026 Enoch-199811
