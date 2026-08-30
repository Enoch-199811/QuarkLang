# QuarkLang 语言设计文档（v0.1 草案）

> 状态：设计草案，随与设计者的讨论迭代；凡标注「待确认」者尚未定稿。
>
> 一句话概括：函数调用产出 **FuncBuffer**（参数 head / 结果 tail / 日志 log），`@` **签名**对调用做第二层包装；**List 是可滚动的双指针缓冲**；语言默认**严格检查**，出错时回放 log 解释原因。

## 0. 术语表

| 术语 | 含义 |
|---|---|
| 签名 (Signature) | `expr @sign(args)` 后缀语法；把一次调用包装成第二层函数，交给实现 Sign 接口的类处理 |
| FuncBuffer | 函数调用的缓冲对象：head（输入参数）、tail（输出结果）、log（执行日志），默认返回整个缓冲区 |
| 滚动 List | 双指针（head/tail）列表；`*` 取开头，`.next()` 变更 head 从头滚到尾；`head()==tail()` 时停止滚动并报错 |
| Sign 接口 | 签名必须实现的接口；默认要求实现 `call` |
| 传址 / Copyd | 语言默认按地址传递；`Copyd<T>` 强制按值复制，`.ptr()` 方法取出 Copyd 包装的地址 |
| 类 / struct | 二者**不区分**；`Local` 开头（如 `LocalMemorize`）是使用方自建类型，不属于官方词典 |
| 严格检查 | 编译期全量类型与语义检查（一律是错误，不是警告）+ 运行期硬错误，一切错误附带可回放的 log |

## 1. 设计目标

1. **函数调用可观测、可追溯**：每次调用的参数、结果、执行过程都留在 FuncBuffer 里；出错时回放 `log` 就能看清「为什么运行错了」。
2. **签名正交于函数本身**：函数只负责计算；缓存、日志等横切关注点由签名包裹，不改函数体。
3. **默认严格、对开发者友好**：宁可编译期报错，不让错误流入运行期；运行期错误必须带上上下文（log 回放）。
4. **小而一致的核心**：List / Array / HashTable / Copyd 组合出大部分数据结构需求。

## 2. 词法与程序结构

顶层结构（EBNF 草案）：

~~~text
program       := { topLevel }
topLevel      := structDecl | implDecl | interfaceDecl | funcDecl | constDecl
structDecl    := "struct" "{" { memberDecl } "}" [ident] ";"
memberDecl    := ident typeName ";"          // 成员格式固定：成员名 成员类型;
implDecl      := "impl" [interfaceName] "{" { funcDecl } "}" typeName ";"
interfaceDecl := "interface" "{" { methodSig } "}" [ident] ";"
funcDecl      := "func" ident "(" [params] ")" block
block         := "{" { stmt } "}"
stmt          := exprStmt | "out" expr ";" | "return" [expr] ";"
               | "log" expr ";"
               | "if" "(" expr ")" block [ "else" block ]
               | "while" "(" expr ")" block
               | "for" "(" ident ":" expr ")" block   // 语法糖：等价 while + next()
               | declStmt
expr          := literal | ident | member | index | listLit
               | unary | binary | call
call          := expr "(" [args] ")" [ "@" signName "(" [signArgs] ")" ]
member        := expr "." ident
unary         := "*" expr | "-" expr | "!" expr
~~~

说明：
- 语句以 `;` 结尾；成员声明固定为「成员名 成员类型;」。
- 变量声明固定为「变量名 类型 [= 初值];」——**名字在前、类型在后**（如 `l List<int> = [1,2];`）。
- 函数体写法与顶层一致，只是多了一层作用域（见 §7）。
- 列表字面量：`[1, 2, 3]`。

## 3. 类型系统

### 3.1 基础类型
数值类型**基本与 C 一致**：

