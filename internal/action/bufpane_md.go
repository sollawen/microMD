package action

import (
	"github.com/micro-editor/micro/v2/internal/buffer"
	"github.com/micro-editor/micro/v2/internal/config"
	"github.com/micro-editor/micro/v2/internal/display"
	"github.com/micro-editor/micro/v2/internal/md"
)

// initMDConfig 在创建 BufWindow 后立刻同步 MD 渲染配置。
// 仅当 buffer 是 MD 文件时才生效，非 MD 文件为 no-op。
// 该函数封装所有 MD 相关逻辑，让 bufpane.go 的 NewBufPaneFromBuf 保持简洁。
func initMDConfig(buf *buffer.Buffer, w *display.BufWindow) {
	if !buf.IsMD { // 单一真源（NewBuffer 算了 IsMarkdownFile）
		return
	}
	w.SetMDConfig(md.MDConfig{
		MDRender:      config.GetGlobalOption("mdrender").(bool),
		MDRenderIdle:  config.GetGlobalOption("mdrenderidle").(float64),
		MDTableAlign:  buf.Settings["mdtablealign"].(bool),
		MDTableBorder: buf.Settings["mdtableborder"].(bool),
		MDBoldItalic:  buf.Settings["mdbolditalic"].(bool),
		MDCodeBlock:   buf.Settings["mdcodeblock"].(bool),
		MDHeading:     buf.Settings["mdheading"].(bool),
		MDList:        buf.Settings["mdlist"].(bool),
		MDLink:        buf.Settings["mdlink"].(bool),
	})
	// Step 1.0：MD 文件默认进入编辑模式（光标所在 segment 回退原生显示）
	w.SetEditMode(true)
}
