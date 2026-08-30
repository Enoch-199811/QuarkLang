# QuarkLang · compiler

**跨系统编译器分支**。v0.2：QuarkLang → C 转译器骨架（跨平台由 C 编译器保证）。

## 用法

~~~sh
go run . testdata/hello.qk          # 输出 C 到 stdout
go run . testdata/hello.qk > hello.c && cc hello.c -o hello && ./hello
go test ./...
~~~

## 支持子集（随迭代扩大）

- `func main(io IOStream) { ... }`
- `io.println(expr, ...)`（int / String 字面量、`+ - * /` 算术、括号）
- 其它语法暂报明确的「compiler v0.2 supports only ...」错误

## 布局

- `main.go` —— `qkc` CLI（读 .qk，输出 C）
- `internal/cgen/` —— 转译器（极简词法/语法 + C 生成）
- `testdata/` —— 测试输入

语言设计见 `docs` 分支的 `spec.md`；解释器见 `interpreter` 分支。
