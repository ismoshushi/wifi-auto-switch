//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// CREATE_NO_WINDOW：为子进程创建控制台但不显示窗口。
const createNoWindow = 0x08000000

// hideWindow 让子进程完全不弹窗口。
//
// 为什么必须有这个：本程序是 GUI 子系统（-H windowsgui），但它会频繁调用
// netsh / ping / powershell 这些「控制台程序」。Windows 在 GUI 进程创建控制台子进程时，
// 默认会给子进程分配一个新的控制台窗口并显示出来 —— 于是探测一次（默认 5 秒）就闪一个黑窗，
// 走到 PowerShell 兜底时更是直接弹出一个 PowerShell 窗口，看起来像被反复打扰。
func hideWindow(cmd *exec.Cmd) *exec.Cmd {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNoWindow
	return cmd
}
