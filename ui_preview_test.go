package main

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// TestRenderPreview 用 Fyne 的软件驱动把界面渲染成 PNG，
// 用于在没有显示器的环境下核对排版与字体效果。
// 运行：go test -run TestRenderPreview -v
func TestRenderPreview(t *testing.T) {
	a := test.NewApp()
	a.Settings().SetTheme(newUITheme())

	opts := []string{"Home-WiFi", "Home-WiFi-5G", "Neighbor-2G"}
	ssidMain := widget.NewSelect(opts, nil)
	ssidMain.SetSelected("Home-WiFi")
	ssidBak := widget.NewSelect(opts, nil)
	ssidBak.SetSelected("Neighbor-2G")

	interval := widget.NewEntry()
	interval.SetText("5")
	thresh := widget.NewEntry()
	thresh.SetText("3")
	recover := widget.NewEntry()
	recover.SetText("60")

	logLabel := widget.NewLabel("20:01:33  手动切主用：Home-WiFi\n" +
		"20:01:36  已切到主用（Home-WiFi）\n" +
		"20:02:10  主网探测失败 (1/3)\n" +
		"20:02:15  主网探测失败 (2/3)\n" +
		"20:02:20  主网连续失败，切换到备用\n" +
		"20:02:23  已切到备用（Neighbor-2G）\n" +
		"20:03:00  主网恢复，开始冷却计时 (60 秒)")
	logLabel.TextStyle = fyne.TextStyle{Monospace: true}
	logLabel.Wrapping = fyne.TextWrapWord
	logScroll := container.NewVScroll(logLabel)
	logScroll.SetMinSize(fyne.NewSize(0, 8*18))

	scanBtn := widget.NewButtonWithIcon("重新扫描", theme.ViewRefreshIcon(), nil)
	copyBtn := widget.NewButtonWithIcon("", theme.ContentCopyIcon(), nil)
	copyBtn.Importance = widget.LowImportance
	clearBtn := widget.NewButtonWithIcon("", theme.ContentClearIcon(), nil)
	clearBtn.Importance = widget.LowImportance
	saveBtn := widget.NewButtonWithIcon("保存配置", theme.DocumentSaveIcon(), nil)
	saveBtn.Importance = widget.HighImportance
	manMain := widget.NewButtonWithIcon("切到主用", theme.HomeIcon(), nil)
	manBak := widget.NewButtonWithIcon("切到备用", theme.MediaSkipNextIcon(), nil)
	pauseBtn := widget.NewButtonWithIcon("暂停监控", theme.MediaPauseIcon(), nil)
	pauseBtn.Importance = widget.LowImportance

	banner, bannerObj := newStatusBanner()
	banner.apply(uiStatus{Mode: "main", SSID: "Home-WiFi", Online: 1, Note: "已加载 3 个网络，选择主用/备用后点「保存配置」"})

	root := assembleRoot(bannerObj,
		newNetworkCard(ssidMain, ssidBak, scanBtn),
		newPolicyCard(interval, thresh, recover),
		newLogCard(logScroll, copyBtn, clearBtn),
		newActionBar(saveBtn, manMain, manBak, pauseBtn),
	)

	w := test.NewWindow(root)
	w.Resize(fyne.NewSize(660, 640))
	capture(t, w, "preview_main.png")

	// 备用状态：验证横幅换色与文案
	banner.apply(uiStatus{Mode: "backup", SSID: "Neighbor-2G", Online: -1, Note: "正在切换到主用：Home-WiFi"})
	capture(t, w, "preview_backup.png")
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
