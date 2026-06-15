package display

import (
	"strings"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
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

// ensureMDConfigReady 检查 MDConfig 的 Colorscheme 是否与当前全局 Colorscheme 一致。
// 启动时 initMDConfig 在 InitColorscheme 之前被调用，拿到的是空值。
// 运行时切换 colorscheme 也会导致指针变化。
// 通过指针比较（比较 map 变量的地址），在渲染时按需刷新，无需改动任何 micro 原生代码。
func (w *BufWindow) ensureMDConfigReady() {
	if w.mdConfig.Colorscheme.StylesRef == nil || w.mdConfig.Colorscheme.StylesRef != &config.Colorscheme {
		w.mdConfig.Colorscheme.Styles = config.Colorscheme
		w.mdConfig.Colorscheme.StylesRef = &config.Colorscheme
		w.mdConfig.Colorscheme.DefStyle = config.DefStyle
	}
}

// hasCursorInside 判定 seg 内是否有任意光标。
//
// 回退判定（displayBufferMD 主循环使用）：
//   - editMode=true（编辑模式）+ 光标在 seg 内 → 走原生
//   - editMode=false（阅读模式）             → 全部走 MD，不回退
func hasCursorInside(seg md.Segment, cursors []*buffer.Cursor) bool {
	for _, c := range cursors {
		// 检查1：光标本身所在行是否在 segment 内
		if c.Y >= seg.BufStartLine && c.Y <= seg.BufEndLine {
			return true
		}
		// 检查2：选区是否与 segment 有交集（仅当存在有效选区时）
		if c.HasSelection() {
			selStart := c.CurSelection[0].Y
			selEnd := c.CurSelection[1].Y
			if selStart > selEnd {
				selStart, selEnd = selEnd, selStart
			}
			if selStart <= seg.BufEndLine && seg.BufStartLine <= selEnd {
				return true
			}
		}
	}
	return false
}

// renderSegmentMD 渲染单个 MD segment 到 screen。
// 返回签名只返回 newVY，viewportRowmap 在渲染时直接写入。
func (w *BufWindow) renderSegmentMD(
	seg md.Segment, vY int,
) (newVY int) {
	bufWidth := w.bufWidth
	bufHeight := w.bufHeight

	// ★ 注入 Buf 到 MDConfig，renderer 通过 cfg.Buf 自行取 lines
	w.mdConfig.Buf = w.Buf

	// ★ 直接传 seg 给 renderer，不再由 display 层取 lines
	rendered := seg.Render(seg, bufWidth, w.mdConfig)

	// 绝对化 BufLine（renderer 产出相对行号，display 层统一绝对化）
	// §4 坐标系说明：MD 渲染器内部产出的是 segment 内相对行号，
	// renderSegmentMD:76-84 统一绝对化：row.BufLine >= 0 的 += seg.BufStartLine
	// 装饰行（-1）不加，保持 -1
	for ri := range rendered.Rows {
		if rendered.Rows[ri].BufLine >= 0 {
			rendered.Rows[ri].BufLine += seg.BufStartLine
		}
		for ci := range rendered.Rows[ri].Cells {
			if rendered.Rows[ri].Cells[ci].BufLine >= 0 {
				rendered.Rows[ri].Cells[ci].BufLine += seg.BufStartLine
			}
		}
	}

	// ★ lineStyles 预计算：覆盖完整范围 BufStartLine~BufEndLine
	lineStyles := map[int][]tcell.Style{}
	for bufLine := seg.BufStartLine; bufLine <= seg.BufEndLine; bufLine++ {
		line := w.Buf.Line(bufLine)
		lineStyles[bufLine] = w.expandLineStyles(bufLine, utf8.RuneCountInString(line), config.DefStyle)
	}

	// ★ 预扫描：找到第一个内容行的 BufLine（绝对行号）
	// 前置装饰行归属此行。若第一个内容行已滚出 viewport，前置装饰行也滚出。
	firstContentBufLine := -1
	for _, row := range rendered.Rows {
		if row.BufLine >= 0 {
			firstContentBufLine = row.BufLine
			break
		}
	}

	// ★ 写入 screen + viewportRowmap：根据 VisibleStart/VisibleEnd 过滤
	lastBufLine := -1
	lastContentBufLine := -1 // 追踪最近的内容行，用于装饰行可见性判断
	segRow := 0              // segmentRow：同一 buffer 行的第几个 softwrap 段
	for _, row := range rendered.Rows {
		if vY >= bufHeight {
			break
		}

		// 可见性判断（用于决定是否渲染，不影响 viewportRowmap 写入）
		effectiveLine := row.BufLine
		if effectiveLine < 0 {
			if lastContentBufLine >= 0 {
				effectiveLine = lastContentBufLine // 装饰行复用最近内容行
			} else if firstContentBufLine >= 0 {
				effectiveLine = firstContentBufLine // 前置装饰行归属第一个内容行
			}
		}
		if effectiveLine < seg.VisibleStart || effectiveLine > seg.VisibleEnd {
			// 不可见行：更新追踪变量但不渲染
			if row.BufLine >= 0 {
				lastContentBufLine = row.BufLine
			}
			lastBufLine = row.BufLine
			continue
		}

		// ★ 直接写 viewportRowmap（用 row.BufLine，非 effectiveLine）
		// effectiveLine 逻辑仅用于可见性判断
		if vY >= 0 && vY < bufHeight {
			if row.BufLine < 0 {
				w.viewportRowmap[vY] = SLoc{Line: -1, Row: -1} // 装饰行
			} else if row.BufLine == lastBufLine {
				segRow++ // 同一 buffer 行的续段
				w.viewportRowmap[vY] = SLoc{Line: row.BufLine, Row: segRow}
			} else {
				segRow = 0 // 新 buffer 行
				w.viewportRowmap[vY] = SLoc{Line: row.BufLine, Row: 0}
			}
		}

		// 更新追踪
		if row.BufLine >= 0 {
			lastContentBufLine = row.BufLine
		}

		// 以下渲染逻辑不变
		softwrapped := row.BufLine >= 0 && row.BufLine == lastBufLine
		w.drawGutterAndLineNumMD(vY, row.BufLine, softwrapped)
		lastBufLine = row.BufLine

		for col, cell := range row.Cells {
			screenX := w.X + w.gutterOffset + col
			screenY := w.Y + vY
			if screenX < w.X+w.gutterOffset || screenX >= w.X+w.gutterOffset+bufWidth {
				continue
			}
			style := cell.Style
			// 确保背景色：如果 cell 没有设置背景色，使用 DefStyle 的背景色
			_, bg, _ := style.Decompose()
			if bg == tcell.ColorDefault {
				_, defBg, _ := config.DefStyle.Decompose()
				style = style.Background(defBg)
			}
			// 记住 cell 原有的非默认背景色（如代码块背景）
			_, cellBg, _ := cell.Style.Decompose()
			if cell.BufLine >= 0 && cell.BufX >= 0 {
				if styles, ok := lineStyles[cell.BufLine]; ok && cell.BufX < len(styles) {
					style = styles[cell.BufX] // highlight 的完整颜色（前景+背景）
					// 如果 cell 有明确的非默认背景色（如代码块），保留它
					if cellBg != tcell.ColorDefault {
						style = style.Background(cellBg)
					}
				}
				// else: 保持 cell.Style（fallback，不覆盖）
			}
			// 叠加 renderInline 的文本属性
			_, _, attrs := cell.Style.Decompose()
			if attrs&tcell.AttrBold != 0 {
				style = style.Bold(true)
			}
			if attrs&tcell.AttrItalic != 0 {
				style = style.Italic(true)
			}
			if attrs&tcell.AttrUnderline != 0 {
				style = style.Underline(true)
			}
			if attrs&tcell.AttrStrikeThrough != 0 {
				style = style.StrikeThrough(true)
			}
			screen.SetContent(screenX, screenY, cell.Rune, cell.Combining, style)
		}

		vY++
	}

	return vY
}



// renderSegmentNative 用原生逻辑渲染一个 segment 到 BufWindow。
// seg 必须已经过 filterSegmentsToVisible 裁剪（BufStartLine/BufEndLine 在可见区内）。
// 不修改 displayBuffer()，从那里复制主循环并按需调整。
// 返回签名只返回 newVY，viewportRowmap 在渲染时直接写入。
func (w *BufWindow) renderSegmentNative(
	seg md.Segment, startVY int,
) (newVY int) {
	b := w.Buf
	bufHeight := w.bufHeight

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
	vloc := buffer.Loc{X: 0, Y: startVY}

	// this represents the current draw position in the buffer (char positions)
	// ★ VisibleStart 是 segment 在 viewport 中的实际起始行（可能 > BufStartLine）
	bloc := buffer.Loc{X: -1, Y: seg.VisibleStart}

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

	// ★ softwrap offset 判断
	// 语义：当 viewport 起始位置正好是 segment 可见范围的起始行时，
	//       需要补偿 softwrap row 偏移（该 buffer 行上方已有部分 row 滚出屏幕）。
	// 等价性证明：
	//   filterSegmentsToVisible 设置 VisibleStart = max(BufStartLine, startY)
	//   displayBufferMD 计算 visibleStart = w.StartLine.Line
	//   因此 VisibleStart == w.StartLine.Line 当且仅当
	//   segment 的可见范围恰好从 viewport 起始行开始。
	//   这与原逻辑（截断后 BufStartLine == w.StartLine.Line）完全等价。
	if softwrap && seg.VisibleStart == w.StartLine.Line {
		// 视口可能从该 buffer 行的中间 screen row 开始，
		// 需要和原版一样跳过上方已滚出屏幕的部分
		vloc.Y -= w.StartLine.Row
	}

	// ★ 循环条件用 seg.VisibleEnd 替代 seg.BufEndLine
	for ; vloc.Y < w.bufHeight && bloc.Y <= seg.VisibleEnd; vloc.Y++ {
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

		// ★ 直接写 viewportRowmap（§5.2 segmentRow 公式）
		// 不需要 rowOffset 分支：vloc.Y -= w.StartLine.Row 让 lineStartVY 可能为负，
		// screenRow - lineStartVY 天然从 StartLine.Row 开始
		for screenRow := lineStartVY; screenRow <= vloc.Y; screenRow++ {
			if screenRow >= 0 && screenRow < bufHeight {
				w.viewportRowmap[screenRow] = SLoc{Line: bloc.Y, Row: screenRow - lineStartVY}
			}
		}

		bloc.X = w.StartCol
		bloc.Y++
		// ★ 末尾 break 条件改为 seg 边界
		if bloc.Y > seg.VisibleEnd {
			vloc.Y++ // 补上 for 循环被 break 跳过的自增
			break
		}
	}

	return vloc.Y
}

// displayBufferMD 是 displayBuffer 的 MD 渲染版本。
// Step 1.0 实现：editMode=true 时，光标所在 segment 回退原生 displayBuffer 渲染，
// 其余 segment 继续走 MD 渲染管线。
func (w *BufWindow) displayBufferMD(editMode bool) {
	b := w.Buf
	if w.Height <= 0 || w.Width <= 0 {
		return
	}
	w.ensureMDConfigReady()

	bufWidth := w.bufWidth
	bufHeight := w.bufHeight

	// 1. 检测可见区域
	visibleStart := w.StartLine.Line
	visibleEnd := visibleStart + bufHeight // 超过也行，detect 会截断
	if visibleEnd >= b.LinesNum() {
		visibleEnd = b.LinesNum() - 1
	}

	// 懒分配：确保容量足够（resize 后 bufHeight 可能变化）
	if cap(w.viewportRowmap) < bufHeight {
		w.viewportRowmap = make([]SLoc, bufHeight)
	}
	w.viewportRowmap = w.viewportRowmap[:bufHeight]
	// 重置为 {Line:-2}（空白）
	for i := range w.viewportRowmap {
		w.viewportRowmap[i] = SLoc{Line: -2}
	}

	// 读 buffer 上的分类结果（事件驱动算好，content-static）
	// 注意：非 MD 文件的 b.MDSegments 保持 nil，filterSegmentsToVisible 要处理 nil
	allSegs := b.MDSegments
	segments := filterSegmentsToVisible(allSegs, visibleStart, visibleEnd)

	cursors := b.GetCursors()


	// 2. 渲染管线主循环
	vY := 0 // 当前 screen 行（相对窗口顶部）

	for _, seg := range segments {
		if editMode && hasCursorInside(seg, cursors) {
			vY = w.renderSegmentNative(seg, vY)
		} else {
			vY = w.renderSegmentMD(seg, vY)
		}
	}

	// 3. 填充剩余空间
	defStyle := config.DefStyle
	for ; vY < bufHeight; vY++ {
		w.drawGutterAndLineNumMD(vY, -1, false) // 空行，不显示行号
		for col := 0; col < bufWidth; col++ {
			screen.SetContent(w.X+w.gutterOffset+col, w.Y+vY, ' ', nil, defStyle)
		}
	}
}

// filterSegmentsToVisible 从事件驱动算好的全 buffer segments 中，筛选当前可见区域。
// 非 MD 文件的 segs 为 nil，直接返回 nil（displayBufferMD 不被调用，原生路径处理）。
// 设置 VisibleStart/VisibleEnd 为 segment 在当前 viewport 中的实际可见范围，
// BufStartLine/BufEndLine 保持不变（完整数据传给 renderer）。
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
		// 至少部分重叠，标记可见范围
		s.VisibleStart = s.BufStartLine
		if s.VisibleStart < startY {
			s.VisibleStart = startY
		}
		s.VisibleEnd = s.BufEndLine
		if s.VisibleEnd > endY {
			s.VisibleEnd = endY
		}
		out = append(out, s)
	}
	return out
}

