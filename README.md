# QuarkLang

一门以「函数调用可观测」为核心的语言：每次函数调用产出 **FuncBuffer**（head 参数 / tail 结果 / log 日志），`@` **签名**（Sign 接口）对调用做第二层包装；**List 是双指针滚动缓冲**（`*` 取开头、`next()` 滚动、耗尽报错）；语言默认**严格检查**，出错时回放 log 定位「为什么运行错了」。

## 布局

- `docs/spec.md` —— 语言设计文档（权威来源，随设计迭代）
- `main.go` —— CLI 入口：`quark <file.qk> [args...]`
- `internal/lang/` —— lexer / parser / typecheck / eval / runtime
- `examples/` —— 示例程序（hello / list / localsorted / memo / io / async / error）

## 构建与运行

~~~sh
go build -o quark .
./quark examples/hello.qk      # Hello World!
./quark examples/async.qk      # 协程：@async() / taskm / channel / yield
./quark examples/error.qk      # ListExhaustedError + log 回放演示
go test ./internal/lang/       # 29 项测试（含 -race 可跑）
~~~

## 状态

v0.1 解释器：滚动 List、FuncBuffer、@memorize / @async 签名、out 多返回值、main(io/env/args)、IO 重定向与执行表、协程系统、编译期静态类型检查。待实现见 `docs/spec.md` §15。