| 类型 | 宽度 | 说明 |
|---|---|---|
| `int` | 32 位有符号 | 与 C `int` 一致；溢出**环绕** |
| `long` | 64 位有符号 | 与 C `long long` 一致；溢出环绕 |
| `float` | 32 位 IEEE 754 | 与 C `float` 一致 |
| `double` | 64 位 IEEE 754 | 与 C `double` 一致 |
| `char` | 8 位 | 单字符 |
| `bool` | 1 字节 | `true` / `false` |
| `String` | — | 字符串 |
| `void` | — | 无值 |

（解释器状态：int 已按 32 位环绕实现，越界 int 字面量是编译错误；long/float/double/char 在类型系统轮落地。）

### 3.2 空接口 interface{}
`interface{}` 是一切类型的父类型（空接口），任何值都可装进去；取出需**显式类型断言**（严格检查要求显式；断言失败是运行时错误）。

### 3.3 List<T>
滚动列表（§4 详述）。所有列表默认都是 List<T>。

### 3.4 Array<T> 与 T[]
`<type>[]` 是 `Array<type>` 的简便写法。Array 是定长、随机访问的序列（与滚动 List 不同）。内置方法（如 `__sort__()` 原地排序）挂在 Array 接口上。
`a int[]` 等价于 `a Array<int>`。

### 3.5 Copyd<T> 与 T[Copyd]
- 语言**默认按地址传递**（引用语义）。
- `Copyd<T>` 标记：该值每次传递（赋值、传参、返回）都会被复制一份。
- `<type>[Copyd]` 是 `Copyd<Array<type>>` 的简便写法。
- `Copyd<T>` 提供 **`.ptr()` 方法**：取出 Copyd 对象包装的地址（默认传址下，只有 Copyd<> 因传递时自动复制，才需要这个显式取地址的出口）。

### 3.6 HashTable<K, V>
键值表，键、值类型参数化（如 `HashTable<List<interface{}>, List<interface{}>>`）。内置 `contains/get/put/remove/size`。

### 3.7 FuncBuffer
函数调用的缓冲（§5 详述）。成员：
- `head List<interface{}>`：输入参数（按实参顺序）。
- `tail List<interface{}>`：输出结果（按 out 出现顺序）。
- `log List<String>`：执行日志（滚动列表，可回放）。

调用表达式的值**默认是整个缓冲区**（所以可写 `fb.head` / `fb.tail` / `fb.log`，或 `*fb.tail` 取结果）。

## 4. 滚动 List<T>（★ 核心）

List 是**双指针滚动缓冲**：内部有 `head` 与 `tail` 两个位置指针，可见元素位于 `[head, tail)` 区间；已消费的空间滚动复用。

| 写法 | 语义 |
|---|---|
| `l.head()` | 返回 head 指针位置 |
| `l.tail()` | 返回 tail 指针位置 |
| `l.head() == l.tail()` | 可见区间为空（「耗尽」/空表） |
| `*l` | 取 head 处的元素（只看，不移动指针） |
| `l.next()` | 返回 head 处元素，并把 head 后移一位（滚动） |
| `l.append(x)` | 在 tail 处写入 x，tail 后移 |
| `l.size()` | 返回 tail - head（可见元素个数） |

严格规则（§11 的一部分）：
- **结束语义（精确）**：`next()` 的迭代**直到 head 滚到 tail（head==tail）才结束**——最后一个元素也会被返回（自然结束点 = 滚到 head==tail）；**不是**在 head==tail 时无声结束——已耗尽（head==tail）后再调用 `next()`（或 `*`）→ **停止滚动并报错**（`ListExhaustedError`，附带当前 FuncBuffer 的 log 回放）。
- 遍历惯用法：`while (l.head() != l.tail()) { x = l.next(); ... }` —— 从头看到尾，每个元素恰好取一次。
- 执行日志 `log` 本身也是滚动 List：诊断回放即对其从头到尾 `next()`。
- `*` 只作用于 List；FuncBuffer 不是 List，**不能**施加 `*`（取结果请用 `*fb.tail`）。
- 遍历也可用语法糖：`for (x : list) { ... }`，等价于 while + next()。

