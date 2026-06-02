package display

import (
	"unicode"

	runewidth "github.com/mattn/go-runewidth"
)

// TableBlock represents a continuous Markdown table region in the buffer
type TableBlock struct {
	StartLine int   // Starting line number (0-based buffer line)
	EndLine   int   // Ending line number (inclusive)
	ColWidths []int // Visual width for each column (max cell width + 1)
}

// tablePadding tracks the current cell state during rendering loop
// Not exported, used only within bufwindow.go
type tablePadding struct {
	colWidths       []int // Column widths for the current table
	pipeCount       int   // Number of | encountered (0-based)
	cellIndex       int   // Current cell index (which column)
	cellVisualWidth int   // Cumulative visual width of current cell content
	escaped         bool  // Whether previous character was backslash
	isSeparator     bool  // Whether current line is a separator line (pad with '-')
}

// codeBlockRange represents a fenced code block range (inclusive line numbers)
type codeBlockRange struct {
	start int
	end   int
}

// getFenceChar returns the fence character ('`' or '~') from a potential code fence line,
// after trimming leading whitespace. Returns 0 if not a fence line.
func getFenceChar(line []byte) byte {
	for len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
		line = line[1:]
	}
	if len(line) == 0 {
		return 0
	}
	return line[0]
}

// isCodeFence checks if a line is a fenced code block delimiter (``` or ~~~).
// Self-contained: trims leading whitespace internally.
func isCodeFence(line []byte) bool {
	// Self-contained trim to avoid callers forgetting to trim
	for len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
		line = line[1:]
	}
	if len(line) == 0 {
		return false
	}
	if line[0] != '`' && line[0] != '~' {
		return false
	}
	fenceChar := line[0]
	count := 0
	for _, b := range line {
		if b == fenceChar {
			count++
		} else {
			break
		}
	}
	return count >= 3
}

// findCodeBlockRanges identifies all fenced code block ranges in the buffer.
// Tracks fence characters to ensure matching open/close fences.
func findCodeBlockRanges(lineBytes func(int) []byte, totalLines int) []codeBlockRange {
	var ranges []codeBlockRange
	inBlock := false
	blockStart := -1
	var openFenceChar byte

	for i := 0; i < totalLines; i++ {
		line := lineBytes(i)
		if isCodeFence(line) {
			ch := getFenceChar(line)
			if !inBlock {
				inBlock = true
				blockStart = i
				openFenceChar = ch
			} else if ch == openFenceChar {
				// Only the same fence character can close the block
				ranges = append(ranges, codeBlockRange{blockStart, i})
				inBlock = false
			}
			// Different fence character or nested: ignore (cannot close)
		}
	}
	if inBlock {
		ranges = append(ranges, codeBlockRange{blockStart, totalLines - 1})
	}
	return ranges
}

// isInCodeBlock checks if a line number falls inside any code block range
func isInCodeBlock(ranges []codeBlockRange, lineNum int) bool {
	for _, r := range ranges {
		if lineNum >= r.start && lineNum <= r.end {
			return true
		}
	}
	return false
}

// stripOuterEmptyCells removes leading/trailing empty cells produced by
// splitTableLine when the line starts/ends with |.
// This aligns cell indices with the rendering loop, which counts cells
// starting from the first pair of | delimiters.
func stripOuterEmptyCells(cells [][]byte) [][]byte {
	if len(cells) > 0 && len(cells[0]) == 0 {
		cells = cells[1:]
	}
	if len(cells) > 0 && len(cells[len(cells)-1]) == 0 {
		cells = cells[:len(cells)-1]
	}
	return cells
}

// splitTableLine splits a line by | delimiter, correctly handling \| escape
func splitTableLine(line []byte) [][]byte {
	var cells [][]byte
	start := 0
	escaped := false
	for i, b := range line {
		if escaped {
			escaped = false
			continue
		}
		if b == '\\' {
			escaped = true
			continue
		}
		if b == '|' {
			cells = append(cells, line[start:i])
			start = i + 1
		}
	}
	// Handle trailing content after last |
	if start < len(line) {
		cells = append(cells, line[start:])
	}
	return cells
}

