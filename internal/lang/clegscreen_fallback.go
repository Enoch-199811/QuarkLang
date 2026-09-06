//go:build !linux && !windows

package lang

import (
	"fmt"
	"sync"
)

var screenMu sync.Mutex

// 无窗口宿主平台（macOS/BSD 等）：present 以 PNG 帧输出回退（帧文件写入 /tmp/cleg-frame.png）。
func screenOpen(w, h int, title string) error {
	return nil // 无宿主：仅记录；帧输出回退
}

func screenPresent(fb *framebuffer) error {
	if fb == nil {
		return fmt.Errorf("ScreenError: 帧缓冲未创建")
	}
	return fb.savePNG("/tmp/cleg-frame.png")
}

func screenClose() error { return nil }
