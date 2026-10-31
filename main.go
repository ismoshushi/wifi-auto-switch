package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"image/color"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

//go:embed assets/icon.png
var iconAppBytes []byte

//go:embed assets/icon_main.png
var iconMainBytes []byte

//go:embed assets/icon_backup.png
var iconBackupBytes []byte

// Config 持久化到 config.json。时间类字段统一用「秒」的整数，避免 Duration 单位歧义。
type Config struct {
	MainSSID      string `json:"main_ssid"`
	BackupSSID    string `json:"backup_ssid"`
	CheckInterval int    `json:"check_interval_sec"` // 探测间隔（秒）
	FailThreshold int    `json:"fail_threshold"`     // 连续失败几次才切备用
	RecoverWait   int    `json:"recover_wait_sec"`   // 主网稳定多久后回切（秒）
	PingTarget    string `json:"ping_target"`        // ping 探测目标（建议国内可达，如 223.5.5.5）
	HTTPTarget    string `json:"http_target"`        // HTTP 探测目标（如 http://1.1.1.1）
}

func defaultConfig() Config {
	return Config{
		CheckInterval: 5,
		FailThreshold: 3,
		RecoverWait:   60,
		PingTarget:    "223.5.5.5",
		HTTPTarget:    "http://1.1.1.1",
	}
}

type appState struct {
	mu              sync.Mutex
	mode            string // "main" / "backup"
	failCount       int
	mainStableSince *time.Time // 备用时主网恢复的起点，用于冷却防乒乓
	running         bool
	paused          bool // 暂停自动切换（仍继续探测与显示状态）
}

var cfg Config
var ctrl wifiController
var st = &appState{mode: "main", running: true}
var logChan = make(chan string, 64)
var desk desktop.App // 托盘引用，供状态变化时换图标

// lastSSIDErrMsg 去重：避免每次探测都刷「获取 SSID 出错」日志
var lastSSIDErrMsg string

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// ---------- 配置读写 ----------

func loadConfig() Config {
	b, err := os.ReadFile("config.json")
	if err != nil {
		return defaultConfig()
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return defaultConfig()
	}
	return c
}

func saveConfig(c Config) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile("config.json", b, 0644)
}

// ---------- 日志（UI + 文件双写） ----------

var logMu sync.Mutex

func logf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	select {
	case logChan <- msg:
	default:
	}
	logMu.Lock()
	if f, err := os.OpenFile("wifi-autoswitch.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644); err == nil {
		_, _ = f.WriteString(time.Now().Format("2006-01-02 15:04:05") + " " + msg + "\n")
		f.Close()
	}
	logMu.Unlock()
}

// ---------- UI 状态（结构化，供顶部状态横幅渲染） ----------

type uiStatus struct {
	Mode   string // "main" / "backup" / ""
	SSID   string // 当前 SSID，空表示未知
	Online int    // 1 通 / -1 不通 / 0 检测中
	Note   string // 附加提示（一行）
}

var statusChan = make(chan uiStatus, 16)
var uiMu sync.Mutex
var lastUI uiStatus

// pushStatus 合并式更新：在共享快照上打补丁，避免各调用点互相覆盖字段。
func pushStatus(upd func(*uiStatus)) {
	uiMu.Lock()
	if upd != nil {
		upd(&lastUI)
	}
	s := lastUI
	uiMu.Unlock()
	select {
	case statusChan <- s:
	default:
	}
}

func onlineState(ok bool) int {
	if ok {
		return 1
	}
	return -1
}

// ---------- 顶部状态横幅 ----------

var (
	colMain    = color.NRGBA{R: 0x16, G: 0xA3, B: 0x4A, A: 0xFF} // 主网：绿
	colBackup  = color.NRGBA{R: 0xEA, G: 0x58, B: 0x0C, A: 0xFF} // 备用：橙
	colUnknown = color.NRGBA{R: 0x64, G: 0x74, B: 0x8B, A: 0xFF} // 未知：灰蓝
)

type statusBanner struct {
	bg    *canvas.Rectangle
	title *canvas.Text
	ssid  *canvas.Text
	conn  *canvas.Text
	dot   *canvas.Rectangle
	note  *canvas.Text
}

