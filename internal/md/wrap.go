package md

import (
	"github.com/mattn/go-runewidth"
	"github.com/micro-editor/tcell/v2"
)

// wrapCells 把一行的 cells 按 width 自动换行成多个 RenderedRow。
// 被 RenderNormal / RenderList / RenderBlockquote 复用。
//
// padStyle 用于填充/占位 Cell 的样式。
//
// 算法：word wrap（按词分组，以空格 Cell 为分界）。
// - 逐词尝试放入当前行：放得下追加，放不下补空格开新行。
// - 单个词超过 width 则强制按字符截断（CJK 等走此路径）。
// - 每行 Cells 数量 = width。
// - 空行返回一个填充空格的 Row（不返回零个 Row）。
// - 行尾/行首空格丢弃。
//
// BufLine 规则：Row.BufLine 始终保留真实 bufLine；
// Cell.BufLine 始终保留正确映射。Cell.BufX：真实字符保留原始偏移，填充/CJK占位为 -1。
func wrapCells(cells []Cell, width int, bufLine int, padStyle tcell.Style) []RenderedRow {
	// 空行 → 返回一个填充空格的 Row
	if len(cells) == 0 {
		return []RenderedRow{makePaddingRow(width, bufLine, padStyle)}
	}

	// 按"词"分组（以空格 Cell 为分界，行尾/行首空格丢弃）
	words := splitWords(cells)

	var rows []RenderedRow
	curCells := make([]Cell, 0, width)
	curWidth := 0
	isFirstRow := true

	for _, word := range words {
		wordWidth := cellDisplayWidth(word)

		if len(curCells) == 0 {
			// 当前行为空，直接放词
			if wordWidth > width {
				// 词本身超 width → 按字符截断，输出完整行
				chunk := truncateToWidth(word, width)
				curCells = append(curCells, chunk...)
				curWidth = width
				rows = appendRow(rows, curCells, width, bufLine, padStyle, isFirstRow)
				isFirstRow = false
				curCells = nil
				curWidth = 0
				// 剩余部分继续
				remaining := word[len(chunk):]
				processOverlong(remaining, width, bufLine, padStyle, &rows, &curCells, &curWidth, &isFirstRow)
			} else {
				curCells = append(curCells, word...)
				curWidth = wordWidth
			}
		} else {
			// 当前行非空，尝试追加
			spaceNeeded := curWidth + 1 + wordWidth // 1 = 词间空格
			if spaceNeeded <= width {
				// 放得下：追加空格 + 词
				curCells = append(curCells, padCell(bufLine, padStyle))
				curWidth++
				curCells = append(curCells, word...)
				curWidth += wordWidth
			} else {
				// 放不下：输出当前行，开新行
				rows = appendRow(rows, curCells, width, bufLine, padStyle, isFirstRow)
				isFirstRow = false
				curCells = nil
				curWidth = 0
				// 把词放到新行
				if wordWidth > width {
					processOverlong(word, width, bufLine, padStyle, &rows, &curCells, &curWidth, &isFirstRow)
				} else {
					curCells = append(curCells, word...)
					curWidth = wordWidth
				}
			}
		}
	}

	// 处理最后一行
	if len(curCells) > 0 {
		rows = appendRow(rows, curCells, width, bufLine, padStyle, isFirstRow)
	} else if len(rows) == 0 {
		// 极端情况：所有内容被丢弃（如全是空格），保证至少一个 Row
		rows = appendRow(rows, nil, width, bufLine, padStyle, isFirstRow)
	}

	return rows
}

// processOverlong 处理超长词的逐行截断。
func processOverlong(word []Cell, width int, bufLine int, padStyle tcell.Style,
	rows *[]RenderedRow, curCells *[]Cell, curWidth *int, isFirstRow *bool) {

	remaining := word
	for len(remaining) > 0 {
		chunk := truncateToWidth(remaining, width)
		if len(chunk) == 0 {
			break
		}
		*rows = appendRow(*rows, chunk, width, bufLine, padStyle, *isFirstRow)
		*isFirstRow = false
		remaining = remaining[len(chunk):]
	}
	*curCells = nil
	*curWidth = 0
}

// splitWords 按"词"分组：以空格 Cell 为分界，行尾/行首空格丢弃。
func splitWords(cells []Cell) [][]Cell {
	var words [][]Cell
	start := -1

	for i, c := range cells {
		if c.Rune == ' ' {
			if start >= 0 {
				words = append(words, cells[start:i])
				start = -1
			}
		} else {
			if start < 0 {
				start = i
			}
		}
	}
	if start >= 0 {
		words = append(words, cells[start:])
	}

	return words
}

// cellDisplayWidth 计算 cells 的总显示宽度（含 CJK 宽字符）。
func cellDisplayWidth(cells []Cell) int {
	w := 0
	for _, c := range cells {
		w += runewidth.RuneWidth(c.Rune)
	}
	return w
}

// truncateToWidth 从 cells 开头截取恰好不超过 maxWidth 显示宽度的 cells。
func truncateToWidth(cells []Cell, maxWidth int) []Cell {
	var result []Cell
	col := 0
	for _, c := range cells {
		rw := runewidth.RuneWidth(c.Rune)
		if col+rw > maxWidth {
			break
		}
		result = append(result, c)
		col += rw
	}
	return result
}

// padCell 返回一个空格填充 Cell。
func padCell(bufLine int, style tcell.Style) Cell {
	return Cell{
		Rune:    ' ',
		Style:   style,
		BufLine: bufLine,
		BufX:    -1,
	}
}

// makePaddingRow 创建一个全空格填充的 Row。
func makePaddingRow(width int, bufLine int, style tcell.Style) RenderedRow {
	row := RenderedRow{
		BufLine: bufLine,
		Cells:   make([]Cell, width),
	}
	for i := range row.Cells {
		row.Cells[i] = Cell{
			Rune:    ' ',
			Style:   style,
			BufLine: bufLine,
			BufX:    -1,
		}
	}
	return row
}

// appendRow 把 cells 填充到 width 并追加到 rows。
// 处理 CJK 占位 Cell 和行尾填充。
func appendRow(rows []RenderedRow, cells []Cell, width int, bufLine int, padStyle tcell.Style, isFirstRow bool) []RenderedRow {
	row := RenderedRow{
		BufLine: bufLine,
		Cells:   make([]Cell, 0, width),
	}

	col := 0
	for _, c := range cells {
		rw := runewidth.RuneWidth(c.Rune)
		row.Cells = append(row.Cells, c)
		col += rw
		if rw == 2 {
			// CJK 宽字符后的占位 Cell（不额外增加 col，rw 已含两列）
			row.Cells = append(row.Cells, Cell{
				Rune:    ' ',
				Style:   padStyle,
				BufLine: c.BufLine,
				BufX:    -1,
			})
		}
	}

	// 填充到 width
	for ; col < width; col++ {
		row.Cells = append(row.Cells, Cell{
			Rune:    ' ',
			Style:   padStyle,
			BufLine: bufLine,
			BufX:    -1,
		})
	}

	return append(rows, row)
}
