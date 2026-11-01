//go:build windows

package transcode

import (
	"os/exec"
	"syscall"
)

// hideWindow 避免转码时弹出命令行黑窗。
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
}
