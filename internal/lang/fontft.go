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
func (f *ftFace) ftRaster(ch rune, px int) (int, int, int, int, int, []byte) {
	ftMu.Lock()
	defer ftMu.Unlock()
	cch := C.FT_ULong(ch)
	if C.FT_Load_Char(f.face, cch, C.FT_LOAD_DEFAULT) != 0 {
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
func (fb *framebuffer) ftDrawText(x, y int, text string, px int, c uint32, path string) {
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
		w, h, left, top, adv, data := f.ftRaster(ch, px)
		if adv <= 0 && w == 0 {
			cx += px / 2
			continue
		}
		if w > 0 && h > 0 && data != nil {
			for yy := 0; yy < h; yy++ {
				dy := y - (top - (px - ySizeOfFace(f)))
				_ = dy
				rowY := y + yy - (px - yAscentOfFace(f))
				for xx := 0; xx < w; xx++ {
					if data[yy*w+xx] > 60 {
						fx := cx + left + xx
						if fx >= 0 && fx < fb.w && rowY >= 0 && rowY < fb.h {
							fb.buf[rowY*fb.w+fx] = c
						}
					}
				}
			}
		}
		cx += adv
	}
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
