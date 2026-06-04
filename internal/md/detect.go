package md

import (
	"strings"

	"github.com/micro-editor/micro/v2/pkg/highlight"
)

// BufferReader 是 detect.go 对 buffer 的最小依赖接口。
// 由 BufWindow 调用 DetectSegments 时传入 w.Buf（*buffer.Buffer 满足此接口）。
type BufferReader interface {
	LinesNum() int
	LineBytes(n int) []byte
	Line(n int) string
	State(n int) highlight.State
}

// detectState 表示扫描器当前的状态。
type detectState int

const (
	stateNormal detectState = iota
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
	var startLine int // 当前多行结构起始行（blockquote/table/list 用）

	// codeblock 边界跟踪：用 highlighter state 转折点
	var codeblockStart int = -1
	var lastState highlight.State
	if visibleStart > 0 {
		lastState = buf.State(visibleStart - 1)
	}

	for y := visibleStart; y <= visibleEnd; y++ {
		if y >= buf.LinesNum() {
			break
		}

		curState := buf.State(y)

		// ── Codeblock 边界：用 highlighter state 转折点 ──
		if lastState == nil && curState != nil {
			codeblockStart = y // 进入 codeblock
		}
		if lastState != nil && curState == nil {
			segments = append(segments, Segment{
				BufStartLine: codeblockStart,
				BufEndLine:   y, // 退出行也包进 codeblock
				Render:       RenderCodeBlock,
			})
			codeblockStart = -1
			lastState = curState
			continue // 退出行已归入 codeblock，不再做字符串匹配
		}

		// codeblock 内部行：不产生独立 segment，跳过
		if curState != nil {
			lastState = curState
			continue
		}

		// ── 非 codeblock 行：字符串匹配 ──
		line := buf.Line(y)
		trimmed := strings.TrimSpace(line)
		reprocess := false

		switch state {
		case stateNormal:
			if strings.HasPrefix(trimmed, ">") {
				state = stateBlockquote
				startLine = y
			} else if strings.Contains(trimmed, "|") && trimmed != "" {
				state = stateTable
				startLine = y
			} else if isListItem(trimmed) {
				state = stateList
				startLine = y
			} else if strings.HasPrefix(trimmed, "#") {
				segments = append(segments, Segment{
					BufStartLine: y,
					BufEndLine:   y,
					Render:       RenderHeading,
				})
			} else if isHR(trimmed) {
				segments = append(segments, Segment{
					BufStartLine: y,
					BufEndLine:   y,
					Render:       RenderHR,
				})
			} else {
				segments = append(segments, Segment{
					BufStartLine: y,
					BufEndLine:   y,
					Render:       RenderParagraph,
				})
			}

		case stateBlockquote:
			if strings.HasPrefix(trimmed, ">") {
				// 继续收集引用块行
			} else {
				segments = append(segments, Segment{
					BufStartLine: startLine,
					BufEndLine:   y - 1,
					Render:       RenderBlockquote,
				})
				state = stateNormal
				reprocess = true
			}

		case stateTable:
			if strings.Contains(trimmed, "|") && trimmed != "" {
				// 继续收集表格行
			} else {
				segments = append(segments, Segment{
					BufStartLine: startLine,
					BufEndLine:   y - 1,
					Render:       RenderTable,
				})
				state = stateNormal
				reprocess = true
			}

		case stateList:
			if isListItem(trimmed) {
				// 继续收集列表项
			} else {
				segments = append(segments, Segment{
					BufStartLine: startLine,
					BufEndLine:   y - 1,
					Render:       RenderList,
				})
				state = stateNormal
				reprocess = true
			}
		}

		if !reprocess {
			lastState = curState
		}
		// reprocess 时 lastState 不变（curState==nil，lastState 也是 nil）
	}

	// 未闭合的 codeblock
	if codeblockStart != -1 {
		segments = append(segments, Segment{
			BufStartLine: codeblockStart,
			BufEndLine:   visibleEnd,
			Render:       RenderCodeBlock,
		})
	}

	// 未闭合的 blockquote/table/list
	switch state {
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
