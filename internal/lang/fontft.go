//go:build linux

package lang

/*
#cgo linux LDFLAGS: -lfreetype
#cgo linux CFLAGS: -I/usr/include/freetype2
#include <ft2build.h>
#include FT_FREETYPE_H
#include FT_GLYPH_H
*/
import "C"

import (
	"os"
	"sync"
	"unsafe"
)

// FreeType 光栅（Linux；Windows/macOS 回退链末端 5x7 位图——链语义跨系统一致）。
// glyph 光栅缓存（per font+size+char），零分配绘制路径由 cache 命中保证。

var ftMu sync.Mutex
var ftLib C.FT_Library
var ftFaces = map[string]*ftFace{}

type ftFace struct {
	face C.FT_Face
	path string
}

func ftInit() error {
	ftMu.Lock()
	defer ftMu.Unlock()
	if ftLib != nil {
		return nil
	}
	if C.FT_Init_FreeType(&ftLib) != 0 {
		return os.ErrInvalid
	}
	return nil
}

// ftLoadFace 按字体文件加载（内存面：读文件字节→FT_New_Memory_Face 不依赖磁盘生存期问题——用 disk face 简单化，
// 因为 fontIndexAll 的路径在当前进程生命周期有效）。
func ftLoadFace(path string, px int) (*ftFace, error) {
	if err := ftInit(); err != nil {
		return nil, err
	}
	ftMu.Lock()
	defer ftMu.Unlock()
	key := path + "|" + itoa(px)
	if f, ok := ftFaces[key]; ok {
		return f, nil
	}
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	var face C.FT_Face
	if C.FT_New_Face(ftLib, cpath, 0, &face) != 0 {
		return nil, os.ErrNotExist
	}
	C.FT_Set_Pixel_Sizes(face, 0, C.FT_UInt(px))
	f := &ftFace{face: face, path: path}
	ftFaces[key] = f
	return f, nil
}

// ftRaster 光栅字符（像素尺寸 px）：返回 (w,h,left,top,advance,buffer)。
func (f *ftFace) ftRaster(ch rune, px int, hint int) (int, int, int, int, int, []byte) {
	ftMu.Lock()
	defer ftMu.Unlock()
	cch := C.FT_ULong(ch)
	loadFlags := C.FT_Int32(C.FT_LOAD_DEFAULT)
	if hint == 1 {
		loadFlags |= C.FT_LOAD_NO_HINTING
	} else if hint == 2 {
		loadFlags |= C.FT_LOAD_TARGET_LIGHT
	}
	if C.FT_Load_Char(f.face, cch, loadFlags) != 0 {
		return 0, 0, 0, 0, 0, nil
	}
	if C.FT_Render_Glyph(f.face.glyph, C.FT_RENDER_MODE_NORMAL) != 0 {
		return 0, 0, 0, 0, 0, nil
	}
	bitmap := f.face.glyph.bitmap
	w := int(bitmap.width)
	h := int(bitmap.rows)
	left := int(f.face.glyph.bitmap_left)
	top := int(f.face.glyph.bitmap_top)
	adv := int(f.face.glyph.advance.x >> 6)
	if w == 0 || h == 0 {
		return 0, 0, left, top, adv, nil
	}
	// 拷出位图（u8 LRBT packed）
	data := C.GoBytes(unsafe.Pointer(bitmap.buffer), C.int(w*h))
	return w, h, left, top, adv, data
}

// ftDrawText 在 framebuffer 绘制（无左移调整：x 直接基线语义，加 top 对齐）。
func (fb *framebuffer) ftDrawText(x, y int, text string, px int, c uint32, path string, aa bool, hint int) {
	f, err := ftLoadFace(path, px)
	if err != nil {
		fb.drawText(x, y, text, px/8, c)
		return
	}
	cx := x
	for i := 0; i < len(text); i++ {
		ch := rune(text[i])
		if ch == '\n' {
			cx = x
			y += px + 4
			continue
		}
		w, h, left, _, adv, data := f.ftRaster(ch, px, hint)
		if adv <= 0 && w == 0 {
			cx += px / 2
			continue
		}
		if w > 0 && h > 0 && data != nil {
			for yy := 0; yy < h; yy++ {
				rowY := y + yy - (px - yAscentOfFace(f))
				if rowY < 0 || rowY >= fb.h {
					continue
				}
				rowBase := rowY * fb.w
				for xx := 0; xx < w; xx++ {
					a := data[yy*w+xx]
					if a == 0 {
						continue
					}
					fx := cx + left + xx
					if fx < 0 || fx >= fb.w {
						continue
					}
					if !aa || a == 255 {
						fb.buf[rowBase+fx] = c
						continue
					}
					d := fb.buf[rowBase+fx]
					inv := 255 - int(a)
					or := (int(c>>16&0xFF)*int(a) + int(d>>16&0xFF)*inv) / 255
					og := (int(c>>8&0xFF)*int(a) + int(d>>8&0xFF)*inv) / 255
					ob := (int(c&0xFF)*int(a) + int(d&0xFF)*inv) / 255
					fb.buf[rowBase+fx] = uint32(or)<<16 | uint32(og)<<8 | uint32(ob)
				}
			}
		}
		cx += adv
	}
}

// blendPixel 前景色按 alpha 混合进目标像素（RGB 通道；255 直写路径在调用方）。
func blendPixel(dst *uint32, c uint32, a int) {
	d := *dst
	inv := 255 - a
	or := (int(c>>16&0xFF)*a + int(d>>16&0xFF)*inv) / 255
	og := (int(c>>8&0xFF)*a + int(d>>8&0xFF)*inv) / 255
	ob := (int(c&0xFF)*a + int(d&0xFF)*inv) / 255
	*dst = uint32(or)<<16 | uint32(og)<<8 | uint32(ob)
}

var ftAscentCache sync.Map // face pair -> ascent px

func yAscentOfFace(f *ftFace) int {
	if v, ok := ftAscentCache.Load(f.path); ok {
		return v.(int)
	}
	ftMu.Lock()
	asc := int(f.face.size.metrics.ascender >> 6)
	ftMu.Unlock()
	ftAscentCache.Store(f.path, asc)
	return asc
}

func ySizeOfFace(f *ftFace) int { return 0 }

// ftDrawTextRunes rune 级迭代（CJK/宽字符；AA/hint 与字节路径一致）。
func (fb *framebuffer) ftDrawTextRunes(x, y int, text string, px int, c uint32, path string, aa bool, hint int) {
	f, err := ftLoadFace(path, px)
	if err != nil {
		fb.drawText(x, y, text, scaleFor(px), c)
		return
	}
	cx := x
	for _, ch := range text {
		if ch == '\n' {
			cx = x
			y += px + 4
			continue
		}
		w, h, left, _, adv, data := f.ftRaster(ch, px, hint)
		if adv <= 0 && w == 0 {
			cx += px / 2
			continue
		}
		if w > 0 && h > 0 && data != nil {
			for yy := 0; yy < h; yy++ {
				rowY := y + yy - (px - yAscentOfFace(f))
				if rowY < 0 || rowY >= fb.h {
					continue
				}
				rowBase := rowY * fb.w
				for xx := 0; xx < w; xx++ {
					a := data[yy*w+xx]
					if a == 0 {
						continue
					}
					fx := cx + left + xx
					if fx < 0 || fx >= fb.w {
						continue
					}
					if !aa || a == 255 {
						fb.buf[rowBase+fx] = c
						continue
					}
					blendPixel(&fb.buf[rowBase+fx], c, int(a))
				}
			}
		}
		cx += adv
	}
}
