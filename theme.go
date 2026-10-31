package main

import (
	"image/color"
	"os"
	"path/filepath"
	"runtime"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// ---------- 柔和中性的浅色配色 ----------
var (
	cBg      = color.NRGBA{R: 0xF4, G: 0xF6, B: 0xFA, A: 0xFF} // 页面底色
	cCard    = color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF} // 卡片
	cBorder  = color.NRGBA{R: 0xE2, G: 0xE7, B: 0xEF, A: 0xFF} // 描边 / 分割线
	cFg      = color.NRGBA{R: 0x1E, G: 0x24, B: 0x30, A: 0xFF} // 主文字
	cFgMuted = color.NRGBA{R: 0x6B, G: 0x76, B: 0x88, A: 0xFF} // 次要文字
	cFgFaint = color.NRGBA{R: 0x98, G: 0xA2, B: 0xB3, A: 0xFF} // 占位 / 提示
	cPrimary = color.NRGBA{R: 0x2C, G: 0x63, B: 0xF0, A: 0xFF} // 主色（蓝）
	cButton  = color.NRGBA{R: 0xEE, G: 0xF1, B: 0xF7, A: 0xFF} // 普通按钮
)

// uiTheme 在 Fyne 默认主题之上覆盖字号、间距、配色与中文字体。
// 重点是「字体统一」：默认主题用不含中文字形的 Noto Sans，中文要靠系统字体回退，
// 两套字体度量不一致 → 中文行高、基线看起来是“偏”的。这里显式指定一款中文字体，
// 让中英文出自同一套度量。
type uiTheme struct {
	base fyne.Theme

	regular fyne.Resource
	bold    fyne.Resource
	italic  fyne.Resource
}

func newUITheme() fyne.Theme {
	t := &uiTheme{base: theme.DefaultTheme()}

	// 每种字重挑一款 TTF（.ttc 集合不被 Fyne 的解析器支持，必须用 .ttf）
	reg := firstFont(
		`C:\Windows\Fonts\Deng.ttf`,
		`C:\Windows\Fonts\NotoSansSC-VF.ttf`,
		`C:\Windows\Fonts\simhei.ttf`,
		`/Library/Fonts/Arial Unicode.ttf`,
	)
	bold := firstFont(
		`C:\Windows\Fonts\Dengb.ttf`,
		`C:\Windows\Fonts\NotoSansSC-VF.ttf`,
		`C:\Windows\Fonts\simhei.ttf`,
	)
	if reg != nil {
		t.regular = reg
		if bold == nil {
			t.bold = reg
		} else {
			t.bold = bold
		}
		// 中文没有真正的斜体字形，斜体沿用常规体，避免渲染成方块
		t.italic = reg
	}
	return t
}

// firstFont 读取第一个存在且是合法 TrueType 字体的文件。
func firstFont(paths ...string) fyne.Resource {
	for _, p := range paths {
		if runtime.GOOS != "windows" && runtime.GOOS != "darwin" {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil || len(data) < 12 {
			continue
		}
		// TrueType 轮廓的 sfnt 版本号：0x00010000 或 "true"
		if !(data[0] == 0x00 && data[1] == 0x01 && data[2] == 0x00 && data[3] == 0x00) &&
			!(data[0] == 't' && data[1] == 'r' && data[2] == 'u' && data[3] == 'e') {
			continue
		}
		return fyne.NewStaticResource(filepath.Base(p), data)
	}
	return nil
}

func (t *uiTheme) Font(style fyne.TextStyle) fyne.Resource {
	// 等宽字体保留默认（时间戳对齐），中文由系统字体回退补齐
	if style.Monospace {
		return t.base.Font(style)
	}
	if t.regular == nil {
		return t.base.Font(style)
	}
	switch {
	case style.Bold && style.Italic:
		return t.bold
	case style.Bold:
		return t.bold
	case style.Italic:
		return t.italic
	default:
		return t.regular
	}
}

func (t *uiTheme) Icon(n fyne.ThemeIconName) fyne.Resource {
	return t.base.Icon(n)
}

// ---------- 字号与间距：比默认略收紧，保证一屏放得下 ----------
func (t *uiTheme) Size(n fyne.ThemeSizeName) float32 {
	switch n {
	case theme.SizeNameText:
		return 13
	case theme.SizeNameHeadingText:
		return 16
	case theme.SizeNameSubHeadingText:
		return 14
	case theme.SizeNameCaptionText:
		return 11
	case theme.SizeNamePadding:
		return 4
	case theme.SizeNameInnerPadding:
		return 7
	case theme.SizeNameLineSpacing:
		return 3
	case theme.SizeNameInputRadius:
		return 7
	case theme.SizeNameSelectionRadius:
		return 6
	case theme.SizeNameInputBorder:
		return 1
	case theme.SizeNameSeparatorThickness:
		return 1
	case theme.SizeNameScrollBar:
		return 10
	case theme.SizeNameScrollBarSmall:
		return 3
	}
	return t.base.Size(n)
}

// ---------- 配色 ----------
func (t *uiTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	switch n {
	case theme.ColorNameBackground:
		return cBg
	case theme.ColorNameForeground:
		return cFg
	case theme.ColorNameButton:
		return cButton
	case theme.ColorNameDisabledButton:
		return color.NRGBA{R: 0xF3, G: 0xF5, B: 0xF9, A: 0xFF}
	case theme.ColorNameDisabled:
		return color.NRGBA{R: 0xAE, G: 0xB6, B: 0xC2, A: 0xFF}
	case theme.ColorNamePlaceHolder:
		return cFgFaint
	case theme.ColorNamePrimary:
		return cPrimary
	case theme.ColorNameForegroundOnPrimary:
		return color.White
	case theme.ColorNameFocus:
		return color.NRGBA{R: 0x2C, G: 0x63, B: 0xF0, A: 0x55}
	case theme.ColorNameSelection:
		return color.NRGBA{R: 0x2C, G: 0x63, B: 0xF0, A: 0x28}
	case theme.ColorNameHover:
		return color.NRGBA{R: 0x10, G: 0x18, B: 0x28, A: 0x0D}
	case theme.ColorNamePressed:
		return color.NRGBA{R: 0x10, G: 0x18, B: 0x28, A: 0x1A}
	case theme.ColorNameInputBackground:
		// 比卡片略深一点，控件在白色卡片上才有边界感
		return color.NRGBA{R: 0xF6, G: 0xF8, B: 0xFC, A: 0xFF}
	case theme.ColorNameInputBorder:
		return color.NRGBA{R: 0xD4, G: 0xDC, B: 0xE8, A: 0xFF}
	case theme.ColorNameSeparator:
		return cBorder
	case theme.ColorNameShadow:
		return color.NRGBA{R: 0x10, G: 0x18, B: 0x28, A: 0x14}
	case theme.ColorNameScrollBar:
		return color.NRGBA{R: 0x10, G: 0x18, B: 0x28, A: 0x33}
	case theme.ColorNameMenuBackground:
		return cCard
	case theme.ColorNameOverlayBackground:
		return cCard
	case theme.ColorNameHeaderBackground:
		return cBg
	case theme.ColorNameHyperlink:
		return cPrimary
	case theme.ColorNameSuccess:
		return color.NRGBA{R: 0x16, G: 0xA3, B: 0x4A, A: 0xFF}
	case theme.ColorNameWarning:
		return color.NRGBA{R: 0xD9, G: 0x77, B: 0x06, A: 0xFF}
	case theme.ColorNameError:
		return color.NRGBA{R: 0xDC, G: 0x26, B: 0x26, A: 0xFF}
	}
	return t.base.Color(n, v)
}