func newStatusBanner() (*statusBanner, fyne.CanvasObject) {
	b := &statusBanner{
		bg:    canvas.NewRectangle(colUnknown),
		title: canvas.NewText("等待检测…", color.White),
		ssid:  canvas.NewText("当前 Wi-Fi 未知", color.White),
		conn:  canvas.NewText("检测中", color.White),
		note:  canvas.NewText("", color.White),
	}
	b.bg.CornerRadius = 12
	b.title.TextSize = 19
	b.title.TextStyle = fyne.TextStyle{Bold: true}
	b.ssid.TextSize = 12
	b.ssid.Color = color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xEB}

	// 右侧状态徽标：半透明白底胶囊 + 小圆点 + 文字
	pillBg := canvas.NewRectangle(color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0x2E})
	pillBg.CornerRadius = 20
	b.dot = canvas.NewRectangle(color.White)
	b.dot.CornerRadius = 4
	b.dot.SetMinSize(fyne.NewSize(7, 7))
	b.conn.TextSize = 11
	badge := container.NewMax(pillBg,
		pad(container.NewHBox(b.dot, b.conn), 4, 4, 10, 9))

	b.note.TextSize = 11
	b.note.Color = color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xDC}
	b.note.Hide()

	head := container.NewHBox(b.title, layout.NewSpacer(), badge)
	inner := container.NewVBox(
		head,
		gapY(4),
		b.ssid,
		b.note,
	)
	return b, container.NewMax(b.bg, pad(inner, 13, 12, 16, 16))
}

func (b *statusBanner) apply(s uiStatus) {
	switch s.Mode {
	case "main":
		b.bg.FillColor = colMain
		b.title.Text = "主网运行中"
	case "backup":
		b.bg.FillColor = colBackup
		b.title.Text = "备用网运行中"
	default:
		b.bg.FillColor = colUnknown
		b.title.Text = "等待检测…"
	}
	if s.SSID != "" {
		b.ssid.Text = "当前 Wi-Fi   " + s.SSID
	} else {
		b.ssid.Text = "当前 Wi-Fi   未知（可能未连接或读取受限）"
	}
	switch s.Online {
	case 1:
		b.conn.Text = "网络正常"
		b.dot.FillColor = color.NRGBA{R: 0xD9, G: 0xFB, B: 0xE3, A: 0xFF}
	case -1:
		b.conn.Text = "网络异常"
		b.dot.FillColor = color.NRGBA{R: 0xFF, G: 0xE1, B: 0xB8, A: 0xFF}
	default:
		b.conn.Text = "检测中"
		b.dot.FillColor = color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xCC}
	}
	b.note.Text = s.Note
	if s.Note == "" {
		b.note.Hide()
	} else {
		b.note.Show()
	}
	b.bg.Refresh()
	b.title.Refresh()
	b.ssid.Refresh()
	b.conn.Refresh()
	b.dot.Refresh()
	b.note.Refresh()
}

// ---------- 连通性探测（HTTP 或 ping 任一通即算通） ----------

func httpOK(target string) bool {
	if target == "" {
		return false
	}
	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(target)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func pingOK(target string) bool {
	if target == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.CommandContext(ctx, "ping", "-n", "1", "-w", "1000", target)
	case "darwin":
		cmd = exec.CommandContext(ctx, "ping", "-c", "1", "-t", "1", target)
	default:
		return false
	}
	return hideWindow(cmd).Run() == nil
}

// ---------- 平台控制器 ----------

type wifiController interface {
	CurrentSSID() (string, error)
	Connect(ctx context.Context, ssid string) error
}

// connectWithRetry：带超时与重试，避免底层命令卡死。
// 单次 30s：内部要等到实际关联成功（最多 20s）才算完成，不能给太短的预算。
func connectWithRetry(c wifiController, ssid string, retries int) error {
	if ssid == "" {
		return fmt.Errorf("SSID 为空，无法连接")
	}
	var lastErr error
	for i := 0; i <= retries; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err := c.Connect(ctx, ssid)
		cancel()
		if err == nil {
			return nil
		}
		lastErr = err
		logf("连接 %q 第 %d 次失败: %v", ssid, i+1, err)
		if i < retries {
			time.Sleep(time.Second)
		}
	}
	return fmt.Errorf("连接 %q 重试 %d 次后仍失败: %w", ssid, retries, lastErr)
}

var ssidRe = regexp.MustCompile(`(?m)^\s*SSID\s*:\s*(.*?)\s*$`)

