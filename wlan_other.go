//go:build !windows

package main

import "fmt"

// wlanAPIConnect 在非 Windows 平台没有原生 WLAN API 实现；
// macOS 走 networksetup，见 macWifi.Connect。
func wlanAPIConnect(ssid string) error {
	return fmt.Errorf("当前平台不支持原生 WlanConnect")
}
