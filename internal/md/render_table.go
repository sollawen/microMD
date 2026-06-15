package md

import (
	"strings"
	"github.com/mattn/go-runewidth"
	"github.com/micro-editor/tcell/v2"
)

// alignType 表示单元格对齐方式
type alignType int

const (
	alignLeft alignType = iota
	alignCenter
	alignRight
)

// parsedTable 解析后的表格结构
type parsedTable struct {
	numCols     int
	sepIdx      int   // separator 行在 lines[] 中的相对索引
	header      []string
	body        [][]string
	alignments  []alignType
}

// splitCells 把 "| a | b | c |" 拆成 ["a", "b", "c"]。
// 处理：首尾有/无 |、空 cell、转义 \|。
func splitCells(line string) []string {
	// 去掉首尾的 |
	trimmed := strings.Trim(line, "|")
	if trimmed == "" {
		return nil
	}

	var cells []string
	start := 0
	escaped := false
	for i, ch := range trimmed {
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		if ch == '|' {
			cell := strings.TrimSpace(trimmed[start:i])
			cells = append(cells, cell)
			start = i + 1
		}
	}
	// 处理最后一个 cell
	if start <= len(trimmed) {
		cell := strings.TrimSpace(trimmed[start:])
		cells = append(cells, cell)
	}

	return cells
}

// isSeparatorLineStr 判断行是否为 Markdown 表格分隔行
func isSeparatorLineStr(line string) bool {
	trimmed := strings.TrimSpace(line)
	trimmed = strings.TrimPrefix(trimmed, "|")
	trimmed = strings.TrimSuffix(trimmed, "|")
	trimmed = strings.TrimSpace(trimmed)

	if len(trimmed) == 0 {
		return false
	}

	// 去掉空格和竖线，只保留 - 和 :
	var cleaned string
	for _, r := range trimmed {
		if r == ' ' || r == '|' {
			continue
		} else if r == '-' || r == ':' {
			cleaned += string(r)
		} else {
			return false
		}
	}

	if len(cleaned) == 0 {
		return false
	}

	// 验证至少有 dash
	for _, r := range cleaned {
		if r == '-' {
			return true
		}
	}
	return false
}

// parseAlignments 从 separator 行解析对齐方式。
// :--- → left, :---: → center, ---: → right
// v0.1 全返回 left，函数骨架先写好。
func parseAlignments(sepLine string, numCols int) []alignType {
	alignments := make([]alignType, numCols)
	// v0.1: 全部默认为 left
	for i := range alignments {
		alignments[i] = alignLeft
	}
	return alignments
}

// parseTable 解析 Markdown 表格结构
func parseTable(lines []string) *parsedTable {
	if len(lines) == 0 {
		return nil
	}

	// 1. 遍历 lines，找 separator 行
	sepIdx := -1
	for i, line := range lines {
		if isSeparatorLineStr(line) {
			sepIdx = i
			break
		}
	}

	if sepIdx < 0 || sepIdx == 0 {
		return nil // 没有 separator 或 separator 在第一行
	}

	// 2. 确定 numCols
	sepCells := splitCells(lines[sepIdx])
	numCols := len(sepCells)
	if numCols == 0 {
		return nil
	}

	// 3. 解析 header（标准 MD 只有一行 header，取最后一行）
	header := splitCells(lines[sepIdx-1])
	// 补空到 numCols
	for len(header) < numCols {
		header = append(header, "")
	}

	// 4. 解析 body
	var body [][]string
	for i := sepIdx + 1; i < len(lines); i++ {
		cells := splitCells(lines[i])
		// 补空到 numCols
		for len(cells) < numCols {
			cells = append(cells, "")
		}
		body = append(body, cells)
	}

	// 5. 解析 alignments
	alignments := parseAlignments(lines[sepIdx], numCols)

	return &parsedTable{
		numCols:    numCols,
		sepIdx:     sepIdx,
		header:     header,
		body:       body,
		alignments: alignments,
	}
}

