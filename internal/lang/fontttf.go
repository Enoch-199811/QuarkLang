package lang

// 纯 Go TTF 光栅器（零外部依赖，跨系统：Linux/macOS/Windows 相同路径）。
// 支持：sfnt/glyf cmap（format 4 + 12）、二次贝塞尔轮廓 + 非零环绕扫描线填充、glyph 缓存。
// font 回退链：名字链（"DejaVu Sans, Consolas, monospace"）逐个系统字体路径尝试 → 内置 5x7 位图兜底。

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type ttfFont struct {
	data       []byte
	numGlyphs  int
	unitsPerEm int
	cmap4      []byte // format 4 段解析用
	cmap4Data  []byte
	cmap4Sub   uint16
	locafmt    byte // 0=short 1=long（head.indexToLocFormat）
	loca       []byte
	glyf       []byte
	hmtx       []byte
	advanceW   uint16
	headOff    int
	name       string
}

var ttfCacheMu sync.Mutex
var ttfCache = map[string]*ttfFont{}

func tag(b []byte, i int) string { return string(b[i : i+4]) }

func parseTTF(data []byte) (*ttfFont, error) {
	if len(data) < 12 {
		return nil, os.ErrInvalid
	}
	f := &ttfFont{data: data}
	var cmapOff, locaOff, glyfOff, hmtxOff, headOff, maxpOff, nameOff, hheaOff int
	numTables := int(binary.BigEndian.Uint16(data[4:6]))
	for i := 0; i < numTables; i++ {
		rec := 12 + i*16
		if rec+16 > len(data) {
			return nil, os.ErrInvalid
		}
		switch tag(data, rec) {
		case "cmap":
			cmapOff = int(binary.BigEndian.Uint32(data[rec+8 : rec+12]))
		case "loca":
			locaOff = int(binary.BigEndian.Uint32(data[rec+8 : rec+12]))
		case "glyf":
			glyfOff = int(binary.BigEndian.Uint32(data[rec+8 : rec+12]))
		case "hmtx":
			hmtxOff = int(binary.BigEndian.Uint32(data[rec+8 : rec+12]))
		case "head":
			headOff = int(binary.BigEndian.Uint32(data[rec+8 : rec+12]))
		case "maxp":
			maxpOff = int(binary.BigEndian.Uint32(data[rec+8 : rec+12]))
		case "name":
			nameOff = int(binary.BigEndian.Uint32(data[rec+8 : rec+12]))
		case "hhea":
			hheaOff = int(binary.BigEndian.Uint32(data[rec+8 : rec+12]))
		}
	}
	if cmapOff == 0 || glyfOff == 0 || locaOff == 0 || headOff == 0 || maxpOff == 0 || hmtxOff == 0 || hheaOff == 0 {
		return nil, os.ErrInvalid
	}
	f.headOff = headOff
	f.numGlyphs = int(binary.BigEndian.Uint16(data[maxpOff+4 : maxpOff+6]))
	f.unitsPerEm = int(binary.BigEndian.Uint16(data[headOff+18 : headOff+20]))
	f.locafmt = data[headOff+50]
	f.loca = data[locaOff:]
	f.glyf = data[glyfOff:]
	f.hmtx = data[hmtxOff:]
	// cmap：选 format 4 或 12（platform 3 enc 1/10 优先）
	best := -1
	var bestFmt int
	nSub := int(binary.BigEndian.Uint16(data[cmapOff+2 : cmapOff+4]))
	for i := 0; i < nSub; i++ {
		rec := cmapOff + 4 + i*8
		if rec+8 > len(data) {
			break
		}
		off := int(binary.BigEndian.Uint32(data[rec+4 : rec+8]))
		start := cmapOff + off
		if start+2 > len(data) {
			continue
		}
		format := int(binary.BigEndian.Uint16(data[start : start+2]))
		if (format == 4 || format == 12) && int(binary.BigEndian.Uint16(data[rec+2:rec+4])) == 3 {
			if format > bestFmt {
				bestFmt = format
				best = start
			}
		}
	}
	if best < 0 {
		return nil, os.ErrInvalid
	}
	f.cmap4Data = data[best:]
	f.cmap4 = data[best:]
	// 字体名（平台 3 英语 nameID=1）
	if nameOff > 0 {
		f.name = ttfName(data, nameOff)
	}
	return f, nil
}

