package md

import "github.com/micro-editor/tcell/v2"

var blockquoteBgStyle tcell.Style = tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(tcell.ColorDarkMagenta)

// RenderBlockquote 渲染引用块。Step 0 只输出背景色。
func RenderBlockquote(lines []string, width int, cfg MDConfig) *RenderedSegment {
	return renderLinesWithBg(lines, width, blockquoteBgStyle)
}
