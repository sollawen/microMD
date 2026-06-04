package md

import "github.com/micro-editor/tcell/v2"

var listBgStyle tcell.Style = tcell.StyleDefault.Background(tcell.ColorDarkSeaGreen)

// RenderList 渲染列表。Step 0 只输出背景色。
func RenderList(lines []string, width int, cfg MDConfig) *RenderedSegment {
	return renderLinesWithBg(lines, width, listBgStyle)
}