**回卷与内存回收（已确认）**：
- `l.reset()`：**head 直接移回 0**、tail 不变，使全部历史元素（含已消费的）重新可见——用于「log 看完一遍还想再看」。
- 容量策略采用「**扩容不覆盖**」：内部数组不足时自动翻倍扩容，已消费空间保留、不被新数据覆盖，保证 reset() 总能回放完整历史。
- 空间回收交给**全局内存管理器**（§14）：内存以 **block（区块）** 为单位管理；区块被更改会记录下来（脏标记），`GlobalMemory.compact()` 依据这些记录清理**无人占用的空间**。
- `memory` 是全局内存 struct 的**默认具体实现**；协程内存同样以 block 为单位、流程一致，协程由协程管理器统一调度，其程序接口为 `taskm`。

示例：

~~~quark
l List<int> = [10, 20, 30];
*l;                        // 10  —— * 取开头（只读）
l.next();                  // 10  —— 取出并滚动
*l;                        // 20
l.next();                  // 20
l.next();                  // 30
l.head() == l.tail();      // true —— 已耗尽
l.next();                  // 错误！ListExhaustedError（停止滚动并报错）
~~~

## 5. FuncBuffer 与执行模型（★ 核心）

~~~quark
struct {
    head List<interface{}>;   // 输入参数（滚动列表）
    tail List<interface{}>;   // 输出结果（滚动列表）
    log  List<String>;        // 执行日志（滚动列表）
} FuncBuffer;
~~~

求值规则：
1. `f(args)` 先构造 FuncBuffer：`head` 依次装入实参值；`tail`、`log` 初始为空。
2. **不带签名**：立即执行函数体（execute）；每个 `out expr;` 把 expr 的值追加进 `tail`。**log 不会自动写入**——只能由程序手动调用（见下）。
3. **带签名** `f(args) @sign(p)`：**不立即执行**；整体改写为 `sign::call(p)(fb)`，把未执行的 fb 交给签名（第二层包装）。签名决定何时 `fb.execute()`（例如 memorize 缓存命中就不必再执行函数体）。最终效果仍是「执行」，只是执行时机由签名决定。
4. `fb.execute()` 为显式触发执行的方法。

log 就是 List<String>，且**只接受手动写入**（解释器不自动记录、不自动输出）：
- 语句 `log expr;`：把 expr 的字符串形式追加到**当前 FuncBuffer** 的 log（手动调用）；
- 也可以对 FuncBuffer 变量直接用 `变量.log.append("...")`（log 本身是滚动 List）——注意 `fb` 只是示例中的**变量名，不是语法**；函数体内并不能直接引用自己的 FuncBuffer（除非它是参数）。
- 出错时（§11.2）运行时回放 log 中已有的（手动写入的）内容，帮助定位「为什么运行错了」。

## 6. 签名与 Sign 接口（★ 核心）

### 6.1 语法与语义

~~~quark
fb FuncBuffer = <fc_name>(<param>) @memorize(mb);   // 执行（声明：名字在前、类型在后）
~~~

语义分两层：
1. 第一层：`<fc_name>(<param>)` 产出 FuncBuffer（head=参数，未执行）。
2. 第二层：签名 `@memorize(mb)` 包装调用 → 执行 `memorize::call(<Prefix>)(<FuncBuffer>)`，**返回结果作为下一个 fb**；与此同时由签名完成横切操作（记忆化）。

- 一般形式：`@<sign>(<Prefix>)`；**`<Prefix>` 就是 @ 处传入的参数**，原样传给 `Sign::call` 作为第一个参数。
- 签名名对应的类型必须实现 `Sign` 接口，否则编译期报错（严格检查）。
- 签名的 `call` 通过 `::` 调用（无 self）。
- 签名返回值可以是 FuncBuffer 的**扩展类型**：如内置签名 `@async()` 返回 **Task**（是否完成 + 完整函数，§14）——本质上仍是「下一个 fb」。