// calcNatWidths 计算每列的自然宽度（最大 cell 视觉宽度）。
// 输入：pt.header + pt.body 的所有 cell
// 输出：natWidths[i] = 第 i 列的最大 runewidth.StringWidth
func calcNatWidths(pt *parsedTable) []int {
	if pt == nil || pt.numCols == 0 {
		return nil
	}

	// 初始化为最小值 1
	maxWidths := make([]int, pt.numCols)
	for i := range maxWidths {
		maxWidths[i] = 1
	}

	// 遍历 header
	for i, cell := range pt.header {
		if i >= pt.numCols {
			break
		}
		w := runewidth.StringWidth(cell)
		if w > maxWidths[i] {
			maxWidths[i] = w
		}
	}

	// 遍历 body
	for _, row := range pt.body {
		for i, cell := range row {
			if i >= pt.numCols {
				break
			}
			w := runewidth.StringWidth(cell)
			if w > maxWidths[i] {
				maxWidths[i] = w
			}
		}
	}

	return maxWidths
}

// cellPadding 是 cell 内容左右各 1 空格的 padding
const cellPadding = 1

// tableFrameworkWidth 计算表格框架的固定开销
// 结构：│ [sp] content [sp] │ ... │
// = numCols + 1 个竖线 + 2*numCols 个 padding 空格 = 3*numCols + 1
func tableFrameworkWidth(numCols int) int {
	return (numCols + 1) + 2*cellPadding*numCols
}

// colEntry 是 waterlineShrink 用的临时结构
type colEntry struct {
	origIdx int
	width   int
}

// waterlineShrink 把 natWidths 缩减到总和 = target。
// 返回保持原始列顺序的结果。
// 每列最小值 clamp 到 2（至少能显示 2 个字符）。
func waterlineShrink(target int, natWidths []int) []int {
	if target <= 0 || len(natWidths) == 0 {
		return nil
	}

	// 克隆一份，防止修改原数组
	widths := make([]int, len(natWidths))
	copy(widths, natWidths)

	// 计算需要缩减的总量
	sum := 0
	for _, w := range widths {
		sum += w
	}
	if sum <= target {
		return widths
	}

	toShrink := sum - target

	// 构建排序后的结构（按宽度降序）
	entries := make([]colEntry, len(widths))
	for i, w := range widths {
		entries[i] = colEntry{origIdx: i, width: w}
	}

	// 降序排序
	for i := 0; i < len(entries)-1; i++ {
		for j := i + 1; j < len(entries); j++ {
			if entries[j].width > entries[i].width {
				entries[i], entries[j] = entries[j], entries[i]
			}
		}
	}

	// 水位线算法
	prefixCount := 1
	result := make([]int, len(widths))
	copy(result, widths)

	for i := 0; i < len(entries)-1 && toShrink > 0; i++ {
		gap := entries[i].width - entries[i+1].width
		canRelease := gap * prefixCount

		if canRelease >= toShrink {
			// 够了：当前 prefixCount 列各缩 toShrink/prefixCount
			perCol := toShrink / prefixCount
			rem := toShrink % prefixCount
			for j := 0; j < prefixCount; j++ {
				result[entries[j].origIdx] -= perCol
				if j < rem {
					result[entries[j].origIdx]--
				}
			}
			toShrink = 0
			break
		}

		// 不够：把这 prefixCount 列拉到 entries[i+1] 的高度
		for j := 0; j < prefixCount; j++ {
			result[entries[j].origIdx] = entries[i+1].width
		}
		toShrink -= canRelease
		prefixCount++
	}

	// 如果循环结束 toShrink > 0：所有列等比缩减
	if toShrink > 0 {
		perCol := toShrink / len(entries)
		rem := toShrink % len(entries)
		for i := range result {
			result[i] -= perCol
			if i < rem {
				result[i]--
			}
		}
	}

	// clamp 到最小值 2
	for i := range result {
		if result[i] < 2 {
			result[i] = 2
		}
	}

	return result
}

// allocColWidths 根据可用宽度分配列宽
// totalAvail < 2*len(natWidths) → return nil（退化）
// sum(natWidths) <= totalAvail → return copy(natWidths)
// else → return waterlineShrink(totalAvail, natWidths)
func allocColWidths(totalAvail int, natWidths []int) []int {
	if len(natWidths) == 0 {
		return nil
	}

	// 情况 C：极端窄屏（每列连 2 个字符都分不到）
	if totalAvail <= len(natWidths)*2 {
		return nil
	}

	sum := 0
	for _, w := range natWidths {
		sum += w
	}

	// 情况 A：放得下
	if sum <= totalAvail {
		result := make([]int, len(natWidths))
		copy(result, natWidths)
		return result
	}

	// 情况 B：水位线缩减
	return waterlineShrink(totalAvail, natWidths)
}

