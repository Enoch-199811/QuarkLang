# QuarkLang 语法清单（当前实现逐条核对）

> 编号供逐项检查：M=宏，K=语句，E=表达式，S=结构/泛型/接口，O=对象模型，T=类型，P=编译工具

## M 宏
| 编号 | 语法 | 说明/已验证例 |
|---|---|---|
| M1 | `#macro name (a, b, ...) { ... }` | 参数列表分隔符 `()` `[]` `{}` 皆可，参数个数不限（逗号分隔，按名替换） |
| M2 | `name(1,2)` / `name[1,2]` / `name{1,2}` | 调用分隔符与定义无关，可混用；实参个数必须等于形参个数 |
| M3 | 参数按名替换 | 主体中同名标识符替换为实参 token 序列 |
| M4 | `#return <expr>` | **宏返回**：展开结果 = `#return` 后的 token（参数替换后），立即终止整个宏展开（inside `#when` 分支同样生效） |
| M5 | `#when (run) { ... }` / `#when (compile) { ... }` | 态选择：解释器 → run 分支；qkc 编译 → compile 分支（**run 分支编译期丢弃 = 设计**） |
| M6 | `#error("msg")` | 选中分支内出现即报预处理错误 |
| M7 | 已移除 | `#insert` / `#execute` / `#exec` / `#ast`（模式匹配时代遗迹，遇到即报已移除） |

## K 语句
| 编号 | 语法 | 说明 |
|---|---|---|
| K1 | `fn name(p int, q float) int { ... }` | 函数；返回 void 用 `void`；参数名在前类型在后 |
| K2 | `x int = 1;` `x int;` | 变量声明（类型在名字后）；`l List<int> = [1,2];` |
| K3 | `x = 2;` `p.x = 3;` `l[i] = 1;` | 赋值；成员/下标赋值 |
| K4 | `if/else` `while` `for (x in List)` | for 只支持 List 迭代 |
| K5 | `try { } catch (e) { }` | 异常捕获（catch 变量装错误信息） |
| K6 | `return e;` `log e;` `delete x;` | log 记录并结束函数；delete 释放块 |
| K7 | `program main;` / `program library;` | 程序声明；library 不可运行 |
| K8 | `pub fn ...` `pub struct ...` `import "util";` | 库导出/导入 |
| K9 | main 参数 | 1-3 个：`(io IOStream)` `+env HashTable<String,String>` `+args List<String>` |
| K10 | **无** break/continue | for/while 无提前退出语句（当前实现无此关键字） |

## E 表达式
| 编号 | 语法 | 说明 |
|---|---|---|
| E1 | `123` `-7` | int；**32 位补码 wrap**（与编译路径一致） |
| E2 | `1.5` `"str"` `true/false` `null` | float/String/bool/null 字面量 |
| E3 | `[1, 2, 3]` | List 字面量 |
| E4 | `.{a: 1, b: 2}` | 匿名结构字面量（字段名可为关键字 `in`/`out`） |
| E5 | `new int[10]` | 堆分配（block 管理）；`l pointer List<int> = new int[10];` |
| E6 | `l[i]` `*l` `l.size()` | 下标；List 取头；List 方法 size/head/tail/next/reset/append/appendAll/toString/__sort__ |
| E7 | `+ - * / %` `<< >>` `== != < <= > >=` `&& || !` | 算术/位移/比较/逻辑；`+` 支持 String 拼接 |
| E8 | `f(x)` `obj.m(x)` `Type::new()` `p.x` | 调用/方法/静态/成员读取 |
| E9 | `expensive(41) @mb();` | **签名调用**：`@` 后缀任意 Sign 实例（mb 是 memorize 实例） |

## S 结构/泛型/接口
| 编号 | 语法 | 说明 |
|---|---|---|
| S1 | `struct { x int; y int; } Point;` | 结构类型；字段 名字在前类型在后 |
| S2 | `struct<T> { val T; next node<T>&; } node;` | 泛型结构；`&` 指针字段；实例化 `node<int>` |
| S3 | `impl { fn translate(self, ...) void {...} fn new() Point {...} } Point;` | 非泛型 impl（同名结构，静态 new + 实例方法 self） |
| S4 | `impl<T> { fn set(self, v T) void {...} fn new() node<T> {...} } node;` | 泛型 impl |
| S5 | `Point::new()` `node::new()` | 静态方法调用（`::` 用于类静态；实例成员用 `.`） |
| S6 | `interface { fn call(prefix void, rec void) void; } Sign;` | 接口声明 |
| S7 | `impl Bad Sign { ... }` | 接口实现（impl 结构 接口）；缺方法 → `missing method` 编译错 |
| S8 | `self.x` | 实例方法内改自身字段 |

## O 对象模型
| 编号 | 语法 | 说明 |
|---|---|---|
| O1 | `taskm.spawn()` → thread 实例 | 全局实例 `taskm`（**点调用**；`::` 报 unknown scope） |
| O2 | `t.pid()` `t.merge(fn, args...)` `t.talk(channel)` | thread 方法 |
| O3 | `taskm.block(t.pid())` `taskm.done(t.pid())` → bool | 阻塞/查询空闲 |
| O4 | `taskm.channel(n?)` → channel；`c.send(v)` `c.recv()` | 通道 |
| O5 | `GlobalMemory.clear()` `.compact()` `.setBlock(n)` | 全局内存实例（点调用；**`::` 不适配**）；`memory` 为同义名 |
| O6 | `mb memorize = memorize::new();` | memorize 是内置 Sign 类——缓存机制**类内部**；`@mb()` 只是注册 |

## T 类型
| 编号 | 类型 | 说明 |
|---|---|---|
| T1 | `int` `long` `char` `float` `bool` `String` | 标量（`long`/`char` 语义同 int，均 32 位） |
| T2 | `List<T>` `HashTable<K,V>` | 容器（HashTable 键按 String 显示散列，值深拷贝） |
| T3 | `T&` 指针 | `next node<T>&`；`null` 可赋指针 |
| T4 | `Copyd<T>`；`int[Copyd]`/`int[]` | Copyd 语义（深拷贝传参）；`int[]` ≈ Copyd<数组> |
| T5 | `pointer T` | `l pointer List<int> = new int[10];` |
| T6 | `void` `IOStream` `InputStream` `OutputStream` `thread` `memorize` | 内建 |
| T7 | `program library;`/`pub` | 库级 |

## P 编译工具
| 编号 | 行为 |
|---|---|
| P1 | `qkc file.qk` 输出 LLVM IR；`qkc -run file.qk` 编译并执行 |
| P2 | 默认 `-O3 -flto=thin`；`QUARK_CFLAGS` 可覆盖 |
| P3 | 缓存：`<tmp>/quarklang-cache`，键=源码+引擎版本+运行时指纹；`QUARK_CACHE` 可改目录 |