// ssidListRe 匹配 `netsh wlan show networks` 的 "SSID 1 : Name" 行
var ssidListRe = regexp.MustCompile(`(?m)^\s*SSID\s+\d+\s*:\s*(.*?)\s*$`)

// macRe 用于 macOS airport -s 输出里定位 BSSID（MAC 地址），其前的文本即 SSID
var macRe = regexp.MustCompile(`([0-9a-fA-F]{2}:){5}[0-9a-fA-F]{2}`)

func parseSSID(output string) string {
	m := ssidRe.FindStringSubmatch(output)
	if m == nil {
		return ""
	}
	return m[1]
}

// parseSSIDList 从 `netsh wlan show networks` 的输出解析出可用网络名（去重）
func parseSSIDList(output string) []string {
	seen := map[string]bool{}
	var list []string
	for _, m := range ssidListRe.FindAllStringSubmatch(output, -1) {
		name := strings.TrimSpace(m[1])
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		list = append(list, name)
	}
	return list
}

// parseProfileList 解析 `netsh wlan show profiles` 的输出，取出「已保存的 Wi‑Fi 配置名」。
// 该命令不需要位置权限，是位置服务被禁用时的可靠来源（主/备切换本就只会切到连过的网络）。
// 过滤规则：只取有缩进、含冒号、冒号后有非空值的行；排除表头（冒号后为空）与 `<无>` 占位。
func parseProfileList(output string) []string {
	seen := map[string]bool{}
	var list []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimRight(line, "\r")
		// 配置行以空格/制表符缩进；表头（如“接口 WLAN 上的配置文件:”）顶格，排除
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			continue
		}
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		name := strings.TrimSpace(line[idx+1:])
		if name == "" || name == "<无>" || strings.EqualFold(name, "<none>") {
			continue
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		list = append(list, name)
	}
	return list
}

// currentSSIDViaPowerShell 用 Get-NetConnectionProfile 取当前连接的 Wi‑Fi 名（不依赖位置权限）。
func currentSSIDViaPowerShell() string {
	ps := `[Console]::OutputEncoding=[System.Text.Encoding]::UTF8; ` +
		`$p = Get-NetConnectionProfile | Where-Object { $_.InterfaceAlias -match 'WLAN|Wi-Fi|Wireless|无线' } | Select-Object -First 1; ` +
		`if ($p) { $p.Name }`
	out, _ := runCapture("powershell", "-NoProfile", "-NonInteractive", "-Command", ps)
	return strings.TrimSpace(out)
}

// parseAirportScan 从 macOS `airport -s` 输出解析 SSID（取每个 BSSID 之前的文本）
func parseAirportScan(output string) []string {
	seen := map[string]bool{}
	var list []string
	for _, line := range strings.Split(output, "\n") {
		loc := macRe.FindStringIndex(line)
		if loc == nil {
			continue
		}
		name := strings.TrimSpace(line[:loc[0]])
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		list = append(list, name)
	}
	return list
}

// runCapture 执行命令并捕获 stdout+stderr，即使退出码非 0 也返回已打印的内容。
// 某些系统上 netsh 在异常情况下仍会输出有用信息，不能因退出码就丢弃。
func runCapture(name string, args ...string) (string, error) {
	// hideWindow：GUI 程序调控制台命令时，不隐藏就会闪一个黑窗（甚至弹出 PowerShell 窗口）
	cmd := hideWindow(exec.Command(name, args...))
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.String(), err
}

// condenseOutput 把多行命令输出压成一句话：取最后 lastN 行非空内容。
// netsh 的报错是「一大段位置权限说明 + 真正的错误」，有用信息在末尾。
func condenseOutput(out string, lastN int) string {
	var kept []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			kept = append(kept, line)
		}
	}
	if len(kept) > lastN {
		kept = kept[len(kept)-lastN:]
	}
	return strings.Join(kept, " / ")
}

