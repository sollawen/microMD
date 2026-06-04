package display

import (
	"fmt"
	"os"
	"strings"

	runewidth "github.com/mattn/go-runewidth"
	"github.com/micro-editor/micro/v2/internal/buffer"
	"github.com/micro-editor/micro/v2/internal/config"
	"github.com/micro-editor/micro/v2/internal/md"
	"github.com/micro-editor/micro/v2/internal/screen"
	"github.com/micro-editor/micro/v2/internal/util"
	"github.com/micro-editor/tcell/v2"
)

// SetMDConfig 设置 MD 渲染配置。
func (w *BufWindow) SetMDConfig(cfg md.MDConfig) {
	w.mdConfig = cfg
}

// hasCursorInside 判定 seg 内是否有任意光标。
//
// 回退判定（displayBufferMD 主循环使用）：
//   - editMode=true（编辑模式）+ 光标在 seg 内 → 走原生
//   - editMode=false（阅读模式）             → 全部走 MD，不回退
func hasCursorInside(seg md.Segment, cursors []*buffer.Cursor) bool {
	for _, c := range cursors {
		if c.Y >= seg.BufStartLine && c.Y <= seg.BufEndLine {
			return true
		}
	}
	return false
}

// renderSegmentMD 渲染单个 MD segment 到 screen。
// 与 renderSegmentNative 对称：纯函数，渲染 + 返回 (newVY, perLineCounts)。
// 不写 mdCache——由 displayBufferMD 主循环统一写。
func (w *BufWindow) renderSegmentMD(
	seg md.Segment, vY int,
) (newVY int, perLineCounts []int) {
	bufWidth := w.bufWidth
	bufHeight := w.bufHeight

	// 取行 → render → 行号绝对化
	lines := w.linesFromBuffer(seg.BufStartLine, seg.BufEndLine)
	rendered := seg.Render(
		lines,
		bufWidth,
		w.mdConfig,
	)

	// 将 renderer 输出的相对 BufLine 转为绝对行号
	for ri := range rendered.Rows {
		rendered.Rows[ri].BufLine += seg.BufStartLine
		for ci := range rendered.Rows[ri].Cells {
			if rendered.Rows[ri].Cells[ci].BufLine >= 0 {
				rendered.Rows[ri].Cells[ci].BufLine += seg.BufStartLine
			}
		}
	}

	// 写入 screen
	for _, row := range rendered.Rows {
		if vY >= bufHeight {
			break
		}

		// 画 gutter + 行号
		w.drawGutterAndLineNumMD(vY, row.BufLine)

		// 画内容
		// LineMatch 是稀疏 map：只在语法组变化的起始位置有条目，
		// 所以需要记住上一次匹配的 group，没命中时继续沿用。
		var lastFg tcell.Color
		var lastAttr tcell.AttrMask
		hasLast := false

		for col, cell := range row.Cells {
			screenX := w.X + w.gutterOffset + col
			screenY := w.Y + vY
			if screenX < w.X+w.gutterOffset || screenX >= w.X+w.gutterOffset+bufWidth {
				continue
			}
			style := cell.Style
			if cell.BufLine >= 0 && cell.BufX >= 0 {
				if group, ok := w.Buf.Match(cell.BufLine)[cell.BufX]; ok {
					s := config.GetColor(group.String())
					lastFg, _, lastAttr = s.Decompose()
					hasLast = true
				}
				if hasLast {
					_, bg, _ := style.Decompose()
					style = tcell.StyleDefault.Foreground(lastFg).Background(bg)
					if lastAttr&tcell.AttrBold != 0 {
						style = style.Bold(true)
					}
					if lastAttr&tcell.AttrBlink != 0 {
						style = style.Blink(true)
					}
					if lastAttr&tcell.AttrReverse != 0 {
						style = style.Reverse(true)
					}
					if lastAttr&tcell.AttrUnderline != 0 {
						style = style.Underline(true)
					}
					if lastAttr&tcell.AttrDim != 0 {
						style = style.Dim(true)
					}
					if lastAttr&tcell.AttrItalic != 0 {
						style = style.Italic(true)
					}
					if lastAttr&tcell.AttrStrikeThrough != 0 {
						style = style.StrikeThrough(true)
					}
				}
			}
			screen.SetContent(screenX, screenY, cell.Rune, cell.Combining, style)
		}

		vY++
	}

	return vY, computeRowCounts(rendered)
}