func ttfName(data []byte, nameOff int) string {
	n := int(binary.BigEndian.Uint16(data[nameOff+2 : nameOff+4]))
	strOff := nameOff + int(binary.BigEndian.Uint16(data[nameOff+4:nameOff+6]))
	for i := 0; i < n; i++ {
		rec := nameOff + 6 + i*12
		if rec+12 > len(data) {
			return ""
		}
		pid := binary.BigEndian.Uint16(data[rec : rec+2])
		nameID := binary.BigEndian.Uint16(data[rec+6 : rec+8])
		l := int(binary.BigEndian.Uint16(data[rec+8 : rec+10]))
		off := int(binary.BigEndian.Uint16(data[rec+10 : rec+12]))
		if pid == 3 && nameID == 1 && strOff+off+l <= len(data) {
			return string(data[strOff+off : strOff+off+l])
		}
	}
	return ""
}

// glyphIndex cmap 查字符→glyph（format 4 或 12）。
func (f *ttfFont) glyphIndex(ch rune) uint32 {
	if ch > 0xFFFF {
		return f.glyphIndex12(uint32(ch))
	}
	if len(f.cmap4) >= 4 {
		format := binary.BigEndian.Uint16(f.cmap4[:2])
		if format == 12 {
			return f.glyphIndex12(uint32(ch))
		}
		return f.glyphIndex4(uint16(ch))
	}
	return 0
}

func (f *ttfFont) glyphIndex4(ch uint16) uint32 {
	d := f.cmap4
	if len(d) < 14 {
		return 0
	}
	segCount := int(binary.BigEndian.Uint16(d[6:8])) / 2
	// 序：endCode(segCount*2), pad2, startCode, idDelta, idRangeOffset, glyphIdArray
	base := 14
	startBase := base + segCount*2 + 2
	deltaBase := startBase + segCount*2
	roBase := deltaBase + segCount*2
	for i := 0; i < segCount; i++ {
		end := binary.BigEndian.Uint16(d[base+i*2 : base+i*2+2])
		start := binary.BigEndian.Uint16(d[startBase+i*2 : startBase+i*2+2])
		if ch >= start && ch <= end {
			delta := binary.BigEndian.Uint16(d[deltaBase+i*2 : deltaBase+i*2+2])
			ro := binary.BigEndian.Uint16(d[roBase+i*2 : roBase+i*2+2])
			if ro == 0 {
				return uint32(uint16(int(ch) + int(int16(delta))))
			}
			idx := roBase + i*2 + int(ro) + int(ch-start)*2
			if idx+2 > len(d) {
				return 0
			}
			g := binary.BigEndian.Uint16(d[idx : idx+2])
			if g == 0 {
				return 0
			}
			return uint32(uint16(int(g) + int(int16(delta))))
		}
	}
	return 0
}

func (f *ttfFont) glyphIndex12(ch uint32) uint32 {
	d := f.cmap4
	if len(d) < 16 {
		return 0
	}
	n := int(binary.BigEndian.Uint32(d[12:16]))
	for i := 0; i < n; i++ {
		rec := 16 + i*12
		if rec+12 > len(d) {
			return 0
		}
		start := binary.BigEndian.Uint32(d[rec : rec+4])
		end := binary.BigEndian.Uint32(d[rec+4 : rec+8])
		if ch >= start && ch <= end {
			gid := binary.BigEndian.Uint32(d[rec+8 : rec+12])
			return gid + (ch - start)
		}
	}
	return 0
}