// scanSSIDs 扫描当前可见的 Wi‑Fi 网络，供 UI 下拉选择，免去手动输入。
func scanSSIDs() ([]string, error) {
	var out string
	var err error
	switch runtime.GOOS {
	case "windows":
		// 优先扫描「附近的网络」（Win11 需开启位置服务）；失败则退回「已保存的网络」（不需位置权限）
		if o, _ := runCapture("netsh", "wlan", "show", "networks"); len(parseSSIDList(o)) > 0 {
			return parseSSIDList(o), nil
		}
		out, err = runCapture("netsh", "wlan", "show", "profiles")
		if list := parseProfileList(out); len(list) > 0 {
			return list, nil
		}
	case "darwin":
		if _, ok := ctrl.(*macWifi); ok {
			out, err = runCapture("/System/Library/PrivateFrameworks/Apple80211.framework/Versions/Current/Resources/airport", "-s")
			if list := parseAirportScan(out); len(list) > 0 {
				return list, nil
			}
		}
	default:
		return nil, fmt.Errorf("不支持的平台: %s", runtime.GOOS)
	}
	if err != nil {
		return nil, fmt.Errorf("扫描失败: %w", err)
	}
	return nil, fmt.Errorf("未发现可用网络（可能 Wi‑Fi 已关闭或无可见热点）")
}

// Windows: netsh（netsh 被位置权限限制时，用 PowerShell Get-NetConnectionProfile 兜底）
type winWifi struct {
	mu       sync.Mutex
	lastSSID string
	lastAt   time.Time
	// netshQuietUntil：netsh 读不到结果后的静默期。
	// 系统关掉位置服务时 netsh 每次都会失败（错误 5），而探测周期只有几秒，
	// 不停重试等于每个周期白起一个进程；静默一段时间后直接走 PowerShell 兜底。
	netshQuietUntil time.Time
	netshWarned     bool
}

const (
	psCacheTTL       = 8 * time.Second
	netshQuietPeriod = 90 * time.Second
)

func (w *winWifi) CurrentSSID() (string, error) {
	w.mu.Lock()
	skipNetsh := time.Now().Before(w.netshQuietUntil)
	w.mu.Unlock()

	if !skipNetsh {
		// 1) netsh wlan show interfaces：位置服务开启时最快，且能读到实时信息
		out, _ := runCapture("netsh", "wlan", "show", "interfaces")
		if ssid := parseSSID(out); ssid != "" {
			w.mu.Lock()
			w.netshQuietUntil = time.Time{}
			w.lastSSID, w.lastAt = ssid, time.Now()
			w.mu.Unlock()
			return ssid, nil
		}
		w.mu.Lock()
		firstTime := !w.netshWarned
		w.netshWarned = true
		w.netshQuietUntil = time.Now().Add(netshQuietPeriod)
		w.mu.Unlock()
		if firstTime {
			logf("netsh 读不到当前网络（多为位置服务被系统限制），改用 PowerShell 兜底；%d 秒内不再重复调 netsh", int(netshQuietPeriod.Seconds()))
		}
	}

	// 2) PowerShell Get-NetConnectionProfile：不依赖位置权限，结果缓存 8s 以免频繁起进程
	w.mu.Lock()
	defer w.mu.Unlock()
	if time.Since(w.lastAt) < psCacheTTL {
		if w.lastSSID != "" {
			return w.lastSSID, nil
		}
		return "", fmt.Errorf("当前无 Wi‑Fi 连接（读取受限：未关联或系统限制）")
	}
	ssid := currentSSIDViaPowerShell()
	w.lastSSID = ssid
	w.lastAt = time.Now()
	if ssid != "" {
		return ssid, nil
	}
	return "", fmt.Errorf("当前无 Wi‑Fi 连接（读取受限：未关联或系统限制）")
}

func (w *winWifi) Connect(ctx context.Context, ssid string) error {
	// 首选：原生 WlanConnect —— 按 profile 直连，不做可用网络枚举，不依赖位置服务。
	// 这是目前唯一能在「位置服务关闭」时跨网络切换的路径（netsh 做不到）。
	if err := wlanAPIConnect(ssid); err != nil {
		logf("原生 WlanConnect 失败: %v，回退 netsh", err)
		out, nerr := hideWindow(exec.CommandContext(ctx, "netsh", "wlan", "connect", "name="+ssid)).CombinedOutput()
		if nerr != nil {
			if msg := condenseOutput(string(out), 3); msg != "" {
				return fmt.Errorf("系统拒绝连接：%s", msg)
			}
			return nerr
		}
	}
	// 返回值只代表「连接请求已被接受」，必须以实际关联结果为准
	// （netsh 曾在未真正切换的情况下返回成功，导致状态机误判）。
	return w.waitAssociated(ctx, ssid, 20*time.Second)
}