// renderSegmentNative 用原生逻辑渲染一个 segment 到 BufWindow。
// seg 必须已经过 filterSegmentsToVisible 裁剪（BufStartLine/BufEndLine 在可见区内）。
// 不修改 displayBuffer()，从那里复制主循环并按需调整。
//
// startVY : 起始 screen 行（相对窗口顶部）
// 返回:
//   newVY         - 渲染后的 vY（=startVY + 实际消耗的 screen 行数）
//   perLineCounts - 每个 buffer 行占几个 screen row（长度 = seg.BufEndLine-seg.BufStartLine+1）
//                   softwrap on 时一行可占多行 screen row
func (w *BufWindow) renderSegmentNative(
	seg md.Segment, startVY int,
) (newVY int, perLineCounts []int) {
	b := w.Buf

	maxWidth := w.gutterOffset + w.bufWidth

	if b.ModifiedThisFrame {
		if b.Settings["diffgutter"].(bool) {
			b.UpdateDiff()
		}
		b.ModifiedThisFrame = false
	}

	var matchingBraces []buffer.Loc
	// bracePairs is defined in buffer.go
	if b.Settings["matchbrace"].(bool) {
		for _, c := range b.GetCursors() {
			if c.HasSelection() {
				continue
			}

			mb, left, found := b.FindMatchingBrace(c.Loc)
			if found {
				matchingBraces = append(matchingBraces, mb)
				if !left {
					if b.Settings["matchbracestyle"].(string) != "highlight" {
						matchingBraces = append(matchingBraces, c.Loc)
					}
				} else {
					matchingBraces = append(matchingBraces, c.Loc.Move(-1, b))
				}
			}
		}
	}

	lineNumStyle := config.DefStyle
	if style, ok := config.Colorscheme["line-number"]; ok {
		lineNumStyle = style
	}
	curNumStyle := config.DefStyle
	if style, ok := config.Colorscheme["current-line-number"]; ok {
		if !b.Settings["cursorline"].(bool) {
			curNumStyle = lineNumStyle
		} else {
			curNumStyle = style
		}
	}

	softwrap := b.Settings["softwrap"].(bool)
	wordwrap := softwrap && b.Settings["wordwrap"].(bool)

	tabsize := util.IntOpt(b.Settings["tabsize"])
	colorcolumn := util.IntOpt(b.Settings["colorcolumn"])

	// this represents the current draw position
	// within the current window
	// [MOD 2] vloc.Y 初值 = startVY, 仅首 seg 保留 softwrap offset
	vloc := buffer.Loc{X: 0, Y: startVY}
	if softwrap && seg.BufStartLine == w.StartLine.Line {
		// 视口可能从该 buffer 行的中间 screen row 开始，
		// 需要和原版一样跳过上方已滚出屏幕的部分
		vloc.Y -= w.StartLine.Row
	}

	// this represents the current draw position in the buffer (char positions)
	// [MOD 1] bloc.Y 初值改为 seg.BufStartLine
	bloc := buffer.Loc{X: -1, Y: seg.BufStartLine}

	cursors := b.GetCursors()

	curStyle := config.DefStyle

	// Parse showchars which is in the format of key1=val1,key2=val2,...
	spacechars := " "
	tabchars := b.Settings["indentchar"].(string)
	var indentspacechars string
	var indenttabchars string
	for _, entry := range strings.Split(b.Settings["showchars"].(string), ",") {
		split := strings.SplitN(entry, "=", 2)
		if len(split) < 2 {
			continue
		}
		key, val := split[0], split[1]
		switch key {
		case "space":
			spacechars = val
		case "tab":
			tabchars = val
		case "ispace":
			indentspacechars = val
		case "itab":
			indenttabchars = val
		}
	}

	// [MOD 3] 循环条件加 bloc.Y <= seg.BufEndLine（双重终止：屏幕满 + buffer 行用完）
	for ; vloc.Y < w.bufHeight && bloc.Y <= seg.BufEndLine; vloc.Y++ {
		// [MOD 5] 记录当前行起始 vY（行内 wrap 时用于差值）
		lineStartVY := vloc.Y

		vloc.X = 0

		currentLine := false
		for _, c := range cursors {
			if !c.HasSelection() && bloc.Y == c.Y && w.active {
				currentLine = true
				break
			}
		}

		s := lineNumStyle
		if currentLine {
			s = curNumStyle
		}

		if vloc.Y >= 0 {
			if w.hasMessage {
				w.drawGutter(&vloc, &bloc)
			}

			if b.Settings["diffgutter"].(bool) {
				w.drawDiffGutter(s, false, &vloc, &bloc)
			}

			if b.Settings["ruler"].(bool) {
				w.drawLineNum(s, false, &vloc, &bloc)
			}
		} else {
			vloc.X = w.gutterOffset
		}

		bline := b.LineBytes(bloc.Y)
		blineLen := util.CharacterCount(bline)

		leadingwsEnd := len(util.GetLeadingWhitespace(bline))
		trailingwsStart := blineLen - util.CharacterCount(util.GetTrailingWhitespace(bline))

		line, nColsBeforeStart, bslice, startStyle := w.getStartInfo(w.StartCol, bloc.Y)
		if startStyle != nil {
			curStyle = *startStyle
		}
		bloc.X = bslice

		// returns the rune to be drawn, style of it and if the bg should be preserved
		getRuneStyle := func(r rune, style tcell.Style, showoffset int, linex int, isplaceholder bool) (rune, tcell.Style, bool) {
			if nColsBeforeStart > 0 || vloc.Y < 0 || isplaceholder {
				return r, style, false
			}

			for _, mb := range matchingBraces {
				if mb.X == bloc.X && mb.Y == bloc.Y {
					if b.Settings["matchbracestyle"].(string) == "highlight" {
						if s, ok := config.Colorscheme["match-brace"]; ok {
							return r, s, false
						} else {
							return r, style.Reverse(true), false
						}
					} else {
						return r, style.Underline(true), false
					}
				}
			}

			if r != '\t' && r != ' ' {
				return r, style, false
			}

			var indentrunes []rune
			switch r {
			case '\t':
				if bloc.X < leadingwsEnd && indenttabchars != "" {
					indentrunes = []rune(indenttabchars)
				} else {
					indentrunes = []rune(tabchars)
				}
			case ' ':
				if linex%tabsize == 0 && bloc.X < leadingwsEnd && indentspacechars != "" {
					indentrunes = []rune(indentspacechars)
				} else {
					indentrunes = []rune(spacechars)
				}
			}

			var drawrune rune
			if showoffset < len(indentrunes) {
				drawrune = indentrunes[showoffset]
			} else {
				// use space if no showchars or after we showed showchars
				drawrune = ' '
			}

			if s, ok := config.Colorscheme["indent-char"]; ok {
				fg, _, _ := s.Decompose()
				style = style.Foreground(fg)
			}

			preservebg := false
			if b.Settings["hltaberrors"].(bool) && bloc.X < leadingwsEnd {
				if s, ok := config.Colorscheme["tab-error"]; ok {
					if b.Settings["tabstospaces"].(bool) && r == '\t' {
						fg, _, _ := s.Decompose()
						style = style.Background(fg)
						preservebg = true
					} else if !b.Settings["tabstospaces"].(bool) && r == ' ' {
						fg, _, _ := s.Decompose()
						style = style.Background(fg)
						preservebg = true
					}
				}
			}

			if b.Settings["hltrailingws"].(bool) {
				if s, ok := config.Colorscheme["trailingws"]; ok {
					if bloc.X >= trailingwsStart && bloc.X < blineLen {
						hl := true
						for _, c := range cursors {
							if c.NewTrailingWsY == bloc.Y {
								hl = false
								break
							}
						}
						if hl {
							fg, _, _ := s.Decompose()
							style = style.Background(fg)
							preservebg = true
						}
					}
				}
			}

			return drawrune, style, preservebg
		}

		draw := func(r rune, combc []rune, style tcell.Style, highlight bool, showcursor bool, preservebg bool) {
			defer func() {
				if nColsBeforeStart <= 0 {
					vloc.X++
				}
				nColsBeforeStart--
			}()

			if nColsBeforeStart > 0 || vloc.Y < 0 {
				return
			}

			if highlight {
				if w.Buf.HighlightSearch && w.Buf.SearchMatch(bloc) {
					style = config.DefStyle.Reverse(true)
					if s, ok := config.Colorscheme["hlsearch"]; ok {
						style = s
					}
				}

				_, origBg, _ := style.Decompose()
				_, defBg, _ := config.DefStyle.Decompose()

				// syntax or hlsearch highlighting with non-default background takes precedence
				// over cursor-line and color-column
				if !preservebg && origBg != defBg {
					preservebg = true
				}

				for _, c := range cursors {
					if c.HasSelection() &&
						(bloc.GreaterEqual(c.CurSelection[0]) && bloc.LessThan(c.CurSelection[1]) ||
							bloc.LessThan(c.CurSelection[0]) && bloc.GreaterEqual(c.CurSelection[1])) {
						// The current character is selected
						style = config.DefStyle.Reverse(true)

						if s, ok := config.Colorscheme["selection"]; ok {
							style = s
						}
					}

					if b.Settings["cursorline"].(bool) && w.active && !preservebg &&
						!c.HasSelection() && c.Y == bloc.Y {
						if s, ok := config.Colorscheme["cursor-line"]; ok {
							fg, _, _ := s.Decompose()
							style = style.Background(fg)
						}
					}
				}

				for _, m := range b.Messages {
					if bloc.GreaterEqual(m.Start) && bloc.LessThan(m.End) ||
						bloc.LessThan(m.End) && bloc.GreaterEqual(m.Start) {
						style = style.Underline(true)
						break
					}
				}

				if s, ok := config.Colorscheme["color-column"]; ok {
					if colorcolumn != 0 && vloc.X-w.gutterOffset+w.StartCol == colorcolumn && !preservebg {
						fg, _, _ := s.Decompose()
						style = style.Background(fg)
					}
				}
			}

			screen.SetContent(w.X+vloc.X, w.Y+vloc.Y, r, combc, style)

			if showcursor {
				for _, c := range cursors {
					if c.X == bloc.X && c.Y == bloc.Y && !c.HasSelection() {
						w.showCursor(w.X+vloc.X, w.Y+vloc.Y, c.Num == 0)
					}
				}
			}
		}

		wrap := func() {
			vloc.X = 0

			if vloc.Y >= 0 {
				if w.hasMessage {
					w.drawGutter(&vloc, &bloc)
				}
				if b.Settings["diffgutter"].(bool) {
					w.drawDiffGutter(lineNumStyle, true, &vloc, &bloc)
				}

				// This will draw an empty line number because the current line is wrapped
				if b.Settings["ruler"].(bool) {
					w.drawLineNum(lineNumStyle, true, &vloc, &bloc)
				}
			} else {
				vloc.X = w.gutterOffset
			}
		}

		type glyph struct {
			r     rune
			combc []rune
			style tcell.Style
			width int
		}

		var word []glyph
		if wordwrap {
			word = make([]glyph, 0, w.bufWidth)
		} else {
			word = make([]glyph, 0, 1)
		}
		wordwidth := 0

		totalwidth := w.StartCol - nColsBeforeStart
		for len(line) > 0 && vloc.X < maxWidth {
			r, combc, size := util.DecodeCharacter(line)
			line = line[size:]

			loc := buffer.Loc{X: bloc.X + len(word), Y: bloc.Y}
			curStyle, _ = w.getStyle(curStyle, loc)

			width := 0

			linex := totalwidth
			switch r {
			case '\t':
				ts := tabsize - (totalwidth % tabsize)
				width = util.Min(ts, maxWidth-vloc.X)
				totalwidth += ts
			default:
				width = runewidth.RuneWidth(r)
				totalwidth += width
			}

			word = append(word, glyph{r, combc, curStyle, width})
			wordwidth += width

			// Collect a complete word to know its width.
			// If wordwrap is off, every single character is a complete "word".
			if wordwrap {
				if !util.IsWhitespace(r) && len(line) > 0 && wordwidth < w.bufWidth {
					continue
				}
			}

			// If a word (or just a wide rune) does not fit in the window
			if vloc.X+wordwidth > maxWidth && vloc.X > w.gutterOffset {
				for vloc.X < maxWidth {
					draw(' ', nil, config.DefStyle, false, false, true)
				}

				// We either stop or we wrap to draw the word in the next line
				if !softwrap {
					break
				} else {
					vloc.Y++
					if vloc.Y >= w.bufHeight {
						break
					}
					wrap()
				}
			}

			for _, r := range word {
				drawrune, drawstyle, preservebg := getRuneStyle(r.r, r.style, 0, linex, false)
				draw(drawrune, r.combc, drawstyle, true, true, preservebg)

				// Draw extra characters for tabs or wide runes
				for i := 1; i < r.width; i++ {
					if r.r == '\t' {
						drawrune, drawstyle, preservebg = getRuneStyle('\t', r.style, i, linex+i, false)
					} else {
						drawrune, drawstyle, preservebg = getRuneStyle(' ', r.style, i, linex+i, true)
					}
					draw(drawrune, nil, drawstyle, true, false, preservebg)
				}
				bloc.X++
			}

			word = word[:0]
			wordwidth = 0

			// If we reach the end of the window then we either stop or we wrap for softwrap
			if vloc.X >= maxWidth {
				if !softwrap {
					break
				} else {
					vloc.Y++
					if vloc.Y >= w.bufHeight {
						break
					}
					wrap()
				}
			}
		}

		style := config.DefStyle
		for _, c := range cursors {
			if b.Settings["cursorline"].(bool) && w.active &&
				!c.HasSelection() && c.Y == bloc.Y {
				if s, ok := config.Colorscheme["cursor-line"]; ok {
					fg, _, _ := s.Decompose()
					style = style.Background(fg)
				}
			}
		}
		for i := vloc.X; i < maxWidth; i++ {
			curStyle := style
			if s, ok := config.Colorscheme["color-column"]; ok {
				if colorcolumn != 0 && i-w.gutterOffset+w.StartCol == colorcolumn {
					fg, _, _ := s.Decompose()
					curStyle = style.Background(fg)
				}
			}
			screen.SetContent(i+w.X, vloc.Y+w.Y, ' ', nil, curStyle)
		}

		if vloc.X != maxWidth {
			// Display newline within a selection
			drawrune, drawstyle, preservebg := getRuneStyle(' ', config.DefStyle, 0, totalwidth, true)
			draw(drawrune, nil, drawstyle, true, true, preservebg)
		}

		// [MOD 5] 统计本行消耗的 screen 行数（用于 mdCache.RowCounts）
		perLineCounts = append(perLineCounts, vloc.Y-lineStartVY+1)

		bloc.X = w.StartCol
		bloc.Y++
		// [MOD 4] 末尾 break 条件改为 seg 边界
		if bloc.Y > seg.BufEndLine {
			vloc.Y++ // 补上 for 循环被 break 跳过的自增
			break
		}
	}

	return vloc.Y, perLineCounts
}

