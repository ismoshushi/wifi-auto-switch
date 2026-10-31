package main

import (
	"errors"
	"fmt"
	"sync"
)

// 支持的语言。新增一门语言时：补一个常量、在 msg 里加一列、把 T() 的选择改掉即可。
const (
	LangZH = "zh"
	LangEN = "en"
)

// msg 一条文案的中英两版。格式串用 fmt 占位（%s / %d）；
// 需要包错误的地方统一用 %v —— 经 Sprintf 拼好的字符串无法再用 %w 包装。
type msg struct {
	zh string
	en string
}

var catalog = map[string]msg{
	// ---------- 应用 / 托盘 ----------
	"app.title":     {"WiFi 自动切换", "WiFi Auto Switch"},
	"app.tray":      {"WiFi 切换", "WiFi Switch"},
	"tray.open":     {"打开设置", "Settings"},
	"tray.toMain":   {"切到主用 Wi-Fi", "Switch to main Wi-Fi"},
	"tray.toBackup": {"切到备用 Wi-Fi", "Switch to backup Wi-Fi"},
	"tray.quit":     {"退出", "Quit"},
	"btn.lang":      {"EN", "中文"},

	// ---------- 模式名（用于拼句子，英文用小写以便嵌入句子） ----------
	"mode.main":   {"主用", "main"},
	"mode.backup": {"备用", "backup"},

	// ---------- 顶部状态横幅 ----------
	"banner.waiting":     {"等待检测…", "Waiting for check…"},
	"banner.main":        {"主网运行中", "Main network"},
	"banner.backup":      {"备用网运行中", "Backup network"},
	"banner.ssid":        {"当前 Wi-Fi   %s", "Current Wi-Fi   %s"},
	"banner.ssidUnknown": {"当前 Wi-Fi   未知（可能未连接或读取受限）", "Current Wi-Fi   unknown (not connected, or unreadable)"},
	"banner.online":      {"网络正常", "Online"},
	"banner.offline":     {"网络异常", "Offline"},
	"banner.checking":    {"检测中", "Checking"},
	"note.paused":        {"已暂停自动切换（仍持续探测）", "Auto-switch paused (still probing)"},
	"note.notConfigured": {"未配置%s Wi-Fi：请先在上方选择并保存", "No %s Wi-Fi configured: pick one above and save"},
	"note.switching":     {"正在切换到%s：%s", "Switching to %s: %s"},
	"note.switchFailed":  {"切%s失败：%v", "Failed to switch to %s: %v"},
	"note.scanFailed":    {"扫描失败：%v", "Scan failed: %v"},
	"note.scanDone":      {"已加载 %d 个网络，选择主用/备用后点「保存配置」", "Loaded %d networks — pick main/backup, then save"},
	"note.saved":         {"配置已保存，新参数已生效", "Config saved, new settings are live"},

	// ---------- 卡片 / 字段 ----------
	"card.network.title":    {"网络", "Network"},
	"card.network.subtitle": {"列表来自附近网络与已保存的网络", "From nearby networks and saved profiles"},
	"card.policy.title":     {"切换策略", "Switch policy"},
	"card.policy.subtitle":  {"连续失败达到阈值才切备用；主用稳定超过冷却时间才回切，避免来回乒乓", "Switch to backup after N consecutive fails; back to main once it stays stable past the cooldown"},
	"card.log.title":        {"运行日志", "Run log"},
	"card.log.count":        {"最近 400 条", "last 400 lines"},
	"field.main":            {"主用 Wi-Fi", "Main Wi-Fi"},
	"field.backup":          {"备用 Wi-Fi", "Backup Wi-Fi"},
	"field.interval":        {"检测间隔（秒）", "Check every (s)"},
	"field.threshold":       {"失败阈值（次）", "Fails before switch"},
	"field.cooldown":        {"回切冷却（秒）", "Cool-down (s)"},
	"ph.main":               {"选择主用 Wi-Fi", "Select main Wi-Fi"},
	"ph.backup":             {"选择备用 Wi-Fi", "Select backup Wi-Fi"},
	"ph.sec":                {"秒", "sec"},
	"ph.times":              {"次", "times"},

	// ---------- 按钮 ----------
	"btn.save":     {"保存配置", "Save"},
	"btn.toMain":   {"切到主用", "To Main"},
	"btn.toBackup": {"切到备用", "To Backup"},
	"btn.pause":    {"暂停监控", "Pause"},
	"btn.resume":   {"继续监控", "Resume"},
	"btn.rescan":   {"重新扫描", "Rescan"},
	"btn.scanning": {"扫描中…", "Scanning…"},

	// ---------- 校验与错误 ----------
	"err.emptySSID":    {"SSID 为空，无法连接", "empty SSID, cannot connect"},
	"err.needMain":     {"请先选择「主用 Wi-Fi」", "Please select your main Wi-Fi first"},
	"err.sameSSID":     {"主用与备用不能是同一个网络", "Main and backup cannot be the same network"},
	"err.unsupported":  {"不支持的平台: %s", "unsupported platform: %s"},
	"err.scan":         {"扫描失败: %v", "scan failed: %v"},
	"err.noNetwork":    {"未发现可用网络（可能 Wi‑Fi 已关闭或无可见热点）", "no network found (Wi-Fi may be off, or no visible hotspot)"},
	"err.giveUp":       {"连接 %q 重试 %d 次后仍失败: %v", "connect %q still failing after %d retries: %v"},
	"err.refused":      {"系统拒绝连接：%v", "connection refused by system: %v"},
	"err.noAssoc":      {"已提交连接请求，但 %q 在 %d 秒内未关联成功（当前：%s）", "connect requested, but %q was not associated within %d s (now: %s)"},
	"err.assocCtx":     {"等待关联超时（目标 %q）", "timed out waiting for association (target %q)"},
	"err.noSSIDWin":    {"当前无 Wi-Fi 连接（读取受限：未关联或系统限制）", "no Wi-Fi connection (unassociated, or blocked by system)"},
	"err.noSSIDMac":    {"未获取到 SSID（airport 输出: %q，错误: %v）", "SSID not available (airport output: %q, err: %v)"},
	"err.notConnected": {"未连接", "not connected"},

	// ---------- 日志 ----------
	"log.connectFail":      {"连接 %q 第 %d 次失败: %v", "connect %q attempt %d failed: %v"},
	"log.manualSwitch":     {"手动切%s：%s", "manual switch to %s: %s"},
	"log.switchedTo":       {"已切到%s（%s）", "switched to %s (%s)"},
	"log.switchFailed":     {"切%s失败: %v", "switch to %s failed: %v"},
	"log.notConfigured":    {"未配置%s Wi-Fi，无法切换", "no %s Wi-Fi configured, cannot switch"},
	"log.mainProbeFail":    {"主网探测失败 (%d/%d)", "main probe failed (%d/%d)"},
	"log.mainFailSwitch":   {"主网连续失败，切换到备用", "main kept failing, switching to backup"},
	"log.switchBackupErr":  {"切换备用失败: %v", "switching to backup failed: %v"},
	"log.mainRecovered":    {"主网恢复，开始冷却计时 (%d 秒)", "main is back, cooling down (%d s)"},
	"log.mainStable":       {"主网已稳定，回切主网", "main is stable again, switching back"},
	"log.switchMainErr":    {"回切主网失败: %v", "switching back to main failed: %v"},
	"log.calibratedMain":   {"检测到已连接主网 %q，状态校准为 main", "already on main network %q, state calibrated to main"},
	"log.calibratedBackup": {"检测到已连接备用网 %q，状态校准为 backup", "already on backup network %q, state calibrated to backup"},
	"log.ssidErr":          {"获取 SSID 出错: %v", "reading SSID failed: %v"},
	"log.netshFallback":    {"netsh 读不到当前网络（多为位置服务被系统限制），改用 PowerShell 兜底；%d 秒内不再重复调 netsh", "netsh cannot read the current network (usually blocked by location permission), falling back to PowerShell; netsh skipped for %d s"},
	"log.wlanNative":       {"原生 WlanConnect 失败: %v，回退 netsh", "native WlanConnect failed: %v, falling back to netsh"},
	"log.paused":           {"已暂停自动切换（不再自动切网，仍持续探测）", "auto-switch paused (no switching, still probing)"},
	"log.resumed":          {"已恢复自动切换", "auto-switch resumed"},
	"log.scanFail":         {"扫描网络失败: %v", "scanning networks failed: %v"},
	"log.scanDone":         {"已加载 %d 个网络（附近扫描或已保存配置）", "loaded %d networks (nearby scan or saved profiles)"},
	"log.copied":           {"日志已复制到剪贴板", "log copied to clipboard"},
	"log.saved":            {"配置已保存：主用=%q 备用=%q 间隔=%ds 阈值=%d 冷却=%ds", "config saved: main=%q backup=%q interval=%ds threshold=%d cooldown=%ds"},
	"log.langSwitched":     {"界面语言已切换为中文", "UI language switched to English"},
}