// cellRuneOffsets 返回每个 cell 内容在原始行中的 rune 起始偏移。
// 逻辑与 splitCells 对齐（找 | 分隔符，跳过 | 和前后空格）。
// 注意：全部用 rune index 计算，不能用 byte offset（CJK 多字节字符会错位）。
func cellRuneOffsets(line string) []int {
	trimmed := strings.Trim(line, "|")
	if trimmed == "" {
		return nil
	}

	// 计算 trimmed 在原始 line 中的 rune 起始偏移
	prefixRunes := 0
	for _, r := range line {
		if r == '|' {
			prefixRunes++
		} else {
			break
		}
	}

	// 转成 rune 数组，用 rune index 操作（避免 byte offset 与 rune index 混淆）
	runes := []rune(trimmed)

	var offsets []int
	start := 0
	escaped := false
	for i, ch := range runes {
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		if ch == '|' {
			// 跳过 cell 前导空格，找到第一个非空格 rune 的 index
			cellStart := start
			for cellStart < i {
				if runes[cellStart] != ' ' && runes[cellStart] != '\t' {
					break
				}
				cellStart++
			}
			offsets = append(offsets, prefixRunes+cellStart)
			start = i + 1
		}
	}
	// 最后一个 cell
	cellStart := start
	for cellStart < len(runes) {
		if runes[cellStart] != ' ' && runes[cellStart] != '\t' {
			break
		}
		cellStart++
	}
	offsets = append(offsets, prefixRunes+cellStart)

	return offsets
}

// adjustRowBufX 修正 rowCells 中每个 Cell 的 BufX，加上 cell 在原始行中的偏移。
func adjustRowBufX(rc rowCells, offsets []int) {
	for cellIdx, cellLines := range rc {
		if cellIdx >= len(offsets) {
			break
		}
		off := offsets[cellIdx]
		for _, line := range cellLines {
			for i := range line {
				if line[i].BufX >= 0 {
					line[i].BufX += off
				}
			}
		}
	}
}

// rowCells 是一个 buffer 行渲染后的中间结果。
// rowCells[cellIdx] = 该 cell 的 screen 行列表
// rowCells[cellIdx][lineIdx] = 该 cell 第 lineIdx 行的 cells（renderInline 后的裸 Cell，未 padding）
// padding 在 assembleRow 统一加。
type rowCells [][][]Cell

// maxLines 返回该行所有 cell 中最大的 screen 行数。
func (rc rowCells) maxLines() int {
	m := 1
	for _, cellLines := range rc {
		if len(cellLines) > m {
			m = len(cellLines)
		}
	}
	return m
}

// renderCell 把一个 cell 的文本渲染成多行内容。
// 返回 [][]Cell：cellLines[k] = 第 k 个 screen 行的 cells。
func renderCell(content string, colWidth int, lineIdx int, style tcell.Style, cfg MDConfig) [][]Cell {
	// 1. renderInline(content, style, cfg, lineIdx) → []Cell
	cells := renderInline(content, style, cfg, lineIdx)
	// 2. wrapCellsRaw(cells, colWidth) → [][]Cell
	return wrapCellsRaw(cells, colWidth)
}

// renderRow 渲染一个 buffer 行的所有 cells，返回 rowCells。
func renderRow(cells []string, colWidths []int, lineIdx int, style tcell.Style, cfg MDConfig) rowCells {
	result := make(rowCells, len(cells))
	for i, cellContent := range cells {
		if i >= len(colWidths) {
			break
		}
		result[i] = renderCell(cellContent, colWidths[i], lineIdx, style, cfg)
	}
	return result
}

