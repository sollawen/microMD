package md

import (
	"os"
	"fmt"
	"time"

	"github.com/mattn/go-runewidth"
	"github.com/micro-editor/tcell/v2"
)

// Cell 是渲染管线输出的最小单位：一个屏幕字符。
type Cell struct {
	Rune         rune         // 要显示的字符
	Combining    []rune       // 组合字符（通常为 nil）
	Style        tcell.Style  // 颜色和字体样式
	BufLine      int          // 对应的 buffer 行号，装饰行为 -1
	BufX         int          // 对应 buffer 行内的 rune 偏移，装饰行为 -1
	IsDecorative bool         // true = 装饰字符，点击忽略
}

// RenderedRow 是渲染后的一行屏幕输出。
type RenderedRow struct {
	Cells   []Cell
	BufLine int  // 这行对应的 buffer 行号（wrap 续行也保留真实行号，display 层决定是否显示行号）
}

// RenderedSegment 是一个渲染片的完整渲染输出。
type RenderedSegment struct {
	Rows         []RenderedRow
	BufStartLine int           // 片起始 buffer 行
	BufEndLine   int           // 片结束 buffer 行（含）
}

// SegmentMeta 是检测步骤输出的轻量元数据，不包含渲染内容。
// 缓存到 BufWindow 上供 Scroll/Diff 使用。
type SegmentMeta struct {
	BufStartLine int
	BufEndLine   int
	RowCounts    []int  // RowCounts[i] = 片内第 i 个 buffer 行占几个 screen row
}

// Segment 是检测步骤的输出单位。每一行 buffer 都属于某个 Segment。
type Segment struct {
	BufStartLine int
	BufEndLine   int
	// Render 是渲染函数。接收 buffer 行内容和宽度，返回渲染结果。
	// Step 0 阶段只输出背景色。
	Render func(lines []string, width int, cfg MDConfig) *RenderedSegment
}

// renderLinesWithBg 是 Step 0 的公共渲染逻辑：
// 将 lines 逐字符输出为 Cell，每行填充到 width 列，全部使用 bgStyle。
// 返回的 RenderedSegment 中所有 BufLine 都是相对行号（从 0 开始）。
// 注意：需要正确处理 CJK 等宽字符（占 2 列），宽字符后补一个空占位 Cell。
func renderLinesWithBg(lines []string, width int, bgStyle tcell.Style) *RenderedSegment {
	result := &RenderedSegment{}
	for lineIdx, line := range lines {
		row := RenderedRow{
			BufLine: lineIdx, // 相对行号，displayBufferMD 调整
		}
		col := 0
		runeIdx := 0
		for _, r := range line {
			rw := runewidth.RuneWidth(r)
			row.Cells = append(row.Cells, Cell{
				Rune:    r,
				Style:   bgStyle,
				BufLine: lineIdx,
				BufX:    runeIdx,
			})
			col += rw
			runeIdx++
			// 宽字符占 2 列，补一个空占位 Cell 保持背景色连续
			if rw == 2 {
				row.Cells = append(row.Cells, Cell{
					Rune:    ' ',
					Style:   bgStyle,
					BufLine: lineIdx,
					BufX:    -1,
				})
			}
		}
		// 填充到 width
		for ; col < width; col++ {
			row.Cells = append(row.Cells, Cell{
				Rune:    ' ',
				Style:   bgStyle,
				BufLine: lineIdx,
				BufX:    -1,
			})
		}
		result.Rows = append(result.Rows, row)
	}
	return result
}

// mdLogFile 是调试日志文件句柄
var mdLogFile *os.File

// MdLogf 写入调试日志到 docs/md-debug.log（导出供 display 包使用）
func MdLogf(format string, args ...interface{}) {
	if mdLogFile == nil {
		var err error
		mdLogFile, err = os.OpenFile("docs/md-debug.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return
		}
	}
	ts := time.Now().Format("15:04:05.000")
	fmt.Fprintf(mdLogFile, "[%s] "+format+"\n", append([]interface{}{ts}, args...)...)
}