### 6.2 Sign 接口

~~~quark
interface {
    func call(prefix P, fb FuncBuffer) FuncBuffer;
} Sign<P>;
~~~

- `call` 是 Sign 的**默认要求**：实现 Sign 必须提供 call；`P` 为 <Prefix> 的类型参数。
- `new()` 是 memorize 独有的静态工厂，**不是** Sign 接口要求——每个 Sign 并不都需要实现 new。

### 6.3 memorize 参考实现

~~~quark
struct {
    pairsParamToResult HashTable<List<interface{}>, List<interface{}>>;
} MemorizeBuffer;

impl Sign {
    // Sign 接口要求的 call；prefix 即 @memorize(mb) 处传进来的 mb
    func call(prefix MemorizeBuffer, fb FuncBuffer) FuncBuffer {
        key List<interface{}> = fb.head;   // 缓存键 = 参数列表（缓存存储 head → tail 映射）
        if (prefix.pairsParamToResult.contains(key)) {
            fb.tail.appendAll(prefix.pairsParamToResult.get(key));
            fb.log.append("memorize: cache hit " + key.toString());
        } else {
            fb.execute();                    // 真正执行被包装函数（填充 tail 与 log）
            prefix.pairsParamToResult.put(key, fb.tail);
            fb.log.append("memorize: cache miss, stored " + key.toString());
        }
        return fb;
    }

    // memorize 独有的工厂方法，不属于 Sign 接口
    func new() MemorizeBuffer {
        MemorizeBuffer m;
        m.pairsParamToResult = HashTable::new();
        return m;
    }
} MemorizeBuffer;
~~~

使用：

~~~quark
mb MemorizeBuffer = memorize::new();
fb FuncBuffer  = expensive(41) @memorize(mb);
* fb.tail;            // 结果
~~~

- memorize 的缓存是「head → tail」映射：键为参数列表（fb.head），值为结果列表（fb.tail）。
- `LocalMemorize` 只是使用方自建的结构体名（Local 前缀约定）；官方词典中的名字是 `memorize`。

## 7. struct / impl / interface

### 7.1 struct

~~~quark
struct {
    x int;
    y List<String>;
} Point;
~~~

- 每个 struct 里都可以写成员；成员格式固定为「成员名 成员类型;」。
- 结尾跟名称；无名称则是匿名结构体。
- 方法不写在 struct 里，通过 impl 实现（见下）。

### 7.2 impl

~~~quark
impl Sign {
    // 函数写法与外面一致，只是多了层作用域
} LocalMemorize;
~~~

- `impl <接口> { ... } <类型>;` 声明「类型实现接口」；编译器**严格检查**接口方法全部实现（缺 call → 编译错误）。
- 方法可选 `self` 参数：
  - 无 self → 只能 `::` 调用（如 `new()`）。
  - 有 self → 用 `.` 调用（如 `io.println(...)`）。
- `impl { ... } <类型>;` 是无接口的自实现：仅添加方法，不声明实现任何接口（因此不能当作 Sign 使用——自然不支持签名）。

### 7.3 interface

~~~quark
interface {
    func call(prefix P, fb FuncBuffer) FuncBuffer;
} Sign<P>;
~~~

- 写法与 struct 相同；结尾跟接口名。
- `interface { } none;` 中的 `none` 只是表示「无名称/不存在」的占位说法——**官方词典里没有 `none` 这个关键字**：匿名接口直接不写名字即可。

## 8. 函数

~~~quark
func LocalSorted(n int, a int[Copyd]) {
    a.__sort__();   // Array 接口的内置方法：原地排序（作用在副本 a 上）
    out n;          // 追加 n 至 tail
    out a;          // 追加 a 至 tail
}
~~~

