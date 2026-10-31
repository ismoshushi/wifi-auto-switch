#!/usr/bin/env bash
# 交叉/本地打包脚本（需先装 fyne CLI 与 CGO 工具链）
#   go install fyne.io/fyne/v2/cmd/fyne@latest
#   Windows 目标：需 MinGW-w64(gcc) 并在 PATH
#   macOS   目标：需 Xcode Command Line Tools
set -e

echo "==> go mod tidy"
go mod tidy

echo "==> 打包 Windows"
fyne package -os windows -icon assets/icon.png -name "WiFiAutoSwitch" -appVersion 0.1.0

echo "==> 打包 macOS (arm64)"
fyne package -os darwin -arch arm64 -icon assets/icon.png -name "WiFiAutoSwitch" -appVersion 0.1.0

echo "done"