// waitAssociated 轮询当前 SSID，直到真的关联到目标网络。
// 这是「切备用成功但实际没切」这类假成功的根本防线。
func (w *winWifi) waitAssociated(ctx context.Context, ssid string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		cur := currentSSIDViaPowerShell()
		if strings.EqualFold(cur, ssid) {
			w.mu.Lock()
			w.lastSSID = cur
			w.lastAt = time.Now()
			w.mu.Unlock()
			return nil
		}
		if time.Now().After(deadline) {
			shown := cur
			if shown == "" {
				shown = "未连接"
			}
			return fmt.Errorf("已提交连接请求，但 %d 秒内未关联到 %q（当前：%s）",
				int(timeout.Seconds()), ssid, shown)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("等待关联超时（目标 %q）", ssid)
		case <-time.After(2 * time.Second):
		}
	}
}

// macOS: airport + networksetup（网卡自动探测，不再硬编码 en0）
type macWifi struct {
	iface string
}

func detectMacWifiInterface() string {
	out, err := hideWindow(exec.Command("networksetup", "-listallhardwareports")).CombinedOutput()
	if err != nil {
		return "en0"
	}
	lines := strings.Split(string(out), "\n")
	for i, line := range lines {
		if strings.Contains(line, "Hardware Port") && strings.Contains(line, "Wi-Fi") {
			if i+1 < len(lines) && strings.Contains(lines[i+1], "Device") {
				parts := strings.SplitN(lines[i+1], ":", 2)
				if len(parts) == 2 {
					return strings.TrimSpace(parts[1])
				}
			}
		}
	}
	return "en0"
}

func (m *macWifi) CurrentSSID() (string, error) {
	out, err := runCapture("/System/Library/PrivateFrameworks/Apple80211.framework/Versions/Current/Resources/airport", "-I")
	ssid := parseSSID(out)
	if ssid == "" {
		return "", fmt.Errorf("未获取到 SSID（airport 输出: %q，错误: %v）", strings.TrimSpace(out), err)
	}
	return ssid, nil
}

func (m *macWifi) Connect(ctx context.Context, ssid string) error {
	return hideWindow(exec.CommandContext(ctx, "networksetup", "-setairportnetwork", m.iface, ssid)).Run()
}

func pickController() wifiController {
	switch runtime.GOOS {
	case "windows":
		return &winWifi{}
	case "darwin":
		return &macWifi{iface: detectMacWifiInterface()}
	default:
		panic("unsupported os: " + runtime.GOOS)
	}
}

// ---------- 托盘图标随状态变色 ----------

func trayResource(mode string) fyne.Resource {
	if mode == "backup" {
		return fyne.NewStaticResource("tray_backup", iconBackupBytes)
	}
	return fyne.NewStaticResource("tray_main", iconMainBytes)
}

func updateTray(mode string) {
	if desk != nil {
		desk.SetSystemTrayIcon(trayResource(mode))
	}
}

// ---------- 状态机主循环 ----------

func monitorLoop() {
	for {
		st.mu.Lock()
		running := st.running
		st.mu.Unlock()
		if !running {
			return
		}
		// 每轮重新读取间隔：用户改完配置立即生效，无需重启
		iv := time.Duration(cfg.CheckInterval) * time.Second
		if iv < 2*time.Second {
			iv = 2 * time.Second
		}
		time.Sleep(iv)

		st.mu.Lock()
		running, paused := st.running, st.paused
		st.mu.Unlock()
		if !running {
			return
		}
		if paused {
			pushStatus(func(s *uiStatus) { s.Note = "已暂停自动切换（仍持续探测）" })
			continue
		}
		checkOnce()
	}
}

