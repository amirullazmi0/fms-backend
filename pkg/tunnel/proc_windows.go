//go:build windows

package tunnel

import (
	"os/exec"
	"syscall"
)

// hideWindow keeps bore from opening a console window on Windows.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
