package md

import (
	"path/filepath"
	"strings"
)

// MDConfig 持有所有 MD 渲染相关的配置。
// 由 BufPane 构造时从 config/buffer 层读取，塞到 BufWindow 上。
// display 和 md 包通过这个结构解耦，不直接 import config.
type MDConfig struct {
	MDRender      bool    // 功能总开关
	MDRenderIdle  float64 // 编辑模式超时秒数（Step 1 用）
	MDTableAlign  bool    // 表格对齐
	MDTableBorder bool    // 表格外框
	MDBoldItalic  bool    // 加粗斜体渲染
	MDCodeBlock   bool    // 代码块渲染
	MDHeading     bool    // 标题渲染
	MDList        bool    // 列表渲染
	MDLink        bool    // 链接渲染
}

// DefaultMDConfig 返回默认配置。
func DefaultMDConfig() MDConfig {
	return MDConfig{
		MDRender:      true,
		MDRenderIdle:  10,
		MDTableAlign:  true,
		MDTableBorder: false,
		MDBoldItalic:  true,
		MDCodeBlock:   true,
		MDHeading:     true,
		MDList:        true,
		MDLink:        true,
	}
}

// IsMarkdownFile 判断文件路径是否为 Markdown 文件。
// BufPane 创建 BufWindow 时调用一次。
func IsMarkdownFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".md" || ext == ".markdown"
}