- 多返回值用 `out expr;`，按出现顺序追加进 FuncBuffer.tail。
- `func main(io IOStream) { io.println("Hello World!"); }` —— main 由运行时注入参数，**顺序固定**为
  `io env(HashTable<String,String>) args(List<String>)`：
  - `io`：IOStream 实例（默认绑定控制台）——**不可省略**（显示数据流的必要工作）；
  - `env`：系统环境变量表；`args`：命令行参数列表；用不到就不声明（不声明即不可用），但必须按顺序取前若干个。

结果消费：

~~~quark
fb FuncBuffer = LocalSorted(3, [1, 3, 2]);
* fb.tail;          // 3        —— * 取结果列表首位（只读）
fb.tail.next();     // 3        —— 滚动取走
fb.tail.next();     // [1,2,3]  —— 排序后的副本（调用方的原数组未被修改，因为 Copyd）
~~~

## 9. 内置方法

- `Array.__sort__()`：原地排序（Array 接口方法）。
- `List`：`head()/tail()/next()/append/appendAll/size/toString`。
- `HashTable`：`contains/get/put/remove/size`。
- `List.reset()`：head 直接移回 0（§4）。
- `GlobalMemory::compact()` / `memory.compact()`：清理无人占用的空间（block 管理，§14）——**不返回任何值**。
- `GlobalMemory::setBlock(n)` / `memory.setBlock(n)`：动态调整 block 脏标记粒度（区块大小）。
- `taskm`（全局变量）：`taskm.spawn(fn, args...)` → pid、`taskm.block(pid)` / `taskm.merge(pid)` → FuncBuffer、`taskm.done(pid)` → bool、`taskm.channel([n])` → Channel（默认容量 1024）。
- （其余内置方法随解释器实现逐步补充。）

## 10. IO 体系

类层次：

~~~text
IOStream         —— 同时含输入输出；main 默认获得（io）
├─ InputStream   —— 输入基类
│   ├─ FileInputStream(path)   —— 文件输入
│   └─ ConsoleInputStream()    —— 控制台输入（无参，固定返回控制台流）
└─ OutputStream  —— 输出基类
    ├─ FileOutputStream(path)  —— 文件输出
    └─ ConsoleOutputStream()   —— 控制台输出（无参，固定返回控制台流）
~~~

重定向（两种等价形式）：

~~~quark
IO::setIn(io, "input.txt");                 // 包级静态：直接给路径，内部构造 File 流
io.setIn(FileInputStream("input.txt"));     // 实例方法：给流对象
IO::setOut(io, "output.txt");
io.setOut(FileOutputStream("output.txt"));
~~~

- `io.println(...)` 等输出方法写在 IOStream 上。
- File 系列、Console 系列都是 InputStream/OutputStream 的子类；Console 系列无需参数，固定返回控制台。

## 11. 严格检查与错误诊断（★ 核心）

### 11.1 编译期（一律是错误，不是警告）
1. 未声明标识符 / 未声明成员访问。
2. 类型不匹配（含泛型实参 List<T>、HashTable<K,V>、Copyd<T>）。
3. `impl Sign` 未提供 `call`（或签名不符）→ 编译错误。
4. `@` 引用的签名类型未实现 Sign → 编译错误。
5. `out` 出现在函数体之外。
6. 变量使用前未初始化。
7. 对非 List 类型调用 `next()/head()/tail()` 或施加 `*`。
8. （可选）函数存在没有任何 out/return 的路径。

### 11.2 运行期（硬错误 + log 回放）
1. `ListExhaustedError`：耗尽（`head()==tail()`）后调用 `next()` 或 `*`——**停止滚动并报错**。
2. 除零、索引越界、类型断言失败、IO 失败（文件不存在等）。
3. 每个运行时错误输出：错误类型 + 源码位置 + **当前 FuncBuffer.log 从头到尾的回放**，直观展示「为什么运行错了」。

诊断输出示例：