// fillCellsToWidth 把 cells 填充到 targetWidth 列。
// 处理 CJK 占位 Cell + 尾部空格。
func fillCellsToWidth(cells []Cell, targetWidth int, padStyle tcell.Style) []Cell {
	result := make([]Cell, 0, targetWidth+5)
	col := 0
	for _, c := range cells {
		rw := runewidth.RuneWidth(c.Rune)
		result = append(result, c)
		col += rw
		if rw == 2 {
			// CJK 占位 Cell（不额外增加 col，rw 已含两列）
			result = append(result, Cell{
				Rune:    ' ',
				Style:   padStyle,
				BufLine: c.BufLine,
				BufX:    -1,
			})
		}
	}
	// 补空格到 targetWidth
	for ; col < targetWidth; col++ {
		result = append(result, Cell{
			Rune:    ' ',
			Style:   padStyle,
			BufLine: -1,
			BufX:    -1,
		})
	}
	return result
}

// assembleRow 把一个 buffer 行的 rowCells 横拼成 []RenderedRow。
// maxH = 该行需要的 screen 行数（= rc.maxLines()）
// colWidths = 各列宽度
// bufLine = buffer 行号（相对），用于 BufLine 映射
// contentStyle = 内容区样式
// borderStyle = 竖线样式
func assembleRow(rc rowCells, maxH int, colWidths []int, bufLine int,
	contentStyle, borderStyle tcell.Style, width int) []RenderedRow {

	result := make([]RenderedRow, maxH)

	for screenLine := 0; screenLine < maxH; screenLine++ {
		row := RenderedRow{
			BufLine: bufLine,
			Cells:   make([]Cell, 0, width),
		}

		for cellIdx, cellLines := range rc {
			// 左竖线（第一个 cell）或中间竖线
			row.Cells = append(row.Cells, Cell{
				Rune:         '│',
				Style:        borderStyle,
				BufLine:      -1,
				BufX:         -1,
				IsDecorative: true,
			})
			// 左 padding
			row.Cells = append(row.Cells, Cell{
				Rune:         ' ',
				Style:        contentStyle,
				BufLine:      -1,
				BufX:         -1,
				IsDecorative: true,
			})

			// cell 内容（或空行填充）
			var contentCells []Cell
			if screenLine < len(cellLines) {
				contentCells = cellLines[screenLine]
			}

			// fillCellsToWidth: 把 contentCells 填充到 colWidths[cellIdx]
			if cellIdx < len(colWidths) {
				filled := fillCellsToWidth(contentCells, colWidths[cellIdx], contentStyle)
				row.Cells = append(row.Cells, filled...)
			}

			// 右 padding
			row.Cells = append(row.Cells, Cell{
				Rune:         ' ',
				Style:        contentStyle,
				BufLine:      -1,
				BufX:         -1,
				IsDecorative: true,
			})
		}
		// 最后一个 cell 的右竖线
		row.Cells = append(row.Cells, Cell{
			Rune:         '│',
			Style:        borderStyle,
			BufLine:      -1,
			BufX:         -1,
			IsDecorative: true,
		})

		// 右侧补空格到 width
		for len(row.Cells) < width {
			row.Cells = append(row.Cells, Cell{
				Rune:         ' ',
				Style:        contentStyle,
				BufLine:      -1,
				BufX:         -1,
				IsDecorative: true,
			})
		}
		if len(row.Cells) > width {
			row.Cells = row.Cells[:width]
		}

		result[screenLine] = row
	}

	return result
}

