// Package view 通过 go:embed 嵌入 HTML 模板，使二进制自包含（无需运行时携带源码目录）。
package view

import "embed"

// FS 持有 internal/view 下的全部 HTML 模板。
//
//go:embed *.html
var FS embed.FS