// isSeparatorLine checks if a line is a Markdown table separator line (|---|---|)
func isSeparatorLine(line []byte) bool {
	trimmed := trimTableMarkers(string(line))
	if len(trimmed) == 0 {
		return false
	}
	// All characters must be -, :, or |
	for _, r := range trimmed {
		if r != '-' && r != ':' && r != '|' {
			return false
		}
	}
	// Must have at least one dash
	for _, r := range trimmed {
		if r == '-' {
			return true
		}
	}
	return false
}

// trimTableMarkers removes leading | and trailing |
func trimTableMarkers(s string) string {
	s = stripPrefixByte(s, '|')
	s = stripSuffixByte(s, '|')
	return s
}

// stripPrefixByte removes leading occurrence of a byte
func stripPrefixByte(s string, b byte) string {
	if len(s) > 0 && s[0] == b {
		return s[1:]
	}
	return s
}

// stripSuffixByte removes trailing occurrence of a byte
func stripSuffixByte(s string, b byte) string {
	if len(s) > 0 && s[len(s)-1] == b {
		return s[:len(s)-1]
	}
	return s
}

// isTableRow checks if a line looks like a Markdown table row (has at least 2 |)
func isTableRow(line []byte) bool {
	pipeCount := 0
	escaped := false
	for _, b := range line {
		if escaped {
			escaped = false
			continue
		}
		if b == '\\' {
			escaped = true
			continue
		}
		if b == '|' {
			pipeCount++
		}
	}
	return pipeCount >= 2
}

// DetectTables scans the buffer for complete table blocks within the visible range.
// It expands upward/downward to find complete table boundaries for accurate column width calculation.
func DetectTables(lineBytes func(int) []byte, startLine, endLine, totalLines int) []TableBlock {
	var blocks []TableBlock

	if totalLines == 0 || startLine < 0 {
		return blocks
	}

	// Clamp endLine to valid range
	if endLine >= totalLines {
		endLine = totalLines - 1
	}

	// Track which lines we've already added to blocks
	processed := make([]bool, totalLines)

	// Identify all fenced code block ranges
	codeBlocks := findCodeBlockRanges(lineBytes, totalLines)

	// Find the first potential table by scanning upward from startLine
	tableStart := findTableStart(lineBytes, startLine, processed, totalLines, codeBlocks)
	if tableStart < 0 {
		return blocks
	}

	// Scan from tableStart to find all tables
	for line := tableStart; line < totalLines; {
		if processed[line] {
			line++
			continue
		}

		// Check if this line could be a table row
		bline := trimLeadingWhitespace(lineBytes(line))
		if !isTableRow(bline) {
			line++
			continue
		}

		// Found potential table start, scan to find complete table
		tbl := findCompleteTable(lineBytes, line, processed, totalLines, codeBlocks)
		if tbl != nil {
			blocks = append(blocks, *tbl)
			line = tbl.EndLine + 1
		} else {
			line++
		}
	}

	return blocks
}

// findTableStart scans upward from startLine to find the first line of a potential table.
// Stops when encountering a line inside a fenced code block.
func findTableStart(lineBytes func(int) []byte, startLine int, processed []bool, totalLines int, codeBlocks []codeBlockRange) int {
	// Scan upward from startLine
	for line := startLine; line >= 0; line-- {
		if processed[line] {
			return -1
		}
		// Stop at code block boundaries
		if isInCodeBlock(codeBlocks, line) {
			return line + 1
		}
		bline := trimLeadingWhitespace(lineBytes(line))
		if !isTableRow(bline) && !isSeparatorLine(bline) {
			// Found non-table content, next line is potential table start
			return line + 1
		}
	}
	return 0
}

