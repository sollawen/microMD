package md

import "github.com/micro-editor/tcell/v2"

// RenderParagraph 渲染普通段落行。Step 0 无特殊处理，使用默认样式。
func RenderParagraph(lines []string, width int, cfg MDConfig) *RenderedSegment {
	return renderLinesWithBg(lines, width, tcell.StyleDefault)
}
