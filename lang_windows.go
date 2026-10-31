//go:build windows

package main

import "syscall"

// systemLanguage 读取 Windows 的界面语言（kernel32!GetUserDefaultUILanguage，
// 不需要 cgo，也不用读注册表）。LANGID 低 10 位是主语言 ID，0x04 = 中文。
func systemLanguage() string {
	k := syscall.NewLazyDLL("kernel32.dll")
	proc := k.NewProc("GetUserDefaultUILanguage")
	r, _, _ := proc.Call()
	if uint16(r)&0x3FF == 0x04 {
		return LangZH
	}
	return LangEN
}
