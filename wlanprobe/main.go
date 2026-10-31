//go:build windows

// wlanprobe：验证直接调用 wlanapi.dll 能否绕开 netsh 的位置权限限制。
// 用法：
//
//	go run ./wlanprobe current           # 实时读当前 SSID（WlanQueryInterface）
//	go run ./wlanprobe connect <SSID>    # 按 profile 连接（WlanConnect）
package main

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

var (
	wlanapi                = syscall.NewLazyDLL("wlanapi.dll")
	procWlanOpenHandle     = wlanapi.NewProc("WlanOpenHandle")
	procWlanCloseHandle    = wlanapi.NewProc("WlanCloseHandle")
	procWlanEnumInterfaces = wlanapi.NewProc("WlanEnumInterfaces")
	procWlanConnect        = wlanapi.NewProc("WlanConnect")
	procWlanQueryInterface = wlanapi.NewProc("WlanQueryInterface")
	procWlanFreeMemory     = wlanapi.NewProc("WlanFreeMemory")
)

type guid struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

type wlanInterfaceInfo struct {
	InterfaceGUID        guid
	InterfaceDescription [256]uint16
	IsState              uint32
}

type wlanInterfaceInfoList struct {
	NumberOfItems uint32
	Index         uint32
	InterfaceInfo [1]wlanInterfaceInfo
}

type dot11SSID struct {
	SSIDLength uint32
	SSID       [32]byte
}

type wlanConnectionParameters struct {
	ConnectionMode   uint32 // 0 = profile 模式
	Profile          *uint16
	Dot11Ssid        *dot11SSID
	DesiredBssidList uintptr
	BssType          uint32 // 1 = infrastructure
	Flags            uint32
}

const (
	wlanClientVersion          = 2
	wlanConnectionModeProfile  = 0
	dot11BssTypeInfrastructure = 1
	// wlan_intf_opcode_current_connection
	opcodeCurrentConnection = 7

	// WLAN_CONNECTION_ATTRIBUTES 里 dot11Ssid 的偏移：
	// isState(4) + wlanConnectionMode(4) + strProfileName[256]WCHAR(512) = 520
	ssidOffset = 520
)

// openAndPickIface 打开客户端句柄并返回第一个 WLAN 接口的 GUID。
func openAndPickIface() (handle uintptr, iface guid, desc string, err error) {
	var negotiated uint32
	if rc, _, _ := procWlanOpenHandle.Call(
		uintptr(wlanClientVersion), 0,
		uintptr(unsafe.Pointer(&negotiated)),
		uintptr(unsafe.Pointer(&handle)),
	); rc != 0 {
		return 0, guid{}, "", fmt.Errorf("WlanOpenHandle rc=%d", rc)
	}
	var list *wlanInterfaceInfoList
	if rc, _, _ := procWlanEnumInterfaces.Call(
		handle, 0, uintptr(unsafe.Pointer(&list)),
	); rc != 0 {
		return 0, guid{}, "", fmt.Errorf("WlanEnumInterfaces rc=%d", rc)
	}
	defer procWlanFreeMemory.Call(uintptr(unsafe.Pointer(list)))
	if list.NumberOfItems == 0 {
		return 0, guid{}, "", fmt.Errorf("没有可用的 WLAN 接口")
	}
	info := &list.InterfaceInfo[0]
	return handle, info.InterfaceGUID, syscall.UTF16ToString(info.InterfaceDescription[:]), nil
}

func currentSSID() (string, uint32, error) {
	handle, iface, _, err := openAndPickIface()
	if err != nil {
		return "", 0, err
	}
	defer procWlanCloseHandle.Call(handle, 0)

	var size uint32
	var data unsafe.Pointer
	var valueType uint32
	rc, _, _ := procWlanQueryInterface.Call(
		handle,
		uintptr(unsafe.Pointer(&iface)),
		uintptr(opcodeCurrentConnection),
		0,
		uintptr(unsafe.Pointer(&size)),
		uintptr(unsafe.Pointer(&data)),
		uintptr(unsafe.Pointer(&valueType)),
	)
	if rc != 0 {
		return "", 0, fmt.Errorf("WlanQueryInterface rc=%d", rc)
	}
	defer procWlanFreeMemory.Call(uintptr(unsafe.Pointer(data)))
	if data == nil || size < ssidOffset+4 {
		return "", 0, fmt.Errorf("查询结果为空（未连接？）")
	}
	// 直接按偏移读 dot11Ssid
	raw := unsafe.Slice((*byte)(data), int(size))
	length := *(*uint32)(unsafe.Pointer(&raw[ssidOffset]))
	if length == 0 || length > 32 {
		return "", 0, fmt.Errorf("未关联（SSID 长度=%d）", length)
	}
	return string(raw[ssidOffset+4 : ssidOffset+4+int(length)]), 0, nil
}

func connectSSID(ssid string) error {
	handle, iface, _, err := openAndPickIface()
	if err != nil {
		return err
	}
	defer procWlanCloseHandle.Call(handle, 0)

	profile, err := syscall.UTF16PtrFromString(ssid)
	if err != nil {
		return err
	}
	params := wlanConnectionParameters{
		ConnectionMode: wlanConnectionModeProfile,
		Profile:        profile,
		BssType:        dot11BssTypeInfrastructure,
	}
	rc, _, _ := procWlanConnect.Call(
		handle,
		uintptr(unsafe.Pointer(&iface)),
		uintptr(unsafe.Pointer(&params)),
		0,
	)
	if rc != 0 {
		return fmt.Errorf("WlanConnect rc=%d", rc)
	}
	return nil
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("用法: wlanprobe current | wlanprobe connect <SSID>")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "current":
		ssid, _, err := currentSSID()
		if err != nil {
			fmt.Printf("读取当前 SSID 失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("当前 SSID = %q\n", ssid)
	case "connect":
		if len(os.Args) < 3 {
			fmt.Println("用法: wlanprobe connect <SSID>")
			os.Exit(2)
		}
		if err := connectSSID(os.Args[2]); err != nil {
			fmt.Printf("连接 %q 失败: %v\n", os.Args[2], err)
			os.Exit(1)
		}
		fmt.Printf("WlanConnect(%q) 成功，请求已提交\n", os.Args[2])
	default:
		fmt.Println("未知子命令:", os.Args[1])
		os.Exit(2)
	}
}
