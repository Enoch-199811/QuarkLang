# QuarkLang

一门以「函数调用可观测」为核心的语言：每次函数调用产出 **FuncBuffer**（head 参数 / tail 结果 / log 日志），`@` **签名**（Sign 接口）对调用做第二层包装；**List 是双指针滚动缓冲**（`*` 取开头、`next()` 滚动、耗尽报错）；语言默认**严格检查**，出错时回放 log 定位「为什么运行错了」。

## 特性

- **FuncBuffer**：head（输入参数）/ tail（out 结果）/ log（执行日志）三件套，调用值默认是整个缓冲区；
- **@ 签名**：`f(args) @memorize(mb)`、`f(args) @async()`——对调用做第二层包装（Sign 接口，`call` 为默认要求）；
- **滚动 List**：`*` 取开头、`next()` 滚到头、`head()==tail()` 耗尽报错、`reset()` 回卷、`for (x : l)` 语法糖；
- **严格检查**：编译期静态类型检查（未声明标识符/成员、类型不匹配、使用前未初始化、签名注册等，错误带行号）+ 运行期硬错误 + log 回放诊断；
- **协程系统**：`@async()`、`taskm::spawn/block/channel`、`yield;`、Task（是否完成 + 完整函数）、IOStream 执行表串行化并发 IO、调度事件自动日志；
- **IO 体系**：`main(io IOStream, env, args)` 数据流注入、`IO::setIn/setOut` 与 `io.setIn/setOut` 重定向、File/Console 流；
- **C 风格数值**：int 32 位环绕、越界字面量编译错误；`Copyd<T>` 传递复制语义。

## 快速开始

~~~sh
go build -o quark .
./quark examples/hello.qk      # Hello World!
go test ./internal/lang/       # 29 项测试（go test -race 同样可跑）
~~~

## 示例

| 示例 | 说明 |
|---|---|
| `examples/hello.qk` | Hello World |
| `examples/list.qk` | 滚动 List：`*` / `next()` / `for` / `reset()` |
| `examples/localsorted.qk` | Copyd 副本排序 + out 多返回值 |
| `examples/memo.qk` | @memorize 缓存（head→tail 映射）+ 手动 log |
| `examples/io.qk` | IO 重定向与 Console/File 流 |
| `examples/async.qk` | 协程：@async() / channel / taskm::spawn·block / 自动日志 |
| `examples/error.qk` | ListExhaustedError + log 回放演示 |

## 布局

- `docs/spec.md` —— 语言设计文档（权威来源，随设计迭代）
- `main.go` —— CLI 入口：`quark <file.qk> [args...]`
- `internal/lang/` —— lexer / parser / typecheck / eval / runtime
- `examples/` —— 示例程序

## 状态

v0.1 解释器已完成：滚动 List、FuncBuffer、@memorize / @async 签名、out 多返回值、main(io/env/args)、IO 重定向与执行表、协程系统、编译期静态类型检查（29 项测试全绿）。待实现见 `docs/spec.md` §15：用户自定义 struct/impl/interface、Copyd<T> 的 .ptr()、block 内存系统。

## 许可证

[MIT](LICENSE) © 2026 Enoch-199811
