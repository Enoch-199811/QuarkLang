package lang

import "sync"

// cleg 显示宿主：跨系统窗口呈现（linux=X11；windows=GDI；其它=帧输出回退）。
// 接口一致：screenOpen(w,h,title) / screenPresent(fb) / screenClose()。
var screenMu sync.Mutex

func screenOpen(w, h int, title string) error {
	screenMu.Lock()
	defer screenMu.Unlock()
	return x11Open(w, h, title)
}

func screenPresent(fb *framebuffer) error {
	screenMu.Lock()
	defer screenMu.Unlock()
	return x11Present(fb)
}

func screenClose() error {
	screenMu.Lock()
	defer screenMu.Unlock()
	return closeScreenX11()
}