// glyphOffsets loca 取 glyph 数据区间。
func (f *ttfFont) glyphOffsets(gid uint32) (int, int) {
	if f.locafmt == 1 {
		o := int(gid) * 4
		if o+8 > len(f.loca) {
			return 0, 0
		}
		a := int(binary.BigEndian.Uint32(f.loca[o : o+4]))
		b := int(binary.BigEndian.Uint32(f.loca[o+4 : o+8]))
		return a, b
	}
	o := int(gid) * 2
	if o+4 > len(f.loca) {
		return 0, 0
	}
	a := int(binary.BigEndian.Uint16(f.loca[o:o+2])) * 2
	b := int(binary.BigEndian.Uint16(f.loca[o+2:o+4])) * 2
	return a, b
}

// rasterGlyph 光栅化字符：返回 w/h/advance/alpha（packed 字节行，已用 px 缩放）。
func (f *ttfFont) rasterGlyph(ch rune, px int) (w, h, adv int, alpha []byte) {
	gid := f.glyphIndex(ch)
	if gid >= uint32(f.numGlyphs) {
		return 0, 0, 0, nil
	}
	a, b := f.glyphOffsets(gid)
	if b <= a || b > len(f.glyf) {
		return 0, 0, 0, nil
	}
	g := f.glyf[a:b]
	if len(g) < 10 {
		return 0, 0, 0, nil
	}
	xMin := int(int16(binary.BigEndian.Uint16(g[2:4])))
	yMin := int(int16(binary.BigEndian.Uint16(g[4:6])))
	xMax := int(int16(binary.BigEndian.Uint16(g[6:8])))
	yMax := int(int16(binary.BigEndian.Uint16(g[8:10])))
	scale := float64(px) / float64(f.unitsPerEm)
	w = int((float64(xMax-xMin) * scale) + 0.5)
	h = int((float64(yMax-yMin) * scale) + 0.5)
	if w <= 0 || h <= 0 {
		if f.advanceW > 0 {
			adv = int(float64(f.advanceW) * scale)
		}
		return
	}
	contours := int(int16(binary.BigEndian.Uint16(g[0:2])))
	// 点位解析
	pts, flags, endPts, err := parseGlyphPts(g, contours)
	if err != nil {
		return 0, 0, 0, nil
	}
	// 轮廓折线（二次贝塞尔扁平化）
	polys := outlinePolys(pts, flags, endPts, scale, xMin, yMin)
	// 扫描线非零填充
	alpha = fillPolys(polys, w, h, scale)
	if f.advanceW > 0 {
		adv = int(float64(f.advanceW) * scale)
	}
	return w, h, adv, alpha
}

// parseGlyphPts 解析简单字形的点序列（flags 重复位）。
func parseGlyphPts(g []byte, contours int) ([][2]int, []bool, []int, error) {
	i := 10
	endPts := make([]int, contours)
	for c := 0; c < contours; c++ {
		if i+2 > len(g) {
			return nil, nil, nil, os.ErrInvalid
		}
		endPts[c] = int(binary.BigEndian.Uint16(g[i : i+2]))
		i += 2
	}
	nPts := 0
	if contours > 0 {
		nPts = endPts[contours-1] + 1
	}
	if i+2 <= len(g) {
		ins := int(binary.BigEndian.Uint16(g[i : i+2]))
		i += 2 + ins*2
	}
	// flags 段只出现一次（repeat 扩展）；x 与 y 数据随后各自按 flags 解码
	flags := make([]byte, nPts)
	for p := 0; p < nPts; {
		if i >= len(g) {
			break
		}
		f := g[i]
		i++
		rep := int(f>>3) & 7
		flags[p] = f
		p++
		for k := 0; k < rep && p < nPts; k++ {
			flags[p] = f
			p++
		}
	}
	on := make([]bool, nPts)
	for p := 0; p < nPts; p++ {
		on[p] = flags[p]&1 == 1
	}
	// x 数据
	xc := make([]int, nPts)
	v := 0
	for p := 0; p < nPts; p++ {
		f := flags[p]
		var dx int
		if f&2 != 0 {
			dx = int(g[i])
			i++
			if f&16 == 0 {
				dx = -dx
			}
		} else {
			dx = int(int16(binary.BigEndian.Uint16(g[i : i+2])))
			i += 2
		}
		v += dx
		xc[p] = v
	}
	// y 数据（复用同一 flags）
	yc := make([]int, nPts)
	v = 0
	for p := 0; p < nPts; p++ {
		f := flags[p]
		var dy int
		if f&4 != 0 {
			dy = int(g[i])
			i++
			if f&32 == 0 {
				dy = -dy
			}
		} else {
			dy = int(int16(binary.BigEndian.Uint16(g[i : i+2])))
			i += 2
		}
		v += dy
		yc[p] = v
	}
	pts := make([][2]int, nPts)
	for p := 0; p < nPts; p++ {
		pts[p] = [2]int{xc[p], yc[p]}
	}
	return pts, on, endPts, nil
}

