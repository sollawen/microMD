package display

import (
	"testing"
)

func TestSplitTableLine(t *testing.T) {
	tests := []struct {
		name     string
		line     []byte
		expected [][]byte
	}{
		{
			name:     "simple table row",
			line:     []byte("| a | bb |ccc|"),
			expected: [][]byte{[]byte(""), []byte(" a "), []byte(" bb "), []byte("ccc")},
		},
		{
			name:     "with escaped pipe",
			line:     []byte("| a\\|b | c |"),
			expected: [][]byte{[]byte(""), []byte(" a\\|b "), []byte(" c ")},
		},
		{
			name:     "no leading pipe",
			line:     []byte("a | b | c"),
			expected: [][]byte{[]byte("a "), []byte(" b "), []byte(" c")},
		},
		{
			name:     "empty cells",
			line:     []byte("|  |  |"),
			expected: [][]byte{[]byte(""), []byte("  "), []byte("  ")},
		},
		{
			name:     "single column",
			line:     []byte("| a |"),
			expected: [][]byte{[]byte(""), []byte(" a ")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := splitTableLine(tt.line)
			if len(result) != len(tt.expected) {
				t.Errorf("splitTableLine() got %d cells, want %d", len(result), len(tt.expected))
				return
			}
			for i, cell := range result {
				if string(cell) != string(tt.expected[i]) {
					t.Errorf("splitTableLine()[%d] = %q, want %q", i, string(cell), string(tt.expected[i]))
				}
			}
		})
	}
}

func TestIsSeparatorLine(t *testing.T) {
	tests := []struct {
		name     string
		line     []byte
		expected bool
	}{
		{
			name:     "simple separator",
			line:     []byte("|---|---|"),
			expected: true,
		},
		{
			name:     "with colons",
			line:     []byte("|:---|:---:|---:|"),
			expected: true,
		},
		{
			name:     "with spaces",
			line:     []byte("| --- | --- |"),
			expected: false, // Contains spaces which are not valid
		},
		{
			name:     "content row",
			line:     []byte("| a | bb |"),
			expected: false,
		},
		{
			name:     "empty line",
			line:     []byte(""),
			expected: false,
		},
		{
			name:     "only dashes without pipes",
			line:     []byte("--- ---"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isSeparatorLine(tt.line)
			if result != tt.expected {
				t.Errorf("isSeparatorLine(%q) = %v, want %v", string(tt.line), result, tt.expected)
			}
		})
	}
}

func TestIsTableRow(t *testing.T) {
	tests := []struct {
		name     string
		line     []byte
		expected bool
	}{
		{
			name:     "simple row",
			line:     []byte("| a | b |"),
			expected: true,
		},
		{
			name:     "no leading pipe",
			line:     []byte("a | b | c"),
			expected: true,
		},
		{
			name:     "single pipe pair",
			line:     []byte("| a |"),
			expected: true, // Has 2 pipes
		},
		{
			name:     "no pipes",
			line:     []byte("just text"),
			expected: false,
		},
		{
			name:     "empty line",
			line:     []byte(""),
			expected: false,
		},
		{
			name:     "with escaped pipe",
			line:     []byte("| a\\|b |"),
			expected: true, // Has 2 real pipes
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isTableRow(tt.line)
			if result != tt.expected {
				t.Errorf("isTableRow(%q) = %v, want %v", string(tt.line), result, tt.expected)
			}
		})
	}
}

