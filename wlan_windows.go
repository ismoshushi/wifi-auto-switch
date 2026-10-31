//go:build windows

package main

import (
	"fmt"
	"syscall"
	"unsafe"
)

// 直接调用 Windows WLAN API（wlanapi.dll）。
//
// 为什么不用 netsh：
// netsh wlan connect 在切换到「非当前网络」时，内部会调用 WlanGetAvailableNetworkList
// 去枚举可用网络；而该接口自 Windows 11 起需要「位置服务」权限，关闭时一律返回
// 错误 5（拒绝访问）。若目标恰好是当前已连的网络，netsh 不枚举、直接返回成功，
// 于是出现「切备用成功、切主用失败」的假成功现象。
//
// WlanConnect 是按 profile 直接发起关联，不做网络枚举，因此不依赖位置权限。

var (
	wlanapiDLL             = syscall.NewLazyDLL("wlanapi.dll")
	procWlanOpenHandle     = wlanapiDLL.NewProc("WlanOpenHandle")
	procWlanCloseHandle    = wlanapiDLL.NewProc("WlanCloseHandle")
	procWlanEnumInterfaces = wlanapiDLL.NewProc("WlanEnumInterfaces")
	procWlanConnect        = wlanapiDLL.NewProc("WlanConnect")
	procWlanFreeMemory     = wlanapiDLL.NewProc("WlanFreeMemory")
)

type wlanGUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

type wlanInterfaceInfo struct {
	InterfaceGUID        wlanGUID
	InterfaceDescription [256]uint16
	IsState              uint32
}

type wlanInterfaceInfoList struct {
	NumberOfItems uint32
	Index         uint32
	InterfaceInfo [1]wlanInterfaceInfo // 变长数组，取第 0 项
}

type wlanDot11SSID struct {
	SSIDLength uint32
	SSID       [32]byte
}

type wlanConnectionParameters struct {
	ConnectionMode   uint32 // 0 = wlan_connection_mode_profile
	Profile          *uint16
	Dot11Ssid        *wlanDot11SSID
	DesiredBssidList uintptr
	BssType          uint32 // 1 = dot11_BSS_type_infrastructure
	Flags            uint32
}

const (
	wlanClientVersion          = 2
	wlanConnectionModeProfile  = 0
	dot11BssTypeInfrastructure = 1
)

// wlanAPIConnect 按已保存的 profile 名发起连接。
// 返回 nil 只表示「连接请求已被系统接受」，实际关联结果需另行确认。
func wlanAPIConnect(ssid string) error {
	var negotiated uint32
	var handle uintptr
	if rc, _, _ := procWlanOpenHandle.Call(
		uintptr(wlanClientVersion), 0,
		uintptr(unsafe.Pointer(&negotiated)),
		uintptr(unsafe.Pointer(&handle)),
	); rc != 0 {
		return fmt.Errorf("WlanOpenHandle 失败 rc=%d", rc)
	}
	defer procWlanCloseHandle.Call(handle, 0)

	var list *wlanInterfaceInfoList
	if rc, _, _ := procWlanEnumInterfaces.Call(
		handle, 0, uintptr(unsafe.Pointer(&list)),
	); rc != 0 {
		return fmt.Errorf("WlanEnumInterfaces 失败 rc=%d", rc)
	}
	if list == nil {
		return fmt.Errorf("WlanEnumInterfaces 返回空指针")
	}
	defer procWlanFreeMemory.Call(uintptr(unsafe.Pointer(list)))
	if list.NumberOfItems == 0 {
		return fmt.Errorf("没有可用的 WLAN 接口（无线网卡已禁用？）")
	}

	iface := &list.InterfaceInfo[0]
	profile, err := syscall.UTF16PtrFromString(ssid)
	if err != nil {
		return fmt.Errorf("SSID 转码失败: %w", err)
	}
	params := wlanConnectionParameters{
		ConnectionMode: wlanConnectionModeProfile,
		Profile:        profile,
		BssType:        dot11BssTypeInfrastructure,
	}
	if rc, _, _ := procWlanConnect.Call(
		handle,
		uintptr(unsafe.Pointer(&iface.InterfaceGUID)),
		uintptr(unsafe.Pointer(&params)),
		0,
	); rc != 0 {
		return fmt.Errorf("WlanConnect 失败 rc=%d", rc)
	}
	return nil
}