// findCompleteTable finds a complete table starting from the given line.
// Skips lines inside fenced code blocks and terminates when entering one.
// Returns nil if no valid table found.
func findCompleteTable(lineBytes func(int) []byte, startLine int, processed []bool, totalLines int, codeBlocks []codeBlockRange) *TableBlock {
	tableStart := startLine
	tableEnd := startLine
	separatorLine := -1
	separatorColCount := 0

	// Scan downward to find all table rows and the separator
	for line := startLine; line < totalLines; line++ {
		if processed[line] {
			// Already processed, table ends before this
			break
		}

		// Code block boundary terminates the table
		if isInCodeBlock(codeBlocks, line) {
			break
		}

		bline := trimLeadingWhitespace(lineBytes(line))

		// Empty line ends the table
		if len(bline) == 0 {
			break
		}

		// Non-table row ends the table (but only after we've seen some content)
		if !isTableRow(bline) && !isSeparatorLine(bline) {
			if tableEnd > startLine || separatorLine > 0 {
				break
			}
			return nil
		}

		if isSeparatorLine(bline) {
			if separatorLine < 0 {
				// First separator found
				separatorLine = line
				cells := splitTableLine(bline)
				cells = stripOuterEmptyCells(cells)
				separatorColCount = len(cells)
				tableEnd = line
			} else {
				// Additional separator, table ends at previous line
				break
			}
		} else {
			// Regular table row
			cells := splitTableLine(bline)
			cells = stripOuterEmptyCells(cells)

			// If we have a separator, validate column count
			if separatorLine > 0 && len(cells) != separatorColCount {
				break
			}

			tableEnd = line
		}
	}

	// A valid table needs:
	// 1. At least one row before separator (header)
	// 2. A separator line
	// 3. At least one row after separator (body)
	if separatorLine < 0 || separatorLine <= tableStart || separatorLine >= tableEnd {
		return nil
	}

	// Mark lines as processed
	for i := tableStart; i <= tableEnd; i++ {
		if i >= 0 && i < totalLines {
			processed[i] = true
		}
	}

	// Calculate column widths
	colWidths := calcColWidths(lineBytes, tableStart, tableEnd)
	if colWidths == nil {
		return nil
	}

	return &TableBlock{
		StartLine: tableStart,
		EndLine:   tableEnd,
		ColWidths: colWidths,
	}
}

// trimLeadingWhitespace trims leading whitespace from a line
func trimLeadingWhitespace(line []byte) []byte {
	i := 0
	for i < len(line) {
		r, size := decodeRune(line[i:])
		if !unicode.IsSpace(r) {
			break
		}
		i += size
	}
	return line[i:]
}

// decodeRune decodes a single rune from UTF-8 bytes
func decodeRune(b []byte) (rune, int) {
	if len(b) == 0 {
		return 0, 0
	}
	r := rune(b[0])
	size := 1
	if r >= 0x80 {
		if r >= 0xF0 {
			size = 4
			r = rune(b[0]&0x07) << 18
			if len(b) >= 2 {
				r |= rune(b[1]&0x3F) << 12
			}
			if len(b) >= 3 {
				r |= rune(b[2]&0x3F) << 6
			}
			if len(b) >= 4 {
				r |= rune(b[3]&0x3F)
			}
		} else if r >= 0xE0 {
			size = 3
			r = rune(b[0]&0x0F) << 12
			if len(b) >= 2 {
				r |= rune(b[1]&0x3F) << 6
			}
			if len(b) >= 3 {
				r |= rune(b[2]&0x3F)
			}
		} else {
			size = 2
			r = rune(b[0]&0x1F) << 6
			if len(b) >= 2 {
				r |= rune(b[1]&0x3F)
			}
		}
	}
	return r, size
}

