package lang

import (
	"bytes"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// ============ 性能基准（高计算场景） ============

// 算术密集循环：1e6 次循环
const loopSrc = `func main(io IOStream) {
    n int = 0;
    i int = 0;
    while (i < 1000000) {
        n = n + i * 2 - 1;
        i = i + 1;
    }
    io.println(n);
}
`

// 递归计算：fib(24)
const fibSrc = `func fib(n int) int {
    if (n < 2) {
        return n;
    }
    return fib(n - 1) + fib(n - 2);
}

func main(io IOStream) {
    io.println(fib(24));
}
`

// 密集函数调用 + List 操作
const callSrc = `func sq(n int) int {
    return n * n;
}

func main(io IOStream) {
    total int = 0;
    i int = 0;
    while (i < 100000) {
        total = total + sq(i);
        i = i + 1;
    }
    io.println(total);
}
`

func benchRun(b *testing.B, src string) {
	prog, err := Compile(src)
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var out bytes.Buffer
		if err := Run(prog, "bench.qk", nil, strings.NewReader(""), &out); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCompile(b *testing.B) {
	for i := 0; i < b.N; i++ {
		src := fibSrc + "\n// " + strconv.Itoa(i)
		if _, err := Compile(src); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEvalLoop1M(b *testing.B)    { benchRun(b, loopSrc) }
func BenchmarkFib24(b *testing.B)         { benchRun(b, fibSrc) }
func BenchmarkFuncCalls100K(b *testing.B) { benchRun(b, callSrc) }

// ============ 临时数据潮汐场景（大量数据快速创建又丢弃） ============
// QuarkLang 内存管理器：block 线性复用 + delete 即时归队，无 GC 停顿、无碎片。

func BenchmarkMemManagerChurn(b *testing.B) {
	m := NewMemoryManager()
	ids := make([]int, 0, 64)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ids = ids[:0]
		for j := 0; j < 100; j++ {
			ids = append(ids, m.Alloc(64, 0))
		}
		for _, id := range ids {
			m.Delete(id)
		}
	}
	b.StopTimer()
	// 复用率：绝大多数分配应命中复用（新 block 只申请一次）
	b.ReportMetric(float64(m.ReusedCount)/float64(m.AllocCalls), "reuse-rate")
	b.ReportMetric(float64(m.NewBlocks), "new-blocks")
}

// 对照：Go 原生 slice 分配 + GC（同潮汐规模）
// Go 对照：大对象潮汐（强制堆分配，制造真实 GC 压力）
var goSink [][]interface{}

func BenchmarkGoSliceChurn(b *testing.B) {
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	gcBefore := stats.NumGC
	for i := 0; i < b.N; i++ {
		for j := 0; j < 100; j++ {
			s := make([]interface{}, 64)
			for k := range s {
				s[k] = k
			}
			goSink = append(goSink, s)
			if len(goSink) > 1000 {
				goSink = goSink[:0]
			} // 潮汐：批量丢弃
		}
	}
	b.StopTimer()
	runtime.ReadMemStats(&stats)
	b.ReportMetric(float64(stats.NumGC-gcBefore)/float64(b.N), "gc-count")
}

// 碎片率：混合大小潮汐后 block 内部碎片率（应趋近 0：占用度最小优先复用）
func BenchmarkFragmentationAfterChurn(b *testing.B) {
	m := NewMemoryManager()
	sizes := []int{8, 16, 32, 64}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var ids []int
		for j := 0; j < 1000; j++ {
			ids = append(ids, m.Alloc(sizes[j%len(sizes)], 0))
		}
		// 混合存活：一半 delete 入空闲队列，一半保留（测真实内部碎片）
		for j := 500; j < 1000; j++ {
			m.Delete(ids[j])
		}
	}
	b.StopTimer()
	b.ReportMetric(m.Fragmentation(), "fragmentation")
}

// Go 对照（大样本触发 GC）
func BenchmarkGoSliceChurnGC(b *testing.B) {
	var sink [][]int
	for i := 0; i < b.N; i++ {
		for j := 0; j < 100000; j++ {
			sink = append(sink, make([]int, 64))
			if len(sink) > 10000 {
				sink = sink[:0]
			}
		}
	}
}
