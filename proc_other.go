//go:build !windows

package main

import "os/exec"

// hideWindow 在非 Windows 平台是空操作（没有控制台窗口这一说）。
func hideWindow(cmd *exec.Cmd) *exec.Cmd {
	return cmd
}