// makeTableTopBorder 生成 ┌─┬─┐ 顶边框
// 表格右边界处画 ┐，右侧补空格到 width。
func makeTableTopBorder(colWidths []int, width int, style tcell.Style, spaceStyle tcell.Style) RenderedRow {
	row := RenderedRow{
		BufLine: -1,
		Cells:   make([]Cell, 0, width),
	}

	// 左上角 ┌
	row.Cells = append(row.Cells, Cell{
		Rune:         '┌',
		Style:        style,
		BufLine:      -1,
		BufX:         -1,
		IsDecorative: true,
	})

	// 每个 cell：─ * (colWidth + 2)，cell 间用 ┬ 连接
	for i, cw := range colWidths {
		if i > 0 {
			row.Cells = append(row.Cells, Cell{
				Rune:         '┬',
				Style:        style,
				BufLine:      -1,
				BufX:         -1,
				IsDecorative: true,
			})
		}
		for j := 0; j < cw+2*cellPadding; j++ {
			row.Cells = append(row.Cells, Cell{
				Rune:         '─',
				Style:        style,
				BufLine:      -1,
				BufX:         -1,
				IsDecorative: true,
			})
		}
	}

	// 右上角 ┐
	row.Cells = append(row.Cells, Cell{
		Rune:         '┐',
		Style:        style,
		BufLine:      -1,
		BufX:         -1,
		IsDecorative: true,
	})

	// 右侧补空格到 width
	for len(row.Cells) < width {
		row.Cells = append(row.Cells, Cell{
			Rune:         ' ',
			Style:        spaceStyle,
			BufLine:      -1,
			BufX:         -1,
			IsDecorative: true,
		})
	}
	if len(row.Cells) > width {
		row.Cells = row.Cells[:width]
	}

	return row
}

// makeTableBottomBorder 生成 └─┴─┘ 底边框
// 表格右边界处画 ┘，右侧补空格到 width。
func makeTableBottomBorder(colWidths []int, width int, style tcell.Style, spaceStyle tcell.Style) RenderedRow {
	row := RenderedRow{
		BufLine: -1,
		Cells:   make([]Cell, 0, width),
	}

	// 左下角 └
	row.Cells = append(row.Cells, Cell{
		Rune:         '└',
		Style:        style,
		BufLine:      -1,
		BufX:         -1,
		IsDecorative: true,
	})

	// 每个 cell：─ * (colWidth + 2)，cell 间用 ┴ 连接
	for i, cw := range colWidths {
		if i > 0 {
			row.Cells = append(row.Cells, Cell{
				Rune:         '┴',
				Style:        style,
				BufLine:      -1,
				BufX:         -1,
				IsDecorative: true,
			})
		}
		for j := 0; j < cw+2*cellPadding; j++ {
			row.Cells = append(row.Cells, Cell{
				Rune:         '─',
				Style:        style,
				BufLine:      -1,
				BufX:         -1,
				IsDecorative: true,
			})
		}
	}

	// 右下角 ┘
	row.Cells = append(row.Cells, Cell{
		Rune:         '┘',
		Style:        style,
		BufLine:      -1,
		BufX:         -1,
		IsDecorative: true,
	})

	// 右侧补空格到 width
	for len(row.Cells) < width {
		row.Cells = append(row.Cells, Cell{
			Rune:         ' ',
			Style:        spaceStyle,
			BufLine:      -1,
			BufX:         -1,
			IsDecorative: true,
		})
	}
	if len(row.Cells) > width {
		row.Cells = row.Cells[:width]
	}

	return row
}