~~~text
error: ListExhaustedError at list.qk:4 (next() on exhausted list)
---- execution log ----
about to consume
...
~~~

（回放内容 = 程序**手动写入** log 的内容；解释器不自动追加。回放由运行时复制 log 后滚动打印，**不消耗**原 log。）

## 12. 示例程序

### 12.1 Hello World

~~~quark
func main(io IOStream) {
    io.println("Hello World!");
}
~~~

### 12.2 LocalSorted（见 §8）

### 12.3 memorize + log 诊断

~~~quark
func expensive(n int) {
    log "running expensive: " + n;      // 手动写入 log
    out n * n;
}

func main(io IOStream) {
    mb MemorizeBuffer = memorize::new();
    fb FuncBuffer  = expensive(41) @memorize(mb);
    io.println(* fb.tail);               // 1681（首次：执行并缓存）
    fb2 FuncBuffer = expensive(41) @memorize(mb);
    io.println(* fb2.tail);              // 1681（缓存命中，函数体未再执行）
    // 手动写入的 log 可以这样回放（从头滚到尾）：
    while (fb.log.head() != fb.log.tail()) {
        io.println(fb.log.next());       // running expensive: 41
    }
    // fb2.log 为空：命中缓存时函数体未执行，自然没有 log 记录
}
~~~

## 13. 开放问题与已确认决议

已确认（来自设计者答复，已合入上文对应章节）：
1. 「类」与「结构体」**不区分**；`Local` 开头（如 `LocalMemorize`）是使用方自建类型，不属于官方词典；官方词典中的名字是 `memorize`。
2. memorize 缓存是「head → tail」映射：键为参数列表（fb.head），值为结果列表（fb.tail）。
3. 取地址统一为 `Copyd.ptr()` 方法（替代 `ptr Copyd` 语法）：取出 Copyd 对象包装的地址（默认传址，只有 Copyd<> 因传递时自动复制才需要这个出口）。
4. `*` 无法作用于 FuncBuffer——FuncBuffer 不是 List。
5. `main` 的 io 参数**不可省略**（显示数据流的必要工作）；参数顺序固定 `io env(HashTable<String,String>) args(List<String>)`，用不到的后两个参数可以不声明。
6. 提供 `for (x : list)` 语法糖（等价 while + next()）。
7. log 就是 List<String>。
8. 编译器**不做** next() 可能耗尽的静态告警。

已确认（2026-08-29 设计者重规划，已合入 §4/§14）：
- `reset()` 直接移到 0（head=0，tail 不变）；容量「扩容不覆盖」。
- 内存以 **block（区块）** 为单位管理；区块被更改会记录（脏标记）；`GlobalMemory.compact()` 依据记录清理无人占用的空间；`memory` 是全局内存 struct 的默认具体实现。
- 协程：有独立的协程内存（同样以 block 为单位、流程一致）；协程拥有自己的执行上下文（thread）；`taskm` 是协程管理器的程序接口。

已确认（2026-08-29 协程系统设计者答复，已合入 §14）：
- 启动：`taskm::spawn(fn, args...)`；或 `f(args) @async()` 签名（本质同样是包装）；**协程函数就是普通函数**。
- 结果：@async/spawn 返回 **Task**（是否完成 + 完整函数）；`taskm::block(t)` 等待并取回完整 FuncBuffer；**结果必须执行完才能拿**。
- 变量：协程函数内声明的变量**自动归协程**（协程独立 block 内存）。
- 通信：`taskm::channel()` + `ch.send(v)` / `ch.recv()`。
- 调度：协作式；协作函数会**自动日志**（调度事件），业务日志仍手动。
- io：协程直接传递 io；IOStream 内置**执行表**，执行时间冲突的操作先入表排队。
- 回收：协程结束自动标记其 block 可回收；`GlobalMemory.compact()` 手动清理。

