package main

import (
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// TestRenderPreview 用 Fyne 的软件驱动把界面渲染成 PNG，
// 用于在没有显示器的环境下核对排版与字体效果（中英各两张）。
// 运行：go test -run TestRenderPreview -v
func TestRenderPreview(t *testing.T) {
	a := test.NewApp()
	a.Settings().SetTheme(newUITheme())

	opts := []string{"Home-WiFi", "Home-WiFi-5G", "Neighbor-2G"}
	ssidMain := widget.NewSelect(opts, nil)
	ssidMain.PlaceHolder = T("ph.main")
	ssidMain.SetSelected("Home-WiFi")
	ssidBak := widget.NewSelect(opts, nil)
	ssidBak.PlaceHolder = T("ph.backup")
	ssidBak.SetSelected("Neighbor-2G")

	interval := widget.NewEntry()
	interval.SetText("5")
	interval.SetPlaceHolder(T("ph.sec"))
	thresh := widget.NewEntry()
	thresh.SetText("3")
	thresh.SetPlaceHolder(T("ph.times"))
	recover := widget.NewEntry()
	recover.SetText("60")
	recover.SetPlaceHolder(T("ph.sec"))

	logLabel := widget.NewLabel("")
	logLabel.TextStyle = fyne.TextStyle{Monospace: true}
	logLabel.Wrapping = fyne.TextWrapWord
	logScroll := container.NewVScroll(logLabel)
	logScroll.SetMinSize(fyne.NewSize(0, 8*18))

	scanBtn := buttonT("btn.rescan", theme.ViewRefreshIcon(), nil)
	copyBtn := widget.NewButtonWithIcon("", theme.ContentCopyIcon(), nil)
	copyBtn.Importance = widget.LowImportance
	clearBtn := widget.NewButtonWithIcon("", theme.ContentClearIcon(), nil)
	clearBtn.Importance = widget.LowImportance
	saveBtn := buttonT("btn.save", theme.DocumentSaveIcon(), nil)
	saveBtn.Importance = widget.HighImportance
	manMain := buttonT("btn.toMain", theme.HomeIcon(), nil)
	manBak := buttonT("btn.toBackup", theme.MediaSkipNextIcon(), nil)
	pauseBtn := buttonT("btn.pause", theme.MediaPauseIcon(), nil)
	pauseBtn.Importance = widget.LowImportance
	langBtn := widget.NewButton(T("btn.lang"), nil)
	langBtn.Importance = widget.LowImportance
	onLangChange(func() { langBtn.SetText(T("btn.lang")) })

	banner, bannerObj := newStatusBanner()

	root := assembleRoot(bannerObj,
		newNetworkCard(ssidMain, ssidBak, scanBtn),
		newPolicyCard(interval, thresh, recover),
		newLogCard(logScroll, copyBtn, clearBtn),
		newActionBar(saveBtn, manMain, manBak, pauseBtn, langBtn),
	)

	w := test.NewWindow(root)
	w.Resize(fyne.NewSize(660, 640))

	// 同一个界面连续渲染四种组合：靠 setLang + refreshLang 切换，和运行时点击按钮完全同一条路径
	shots := []struct {
		lang string
		file string
		mode string
		ssid string
		on   int
		note func() string
	}{
		{LangZH, "screenshot-main.png", "main", "Home-WiFi", 1, func() string { return T("note.scanDone", 3) }},
		{LangZH, "screenshot-backup.png", "backup", "Neighbor-2G", -1, func() string { return T("note.switching", T("mode.main"), "Home-WiFi") }},
		{LangEN, "screenshot-en-main.png", "main", "Home-WiFi", 1, func() string { return T("note.scanDone", 3) }},
		{LangEN, "screenshot-en-backup.png", "backup", "Neighbor-2G", -1, func() string { return T("note.switching", T("mode.main"), "Home-WiFi") }},
	}
	for _, sh := range shots {
		setLang(sh.lang)
		refreshLang()
		w.SetTitle(T("app.title"))
		w.Content().Refresh()
		logLabel.SetText(sampleLog())
		// 状态文案必须切完语言再取，否则会带着上一种语言的字符串截图
		banner.apply(uiStatus{Mode: sh.mode, SSID: sh.ssid, Online: sh.on, Note: sh.note()})
		capture(t, w, sh.file)
	}
}

// sampleLog 用真实的日志文案拼几行示例，保证截图里的日志也是当前语言。
func sampleLog() string {
	stamps := []string{"20:01:33", "20:01:36", "20:02:10", "20:02:15", "20:02:20", "20:02:23", "20:03:00"}
	lines := []string{
		T("log.manualSwitch", T("mode.main"), "Home-WiFi"),
		T("log.switchedTo", T("mode.main"), "Home-WiFi"),
		T("log.mainProbeFail", 1, 3),
		T("log.mainProbeFail", 2, 3),
		T("log.mainFailSwitch"),
		T("log.switchedTo", T("mode.backup"), "Neighbor-2G"),
		T("log.mainRecovered", 60),
	}
	var b strings.Builder
	for i, l := range lines {
		b.WriteString(stamps[i] + "  " + l + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func capture(t *testing.T, w fyne.Window, name string) {
	if err := os.MkdirAll("docs", 0o755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	img := w.Canvas().Capture()
	f, err := os.Create(filepath.Join("docs", name))
	if err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("encode: %v", err)
	}
	t.Logf("saved %s (%dx%d)", name, img.Bounds().Dx(), img.Bounds().Dy())
}

// TestCatalogCoverage 守住两件事：文案表里每条都有中英两版；代码里用到的 key 都真实存在。
// 少一条在界面上就是直接显示 key 本身，很难在肉眼验收时发现，所以交给测试拦。
func TestCatalogCoverage(t *testing.T) {
	for k, m := range catalog {
		if strings.TrimSpace(m.zh) == "" {
			t.Errorf("key %q 缺中文文案", k)
		}
		if strings.TrimSpace(m.en) == "" {
			t.Errorf("key %q 缺英文文案", k)
		}
		// 两版的占位符数量必须一致，否则某一语言会打出 %!s(MISSING)
		if n, m2 := strings.Count(m.zh, "%"), strings.Count(m.en, "%"); n != m2 {
			t.Errorf("key %q 占位符数量不一致: zh=%d en=%d", k, n, m2)
		}
	}

	useRe := regexp.MustCompile(`(?:T|Terr|textT|buttonT)\("([a-z][a-zA-Z0-9.]*)"`)
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || f == "i18n.go" {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range useRe.FindAllStringSubmatch(string(src), -1) {
			if _, ok := catalog[m[1]]; !ok {
				t.Errorf("%s 用了不存在的文案 key %q", f, m[1])
			}
		}
	}
}
