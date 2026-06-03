package md

import "github.com/micro-editor/tcell/v2"

var hrBgStyle tcell.Style = tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(tcell.ColorDarkRed)

// RenderHR 渲染分割线。Step 0 只输出背景色。
func RenderHR(lines []string, width int, cfg MDConfig) *RenderedSegment {
	return renderLinesWithBg(lines, width, hrBgStyle)
}