已确认（2026-08-30 设计者答复，已合入 §5/§9/§14）：
1. **没有 yield 语法**（此前为我方臆造，已从语言与实现中删除）；协作等待原语只有 `taskm.block`。
2. channel 支持指定容量：`taskm.channel(n)`，**默认 1024**。
3. IOStream 执行表：按到达时间**先进先出**，**优先级读高于写**（v0.1 以读写锁近似实现）。
4. 调度事件：**done** = 是否结束（`taskm.done(pid)` = 该协程线程是否没有函数占用）；**spawn() 返回 pid**；**merge() 把函数并入线程**（v0.1 等价 block）；**taskm 是全局变量**，正确语法是 `taskm.函数()`（不是 `taskm::`）。
5. `compact()` **根本不返回**；block 脏标记粒度可用 `memory.setBlock(n)` / `GlobalMemory::setBlock(n)` 动态调整。

（无待确认项。）

已确认（本轮新增，已合入上文）：
- 数值宽度基本与 C 一致（§3.1 类型表；int=32 位环绕、long=64、float=32、double=64、char=8）。
- log **只接受手动写入**：`log expr;` 语句 / `fb.log.append(...)`；解释器不自动记录、不自动输出（§5、§11.2 已更新）。(fb不是语法，是名字，函数内压根不能直接用)

## 14. 内存模型与协程

### 14.1 内存模型（block 管理）
- 内存由**全局内存管理器**统一管理，以 **block（区块）** 为基本单位。
- 区块被更改（写入）时会被记录（脏标记）；`GlobalMemory.compact()` 依据这些记录清理**没有人占用的空间**（无引用区块）——**compact() 不返回任何值**。
- block 脏标记粒度可动态调整：`memory.setBlock(n)` / `GlobalMemory::setBlock(n)`。
- `memory` 是全局内存 struct 的**默认具体实现**（全局实例）。
- 协程内存同样以 block 为单位、流程一致。

### 14.2 协程（协作函数）
- **协程函数就是普通函数**（「函数没有那么特殊」）：没有特殊声明，任何 func 都能作为协程体运行。
- **taskm 是全局变量**：调用语法是 `taskm.函数()`（**不是** `taskm::`）。
- **启动两种方式**：
  1. `taskm.spawn(fn, args...)` —— 显式启动（fn 为函数引用，如 `taskm.spawn(expensive, 41)`），**返回 pid**（协程标识）；
  2. `f(args) @async()` —— 内置**签名** async（与 @memorize 一样本质是包装），直接异步运行，返回 **Task** =「是否完成 + 完整的函数」（Task 是 FuncBuffer 的扩展：head/tail/log 齐全，`t.done()` → bool）。
- **拿结果（必须执行完才能拿）**：
  - `taskm.block(pid)` → 等待执行完成，返回该协程的完整 FuncBuffer；
  - `taskm.merge(pid)` → **把协程函数并入当前线程**并汇合其结果（v0.1 等价于 block）；
  - `taskm.done(pid)` → bool：该协程的**线程是否空闲（没有函数占用）**，v0.1 即协程是否结束。
- **协程变量**：协程函数内声明的变量**自动归协程**（协程独立 block 内存）；全局变量仍共享。
- **调度**：协作式（**没有 yield 语法**）。切换点：channel send/recv 阻塞、`taskm.block` / `taskm.merge` 等待。
- **自动日志**：调度事件（spawn / done）由运行时**自动写入**协程自己的 log（spawn 事件带 pid；业务日志仍需手动 `log` 语句）。
- **通信**：`taskm.channel([n])` 创建 channel（**默认容量 1024**，内部为 block 缓冲），`ch.send(v)` / `ch.recv()`；不可完成时让出（协作式切换点）。
- **io**：协程直接传递 io；IOStream 内置**执行表**：按到达时间**先进先出**，**优先级读高于写**（v0.1 以读写锁近似）。
- **回收**：协程结束自动标记其 block 可回收；`GlobalMemory.compact()` 手动清理。

