package md

import "github.com/micro-editor/tcell/v2"

var headingBgStyle tcell.Style = tcell.StyleDefault.Background(tcell.Color(17))

// RenderHeading 渲染标题行。Step 0 只输出背景色。
func RenderHeading(lines []string, width int, cfg MDConfig) *RenderedSegment {
	return renderLinesWithBg(lines, width, headingBgStyle)
}