// computeRowCounts 已删除，使用 computeVisibleRowCounts 替代。

// drawGutterAndLineNumMD 在指定 screen 行绘制 gutter 和行号。
// bufLine 为 -1 表示空行/装饰行。
// softwrapped=true 表示该行是 wrap 续行，行号留空但 diff 标志照画。
func (w *BufWindow) drawGutterAndLineNumMD(vY int, bufLine int, softwrapped bool) {
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
		w.drawDiffGutter(lineNumStyle, softwrapped, &vloc, &bloc)
	}
	if b.Settings["ruler"].(bool) {
		if bufLine >= 0 && !softwrapped {
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

// expandLineStyles 将稀疏 Match map 展开为稠密 style 数组。
// result[i] = 第 i 个 rune 经过 highlighter + colorscheme 之后的完整 style。
// 用于 renderSegmentMD：预计算稠密数组后，按 BufX 直接查颜色，解决标记隐藏后的锚点丢失问题。
func (w *BufWindow) expandLineStyles(bufLine int, runeCount int, baseStyle tcell.Style) []tcell.Style {
	charStyles := make([]tcell.Style, runeCount)
	match := w.Buf.Match(bufLine)
	curStyle := baseStyle
	for i := 0; i < runeCount; i++ {
		if group, ok := match[i]; ok {
			curStyle = config.GetColor(group.String())
		}
		charStyles[i] = curStyle
	}

	return charStyles
}

// ScreenRowToLine 将屏幕行偏移（相对 viewport 顶部）映射为 buffer 行号。
// 使用 viewportRowmap 直接查找，O(1)。
// 装饰行（-1）和空白区域（-2）一视同仁：点击装饰行等于点击空白，返回 (0, false)。
// 返回 (bufferLine, true) 表示成功映射，(0, false) 表示应回退原始 Scroll 逻辑。
func (w *BufWindow) ScreenRowToLine(screenOffset int) (int, bool) {
	if screenOffset < 0 || screenOffset >= len(w.viewportRowmap) {
		return 0, false
	}
	if w.viewportRowmap[screenOffset].Line >= 0 {
		return w.viewportRowmap[screenOffset].Line, true
	}
	// 装饰行（-1）和空白区域（-2）一视同仁：没有对应 buffer 行
	return 0, false
}

// LineToScreenRow 将 (bufferLine, segmentRow) 二元组精确匹配为屏行。
// 用于 Relocate 滚动判定。线性扫描 O(bufHeight)，bufHeight 通常 ≤ 终端高度。
func (w *BufWindow) LineToScreenRow(line, row int) (int, bool) {
	for i, v := range w.viewportRowmap {
		if v.Line == line && v.Row == row {
			return i, true
		}
	}
	return 0, false
}

// relocateVerticalMD 是 Relocate 的 MD 垂直滚动分支。
// 判定：LineToScreenRow 精确匹配光标 (Line, Row) → 屏行。
// 动作：向下读 viewportRowmap[delta]（含 segmentRow），向上沿用原生算术。
// 边界场景（首帧 / 光标跳出视口 / delta 落空白尾）走 relocateVerticalNativeFallback。
func (w *BufWindow) relocateVerticalMD(c SLoc, scrollmargin, height int) bool {
	n := len(w.viewportRowmap)
	if n == 0 {
		return w.relocateVerticalNativeFallback(c, scrollmargin, height) // 首帧
	}
	cursorRow, ok := w.LineToScreenRow(c.Line, c.Row)
	if !ok {
		return w.relocateVerticalNativeFallback(c, scrollmargin, height) // 光标跳出视口
	}

	botMarginRow := height - 1 - scrollmargin
	if cursorRow > botMarginRow {
		delta := cursorRow - botMarginRow
		if delta >= n {
			return w.relocateVerticalNativeFallback(c, scrollmargin, height)
		}
		loc := w.viewportRowmap[delta]
		// delta 落装饰/空白：向下找首个内容行作为新视口顶
		for loc.Line < 0 && delta+1 < n {
			delta++
			loc = w.viewportRowmap[delta]
		}
		if loc.Line < 0 {
			return w.relocateVerticalNativeFallback(c, scrollmargin, height)
		}
		w.StartLine = loc
		return true
	}
	if cursorRow < scrollmargin {
		w.StartLine = w.Scroll(c, -scrollmargin) // 向上：段内 1:1
		return true
	}
	return true
}

// relocateVerticalNativeFallback 复刻 micro 原生垂直 Relocate（1:1 假设），
// 供 MD 路径边界场景兜底。
//
// ⚠️ 此函数需与 bufwindow.go 中 Relocate 的非 MD 分支保持同步。
// micro 的 Relocate 垂直逻辑历史稳定，但修改其 else 分支时务必同步本函数。
func (w *BufWindow) relocateVerticalNativeFallback(c SLoc, scrollmargin, height int) bool {
	bStart := SLoc{0, 0}
	bEnd := w.SLocFromLoc(w.Buf.End())
	ret := false
	if c.LessThan(w.Scroll(w.StartLine, scrollmargin)) && c.GreaterThan(w.Scroll(bStart, scrollmargin-1)) {
		w.StartLine = w.Scroll(c, -scrollmargin)
		ret = true
	} else if c.LessThan(w.StartLine) {
		w.StartLine = c
		ret = true
	}
	if c.GreaterThan(w.Scroll(w.StartLine, height-1-scrollmargin)) && c.LessEqual(w.Scroll(bEnd, -scrollmargin)) {
		w.StartLine = w.Scroll(c, -height+1+scrollmargin)
		ret = true
	} else if c.GreaterThan(w.Scroll(bEnd, -scrollmargin)) && c.GreaterThan(w.Scroll(w.StartLine, height-1)) {
		w.StartLine = w.Scroll(bEnd, -height+1)
		ret = true
	}
	return ret
}