func checkOnce() {
	st.mu.Lock()
	mode := st.mode
	st.mu.Unlock()

	ok := httpOK(cfg.HTTPTarget) || pingOK(cfg.PingTarget)
	currentSSID, err := ctrl.CurrentSSID()
	if err != nil {
		msg := fmt.Sprintf("获取 SSID 出错: %v", err)
		if msg != lastSSIDErrMsg { // 去重，仅在状态变化时记一条
			logf("%s", msg)
			lastSSIDErrMsg = msg
		}
		pushStatus(func(s *uiStatus) {
			s.Mode = mode
			s.SSID = ""
			s.Online = onlineState(ok)
		})
		return
	}
	lastSSIDErrMsg = ""

	// 用「实际当前 SSID」校准内部状态：已经在某个已知网络上时直接认定该模式，
	// 避免「其实已在主网、却因切网命令失败永远卡在 backup、反复回切」的死循环。
	st.mu.Lock()
	if currentSSID != "" {
		if cfg.MainSSID != "" && strings.EqualFold(currentSSID, cfg.MainSSID) && st.mode != "main" {
			st.mode = "main"
			st.failCount = 0
			st.mainStableSince = nil
			logf("检测到已连接主网 %q，状态校准为 main", currentSSID)
			updateTray("main")
		} else if cfg.BackupSSID != "" && strings.EqualFold(currentSSID, cfg.BackupSSID) && st.mode != "backup" {
			st.mode = "backup"
			st.failCount = 0
			st.mainStableSince = nil
			logf("检测到已连接备用网 %q，状态校准为 backup", currentSSID)
			updateTray("backup")
		}
	}
	mode = st.mode
	st.mu.Unlock()

	pushStatus(func(s *uiStatus) {
		s.Mode = mode
		s.SSID = currentSSID
		s.Online = onlineState(ok)
		s.Note = ""
	})

	st.mu.Lock()
	switch mode {
	case "main":
		if !ok {
			st.failCount++
			logf("主网探测失败 (%d/%d)", st.failCount, cfg.FailThreshold)
			if st.failCount >= cfg.FailThreshold {
				logf("主网连续失败，切换到备用")
				if e := connectWithRetry(ctrl, cfg.BackupSSID, 2); e != nil {
					logf("切换备用失败: %v", e)
				} else {
					st.mode = "backup"
					st.failCount = 0
					updateTray("backup")
				}
			}
		} else {
			st.failCount = 0
		}
	case "backup":
		if ok {
			if st.mainStableSince == nil {
				now := time.Now()
				st.mainStableSince = &now
				logf("主网恢复，开始冷却计时 (%d 秒)", cfg.RecoverWait)
			} else if time.Since(*st.mainStableSince) >= time.Duration(cfg.RecoverWait)*time.Second {
				logf("主网已稳定，回切主网")
				if e := connectWithRetry(ctrl, cfg.MainSSID, 2); e != nil {
					logf("回切主网失败: %v", e)
				} else {
					st.mode = "main"
					st.mainStableSince = nil
					st.failCount = 0
					updateTray("main")
				}
			}
		} else {
			st.mainStableSince = nil
		}
	}
	st.mu.Unlock()
}

// ---------- 主程序 / UI ----------