// calcColWidths calculates the visual width for each column in a table
// Uses raw cell content (no TrimSpace) to match rendering loop's cellVisualWidth
func calcColWidths(lineBytes func(int) []byte, startLine, endLine int) []int {
	if startLine > endLine {
		return nil
	}

	// First pass: find separator line and count columns
	separatorFound := false
	colCount := 0

	for line := startLine; line <= endLine; line++ {
		bline := trimLeadingWhitespace(lineBytes(line))
		if isSeparatorLine(bline) {
			cells := splitTableLine(bline)
			cells = stripOuterEmptyCells(cells)
			colCount = len(cells)
			separatorFound = true
			break
		}
	}

	if !separatorFound || colCount == 0 {
		return nil
	}

	// Initialize column max widths
	colMaxWidths := make([]int, colCount)

	// Second pass: calculate max visual width for each column
	for line := startLine; line <= endLine; line++ {
		bline := trimLeadingWhitespace(lineBytes(line))
		if isSeparatorLine(bline) {
			continue // Skip separator line
		}
		if !isTableRow(bline) {
			continue
		}

		cells := splitTableLine(bline)
		cells = stripOuterEmptyCells(cells)
		for i, cell := range cells {
			if i >= colCount {
				break
			}
			// Calculate visual width using runewidth
			// Do NOT trim spaces - must match rendering loop's cellVisualWidth
			vw := runewidth.StringWidth(string(cell))
			if vw > colMaxWidths[i] {
				colMaxWidths[i] = vw
			}
		}
	}

	// Column width = max cell width + 1 (for spacing after content)
	result := make([]int, colCount)
	for i := 0; i < colCount; i++ {
		result[i] = colMaxWidths[i] + 1
	}

	return result
}

// PaddingAction represents padding to be injected between table cells.
// Returned by tablePadding.ProcessRune when a non-first pipe is encountered.
type PaddingAction struct {
	Count int  // Number of padding characters to insert
	Char  rune // Character to use (' ' for content rows, '-' for separator rows)
}

// ProcessRune processes a single rune during table rendering.
// It updates internal cell tracking state and returns a PaddingAction
// when padding should be injected (i.e., after a non-first pipe delimiter).
// Returns nil if no padding is needed.
func (tp *tablePadding) ProcessRune(r rune, width int) *PaddingAction {
	if r == '\\' {
		tp.cellVisualWidth += width
		tp.escaped = true
		return nil
	}

	if r == '|' && !tp.escaped {
		var action *PaddingAction
		if tp.pipeCount > 0 && tp.cellIndex < len(tp.colWidths) {
			pad := tp.colWidths[tp.cellIndex] - tp.cellVisualWidth
			if pad > 0 {
				action = &PaddingAction{Count: pad}
				if tp.isSeparator {
					action.Char = '-'
				} else {
					action.Char = ' '
				}
			}
			tp.cellIndex++
			tp.cellVisualWidth = 0
		}
		tp.pipeCount++
		tp.escaped = false
		return action
	}

	if r != '|' || tp.escaped {
		tp.cellVisualWidth += width
	}
	tp.escaped = false
	return nil
}

// findTableBlock finds the table block containing the given line number
func findTableBlock(blocks []TableBlock, lineNum int) *TableBlock {
	for i := range blocks {
		if lineNum >= blocks[i].StartLine && lineNum <= blocks[i].EndLine {
			return &blocks[i]
		}
	}
	return nil
}

// GetTableBlocksForVisibleRange returns table blocks that overlap with visible range
func GetTableBlocksForVisibleRange(lineBytes func(int) []byte, visibleStart, visibleEnd, totalLines int) []TableBlock {
	// Extend range to scan full tables
	scanStart := visibleStart
	scanEnd := visibleEnd

	// Extend upward to find table start
	for scanStart > 0 {
		bline := trimLeadingWhitespace(lineBytes(scanStart - 1))
		if !isTableRow(bline) && !isSeparatorLine(bline) {
			break
		}
		scanStart--
	}

	// Extend downward to find table end
	for scanEnd < totalLines-1 {
		bline := trimLeadingWhitespace(lineBytes(scanEnd + 1))
		if !isTableRow(bline) && !isSeparatorLine(bline) {
			break
		}
		scanEnd++
	}

	return DetectTables(lineBytes, scanStart, scanEnd, totalLines)
}