// outlinePolys 按轮廓终点列表切分，逐轮廓扁平化（二次贝塞尔 8 段取样）。
func outlinePolys(pts [][2]int, flags []bool, endPts []int, scale float64, xMin, yMin int) [][][4]float64 {
	var polys [][][4]float64
	prevEnd := -1
	for _, ep := range endPts {
		if ep >= len(pts) {
			break
		}
		seg := flattenContour(pts, flags, prevEnd+1, ep, scale, xMin, yMin)
		if len(seg) > 2 {
			polys = append(polys, seg)
		}
		prevEnd = ep
	}
	return polys
}

func flattenContour(pts [][2]int, flags []bool, start, end int, scale float64, xMin, yMin int) [][4]float64 {
	n := end - start + 1
	if n < 2 {
		return nil
	}
	seq := make([]int, n)
	for i := 0; i < n; i++ {
		seq[i] = start + i
	}
	var poly [][4]float64
	prevOn := -1
	prevOff := -1
	for _, idx := range seq {
		p := pts[idx]
		if flags[idx] {
			if prevOff >= 0 {
				p0 := pts[seq[0]]
				if prevOn >= 0 {
					p0 = pts[prevOn]
				}
				poly = append(poly, quadBez(p0, pts[prevOff], p, scale, xMin, yMin)...)
				prevOff = -1
			} else {
				poly = append(poly, toF(p, scale, xMin, yMin))
			}
			prevOn = idx
		} else {
			if prevOff >= 0 {
				mid := [2]int{(pts[prevOff][0] + p[0]) / 2, (pts[prevOff][1] + p[1]) / 2}
				p0 := pts[seq[0]]
				if prevOn >= 0 {
					p0 = pts[prevOn]
				}
				poly = append(poly, quadBez(p0, pts[prevOff], mid, scale, xMin, yMin)...)
				prevOn = -1
			}
			prevOff = idx
		}
	}
	if prevOff >= 0 {
		p0 := pts[seq[0]]
		if prevOn >= 0 {
			p0 = pts[prevOn]
		}
		poly = append(poly, quadBez(p0, pts[prevOff], pts[seq[0]], scale, xMin, yMin)...)
	}
	return poly
}

func toF(p [2]int, scale float64, xMin, yMin int) [4]float64 {
	return [4]float64{(float64(p[0]-xMin) * scale), (float64(p[1]-yMin) * scale), 0, 0}
}

// quadBez 二次贝塞尔（p0, ctrl, p2）→ 8 段折线。
func quadBez(p0, ctrl, p2 [2]int, scale float64, xMin, yMin int) [][4]float64 {
	out := make([][4]float64, 0, 9)
	const n = 8
	for k := 1; k <= n; k++ {
		t := float64(k) / float64(n)
		mt := 1 - t
		x := mt*mt*float64(p0[0]) + 2*mt*t*float64(ctrl[0]) + t*t*float64(p2[0])
		y := mt*mt*float64(p0[1]) + 2*mt*t*float64(ctrl[1]) + t*t*float64(p2[1])
		out = append(out, [4]float64{(x - float64(xMin)) * scale, (y - float64(yMin)) * scale, 0, 0})
	}
	return out
}