// makeTableSeparator 生成 ├─┼─┤ 分隔线
// 表格右边界处画 ┤，右侧补空格到 width。
// bufLine 参数：header 分隔线传 pt.sepIdx（对应 buffer 行），行间分隔线传 -1（纯装饰）。
func makeTableSeparator(colWidths []int, width int, style tcell.Style, spaceStyle tcell.Style, bufLine int) RenderedRow {
	row := RenderedRow{
		BufLine: bufLine,
		Cells:   make([]Cell, 0, width),
	}

	// 左端 ├
	row.Cells = append(row.Cells, Cell{
		Rune:         '├',
		Style:        style,
		BufLine:      -1,
		BufX:         -1,
		IsDecorative: true,
	})

	// 每个 cell：─ * (colWidth + 2)，cell 间用 ┼ 连接
	for i, cw := range colWidths {
		if i > 0 {
			row.Cells = append(row.Cells, Cell{
				Rune:         '┼',
				Style:        style,
				BufLine:      -1,
				BufX:         -1,
				IsDecorative: true,
			})
		}
		for j := 0; j < cw+2*cellPadding; j++ {
			row.Cells = append(row.Cells, Cell{
				Rune:         '─',
				Style:        style,
				BufLine:      -1,
				BufX:         -1,
				IsDecorative: true,
			})
		}
	}

	// 右端 ┤
	row.Cells = append(row.Cells, Cell{
		Rune:         '┤',
		Style:        style,
		BufLine:      -1,
		BufX:         -1,
		IsDecorative: true,
	})

	// 右侧补空格到 width
	for len(row.Cells) < width {
		row.Cells = append(row.Cells, Cell{
			Rune:         ' ',
			Style:        spaceStyle,
			BufLine:      -1,
			BufX:         -1,
			IsDecorative: true,
		})
	}
	if len(row.Cells) > width {
		row.Cells = row.Cells[:width]
	}

	return row
}
// RenderTable 渲染 Markdown 表格。
// 完整实现：解析 → 列宽分配 → cell 渲染 → 行对齐 → 横向拼装 → 装饰行
func RenderTable(seg Segment, width int, cfg MDConfig) *RenderedSegment {
	lines := linesFromBuf(cfg.Buf, seg.BufStartLine, seg.BufEndLine)
	// 0. 极端情况：lines 为空 → 退化
	if len(lines) == 0 {
		return renderLinesWithBg(lines, width, cfg.Colorscheme.DefStyle)
	}

	// 1. 解析
	pt := parseTable(lines)
	if pt == nil || pt.numCols == 0 {
		return renderLinesWithBg(lines, width, cfg.Colorscheme.DefStyle)
	}
	if !cfg.MDTableAlign {
		pt.alignments = make([]alignType, pt.numCols) // 全 alignLeft
	}

	// 2. 自然宽度
	natWidths := calcNatWidths(pt)
	if natWidths == nil {
		return renderLinesWithBg(lines, width, cfg.Colorscheme.DefStyle)
	}

	// 3. 分配列宽
	framework := tableFrameworkWidth(pt.numCols)
	availContent := width - framework
	colWidths := allocColWidths(availContent, natWidths)
	if colWidths == nil {
		return renderLinesWithBg(lines, width, cfg.Colorscheme.DefStyle) // 极端窄屏退化
	}

	result := &RenderedSegment{
		BufStartLine: 0,
		BufEndLine:   len(lines) - 1,
	}

	// 样式
	contentStyle := tcell.StyleDefault
	borderStyle := resolveStyle(cfg.Colorscheme.Styles, "md-frame", cfg.Colorscheme.DefStyle)

	// 4. 顶边框（可选）
	if cfg.MDTableBorder {
		result.Rows = append(result.Rows, makeTableTopBorder(colWidths, width, borderStyle, contentStyle))
	}

	// 5. Header 行
	headerRow := renderRow(pt.header, colWidths, pt.sepIdx-1, contentStyle.Bold(true), cfg)
	adjustRowBufX(headerRow, cellRuneOffsets(lines[pt.sepIdx-1]))
	maxH := headerRow.maxLines()
	result.Rows = append(result.Rows,
		assembleRow(headerRow, maxH, colWidths, pt.sepIdx-1, contentStyle.Bold(true), borderStyle, width)...)

	// 6. 分隔线（header 和 body 之间必有）
	// header 分隔线对应 buffer 行 pt.sepIdx
	result.Rows = append(result.Rows, makeTableSeparator(colWidths, width, borderStyle, contentStyle, pt.sepIdx))

	// 7. Body 行（行间插分隔线）
	for bodyIdx, bodyCells := range pt.body {
		bufLine := pt.sepIdx + 1 + bodyIdx // 相对行号
		rowRc := renderRow(bodyCells, colWidths, bufLine, contentStyle, cfg)
		adjustRowBufX(rowRc, cellRuneOffsets(lines[pt.sepIdx+1+bodyIdx]))
		maxH := rowRc.maxLines()
		result.Rows = append(result.Rows,
			assembleRow(rowRc, maxH, colWidths, bufLine, contentStyle, borderStyle, width)...)
		// 行间分隔线（最后一行不画，底边框会兜底；无边框时也画，保持行间分隔）
		// 行间分隔线是纯装饰，无对应 buffer 行
		if bodyIdx < len(pt.body)-1 {
			result.Rows = append(result.Rows, makeTableSeparator(colWidths, width, borderStyle, contentStyle, -1))
		}
	}

	// 8. 底边框（可选）
	if cfg.MDTableBorder {
		result.Rows = append(result.Rows, makeTableBottomBorder(colWidths, width, borderStyle, contentStyle))
	}

	return result
}