var (
	langMu    sync.RWMutex
	langCur   = LangZH
	tranMu    sync.Mutex
	tranFuncs []func()
)

// lang 返回当前界面语言。
func lang() string {
	langMu.RLock()
	defer langMu.RUnlock()
	return langCur
}

// setLang 设置当前界面语言（只接受已知值，其它一律回落中文）。
func setLang(l string) {
	if l != LangEN {
		l = LangZH
	}
	langMu.Lock()
	langCur = l
	langMu.Unlock()
}

// T 取一条文案：key 缺失时返回 key 本身，方便一眼看出漏翻的条目。
// 带参数时按 fmt 规则填充，参数顺序应与文案里的占位符一致。
func T(key string, args ...any) string {
	m, ok := catalog[key]
	if !ok {
		return key
	}
	s := m.zh
	if lang() == LangEN {
		s = m.en
	}
	if len(args) > 0 {
		s = fmt.Sprintf(s, args...)
	}
	return s
}

// Terr 生成一条已翻译的错误。参数里若含 % 不会破坏格式（文案才是格式串）。
func Terr(key string, args ...any) error {
	return errors.New(T(key, args...))
}

// onLangChange 注册一个「切换语言时需要重刷」的闭包。
// 控件本身不感知语言，靠这些闭包把新文案写回去。
func onLangChange(f func()) {
	tranMu.Lock()
	tranFuncs = append(tranFuncs, f)
	tranMu.Unlock()
}

// refreshLang 把当前语言重新应用到所有已注册的控件上。
func refreshLang() {
	tranMu.Lock()
	fs := make([]func(), len(tranFuncs))
	copy(fs, tranFuncs)
	tranMu.Unlock()
	for _, f := range fs {
		f()
	}
}