func TestStripOuterEmptyCells(t *testing.T) {
	tests := []struct {
		name     string
		input    [][]byte
		expected [][]byte
	}{
		{
			name:     "both ends empty",
			input:    [][]byte{[]byte(""), []byte(" a "), []byte(" b "), []byte("")},
			expected: [][]byte{[]byte(" a "), []byte(" b ")},
		},
		{
			name:     "only leading empty",
			input:    [][]byte{[]byte(""), []byte(" a "), []byte(" b ")},
			expected: [][]byte{[]byte(" a "), []byte(" b ")},
		},
		{
			name:     "only trailing empty",
			input:    [][]byte{[]byte(" a "), []byte(" b "), []byte("")},
			expected: [][]byte{[]byte(" a "), []byte(" b ")},
		},
		{
			name:     "no empty ends",
			input:    [][]byte{[]byte("a "), []byte(" b ")},
			expected: [][]byte{[]byte("a "), []byte(" b ")},
		},
		{
			name:     "single empty cell",
			input:    [][]byte{[]byte("")},
			expected: [][]byte{},
		},
		{
			name:     "empty input",
			input:    [][]byte{},
			expected: [][]byte{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stripOuterEmptyCells(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("stripOuterEmptyCells() got %d cells, want %d", len(result), len(tt.expected))
				return
			}
			for i := range result {
				if string(result[i]) != string(tt.expected[i]) {
					t.Errorf("stripOuterEmptyCells()[%d] = %q, want %q", i, string(result[i]), string(tt.expected[i]))
				}
			}
		})
	}
}

func TestCalcColWidths(t *testing.T) {
	// Table without leading/trailing | (splitTableLine returns no empty ends)
	lineBytes := func(lineNum int) []byte {
		lines := [][]byte{
			[]byte("模块 | 宽度 | 备注"),
			[]byte("------|------|------"),
			[]byte("buf | 缓冲区渲染 | 主要改这里 "),
			[]byte("softwrap | 软换行 | 需要适配"),
		}
		if lineNum >= 0 && lineNum < len(lines) {
			return lines[lineNum]
		}
		return nil
	}

	colWidths := calcColWidths(lineBytes, 0, 3)

	// splitTableLine for "模块 | 宽度 | 备注" -> ["模块 ", " 宽度 ", " 备注"]
	// stripOuterEmptyCells -> no change (no leading/trailing empty)
	// widths: "模块 " = 5, " 宽度 " = 6, " 备注" = 4
	//
	// "buf | 缓冲区渲染 | 主要改这里 "
	// -> ["buf ", " 缓冲区渲染 ", " 主要改这里 "]
	// widths: 4, 12, 11
	//
	// "softwrap | 软换行 | 需要适配"
	// -> ["softwrap ", " 软换行 ", " 需要适配"]
	// widths: 9, 8, 8
	//
		// Max: col0=9, col1=12, col2=12 -> +1 -> [10, 13, 13]
	expected := []int{10, 13, 13}

	if len(colWidths) != 3 {
		t.Errorf("calcColWidths() returned %d columns, want 3", len(colWidths))
		return
	}

	for i, exp := range expected {
		if colWidths[i] != exp {
			t.Errorf("calcColWidths()[%d] = %d, want %d", i, colWidths[i], exp)
		}
	}
}

func TestCalcColWidthsWithPipes(t *testing.T) {
	// Table with leading/trailing | (the typical Markdown table format)
	lineBytes := func(lineNum int) []byte {
		lines := [][]byte{
			[]byte("| col1 | col2 |"),
			[]byte("|------|------|"),
			[]byte("| a | b |"),
			[]byte("| ccc | ddd |"),
		}
		if lineNum >= 0 && lineNum < len(lines) {
			return lines[lineNum]
		}
		return nil
	}

	colWidths := calcColWidths(lineBytes, 0, 3)

	// splitTableLine for "| col1 | col2 |" -> ["", " col1 ", " col2 ", ""]
	// After stripOuterEmptyCells -> [" col1 ", " col2 "]
	// widths: " col1 " = 6, " col2 " = 6
	//
	// "| ccc | ddd |" -> ["", " ccc ", " ddd ", ""]
	// After strip -> [" ccc ", " ddd "]
	// widths: " ccc " = 6, " ddd " = 6
	//
	// Max: col0=6, col1=6 -> +1 -> [7, 7]
	expected := []int{7, 7}

	if len(colWidths) != 2 {
		t.Errorf("calcColWidths() returned %d columns, want 2", len(colWidths))
		return
	}

	for i, exp := range expected {
		if colWidths[i] != exp {
			t.Errorf("calcColWidths()[%d] = %d, want %d", i, colWidths[i], exp)
		}
	}
}