解释器状态（v0.1）：内存由宿主 Go GC 管理（`compact()` 无返回值、为兼容性入口，`memory.setBlock(n)` 存字段）；协程以 Go goroutine 实现——`taskm.spawn→pid / block / merge / done(pid) / channel([n] 默认 1024)`、`@async()`、Task（done + 完整 FuncBuffer）、IOStream 执行表（读写锁：FIFO、读优先）、调度事件自动日志（spawn 带 pid / done）已落地；无 yield 语法；block 级脏标记与真实回收待内存系统轮。

## 15. 实现路线（Go 解释器）

模块划分（按此顺序实现）：

1. `lexer` —— 词法（关键字：func/struct/impl/interface/out/ptr/if/while/return 等）。
2. `parser` —— 按 §2 EBNF 生成语法树。
3. `typecheck` —— §11.1 全部严格检查 + impl/接口一致性检查。
4. `eval` —— 求值器：List 滚动语义（双指针、耗尽报错）、FuncBuffer（head/tail/log、execute）、签名改写与 `Sign::call` 调用。
5. `runtime` —— 内置类型与接口（List/Array/HashTable/Copyd、`__sort__`）、IO 体系（§10）、错误类型与 log 回放诊断。
6. `examples/` —— §12 全部示例 + 回归测试（含耗尽报错与 log 回放的断言）。

实现状态（2026-08-29，v0.1 解释器）：
- ✅ lexer / parser / eval / runtime：滚动 List（`*`、`next()`、耗尽报错）、FuncBuffer（head/tail/log）、@签名 + 内置 memorize（head→tail 缓存）、out 多返回值、main(io/env/args)、IO 重定向与 Console/File 流、`for (x : list)`、`__sort__`、严格运行时错误 + log 回放。
- ✅ 验证：`go test` 35 项全绿（含 `-race` 数据竞争检测）；`examples/` 8 个示例全部正确运行（含 error.qk 的 ListExhaustedError + log 回放、struct.qk 的用户自定义 Sign）。
- ✅ 2026-08-29：log 仅手动写入（`log expr;` / `fb.log.append(...)`）；int 32 位环绕 + 越界字面量编译错误；`List.reset()`（head→0）。
- ⏳ 待实现：Copyd<T> 运行时包装与 `.ptr()`；block 级脏标记与真实内存回收（§14 内存系统轮）。
- ✅ 2026-08-30 协程系统修订：taskm 为**全局变量**（`taskm.spawn(...)`→pid、`taskm.block(pid)`/`taskm.merge(pid)`→FuncBuffer、`taskm.done(pid)`→bool、`taskm.channel([n])` 默认容量 1024）；**删除 yield 语法**；IOStream 执行表改为 FIFO + 读优先（读写锁）；`compact()` 无返回值、`memory.setBlock(n)`/`GlobalMemory::setBlock(n)` 动态调整 block 粒度；自动日志（spawn 带 pid / done）。
- ✅ 2026-08-29 编译期静态类型检查 pass：类型推断 + §11.1 检查前置到编译期（未声明标识符/成员、类型不匹配、签名注册与 Prefix 类型、调用实参个数与类型、使用前未初始化、非 List 施加 `*`/next 等、main 参数个数），错误一律带行号；Array/Copyd 静态归一化为 List（复制语义由运行时按注解处理）。
- ✅ 2026-08-29 用户自定义 struct/impl/interface：结构体（成员、零值实例、字段读写）、interface（方法签名 + 泛型参数）、impl（self 实例方法用 `.` 调用、无 self 静态方法用 `::` 调用）、`impl Sign` 接口一致性检查（缺 call/参数个数不符 → 编译错误）、带返回类型注解的函数（`func f(...) T` 直接返回 `return` 的值，无注解则返回 FuncBuffer）、用户类型实现 Sign 可作为自定义签名（`f(args) @LocalMemorize(mb)`，spec §6.3 参考实现可作为用户代码运行）。