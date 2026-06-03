package md

import (
	"strings"
)

// BufferReader 是 detect.go 对 buffer 的最小依赖接口。
// 由 BufWindow 调用 DetectSegments 时传入 w.Buf（*buffer.Buffer 满足此接口）。
type BufferReader interface {
	LinesNum() int
	LineBytes(n int) []byte
	Line(n int) string
}

// detectState 表示扫描器当前的状态。
type detectState int

const (
	stateNormal     detectState = iota
	stateCodeblock
	stateBlockquote
	stateTable
	stateList
)

// DetectSegments 扫描 buffer 的可见区域，返回 []Segment。
// 每个 Segment 标记了它负责的 buffer 行范围和渲染函数。
// visibleStart/visibleEnd 是 buffer 行号范围（含两端）。
// buf 是 buffer 引用，用于读取行内容。
// bufWidth 是渲染区域宽度（列数）。
//   - Step 0: 透传给 renderer，detect 自身不使用
//   - Step 1+: detect 用于计算各渲染片的视觉行高，输出到 SegmentMeta 缓存，供 Scroll/Diff 查询
func DetectSegments(
	buf BufferReader,
	visibleStart, visibleEnd int,
	bufWidth int,
) []Segment {
	segments := []Segment{}
	state := stateNormal
	var startLine int // 当前多行结构起始行

	i := visibleStart
	for i <= visibleEnd {
		if i >= buf.LinesNum() {
			break
		}

		line := buf.Line(i)
		trimmed := strings.TrimSpace(line)

		// reprocessFlag: true 表示当前行需要在新状态下重新判断
		reprocess := false

		switch state {
		case stateNormal:
			// 块结构优先检查（多行结构优先级高于单行）
			if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
				state = stateCodeblock
				startLine = i
			} else if strings.HasPrefix(trimmed, ">") {
				state = stateBlockquote
				startLine = i
			} else if strings.Contains(trimmed, "|") && trimmed != "" {
				state = stateTable
				startLine = i
			} else if isListItem(trimmed) {
				state = stateList
				startLine = i
			} else if strings.HasPrefix(trimmed, "#") {
				// 单行标题
				segments = append(segments, Segment{
					BufStartLine: i,
					BufEndLine:   i,
					Render:       RenderHeading,
				})
			} else if isHR(trimmed) {
				// 单行分割线
				segments = append(segments, Segment{
					BufStartLine: i,
					BufEndLine:   i,
					Render:       RenderHR,
				})
			} else {
				// 兜底：段落
				segments = append(segments, Segment{
					BufStartLine: i,
					BufEndLine:   i,
					Render:       RenderParagraph,
				})
			}

		case stateCodeblock:
			if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
				// 代码块闭合，包含当前行
				segments = append(segments, Segment{
					BufStartLine: startLine,
					BufEndLine:   i,
					Render:       RenderCodeBlock,
				})
				state = stateNormal
				// 代码块闭合后，当前行已处理，进入下一行
			} else {
				// 继续收集代码块内容，当前行已处理
			}

		case stateBlockquote:
			if strings.HasPrefix(trimmed, ">") {
				// 继续收集引用块行
			} else {
				// 引用块结束
				segments = append(segments, Segment{
					BufStartLine: startLine,
					BufEndLine:   i - 1,
					Render:       RenderBlockquote,
				})
				state = stateNormal
				reprocess = true // 当前行需要在新状态下重新判断
			}

		case stateTable:
			if strings.Contains(trimmed, "|") && trimmed != "" {
				// 继续收集表格行
			} else {
				// 表格结束
				segments = append(segments, Segment{
					BufStartLine: startLine,
					BufEndLine:   i - 1,
					Render:       RenderTable,
				})
				state = stateNormal
				reprocess = true // 当前行需要在新状态下重新判断
			}

		case stateList:
			if isListItem(trimmed) {
				// 继续收集列表项
			} else {
				// 列表结束
				segments = append(segments, Segment{
					BufStartLine: startLine,
					BufEndLine:   i - 1,
					Render:       RenderList,
				})
				state = stateNormal
				reprocess = true // 当前行需要在新状态下重新判断
			}
		}

		// 根据是否需要重新处理来决定是否递增 i
		if !reprocess {
			i++
		}
	}

	// 处理末尾未闭合的状态
	switch state {
	case stateCodeblock:
		segments = append(segments, Segment{
			BufStartLine: startLine,
			BufEndLine:   visibleEnd,
			Render:       RenderCodeBlock,
		})
	case stateBlockquote:
		segments = append(segments, Segment{
			BufStartLine: startLine,
			BufEndLine:   visibleEnd,
			Render:       RenderBlockquote,
		})
	case stateTable:
		segments = append(segments, Segment{
			BufStartLine: startLine,
			BufEndLine:   visibleEnd,
			Render:       RenderTable,
		})
	case stateList:
		segments = append(segments, Segment{
			BufStartLine: startLine,
			BufEndLine:   visibleEnd,
			Render:       RenderList,
		})
	}

	return segments
}

// isHR 判断是否为水平分割线：--- 或 *** 或 ___ (至少 3 个，可更多)
func isHR(s string) bool {
	if len(s) < 3 {
		return false
	}
	c := s[0]
	if c != '-' && c != '*' && c != '_' {
		return false
	}
	for i := 1; i < len(s); i++ {
		if s[i] != c {
			return false
		}
	}
	return true
}

// isListItem 判断是否为列表项：以 "- " / "* " / "+ " 开头，或 "1. " 等数字序号
func isListItem(s string) bool {
	if len(s) < 2 {
		return false
	}
	if s[0] == '-' || s[0] == '*' || s[0] == '+' {
		return s[1] == ' '
	}
	// 数字序号: "1. " / "12. " 等
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i > 0 && i+1 < len(s) && s[i] == '.' && s[i+1] == ' ' {
		return true
	}
	return false
}
