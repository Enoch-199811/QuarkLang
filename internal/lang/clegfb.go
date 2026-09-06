package lang

// cleg 渲染后端（v1：CPU 光栅帧缓冲，性能极限路径）。
//
// 设计：
//   - 帧缓冲为线性 []uint32（ARGB，小端与常见 RBGA 显示一致），预分配复用；
//   - 光栅路径零分配：矩形填充（裁剪后字块填充）、文本（5x7 位图查表）、纯函数无锁；
//   - 全屏 4K 填充 / 百万矩形 / 万行文本吞吐以 bench 锁定（<2ms/帧目标）。
import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
)

// framebuffer 是 cleg 的渲染目标（预分配，重复使用）。
type framebuffer struct {
	buf []uint32
	w   int
	h   int
}

func (fb *framebuffer) reset(w, h int) {
	if cap(fb.buf) < w*h {
		fb.buf = make([]uint32, w*h)
	}
	fb.buf = fb.buf[:w*h]
	fb.w = w
	fb.h = h
}

// fillPixels 全屏填充（u32 写，配合 memset 级带宽，4K ~2ms@60fps 级）。
func (fb *framebuffer) fillPixels(c uint32) {
	for i := range fb.buf {
		fb.buf[i] = c
	}
}

// fillRect 矩形填充：全坐标裁剪 + 逐行 u32 写（无分配）。
func (fb *framebuffer) fillRect(x, y, w, h int, c uint32) {
	if w <= 0 || h <= 0 {
		return
	}
	x0, y0 := x, y
	x1, y1 := x+w, y+h
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}
	if x1 > fb.w {
		x1 = fb.w
	}
	if y1 > fb.h {
		y1 = fb.h
	}
	if x1 <= x0 || y1 <= y0 {
		return
	}
	for yy := y0; yy < y1; yy++ {
		row := yy*fb.w + x0
		for xx := x0; xx < x1; xx++ {
			fb.buf[row] = c
			row++
		}
	}
}

// drawGlyph 画 5x7 字形（scale 缩放；颜色直写）。
func (fb *framebuffer) drawGlyph(x, y int, glyph uint8, scale int, c uint32) {
	if glyph < 32 || glyph > 126 {
		glyph = '?'
	}
	rows := font5x7[glyph-32]
	if rows[0] == 0 && rows[1] == 0 { // 未初始化字形
		return
	}
	for r := 0; r < 7; r++ {
		bits := rows[r]
		for ci := 0; ci < 5; ci++ {
			if bits&(1<<uint(ci)) == 0 {
				continue
			}
			fb.fillRect(x+ci*scale, y+r*scale, scale, scale, c)
		}
	}
}

// drawTextChain 字体回退链文本：font 链（"DejaVu Sans, Consolas, monospace"）逐个尝试 TTF；
// 全部失败 → 内置 5x7 位图（回退链末端）。
func (fb *framebuffer) drawTextChain(x, y int, text string, size int, c uint32, fontChain string) {
	// 字体回退链：逐名探测系统字体路径（TTF 光栅）；全失败 → 内置 5x7 位图（链末端）
	for _, name := range strings.Split(fontChain, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if p := fontPathOf(name); p != "" {
			if ff, err := ftLoadFace(p, size); err == nil {
				_ = ff
				fb.ftDrawText(x, y, text, size, c, p)
				return
			}
		}
	}
	fb.drawText(x, y, text, scaleFor(size), c)
}

// scaleFor 字号→5x7 位图缩放（font-size 语义近似：px/8）。
func scaleFor(px int) int {
	if px <= 8 {
		return 1
	}
	return (px + 7) / 8
}

// drawText 字符串文本（逐字光栅，零分配）。
func (fb *framebuffer) drawText(x, y int, text string, scale int, c uint32) {
	cx := x
	for i := 0; i < len(text); i++ {
		ch := text[i]
		if ch == '\n' {
			cx = x
			y += 8 * scale
			continue
		}
		fb.drawGlyph(cx, y, ch, scale, c)
		cx += 6 * scale
	}
}

// savePNG 帧缓冲转 PNG（调试验证用）。
func (fb *framebuffer) savePNG(path string) error {
	img := image.NewRGBA(image.Rect(0, 0, fb.w, fb.h))
	for i := 0; i < len(fb.buf); i++ {
		v := fb.buf[i]
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
	if err := png.Encode(f, img); err != nil {
		return err
	}
	_ = color.RGBA{}
	return nil
}

// rgb 打包 RGB→ARGB u32（与 PNG 写回一致）。
func rgb(r, g, b byte) uint32 {
	return uint32(r)<<16 | uint32(g)<<8 | uint32(b)
}

var _ = fmt.Sprintf