func main() {
	cfg = loadConfig()
	ctrl = pickController()

	a := app.NewWithID("com.hoo.wifiautoswitch")
	a.Settings().SetTheme(newUITheme())
	a.SetIcon(fyne.NewStaticResource("icon", iconAppBytes))
	w := a.NewWindow("WiFi 自动切换")
	// 高度必须小于屏幕可用高度（1080p@125% 时约 826 逻辑像素），否则底部操作栏会被顶出屏幕
	w.Resize(fyne.NewSize(660, 640))
	w.CenterOnScreen()

	// ---------- 顶部状态横幅 ----------
	banner, bannerObj := newStatusBanner()

	// ---------- 控件 ----------
	initialOpts := []string{}
	for _, s := range []string{cfg.MainSSID, cfg.BackupSSID} {
		if s != "" && !contains(initialOpts, s) {
			initialOpts = append(initialOpts, s)
		}
	}
	ssidMain := widget.NewSelect(initialOpts, nil)
	ssidMain.PlaceHolder = "选择主用 Wi-Fi"
	ssidBak := widget.NewSelect(initialOpts, nil)
	ssidBak.PlaceHolder = "选择备用 Wi-Fi"
	if cfg.MainSSID != "" {
		ssidMain.SetSelected(cfg.MainSSID)
	}
	if cfg.BackupSSID != "" {
		ssidBak.SetSelected(cfg.BackupSSID)
	}

	interval := widget.NewEntry()
	interval.SetText(strconv.Itoa(cfg.CheckInterval))
	interval.SetPlaceHolder("秒")
	thresh := widget.NewEntry()
	thresh.SetText(strconv.Itoa(cfg.FailThreshold))
	thresh.SetPlaceHolder("次")
	recover := widget.NewEntry()
	recover.SetText(strconv.Itoa(cfg.RecoverWait))
	recover.SetPlaceHolder("秒")

	// 日志区：用 Label 而不是 Disable 的 Entry。
	// Disable 的 Entry 会把文字画成「禁用灰」(ColorNameDisabled)，日志几乎看不清；
	// Label 用默认前景色（深灰 #1E2430），并且没有输入框的边框和光标。
	// 代价是不能拖选，所以卡片头部配了「复制日志」按钮。
	logLabel := widget.NewLabel("")
	logLabel.TextStyle = fyne.TextStyle{Monospace: true}
	logLabel.Wrapping = fyne.TextWrapWord
	logLabel.Importance = widget.MediumImportance
	logScroll := container.NewVScroll(logLabel)
	logScroll.SetMinSize(fyne.NewSize(0, 8*18))

	// 日志消费：带时间戳，只保留最近 400 行，避免无限增长拖慢 UI
	go func() {
		var lines []string
		for line := range logChan {
			lines = append(lines, time.Now().Format("15:04:05")+"  "+line)
			if len(lines) > 400 {
				lines = lines[len(lines)-400:]
			}
			logLabel.SetText(strings.Join(lines, "\n"))
			logScroll.ScrollToBottom()
		}
	}()
	// 状态消费：只刷新横幅，不重建控件
	go func() {
		for s := range statusChan {
			banner.apply(s)
		}
	}()

	// ---------- 手动切换（按钮与托盘菜单共用） ----------
	switchTo := func(mode string) {
		target, label := cfg.MainSSID, "主用"
		if mode == "backup" {
			target, label = cfg.BackupSSID, "备用"
		}
		if target == "" {
			logf("未配置%s Wi-Fi，无法切换", label)
			pushStatus(func(s *uiStatus) { s.Note = "未配置" + label + " Wi-Fi：请先在上方选择并保存" })
			return
		}
		logf("手动切%s：%s", label, target)
		pushStatus(func(s *uiStatus) { s.Note = "正在切换到" + label + "：" + target })
		go func() {
			if err := connectWithRetry(ctrl, target, 2); err != nil {
				logf("切%s失败: %v", label, err)
				pushStatus(func(s *uiStatus) { s.Note = "切" + label + "失败：" + err.Error() })
				return
			}
			st.mu.Lock()
			st.mode = mode
			st.failCount = 0
			st.mainStableSince = nil
			st.mu.Unlock()
			updateTray(mode)
			logf("已切到%s（%s）", label, target)
			pushStatus(func(s *uiStatus) { s.Mode = mode; s.SSID = target; s.Note = "" })
		}()
	}

	// ---------- 底部操作栏按钮 ----------
	saveBtn := widget.NewButtonWithIcon("保存配置", theme.DocumentSaveIcon(), func() {
		c := cfg
		c.MainSSID = ssidMain.Selected
		c.BackupSSID = ssidBak.Selected
		setInt := func(text string, dst *int, min int) {
			if v, err := strconv.Atoi(strings.TrimSpace(text)); err == nil && v >= min {
				*dst = v
			}
		}
		setInt(interval.Text, &c.CheckInterval, 2)
		setInt(thresh.Text, &c.FailThreshold, 1)
		setInt(recover.Text, &c.RecoverWait, 0)

		if c.MainSSID == "" {
			dialog.ShowError(fmt.Errorf("请先选择「主用 Wi-Fi」"), w)
			return
		}
		if c.BackupSSID != "" && c.MainSSID == c.BackupSSID {
			dialog.ShowError(fmt.Errorf("主用与备用不能是同一个网络"), w)
			return
		}
		if err := saveConfig(c); err != nil {
			dialog.ShowError(err, w)
			return
		}
		cfg = c
		// 回填规范化后的数值，让界面与实际生效值一致
		interval.SetText(strconv.Itoa(c.CheckInterval))
		thresh.SetText(strconv.Itoa(c.FailThreshold))
		recover.SetText(strconv.Itoa(c.RecoverWait))
		logf("配置已保存：主用=%q 备用=%q 间隔=%ds 阈值=%d 冷却=%ds",
			c.MainSSID, c.BackupSSID, c.CheckInterval, c.FailThreshold, c.RecoverWait)
		pushStatus(func(s *uiStatus) { s.Note = "配置已保存，新参数已生效" })
	})
	saveBtn.Importance = widget.HighImportance

	manMain := widget.NewButtonWithIcon("切到主用", theme.HomeIcon(), func() { switchTo("main") })
	manBak := widget.NewButtonWithIcon("切到备用", theme.MediaSkipNextIcon(), func() { switchTo("backup") })

	pauseBtn := widget.NewButtonWithIcon("暂停监控", theme.MediaPauseIcon(), nil)
	pauseBtn.Importance = widget.LowImportance
	pauseBtn.OnTapped = func() {
		st.mu.Lock()
		st.paused = !st.paused
		paused := st.paused
		st.mu.Unlock()
		if paused {
			pauseBtn.SetText("继续监控")
			pauseBtn.SetIcon(theme.MediaPlayIcon())
			logf("已暂停自动切换（不再自动切网，仍持续探测）")
		} else {
			pauseBtn.SetText("暂停监控")
			pauseBtn.SetIcon(theme.MediaPauseIcon())
			logf("已恢复自动切换")
		}
	}

	// ---------- 扫描（按钮与启动自动扫描复用） ----------
	doScan := func() {
		list, err := scanSSIDs()
		if err != nil {
			logf("扫描网络失败: %v", err)
			pushStatus(func(s *uiStatus) { s.Note = "扫描失败：" + err.Error() })
			return
		}
		// 保留已保存的 SSID（即使本次未扫到，例如隐藏网络），追加到列表末尾
		opts := append([]string{}, list...)
		for _, saved := range []string{cfg.MainSSID, cfg.BackupSSID} {
			if saved != "" && !contains(opts, saved) {
				opts = append(opts, saved)
			}
		}
		ssidMain.SetOptions(opts)
		ssidBak.SetOptions(opts)
		if cfg.MainSSID != "" {
			ssidMain.SetSelected(cfg.MainSSID)
		}
		if cfg.BackupSSID != "" {
			ssidBak.SetSelected(cfg.BackupSSID)
		}
		logf("已加载 %d 个网络（附近扫描或已保存配置）", len(list))
		pushStatus(func(s *uiStatus) {
			s.Note = fmt.Sprintf("已加载 %d 个网络，选择主用/备用后点「保存配置」", len(list))
		})
	}
	var scanBtn *widget.Button
	scanBtn = widget.NewButtonWithIcon("重新扫描", theme.ViewRefreshIcon(), func() {
		scanBtn.Disable()
		scanBtn.SetText("扫描中…")
		go func() {
			doScan()
			scanBtn.SetText("重新扫描")
			scanBtn.Enable()
		}()
	})

	// 日志是 Label，不能拖选，所以给一个「复制全部日志」按钮
	copyLogBtn := widget.NewButtonWithIcon("", theme.ContentCopyIcon(), func() {
		w.Clipboard().SetContent(logLabel.Text)
		logf("日志已复制到剪贴板")
	})
	copyLogBtn.Importance = widget.LowImportance

	clearLogBtn := widget.NewButtonWithIcon("", theme.ContentClearIcon(), func() { logLabel.SetText("") })
	clearLogBtn.Importance = widget.LowImportance

	// ---------- 布局 ----------
	cardNet := newNetworkCard(ssidMain, ssidBak, scanBtn)
	cardPolicy := newPolicyCard(interval, thresh, recover)
	cardLog := newLogCard(logScroll, copyLogBtn, clearLogBtn)
	actionBar := newActionBar(saveBtn, manMain, manBak, pauseBtn)

	w.SetContent(assembleRoot(bannerObj, cardNet, cardPolicy, cardLog, actionBar))

	// 关闭窗口 -> 最小化到托盘，而非退出（托盘常驻）
	w.SetCloseIntercept(func() {
		w.Hide()
	})

	// ---------- 系统托盘（Windows / macOS） ----------
	if d, ok := a.(desktop.App); ok {
		desk = d
		menu := fyne.NewMenu("WiFi 切换",
			fyne.NewMenuItem("打开设置", func() { w.Show() }),
			fyne.NewMenuItemSeparator(),
			fyne.NewMenuItem("切到主用 Wi-Fi", func() { go switchTo("main") }),
			fyne.NewMenuItem("切到备用 Wi-Fi", func() { go switchTo("backup") }),
			fyne.NewMenuItemSeparator(),
			fyne.NewMenuItem("退出", func() {
				st.mu.Lock()
				st.running = false
				st.mu.Unlock()
				a.Quit()
			}),
		)
		d.SetSystemTrayMenu(menu)
		d.SetSystemTrayIcon(trayResource(st.mode))
	}

	// 启动后自动扫描一次可用网络，填充下拉框
	go doScan()

	go monitorLoop()
	w.ShowAndRun()
}
