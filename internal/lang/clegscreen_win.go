//go:build windows

package lang

import "sync"

var screenMu sync.Mutex

func screenOpen(w, h int, title string) error {
	screenMu.Lock()
	defer screenMu.Unlock()
	return screenOpenWin(w, h, title)
}

func screenPresent(fb *framebuffer) error {
	screenMu.Lock()
	defer screenMu.Unlock()
	return presentWin(fb)
}

func screenClose() error {
	screenMu.Lock()
	defer screenMu.Unlock()
	return closeScreenWin()
}