// displayBufferMD 是 displayBuffer 的 MD 渲染版本。
// Step 1.0 实现：editMode=true 时，光标所在 segment 回退原生 displayBuffer 渲染，
// 其余 segment 继续走 MD 渲染管线。
func (w *BufWindow) displayBufferMD(editMode bool) {
	b := w.Buf
	if w.Height <= 0 || w.Width <= 0 {
		return
	}

	bufWidth := w.bufWidth
	bufHeight := w.bufHeight

	// 1. 检测可见区域
	visibleStart := w.StartLine.Line
	visibleEnd := visibleStart + bufHeight // 超过也行，detect 会截断
	if visibleEnd >= b.LinesNum() {
		visibleEnd = b.LinesNum() - 1
	}

	// 每帧清空 mdCache（防止无限增长）
	w.mdCache = w.mdCache[:0]

	// 读 buffer 上的分类结果（事件驱动算好，content-static）
	// 注意：非 MD 文件的 b.MDSegments 保持 nil，filterSegmentsToVisible 要处理 nil
	allSegs := b.MDSegments
	segments := filterSegmentsToVisible(allSegs, visibleStart, visibleEnd)

	cursors := b.GetCursors()

	// 2. 渲染管线主循环
	vY := 0 // 当前 screen 行（相对窗口顶部）

	// DEBUG Step1.0
	var _debugLog strings.Builder
	fmt.Fprintf(&_debugLog, "=== displayBufferMD editMode=%v visibleStart=%d visibleEnd=%d StartLine.Line=%d StartLine.Row=%d bufHeight=%d\n", editMode, visibleStart, visibleEnd, w.StartLine.Line, w.StartLine.Row, bufHeight)
	for ci, co := range cursors {
		fmt.Fprintf(&_debugLog, "  cursor[%d] Y=%d X=%d\n", ci, co.Y, co.X)
	}
	for i, seg := range segments {
		fmt.Fprintf(&_debugLog, "  seg[%d] BufStartLine=%d BufEndLine=%d", i, seg.BufStartLine, seg.BufEndLine)
		var perLine []int
		if editMode && hasCursorInside(seg, cursors) {
			fmt.Fprintf(&_debugLog, " → NATIVE startVY=%d", vY)
			vY, perLine = w.renderSegmentNative(seg, vY)
			fmt.Fprintf(&_debugLog, " newVY=%d perLine=%v\n", vY, perLine)
		} else {
			fmt.Fprintf(&_debugLog, " → MD startVY=%d", vY)
			vY, perLine = w.renderSegmentMD(seg, vY)
			fmt.Fprintf(&_debugLog, " newVY=%d perLine=%v\n", vY, perLine)
		}
		w.mdCache = append(w.mdCache, md.SegmentMeta{
			BufStartLine: seg.BufStartLine,
			BufEndLine:   seg.BufEndLine,
			RowCounts:    perLine,
		})
	}
	os.WriteFile("docs/debug_step1.log", []byte(_debugLog.String()), 0644)

	// 3. 填充剩余空间
	defStyle := config.DefStyle
	for ; vY < bufHeight; vY++ {
		w.drawGutterAndLineNumMD(vY, -1) // 空行，不显示行号
		for col := 0; col < bufWidth; col++ {
			screen.SetContent(w.X+w.gutterOffset+col, w.Y+vY, ' ', nil, defStyle)
		}
	}
}

