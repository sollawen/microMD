package md

import "github.com/micro-editor/tcell/v2"

var paragraphBgStyle tcell.Style = tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(tcell.Color236)

// RenderParagraph 渲染普通段落行。Step 0 用深灰背景标识。
func RenderParagraph(lines []string, width int, cfg MDConfig) *RenderedSegment {
	return renderLinesWithBg(lines, width, paragraphBgStyle)
}
