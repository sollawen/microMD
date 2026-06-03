package md

import "github.com/micro-editor/tcell/v2"

var codeblockBgStyle tcell.Style = tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(tcell.ColorDimGray)

// RenderCodeBlock 渲染代码块。Step 0 只输出背景色。
func RenderCodeBlock(lines []string, width int, cfg MDConfig) *RenderedSegment {
	return renderLinesWithBg(lines, width, codeblockBgStyle)
}
