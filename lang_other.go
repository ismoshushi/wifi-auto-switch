//go:build !windows

package main

import (
	"os"
	"strings"
)

// systemLanguage 在 macOS / Linux 上按 POSIX 惯例读环境变量：
// 以 zh 开头视为中文，其余非空值视为英文，全都取不到时默认英文。
func systemLanguage() string {
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		v := os.Getenv(k)
		if v == "" {
			continue
		}
		if strings.HasPrefix(strings.ToLower(v), "zh") {
			return LangZH
		}
		return LangEN
	}
	return LangEN
}
