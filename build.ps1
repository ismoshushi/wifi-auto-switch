# 本地打包脚本（Windows，需先装 fyne CLI 与 MinGW-w64 gcc）
#   go install fyne.io/fyne/v2/cmd/fyne@latest
#   MinGW-w64 的 gcc.exe 需在 PATH 中

go mod tidy
fyne package -os windows -icon assets/icon.png -name "WiFiAutoSwitch" -appVersion 0.1.0
Write-Host "done"
