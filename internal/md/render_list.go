package md

import (
	"github.com/micro-editor/tcell/v2"
)

// marker 类型常量
const (
	markerBullet = iota
	markerCheckboxOff
	markerCheckboxOn
	markerOrdered
)

// listItemInfo 存储解析后的列表项信息
type listItemInfo struct {
	indentWidth      int    // 前导缩进的显示宽度（空格=1，tab=4）
	indentRuneCount  int    // 前导缩进的 rune 数量（用于跳过到 marker 位置）
	markerType       int    // markerBullet / markerCheckboxOff / markerCheckboxOn / markerOrdered
	markerRuneCount  int    // 原始 marker 在行中的 rune 数（含后面紧跟的空格）
	contentStartRune int    // content 在原始行中的起始 rune 偏移
	content          string // marker 之后的文本
}

// parseListItem 逐 rune 扫描一行，解析出列表项的各段信息
func parseListItem(line string) listItemInfo {
	info := listItemInfo{
		markerType: -1, // 默认值，-1 表示不匹配列表模式
	}

	runes := []rune(line)
	runeCount := len(runes)
	if runeCount == 0 {
		return info
	}

	// 1. 统计前导缩进（空格计1，tab计4）
	indentWidth := 0
	indentRuneCount := 0
	for i := 0; i < runeCount; i++ {
		if runes[i] == ' ' {
			indentWidth++
			indentRuneCount++
		} else if runes[i] == '\t' {
			indentWidth += 4
			indentRuneCount++
		} else {
			break
		}
	}
	info.indentWidth = indentWidth
	info.indentRuneCount = indentRuneCount

	// 2. 跳过缩进 rune（空格和 tab），找到第一个非空白 rune
	pos := 0
	for pos < runeCount && (runes[pos] == ' ' || runes[pos] == '\t') {
		pos++
	}
	if pos >= runeCount {
		return info
	}

	first := runes[pos]

	// 3. 判断 marker 类型
	if first == '-' || first == '*' || first == '+' {
		// bullet 或 checkbox
		if pos+5 < runeCount {
			// 检查 "- [ ] " (rune 序列: ' ','[',' ',']',' ')
			if runes[pos+1] == ' ' && runes[pos+2] == '[' && runes[pos+3] == ' ' && runes[pos+4] == ']' && runes[pos+5] == ' ' {
				info.markerType = markerCheckboxOff
				info.markerRuneCount = 6 // "- [ ] " = 6 runes
			} else if runes[pos+1] == ' ' && runes[pos+2] == '[' && runes[pos+3] == 'x' && runes[pos+4] == ']' && runes[pos+5] == ' ' {
				// checkbox: "- [x] "
				info.markerType = markerCheckboxOn
				info.markerRuneCount = 6 // "- [x] " = 6 runes
			} else if runes[pos+1] == ' ' && runes[pos+2] == '[' && runes[pos+3] == 'X' && runes[pos+4] == ']' && runes[pos+5] == ' ' {
				// checkbox: "- [X] "
				info.markerType = markerCheckboxOn
				info.markerRuneCount = 6 // "- [X] " = 6 runes
			}
		}
		// 如果没有匹配 checkbox，检查普通 bullet
		if info.markerType == -1 && pos+1 < runeCount && runes[pos+1] == ' ' {
			info.markerType = markerBullet
			info.markerRuneCount = 2 // "- " = 2 runes
		}
	} else if first >= '0' && first <= '9' {
		// 数字序号: "1. " / "12. " 等
		// 读取连续的数字
		numEnd := pos
		for numEnd < runeCount && runes[numEnd] >= '0' && runes[numEnd] <= '9' {
			numEnd++
		}
		numCount := numEnd - pos
		// 检查下一个 rune 是 '.' 且再下一个是空格
		if numCount > 0 && numEnd < runeCount && runes[numEnd] == '.' && numEnd+1 < runeCount && runes[numEnd+1] == ' ' {
			info.markerType = markerOrdered
			info.markerRuneCount = numCount + 2 // 数字 + "." + 空格
		}
	}

	// 4. 计算 content 起始位置
	if info.markerType != -1 {
		info.contentStartRune = info.indentRuneCount + info.markerRuneCount
		if info.contentStartRune <= runeCount {
			info.content = string(runes[info.contentStartRune:])
		} else {
			// marker 超出范围，content 为空
			info.content = ""
		}
	}

	return info
}

// markerDisplayRune 返回 marker 类型的显示字符
func markerDisplayRune(mt int) rune {
	switch mt {
	case markerBullet:
		return '-'
	case markerCheckboxOff:
		return '◯' // U+25EF
	case markerCheckboxOn:
		return '✔' // U+2714
	default:
		return 0 // ordered 不走此路径
	}
}

// makeIndentCells 生成前导缩进的 Cell 数组。
// 空格 → 1个空格Cell，tab → 展开4个空格Cell。
func makeIndentCells(lineRunes []rune, markerPos int, bufLine int, style tcell.Style) []Cell {
	var cells []Cell
	for i := 0; i < markerPos; i++ {
		if lineRunes[i] == '\t' {
			for j := 0; j < 4; j++ {
				cells = append(cells, Cell{
					Rune:    ' ',
					Style:   style,
					BufLine: bufLine,
					BufX:    i,
				})
			}
		} else {
			cells = append(cells, Cell{
				Rune:    ' ',
				Style:   style,
				BufLine: bufLine,
				BufX:    i,
			})
		}
	}
	return cells
}