// linesFromBuffer 从 buffer 读取 start..end 行（含两端），返回 []string。
func (w *BufWindow) linesFromBuffer(start, end int) []string {
	lines := make([]string, 0, end-start+1)
	for i := start; i <= end && i < w.Buf.LinesNum(); i++ {
		lines = append(lines, w.Buf.Line(i))
	}
	return lines
}

// filterSegmentsToVisible 从事件驱动算好的全 buffer segments 中，截出当前可见区域。
// 非 MD 文件的 segs 为 nil，直接返回 nil（displayBufferMD 不被调用，原生路径处理）。
// 返回的 segment 列表中 BufStartLine/BufEndLine 已被截断到 [startY, endY]。
func filterSegmentsToVisible(segs []md.Segment, startY, endY int) []md.Segment {
	if segs == nil {
		return nil
	}
	var out []md.Segment
	for _, s := range segs {
		if s.BufEndLine < startY {
			continue // 完全在可视范围之上
		}
		if s.BufStartLine > endY {
			continue // 完全在可视范围之下
		}
		// 至少部分重叠，截断到可视范围
		if s.BufStartLine < startY {
			s.BufStartLine = startY
		}
		if s.BufEndLine > endY {
			s.BufEndLine = endY
		}
		out = append(out, s)
	}
	return out
}

