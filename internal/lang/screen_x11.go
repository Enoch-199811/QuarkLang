//go:build linux

package lang

/*
#cgo linux LDFLAGS: -lX11
#include <X11/Xlib.h>
#include <stdint.h>
#include <X11/Xlib.h>
#include <X11/Xutil.h>
#include <stdlib.h>
#include <string.h>
static void qk_destroy_image(XImage* im) { XDestroyImage(im); }
*/
import "C"

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"unsafe"
)

// X11 显示宿主：窗口 + XImage 提交（一次 malloc 拷贝，FFI 安全）。

var (
	x11Display *C.Display
	x11Win     C.Window
	x11Img     *C.XImage
	x11Data    unsafe.Pointer
)

func x11Open(w, h int, title string) error {
	d := C.XOpenDisplay(nil)
	if d == nil {
		return fmt.Errorf("XError: 无法打开 DISPLAY")
	}
	x11Display = d
	root := C.XDefaultRootWindow(d)
	win := C.XCreateSimpleWindow(d, root, 0, 0, C.uint(w), C.uint(h), 0, 0x10AAAAAA, 0x0A1E38)
	ctitle := C.CString(title)
	defer C.free(unsafe.Pointer(ctitle))
	C.XStoreName(d, win, ctitle)
	C.XMapWindow(d, win)
	C.XSync(d, 0)
	x11Win = win
	// 生命周期固定：Image + data 缓冲只在 open 时分配一次
	img := C.XCreateImage(d, C.XDefaultVisual(d, C.XDefaultScreen(d)),
		C.uint(24), C.ZPixmap, 0, nil, C.uint(w), C.uint(h), 32, 0)
	if img == nil {
		return fmt.Errorf("XError: XCreateImage 失败")
	}
	x11Data = C.malloc(C.size_t(w * h * 4))
	img.data = (*C.char)(x11Data)
	x11Img = img
	return nil
}

func x11Present(fb *framebuffer) error {
	if x11Display == nil || x11Win == 0 {
		return fmt.Errorf("XError: 窗口未打开 (qkscreen_open)")
	}
	if fb == nil {
		return fmt.Errorf("XError: 帧缓冲未创建")
	}
	w, h := fb.w, fb.h
	C.memcpy(x11Data, unsafe.Pointer(&fb.buf[0]), C.size_t(w*h*4))
	C.XPutImage(x11Display, x11Win, C.XDefaultGC(x11Display, C.XDefaultScreen(x11Display)),
		x11Img, 0, 0, 0, 0, C.uint(w), C.uint(h))
	C.XFlush(x11Display)
	return nil
}

func closeScreenX11() error {
	if x11Display == nil {
		return nil
	}
	if x11Img != nil {
		C.qk_destroy_image(x11Img)
		x11Img = nil
	}
	C.XDestroyWindow(x11Display, x11Win)
	C.XCloseDisplay(x11Display)
	x11Display = nil
	x11Win = 0
	return nil
}

// fallbackFrame（macOS/其它）：帧输出为 PNG（无窗口宿主时回退）。
func fallbackFrame(path string, w, h int, pix []uint32) error {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(pix); i++ {
		v := pix[i]
		img.Pix[i*4+0] = uint8(v >> 16)
		img.Pix[i*4+1] = uint8(v >> 8)
		img.Pix[i*4+2] = uint8(v)
		img.Pix[i*4+3] = 0xFF
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}
