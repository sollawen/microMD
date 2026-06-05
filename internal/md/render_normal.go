package md

import "github.com/micro-editor/tcell/v2"

var normalBgStyle tcell.Style = tcell.StyleDefault.Background(tcell.Color236)

// RenderNormal 渲染普通文本行。Step 0 用深灰背景标识。
func RenderNormal(lines []string, width int, cfg MDConfig) *RenderedSegment {
	return renderLinesWithBg(lines, width, normalBgStyle)
}