// fillPolys 扫描线非零环绕填充（poly=[x,y] 折线列）。
func fillPolys(polys [][][4]float64, w, h int, scale float64) []byte {
	alpha := make([]byte, w*h)
	for y := 0; y < h; y++ {
		yy := float64(y) + 0.5
		var xs []float64
		for _, poly := range polys {
			n := len(poly)
			for i := 0; i < n; i++ {
				p1 := poly[i]
				p2 := poly[(i+1)%n]
				if (p1[1] <= yy && p2[1] > yy) || (p2[1] <= yy && p1[1] > yy) {
					if !(p2[1] == p1[1]) {
						t := (yy - p1[1]) / (p2[1] - p1[1])
						xs = append(xs, p1[0]+t*(p2[0]-p1[0]))
					}
				}
			}
		}
		// 简单排序（交叉数少，插入排序）
		for i := 1; i < len(xs); i++ {
			for j := i; j > 0 && xs[j] < xs[j-1]; j-- {
				xs[j], xs[j-1] = xs[j-1], xs[j]
			}
		}
		for i := 0; i+1 < len(xs); i += 2 {
			x0 := int(xs[i] - 0.5)
			x1 := int(xs[i+1] + 0.5)
			for x := x0; x <= x1; x++ {
				if x >= 0 && x < w && y >= 0 && y < h {
					alpha[y*w+x] = 255
				}
			}
		}
	}
	return alpha
}

// fontPathsOf 字名→候选文件路径（跨系统扫描：linux /usr/share/fonts + /usr/local/share/fonts；macOS /System/Library/Fonts + /Library/Fonts；Windows C:\\Windows\\Fonts）。
var fontRoots = func() []string {
	var r []string
	if os.PathSeparator == '/' {
		r = []string{"/usr/share/fonts", "/usr/local/share/fonts", filepath.Join(os.Getenv("HOME"), ".fonts")}
		if _, err := os.Stat("/System/Library/Fonts"); err == nil {
			r = append(r, "/System/Library/Fonts", "/Library/Fonts")
		}
	} else {
		wd := os.Getenv("WINDIR")
		if wd == "" {
			wd = "C:\\Windows"
		}
		r = []string{filepath.Join(wd, "Fonts")}
	}
	return r
}()

var fontIndexOnce sync.Once
var fontIndex []string // 所有候选 .ttf/.otf 路径（含目录惰性扫描）

func fontIndexAll() []string {
	fontIndexOnce.Do(func() {
		for _, root := range fontRoots {
			_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
				if err == nil && !info.IsDir() {
					ext := strings.ToLower(filepath.Ext(p))
					if ext == ".ttf" || ext == ".otf" || strings.Contains(filepath.Base(p), ".ttc") {
						fontIndex = append(fontIndex, p)
					}
				}
				return nil
			})
		}
	})
	return fontIndex
}

// loadFont 加载字名（回退链单项）。
func loadFont(name string, px int) (*ttfFont, error) {
	name = normalizeFontName(name)
	ttfCacheMu.Lock()
	defer ttfCacheMu.Unlock()
	key := name + "|" + itoa(px)
	if f, ok := ttfCache[key]; ok {
		return f, nil
	}
	for _, p := range fontIndexAll() {
		base := normalizeFontName(strings.TrimSuffix(strings.TrimSuffix(filepath.Base(p), filepath.Ext(p)), ""))
		if strings.Contains(base, name) || strings.Contains(name, base) {
			data, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			f, err := parseTTF(data)
			if err == nil {
				ttfCache[key] = f
				return f, nil
			}
		}
	}
	return nil, os.ErrNotExist
}

// fontPathOf 字名→命中路径（回退链单项探测）。
func fontPathOf(name string) string {
	name = normalizeFontName(name)
	for _, p := range fontIndexAll() {
		base := normalizeFontName(strings.TrimSuffix(strings.TrimSuffix(filepath.Base(p), filepath.Ext(p)), ""))
		if strings.Contains(base, name) || strings.Contains(name, base) {
			return p
		}
	}
	return ""
}

// normalizeFontName 字体名规范化（去大小写/空格/连字符）。
func normalizeFontName(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		if r == ' ' || r == '-' || r == '_' || r == ',' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

const defaultFontChain = "monospace"
