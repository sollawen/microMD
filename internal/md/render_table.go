package md

import "github.com/micro-editor/tcell/v2"

var tableBgStyle tcell.Style = tcell.StyleDefault.Background(tcell.Color(22))

// RenderTable 渲染表格。Step 0 只输出背景色。
func RenderTable(lines []string, width int, cfg MDConfig) *RenderedSegment {
	return renderLinesWithBg(lines, width, tableBgStyle)
}