// makeMarkerCells 根据 marker 类型生成 marker 的 Cell 数组
func makeMarkerCells(info listItemInfo, bufLine int, style tcell.Style, checkboxStyle tcell.Style, lineRunes []rune) []Cell {
	if info.markerType == markerOrdered {
		// 有序列表：提取原始 marker 文本
		markerText := lineRunes[info.indentRuneCount:info.contentStartRune]
		cells := make([]Cell, 0, len(markerText))
		for i, r := range markerText {
			cells = append(cells, Cell{
				Rune:    r,
				Style:   style,
				BufLine: bufLine,
				BufX:    info.indentRuneCount + i,
			})
		}
		return cells
	}

	// 无序/checkbox：使用美化后的显示字符
	r := markerDisplayRune(info.markerType)
	// Checkbox markers use checkboxStyle, regular bullets use style
	markerStyle := style
	if info.markerType == markerCheckboxOff || info.markerType == markerCheckboxOn {
		markerStyle = checkboxStyle
	}
	cells := []Cell{
		{Rune: r, Style: markerStyle, BufLine: bufLine, BufX: -1, IsDecorative: true},
		{Rune: ' ', Style: markerStyle, BufLine: bufLine, BufX: -1},
	}
	return cells
}

// makePaddingCells 生成 n 个空格填充 Cell（用于悬挂缩进续行）
func makePaddingCells(n, bufLine int, style tcell.Style) []Cell {
	cells := make([]Cell, n)
	for i := range cells {
		cells[i] = Cell{
			Rune:    ' ',
			Style:   style,
			BufLine: bufLine,
			BufX:    -1,
		}
	}
	return cells
}

// RenderList 渲染列表，支持 marker 美化、checkbox 替换和悬挂缩进换行。
func RenderList(seg Segment, width int, cfg MDConfig) *RenderedSegment {
	lines := linesFromBuf(cfg.Buf, seg.BufStartLine, seg.BufEndLine)
	if !cfg.MDList {
		return RenderNormal(seg, width, cfg)
	}

	result := &RenderedSegment{}
	style := tcell.StyleDefault
	listStyle := resolveStyle(cfg.Colorscheme.Styles, "md-list", cfg.Colorscheme.DefStyle)
	checkboxStyle := resolveStyle(cfg.Colorscheme.Styles, "md-checkbox", cfg.Colorscheme.DefStyle)

	for lineIdx, line := range lines {
		// ── 解析 ──
		info := parseListItem(line)
		runes := []rune(line)

		// 不匹配列表模式，退化为普通渲染
		if info.markerType < 0 {
			cells := renderInline(line, style, cfg, lineIdx)
			rows := wrapCells(cells, width, lineIdx, style)
			result.Rows = append(result.Rows, rows...)
			continue
		}

		// ── 构造前缀 ──
		indentCells := makeIndentCells(runes, info.indentRuneCount, lineIdx, style)
		markerCells := makeMarkerCells(info, lineIdx, listStyle, checkboxStyle, runes)
		prefixCells := make([]Cell, 0, len(indentCells)+len(markerCells))
		prefixCells = append(prefixCells, indentCells...)
		prefixCells = append(prefixCells, markerCells...)
		prefixWidth := cellDisplayWidth(prefixCells)
		contentWidth := width - prefixWidth

		// 极端情况：前缀比屏幕宽或几乎占满，无法有效显示内容
		if contentWidth < 2 {
			cells := renderInline(line, style, cfg, lineIdx)
			rows := wrapCells(cells, width, lineIdx, style)
			result.Rows = append(result.Rows, rows...)
			continue
		}

		// ── 处理内容 ──
		content := string(runes[info.contentStartRune:])
		contentCells := renderInline(content, style, cfg, lineIdx)

		// 修正 contentCells 的 BufX：renderInline 产出的是相对于 content 的偏移
		// 需要加上 contentStartRune 才是原始行中的真实偏移
		for i := range contentCells {
			if contentCells[i].BufX >= 0 {
				contentCells[i].BufX += info.contentStartRune
			}
		}

		// ── Wrap（只 wrap 内容部分） ──
		contentRows := wrapCells(contentCells, contentWidth, lineIdx, style)

		// ── 组装：首行拼完整前缀，续行拼悬挂缩进 ──
		for rowIdx, row := range contentRows {
			if rowIdx == 0 {
				// 首行：prefixCells（indent + marker）+ content
				row.Cells = append(prefixCells, row.Cells...)
			} else {
				// 续行：prefixWidth 个空格 + content（悬挂缩进）
				hangCells := makePaddingCells(prefixWidth, lineIdx, style)
				row.Cells = append(hangCells, row.Cells...)
			}
			result.Rows = append(result.Rows, row)
		}
	}

	return result
}