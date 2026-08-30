package lang

import (
	"bufio"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Value is any QuarkLang runtime value.
type Value interface {
	TypeName() string
	String() string
}

// ---- scalars ----

type IntV int64

func (v IntV) TypeName() string { return "int" }
func (v IntV) String() string   { return strconv.FormatInt(int64(v), 10) }

type FloatV float64

func (v FloatV) TypeName() string { return "float" }
func (v FloatV) String() string   { return strconv.FormatFloat(float64(v), 'f', -1, 64) }

type BoolV bool

func (v BoolV) TypeName() string { return "bool" }
func (v BoolV) String() string {
	if bool(v) {
		return "true"
	}
	return "false"
}

type StrV string

func (v StrV) TypeName() string { return "String" }
func (v StrV) String() string   { return string(v) }

// NilV is the unit value.
type NilV struct{}

func (NilV) TypeName() string { return "nil" }
func (NilV) String() string   { return "nil" }

// ---- rolling List<T> (spec §4) ----

// List is a rolling two-pointer buffer: visible elements live in [head, tail).
// mem/blockID 挂接全局内存管理器（写入时标记所属 block 为脏）。
type List struct {
	items   []Value
	head    int
	tail    int
	mem     *MemoryManager
	blockID int
}

func NewList(items ...Value) *List {
	return &List{items: items, tail: len(items)}
}

func (l *List) TypeName() string { return "List" }

func (l *List) String() string {
	parts := make([]string, 0, l.tail-l.head)
	for i := l.head; i < l.tail; i++ {
		parts = append(parts, l.items[i].String())
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// Head returns the head pointer position.
func (l *List) Head() int { return l.head }

// Tail returns the tail pointer position.
func (l *List) Tail() int { return l.tail }

// Size returns tail-head, the number of visible elements.
func (l *List) Size() int { return l.tail - l.head }

// Peek returns the element at the head without moving the pointer ('*list').
func (l *List) Peek() (Value, error) {
	if l.head == l.tail {
		return nil, fmt.Errorf("ListExhaustedError: list is exhausted (head()==tail()); '*' stops and errors")
	}
	return l.items[l.head], nil
}

// Next returns the head element and rolls the head pointer forward one step.
func (l *List) Next() (Value, error) {
	if l.head == l.tail {
		return nil, fmt.Errorf("ListExhaustedError: list is exhausted (head()==tail()); next() stops and errors")
	}
	v := l.items[l.head]
	l.head++
	return v, nil
}

// Reset moves the head pointer back to 0, making the full history visible again.
func (l *List) Reset() { l.head = 0 }

// Append writes v at the tail and moves the tail forward.
func (l *List) Append(v Value) {
	l.items = append(l.items, v)
	l.tail++
	if l.mem != nil {
		l.mem.MarkDirty(l.blockID)
	}
}

// AppendAll copies every visible element of o onto the tail (o is not consumed).
func (l *List) AppendAll(o *List) {
	for i := o.head; i < o.tail; i++ {
		l.Append(o.items[i])
	}
}

// Get returns the i-th visible element (0-based, relative to head).
func (l *List) Get(i int) (Value, error) {
	idx := l.head + i
	if i < 0 || idx >= l.tail {
		return nil, fmt.Errorf("IndexOutOfBoundsError: index %d out of range [0,%d)", i, l.Size())
	}
	return l.items[idx], nil
}

// copyVisible returns a new List with a copy of the visible range.
func (l *List) copyVisible() *List {
	items := make([]Value, 0, l.Size())
	for i := l.head; i < l.tail; i++ {
		items = append(items, l.items[i])
	}
	return NewList(items...)
}

// sortInPlace sorts the visible range (int/float/String elements only).
func (l *List) sortInPlace() error {
	items := l.items[l.head:l.tail]
	var sortErr error
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		switch av := a.(type) {
		case IntV:
			switch bv := b.(type) {
			case IntV:
				return av < bv
			case FloatV:
				return float64(av) < float64(bv)
			}
		case FloatV:
			switch bv := b.(type) {
			case FloatV:
				return float64(av) < float64(bv)
			case IntV:
				return float64(av) < float64(bv)
			}
		case StrV:
			if bv, ok := b.(StrV); ok {
				return av < bv
			}
		}
		sortErr = fmt.Errorf("TypeError: __sort__ supports int/float/String elements only, got %s and %s", a.TypeName(), b.TypeName())
		return false
	})
	return sortErr
}

// ---- HashTable<K,V> (spec §3.6) ----

type HashTable struct {
	m map[string]Value
}

func NewHashTable() *HashTable { return &HashTable{m: map[string]Value{}} }

func (h *HashTable) TypeName() string { return "HashTable" }

func (h *HashTable) String() string {
	parts := make([]string, 0, len(h.m))
	for k, v := range h.m {
		parts = append(parts, k+" -> "+v.String())
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

// Put stores a deep copy of v under key k.
func (h *HashTable) Put(k, v Value) { h.m[hashKey(k)] = deepCopy(v) }

// Get returns the value stored under k.
func (h *HashTable) Get(k Value) (Value, bool) {
	v, ok := h.m[hashKey(k)]
	return v, ok
}

// Contains reports whether k is present.
func (h *HashTable) Contains(k Value) bool {
	_, ok := h.m[hashKey(k)]
	return ok
}

// Remove deletes the entry for k.
func (h *HashTable) Remove(k Value) { delete(h.m, hashKey(k)) }

// Size returns the number of entries.
func (h *HashTable) Size() int { return len(h.m) }

// hashKey builds a stable structural key for any Value.
func hashKey(v Value) string { return v.TypeName() + ":" + v.String() }

// ---- FuncBuffer (spec §5) ----

// Func is a compiled QuarkLang function. Ret != "" means the call yields the
// value of `return expr;`; otherwise the call yields the whole FuncBuffer.
type Func struct {
	Name   string
	Params []Param
	Ret    string
	Body   *Block
	Pos    Pos
}

// execCtx 是函数执行的内部上下文（v2：语言面不再有 FuncBuffer）。
// 函数执行记录日志（log），结果由 return 直接产生。
type execCtx struct {
	Fn       *Func
	Args     []Value
	Log      *List
	result   Value
	executed bool
	pos      Pos
}

func NewExecCtx(fn *Func, args []Value, pos Pos) *execCtx {
	items := make([]Value, len(args))
	copy(items, args)
	return &execCtx{
		Fn:   fn,
		Args: items,
		Log:  NewList(),
		pos:  pos,
	}
}

// ---- IO objects (spec §10) ----

// IOStream is the default main() parameter: input + output + redirectable.
// mu implements the 执行表 (spec §14.2): FIFO by arrival time, reads (RLock)
// have priority over writes (Lock).
type IOStream struct {
	In  io.Reader
	Out io.Writer
	rd  *bufio.Reader
	mu  sync.RWMutex
}

func (s *IOStream) TypeName() string { return "IOStream" }
func (s *IOStream) String() string   { return "<IOStream>" }

// InputStream is the input base type (File* / Console* subclasses).
type InputStream struct{ R io.Reader }

func (s *InputStream) TypeName() string { return "InputStream" }
func (s *InputStream) String() string   { return "<InputStream>" }

// OutputStream is the output base type.
type OutputStream struct{ W io.Writer }

func (s *OutputStream) TypeName() string { return "OutputStream" }
func (s *OutputStream) String() string   { return "<OutputStream>" }

// MemorizeBuffer is the state of the built-in memorize signature.
type MemorizeBuffer struct{ Table *HashTable }

func (m *MemorizeBuffer) TypeName() string { return "memorize" }
func (m *MemorizeBuffer) String() string   { return "<memorize buffer>" }

// Memory is the default concrete instance of the global memory struct
// (block-managed; spec §14). v0.1: memory is backed by the host Go GC;
// compact() returns nothing (no value) and BlockSize is the dynamic
// block-granularity setting (memory.setBlock(n)).
type Memory struct{ BlockSize int }

func (m *Memory) TypeName() string { return "memory" }
func (m *Memory) String() string   { return "<memory>" }

// globalMemory is the built-in `memory` identifier.
var globalMemory = &Memory{BlockSize: 4096}

// TaskManager is the coroutine manager; taskm is a GLOBAL VARIABLE, so the
// correct call syntax is taskm.spawn(...) / taskm.block(pid) / taskm.done(pid)
// / taskm.merge(pid) / taskm.channel([n]) (spec §14.2).
type TaskManager struct{}

func (t *TaskManager) TypeName() string { return "taskm" }
func (t *TaskManager) String() string   { return "<taskm>" }

// globalTaskm is the built-in `taskm` identifier.
var globalTaskm = &TaskManager{}

// FuncValue is a first-class function reference (for taskm::spawn etc.).
type FuncValue struct{ fn *Func }

func (f *FuncValue) TypeName() string { return "func" }
func (f *FuncValue) String() string   { return "<func " + f.fn.Name + ">" }

// Task 是线程（taskm）的执行上下文：done = 线程是否空闲。
type Task struct {
	ctx     *execCtx
	doneCh  chan struct{}
	err     error
	Pid     int
	BlockID int
	Busy    bool // 是否有函数占用（done 即 !Busy）
}

func (t *Task) TypeName() string { return "Task" }
func (t *Task) String() string   { return "<Task " + itoa(t.Pid) + ">" }

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}

// StructValue is an instance of a user-defined struct.
type StructValue struct {
	SType  string
	Fields map[string]Value
}

func (s *StructValue) TypeName() string { return s.SType }

func (s *StructValue) String() string {
	parts := make([]string, 0, len(s.Fields))
	for k, v := range s.Fields {
		parts = append(parts, k+"="+v.String())
	}
	return "<" + s.SType + " {" + strings.Join(parts, ", ") + "}>"
}

// CopydValue 包装 Copyd<T> 参数值；.ptr() 取出包装的地址。
type CopydValue struct{ V Value }

func (c *CopydValue) TypeName() string { return "Copyd" }
func (c *CopydValue) String() string   { return c.V.String() } // Copyd 透明

// Channel is the coroutine communication primitive (block-buffered).
type Channel struct{ ch chan Value }

// NewChannel 创建容量为 cap 的缓冲 channel。
func NewChannel(cap int) *Channel { return &Channel{ch: make(chan Value, cap)} }

func (c *Channel) TypeName() string { return "Channel" }
func (c *Channel) String() string   { return "<Channel>" }

// ---- deep copy (Copyd semantics; HashTable stores deep copies) ----

func deepCopy(v Value) Value {
	switch t := v.(type) {
	case *List:
		items := make([]Value, len(t.items))
		for i, it := range t.items {
			items[i] = deepCopy(it)
		}
		return &List{items: items, head: t.head, tail: t.tail}
	case *HashTable:
		h := NewHashTable()
		for k, it := range t.m {
			h.m[k] = deepCopy(it)
		}
		return h
	default:
		return v
	}
}
