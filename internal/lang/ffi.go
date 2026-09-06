//go:build (linux || darwin) && cgo

package lang

/*
#cgo linux LDFLAGS: -ldl -lffi
#cgo darwin LDFLAGS: -ldl -lffi
#cgo windows LDFLAGS: -lffi
#include <stdlib.h>
#include <stdint.h>
void* qk_dlopen(const char* n);
void* qk_dlsym(void* h, const char* s);
const char* qk_dlerror(void);
int qk_ffi(void* fn, const int* types, const double* nums, void** ptrs,
           int n, int rettype, int64_t* ret_i, double* ret_f, void* ret_p_slot);
*/
import "C"

import (
	"errors"
	"strings"
	"sync"
	"unsafe"
)

// FFI 类型码（与 ffi_shim.c 一致）。
const (
	ffiVoid = 0
	ffiInt  = 1 // int32
	ffiF32  = 2 // float（单精度）
	ffiF64  = 3 // double
	ffiPtr  = 4 // 指针 / 字符串
	ffiBool = 5
	ffiLong = 6 // int64
)

// resolve 查找导出符号。
func (lh *libHandle) resolve(sym string) (unsafe.Pointer, error) {
	// 符号查找单线程化（dlsym 自带锁，但保持一致性）
	cname := C.CString(sym)
	defer C.free(unsafe.Pointer(cname))
	fn := C.qk_dlsym(lh.h, cname)
	if fn == nil {
		return nil, errors.New("FFISymbolError: symbol not found: " + sym)
	}
	return fn, nil
}

// ffiCall 按声明签名调用导入函数（跨系统：POSIX/Windows 均经 libffi 走 FFI_DEFAULT_ABI）。
func ffiCall(fn unsafe.Pointer, paramTypes []int, nums []float64, ptrs []unsafe.Pointer, retType int) (int64, float64, unsafe.Pointer, error) {
	n := len(paramTypes)
	if n > 32 {
		return 0, 0, nil, errors.New("FFIError: too many args (>32)")
	}
	var (
		retI     int64
		retF     float64
		retPSlot [1]uintptr // 指针返回槽（8 字节，C 侧写入）
	)
	ctypes := make([]C.int, n)
	cnums := make([]C.double, n)
	cptrs := make([]unsafe.Pointer, n)
	for i := 0; i < n; i++ {
		ctypes[i] = C.int(paramTypes[i])
		cnums[i] = C.double(nums[i])
		cptrs[i] = ptrs[i]
	}
	var t0 [1]C.int
	var d0 [1]C.double
	var pp0 [1]unsafe.Pointer
	if len(ctypes) == 0 {
		ctypes = t0[:]
		cnums = d0[:]
		cptrs = pp0[:]
	}
	rc := C.qk_ffi(fn, &ctypes[0], &cnums[0], &cptrs[0], C.int(n), C.int(retType),
		(*C.int64_t)(unsafe.Pointer(&retI)), (*C.double)(unsafe.Pointer(&retF)),
		unsafe.Pointer(&retPSlot[0]))
	if rc != 0 {
		return 0, 0, nil, errors.New("FFIError: ffi call failed rc=" + itoa(int(rc)))
	}
	return retI, retF, unsafe.Pointer(retPSlot[0]), nil
}

// ffiTypeOf 把语言类型串映射为 FFI 类型码（int/long/char/bool→i32 或 i64 由声明；f32 专用单精度）。
func ffiTypeOf(typ string) int {
	switch {
	case typ == "f32":
		return ffiF32
	case typ == "float" || typ == "double":
		return ffiF64
	case typ == "int" || typ == "char" || typ == "bool":
		return ffiInt
	case typ == "long":
		return ffiLong
	default:
		return ffiPtr // String / 指针 / 其它
	}
}
