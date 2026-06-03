package md

import "github.com/micro-editor/tcell/v2"

// renderInline 处理行内的 Markdown 标记（加粗、斜体、链接等）。
// 输入一行字符串，输出 []Cell。
// Step 0 是空壳，直接原样输出。
func renderInline(line string, baseStyle tcell.Style, bufLineOffset int) []Cell {
	cells := make([]Cell, 0, len(line))
	for x, r := range line {
		cells = append(cells, Cell{
			Rune:    r,
			Style:   baseStyle,
			BufLine: bufLineOffset,
			BufX:    x,
		})
	}
	return cells
}
