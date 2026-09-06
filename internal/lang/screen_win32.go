//go:build windows

package lang

/*
#cgo windows LDFLAGS: -lgdi32 -luser32
#include <windows.h>
// 简单回调
LRESULT CALLBACK qk_wndproc(HWND h, UINT m, WPARAM w, LPARAM l) {
	return DefWindowProcA(h, m, w, l);
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// Win32 GDI 屏显：CreateWindowA + DIB 截面 + SetDIBitsToDevice + UpdateWindow。

var (
	g_hwnd   C.HWND
	g_dc     C.HDC
	g_dib    C.HBITMAP
	g_bits   unsafe.Pointer
	g_w, g_h int
	g_class  [64]byte
)

func x11Open(w int, h int, title string) error {
	_ = w
	_ = h
	return fmt.Errorf("WinGDI: x11Open 非 Windows 路径")
}

func screenOpenWin(w, h int, title string) error {
	if g_hwnd != nil {
		return fmt.Errorf("WinGDI: 窗口已打开")
	}
	cls := C.CString("qkcleg")
	temp := C.CString(title)
	defer C.free(unsafe.Pointer(temp))
	defer C.free(unsafe.Pointer(cls))
	var wc C.WNDCLASSA
	wc.lpfnWndProc = (C.WNDPROC)(C.qk_wndproc)
	wc.hInstance = C.GetModuleHandleA(nil)
	wc.lpszClassName = cls
	C.RegisterClassA(&wc)
	hw := C.CreateWindowExA(0, cls, temp, C.WS_OVERLAPPEDWINDOW,
		C.CW_USEDEFAULT, C.CW_USEDEFAULT, C.int(w), C.int(h),
		nil, nil, wc.hInstance, nil)
	if hw == nil {
		return fmt.Errorf("WinGDI: CreateWindowExA 失败")
	}
	g_hwnd = hw
	g_w, g_h = w, h
	dc := C.GetDC(hw)
	if dc == nil {
		return fmt.Errorf("WinGDI: GetDC 失败")
	}
	g_dc = dc
	// 32 位自下而上 DIB 截面（与帧缓冲 u32 同布局）
	var bi C.BITMAPINFO
	bi.bmiHeader.biSize = C.DWORD(unsafe.Sizeof(bi.bmiHeader))
	bi.bmiHeader.biWidth = C.LONG(w)
	bi.bmiHeader.biHeight = -C.LONG(h)
	bi.bmiHeader.biPlanes = 1
	bi.bmiHeader.biBitCount = 32
	bi.bmiHeader.biCompression = C.BI_RGB
	var bits unsafe.Pointer
	hb := C.CreateDIBSection(dc, &bi, C.DIB_RGB_COLORS, &bits, nil, 0)
	if hb == nil {
		return fmt.Errorf("WinGDI: CreateDIBSection 失败")
	}
	g_dib = hb
	g_bits = bits
	C.ShowWindow(hw, C.SW_SHOW)
	C.UpdateWindow(hw)
	return nil
}

func presentWin(fb *framebuffer) error {
	if g_hwnd == nil {
		return fmt.Errorf("WinGDI: 窗口未打开")
	}
	w, h := fb.w, fb.h
	if w > g_w || h > g_h {
		w, h = g_w, g_h
	}
	C.memcpy(g_bits, unsafe.Pointer(&fb.buf[0]), C.size_t(w*h*4))
	var bi C.BITMAPINFO
	bi.bmiHeader.biSize = C.DWORD(unsafe.Sizeof(bi.bmiHeader))
	bi.bmiHeader.biWidth = C.LONG(fb.w)
	bi.bmiHeader.biHeight = -C.LONG(fb.h)
	bi.bmiHeader.biPlanes = 1
	bi.bmiHeader.biBitCount = 32
	bi.bmiHeader.biCompression = C.BI_RGB
	C.SetDIBitsToDevice(g_dc, 0, 0, C.DWORD(fb.w), C.DWORD(fb.h),
		0, 0, 0, C.UINT(fb.h), g_bits, &bi, C.UINT(C.DIB_RGB_COLORS))
	C.UpdateWindow(g_hwnd)
	return nil
}

func closeScreenWin() error {
	if g_hwnd != nil {
		C.DeleteObject(C.HGDIOBJ(g_dib))
		C.ReleaseDC(g_hwnd, g_dc)
		C.DestroyWindow(g_hwnd)
		g_hwnd = nil
		g_dib = nil
		g_dc = nil
		g_bits = nil
	}
	return nil
}
