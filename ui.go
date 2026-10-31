package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

// ---------- 统一的字号梯度（改这里就能整体缩放界面文字） ----------
const (
	fsCardTitle = 14 // 卡片标题
	fsLabel     = 11 // 字段小标题
	fsTiny      = 10 // 辅助说明
)

// ---------- 通用间距 / 排版小工具 ----------

// pad 按四边自定义内边距包裹对象。
func pad(obj fyne.CanvasObject, top, bottom, left, right float32) fyne.CanvasObject {
	return container.New(layout.NewCustomPaddedLayout(top, bottom, left, right), obj)
}

// gapY 垂直方向固定高度的空白，用来给卡片之间留呼吸感。
func gapY(h float32) fyne.CanvasObject {
	r := canvas.NewRectangle(color.Transparent)
	r.SetMinSize(fyne.NewSize(0, h))
	return r
}

// textC 生成一枚可直接放进布局的文字（字号、字重、颜色可控）。
func textC(s string, size float32, c color.Color, bold bool) *canvas.Text {
	t := canvas.NewText(s, c)
	t.TextSize = size
	t.TextStyle = fyne.TextStyle{Bold: bold}
	return t
}

// caption 字段小标题：小号灰字，放在控件上方。
func caption(s string) fyne.CanvasObject {
	return textC(s, fsLabel, cFgMuted, false)
}

// fieldRow 一个字段：上面是小标题，下面是控件。
func fieldRow(label string, ctrl fyne.CanvasObject) fyne.CanvasObject {
	return container.NewVBox(caption(label), gapY(2), ctrl)
}

// divider 1px 浅色分隔线（不用 widget.Separator，颜色更可控）。
func divider() fyne.CanvasObject {
	r := canvas.NewRectangle(cBorder)
	r.SetMinSize(fyne.NewSize(0, 1))
	return r
}

// cardBg 圆角白底 + 1px 描边。
func cardBg() *canvas.Rectangle {
	bg := canvas.NewRectangle(cCard)
	bg.CornerRadius = 10
	bg.StrokeColor = cBorder
	bg.StrokeWidth = 1
	return bg
}

// sectionCard 圆角白卡片：标题 + 副标题 + 内容，自带内边距与描边。
func sectionCard(title, subtitle string, body fyne.CanvasObject) fyne.CanvasObject {
	return sectionCardAction(title, subtitle, nil, body)
}

// sectionCardAction 同上，但可在标题行右侧放一个操作按钮（如「重新扫描」）。
func sectionCardAction(title, subtitle string, action fyne.CanvasObject, body fyne.CanvasObject) fyne.CanvasObject {
	head := container.NewVBox(textC(title, fsCardTitle, cFg, true))
	if subtitle != "" {
		head.Add(textC(subtitle, fsLabel, cFgMuted, false))
	}
	header := head
	if action != nil {
		header = container.NewBorder(nil, nil, nil, action, head)
	}

	inner := container.NewVBox(header, gapY(7), body)
	return container.NewMax(cardBg(), pad(inner, 11, 11, 13, 13))
}

// ---------- 三个功能区卡片 ----------

func newNetworkCard(ssidMain, ssidBak *widget.Select, scanBtn *widget.Button) fyne.CanvasObject {
	// 两个下拉并排，避免上下堆叠把卡片撑得过高
	grid := container.NewGridWithColumns(2,
		fieldRow("主用 Wi-Fi", ssidMain),
		fieldRow("备用 Wi-Fi", ssidBak),
	)
	return sectionCardAction("网络", "列表来自附近网络与已保存的网络", scanBtn,
		container.NewVBox(grid))
}

func newPolicyCard(interval, thresh, recover *widget.Entry) fyne.CanvasObject {
	grid := container.NewGridWithColumns(3,
		fieldRow("检测间隔（秒）", interval),
		fieldRow("失败阈值（次）", thresh),
		fieldRow("回切冷却（秒）", recover),
	)
	return sectionCard("切换策略", "连续失败达到阈值才切备用；主用稳定超过冷却时间才回切，避免来回乒乓",
		container.NewVBox(grid))
}

func newLogCard(logView fyne.CanvasObject, copyBtn, clearBtn *widget.Button) fyne.CanvasObject {
	head := container.NewHBox(
		textC("运行日志", fsCardTitle, cFg, true),
		layout.NewSpacer(),
		textC("最近 400 条", fsTiny, cFgFaint, false),
		copyBtn,
		clearBtn,
	)

	// 内凹面板：浅色底 + 细描边，把日志区和白色卡片区分开（文字本身用深色前景）
	panel := canvas.NewRectangle(color.NRGBA{R: 0xF8, G: 0xFA, B: 0xFD, A: 0xFF})
	panel.CornerRadius = 8
	panel.StrokeColor = cBorder
	panel.StrokeWidth = 1
	body := container.NewMax(panel, pad(logView, 7, 7, 9, 7))

	return container.NewMax(cardBg(),
		pad(container.NewBorder(head, nil, nil, nil, body), 9, 9, 11, 11))
}

func newActionBar(saveBtn, manMain, manBak, pauseBtn *widget.Button) fyne.CanvasObject {
	row := container.NewHBox(saveBtn, manMain, manBak, layout.NewSpacer(), pauseBtn)
	return container.NewVBox(divider(), pad(row, 8, 6, 14, 14))
}

// assembleRoot 组装整个窗口：
// 顶部状态横幅、底部操作栏钉死在窗口上下边缘（永远可见），中间配置区按内容自然高度、
// 日志卡片吃掉剩余高度。这样窗口一旦缩到小于内容高度，被压缩的只是日志，按钮不会跑丢。
func assembleRoot(bannerObj, netCard, policyCard, logCard, actionBar fyne.CanvasObject) fyne.CanvasObject {
	settings := pad(container.NewVBox(netCard, gapY(10), policyCard), 2, 10, 14, 14)
	body := container.NewBorder(settings, nil, nil, nil, pad(logCard, 0, 10, 14, 14))

	return container.NewBorder(
		pad(bannerObj, 12, 4, 14, 14),
		actionBar,
		nil, nil,
		body,
	)
}