func TestFindTableBlock(t *testing.T) {
	blocks := []TableBlock{
		{StartLine: 0, EndLine: 3, ColWidths: []int{10, 11}},
		{StartLine: 10, EndLine: 15, ColWidths: []int{5, 6}},
	}

	tests := []struct {
		lineNum   int
		wantStart int
		wantEnd   int
		wantNil   bool
	}{
		{0, 0, 3, false},
		{1, 0, 3, false},
		{3, 0, 3, false},
		{5, -1, -1, true},
		{10, 10, 15, false},
		{15, 10, 15, false},
		{20, -1, -1, true},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			result := findTableBlock(blocks, tt.lineNum)
			if tt.wantNil {
				if result != nil {
					t.Errorf("findTableBlock(blocks, %d) = %v, want nil", tt.lineNum, result)
				}
				return
			}
			if result == nil {
				t.Errorf("findTableBlock(blocks, %d) = nil, want block starting at %d", tt.lineNum, tt.wantStart)
				return
			}
			if result.StartLine != tt.wantStart || result.EndLine != tt.wantEnd {
				t.Errorf("findTableBlock(blocks, %d) = (StartLine=%d, EndLine=%d), want (StartLine=%d, EndLine=%d)",
					tt.lineNum, result.StartLine, result.EndLine, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

func TestDetectTables(t *testing.T) {
	lines := [][]byte{
		[]byte(""),
		[]byte("| col1 | col2 |"),     // header with leading/trailing |
		[]byte("|------|------|"),    // separator
		[]byte("| a | b |"),
		[]byte("| ccc | d |"),
		[]byte(""),
		[]byte("| x | y |"),          // second table header
		[]byte("|---|---|---|"),      // 3-column separator
		[]byte("| 1 | 2 | 3 |"),
	}

	lineBytes := func(lineNum int) []byte {
		if lineNum >= 0 && lineNum < len(lines) {
			return lines[lineNum]
		}
		return nil
	}

	blocks := DetectTables(lineBytes, 1, 5, len(lines))

	if len(blocks) == 0 {
		t.Error("DetectTables() returned no blocks, expected at least 1")
		return
	}

	// First table should be lines 1-4
	block := blocks[0]
	if block.StartLine != 1 {
		t.Errorf("First block StartLine = %d, want 1", block.StartLine)
	}
	if block.EndLine != 4 {
		t.Errorf("First block EndLine = %d, want 4", block.EndLine)
	}
	// After stripOuterEmptyCells, 2 leading/trailing pipe columns -> 2 actual columns
	if len(block.ColWidths) != 2 {
		t.Errorf("First block has %d columns, want 2", len(block.ColWidths))
	}
}

// === Code block exclusion tests ===

func TestIsCodeFence(t *testing.T) {
	tests := []struct {
		name     string
		line     []byte
		expected bool
	}{
		{"backtick fence", []byte("```"), true},
		{"backtick fence with lang", []byte("```go"), true},
		{"tilde fence", []byte("~~~"), true},
		{"tilde fence with lang", []byte("~~~python"), true},
		{"long backtick fence", []byte("``````"), true},
		{"short backtick", []byte("``"), false},
		{"single backtick", []byte("`"), false},
		{"empty", []byte(""), false},
		{"indented backtick fence", []byte("    ```"), true},
		{"tab-indented tilde fence", []byte("\t~~~"), true},
		{"regular text", []byte("some text"), false},
		{"dash line", []byte("---"), false},
		{"mixed fence chars", []byte("`~~"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isCodeFence(tt.line)
			if result != tt.expected {
				t.Errorf("isCodeFence(%q) = %v, want %v", string(tt.line), result, tt.expected)
			}
		})
	}
}

func TestFindCodeBlockRanges(t *testing.T) {
	tests := []struct {
		name     string
		lines    [][]byte
		expected []codeBlockRange
	}{
		{
			name: "simple fenced block",
			lines: [][]byte{
				[]byte("text"),
				[]byte("```"),
				[]byte("| a | b |"),
				[]byte("```"),
				[]byte("text"),
			},
			expected: []codeBlockRange{{1, 3}},
		},
		{
			name: "tilde fence",
			lines: [][]byte{
				[]byte("~~~"),
				[]byte("| a | b |"),
				[]byte("~~~"),
			},
			expected: []codeBlockRange{{0, 2}},
		},
		{
			name: "indented fence",
			lines: [][]byte{
				[]byte("    ```"),
				[]byte("code"),
				[]byte("    ```"),
			},
			expected: []codeBlockRange{{0, 2}},
		},
		{
			name: "unclosed fence",
			lines: [][]byte{
				[]byte("```"),
				[]byte("code"),
				[]byte("more"),
			},
			expected: []codeBlockRange{{0, 2}},
		},
		{
			name: "mixed fence chars no match",
			lines: [][]byte{
				[]byte("```"),
				[]byte("code"),
				[]byte("~~~"),
				[]byte("text"),
			},
			expected: []codeBlockRange{{0, 3}}, // ~~~ cannot close ``` fence, block extends to EOF
		},
		{
			name: "matching fence chars",
			lines: [][]byte{
				[]byte("```"),
				[]byte("code"),
				[]byte("```"),
				[]byte("~~~"),
				[]byte("code2"),
				[]byte("~~~"),
			},
			expected: []codeBlockRange{{0, 2}, {3, 5}},
		},
		{
			name: "no fences",
			lines: [][]byte{
				[]byte("text"),
				[]byte("| a | b |"),
				[]byte("more"),
			},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lineBytes := func(lineNum int) []byte {
				if lineNum >= 0 && lineNum < len(tt.lines) {
					return tt.lines[lineNum]
				}
				return nil
			}
			result := findCodeBlockRanges(lineBytes, len(tt.lines))
			if len(result) != len(tt.expected) {
				t.Errorf("findCodeBlockRanges() got %d ranges, want %d", len(result), len(tt.expected))
				return
			}
			for i, r := range result {
				if r.start != tt.expected[i].start || r.end != tt.expected[i].end {
					t.Errorf("findCodeBlockRanges()[%d] = {%d, %d}, want {%d, %d}",
						i, r.start, r.end, tt.expected[i].start, tt.expected[i].end)
				}
			}
		})
	}
}

func TestDetectTablesSkipsCodeBlocks(t *testing.T) {
	lines := [][]byte{
		[]byte("| col1 | col2 |"),     // 0: normal table header
		[]byte("|------|------|"),    // 1: separator
		[]byte("| a | b |"),          // 2: normal row
		[]byte("```"),                // 3: code fence open
		[]byte("| x | y |"),          // 4: table inside code block - should be ignored
		[]byte("|------|------|"),    // 5: separator inside code block
		[]byte("| p | q |"),          // 6: row inside code block
		[]byte("```"),                // 7: code fence close
		[]byte("| c | d |"),          // 8: normal table row (continues first table)
		[]byte(""),                   // 9: empty - ends first table
	}

	lineBytes := func(lineNum int) []byte {
		if lineNum >= 0 && lineNum < len(lines) {
			return lines[lineNum]
		}
		return nil
	}

	blocks := DetectTables(lineBytes, 0, 9, len(lines))

	if len(blocks) == 0 {
		t.Fatal("DetectTables() returned no blocks, expected 1")
	}

	block := blocks[0]
	if block.StartLine != 0 {
		t.Errorf("block.StartLine = %d, want 0", block.StartLine)
	}
	if block.EndLine != 2 {
		t.Errorf("block.EndLine = %d, want 2 (code block breaks the table)", block.EndLine)
	}
}

func TestDetectTablesInsideCodeBlock(t *testing.T) {
	// Entire table is inside a code block - should not be detected
	lines := [][]byte{
		[]byte("```"),
		[]byte("| col1 | col2 |"),
		[]byte("|------|------|"),
		[]byte("| a | b |"),
		[]byte("```"),
	}

	lineBytes := func(lineNum int) []byte {
		if lineNum >= 0 && lineNum < len(lines) {
			return lines[lineNum]
		}
		return nil
	}

	blocks := DetectTables(lineBytes, 0, 4, len(lines))
	if len(blocks) != 0 {
		t.Errorf("DetectTables() returned %d blocks, want 0 (table is inside code block)", len(blocks))
	}
}