// computeRowCounts 计算 render 后的 RenderedSegment 中，每个 buffer 行占几个 screen row。
// RenderedRow.BufLine：首行是有效 buffer 行号，续行/装饰行为 -1。
// 所以用 BufLine 变化点切分，统计连续相同 BufLine 的 Row 数。
func computeRowCounts(rendered *md.RenderedSegment) []int {
	counts := make([]int, 0, len(rendered.Rows))
	lastBufLine := -2 // 用 -2 保证首次遇到 BufLine==-1（装饰行）也能起一个计数
	for _, row := range rendered.Rows {
		if row.BufLine != lastBufLine {
			counts = append(counts, 1)
			lastBufLine = row.BufLine
		} else {
			counts[len(counts)-1]++
		}
	}
	return counts
}

// drawGutterAndLineNumMD 在指定 screen 行绘制 gutter 和行号。
// bufLine 为 -1 表示空行/续行，行号位留空。
func (w *BufWindow) drawGutterAndLineNumMD(vY int, bufLine int) {
	b := w.Buf
	vloc := buffer.Loc{X: 0, Y: vY}
	bloc := buffer.Loc{X: 0, Y: bufLine}

	lineNumStyle := config.DefStyle
	if style, ok := config.Colorscheme["line-number"]; ok {
		lineNumStyle = style
	}

	if w.hasMessage && bufLine >= 0 {
		w.drawGutter(&vloc, &bloc)
	}
	if b.Settings["diffgutter"].(bool) && bufLine >= 0 {
		w.drawDiffGutter(lineNumStyle, false, &vloc, &bloc)
	}
	if b.Settings["ruler"].(bool) {
		if bufLine >= 0 {
			w.drawLineNum(lineNumStyle, false, &vloc, &bloc)
		} else {
			// 续行/空行/装饰行：行号位留空
			for vloc.X < w.gutterOffset {
				screen.SetContent(w.X+vloc.X, w.Y+vY, ' ', nil, lineNumStyle)
				vloc.X++
			}
		}
	}
}
