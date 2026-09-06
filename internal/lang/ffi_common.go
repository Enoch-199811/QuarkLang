package lang

import (
	"strings"
	"sync"
	"unsafe"
)

// libObj 是运行时库绑定对象（跨系统共用）：库名 + 句柄（懒加载）+ 导出符号签名表。
type libObj struct {
	name    string
	lib     string // 系统库名（dlopen："libGL.so.1" / "opengl32"）
	handle  *libHandle
	methods map[string]*Func
}

// libHandle 是运行时加载的系统库句柄（跨系统：dlopen/LoadLibrary）。
// libHandle 是运行时加载的系统库句柄（跨系统：dlopen/LoadLibrary）。
type libHandle struct {
	h   unsafe.Pointer
	mu  sync.Mutex
	err string
}

// dlopenLib 加载系统库（跨系统库名解析：原名 → libX.so.6/.so/.dylib/.dll）。
func dlopenLib(name string) (*libHandle, error) {
	cands := []string{name}
	if !strings.ContainsAny(name, "/.") && !strings.HasPrefix(name, "lib") &&
		!strings.HasSuffix(name, ".dll") && !strings.HasSuffix(name, ".dylib") {
		// 裸短名：按平台补位尝试
		cands = append(cands,
			"lib"+name+".so.6", "lib"+name+".so", "lib"+name+".dylib", name+".dll",
			"lib"+name+".so.1", "lib"+name+".so.0")
	}
	var lastErr string
	for _, c := range cands {
		cc := C.CString(c)
		h := C.qk_dlopen(cc)
		C.free(unsafe.Pointer(cc))
		if h != nil {
			return &libHandle{h: h}, nil
		}
		if msg := C.GoString(C.qk_dlerror()); msg != "" && msg != lastErr {
			lastErr = msg
		}
	}
	if lastErr == "" {
		lastErr = "library not found"
	}
	return nil, errors.New("FFILibraryError: cannot load " + name + " (" + lastErr + ")")
}
