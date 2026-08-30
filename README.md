# QuarkLang · compiler

**跨系统编译器分支**。后端：**LLVM IR**（不经 C 转译）——`qkc` 生成文本 LLVM IR，`llc`/`clang` 产出任意 LLVM 支持平台的原生代码。

## 用法

~~~sh
go run . testdata/hello.qk          # 输出 LLVM IR 到 stdout
go run . -run testdata/hello.qk     # clang 编译 IR 为原生二进制并执行
go test ./...
~~~

## 支持子集（随迭代扩大）

- `func main(io IOStream) { ... }`
- 变量：`x int = expr;` / `s String = "...";` 与赋值 `x = expr;`（alloca/store/load）
- 控制流：`if (cond) {...} else {...}`、`while (cond) {...}`（基本块 + br）
- 表达式：算术 `+ - * / %`、比较 `== != < <= > >=`（icmp）、`&& || !`、布尔字面量（select true/false 输出）
- `io.println(expr, ...)`（格式串按编译后实际类型生成）
- 其它语法暂报明确的「compiler v0.2 supports only ...」错误

## 布局

- `main.go` —— `qkc` CLI（读 .qk，输出 LLVM IR；`-run` 编译执行）
- `internal/cgen/` —— 极简词法/语法 + **LLVM IR 发射器**（零外部依赖，字符串常量/printf/SSA 值）
- `testdata/` —— 测试输入

语言设计见 `docs` 分支的 `spec.md`；解释器见 `interpreter` 分支。
