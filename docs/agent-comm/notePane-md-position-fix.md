# NotePane MD 位置计算修复

## 问题

MD 文件开启渲染后，NotePane 位置计算偏高，上边框会盖住光标。

### 根因

MD 渲染会插入**装饰行**（如 H1 的 `===`、H2 的 `---`、blockquote 边框等），这些行不对应任何 buffer line，但占据屏幕行。

当前 `locToScreenRow()` 使用 `SLocFromLoc()` + `Diff()`，这两个函数只理解 softwrap（一个 buffer line 被折成多个 screen row），不知道装饰行的存在：

```
buffer line 0: # Heading    → 渲染为 2 screen rows（内容行 + === 装饰行）
buffer line 1: normal text  → 渲染为 1 screen row

Diff(StartLine={0,0}, SLocFromLoc({x,1})) = 1   ← 算出来 cursor 在 screen row 1
实际 cursor 在 screen row 2                      ← 差了 1 行（装饰行）
```

`lowestCursorScreenRow()` 返回值偏小，NotePane 上边框 = lowestRow + 1 太靠上，盖住光标。

## 现有基础设施

BufWindow 已有 `mdCache`（每帧 `displayBufferMD` 后存储），结构为 `[]md.SegmentMeta`：

```go
type SegmentMeta struct {
    BufStartLine int
    BufEndLine   int
    RowBufLines  []int  // RowBufLines[i] = 第 i 个 screen row 的 BufLine（装饰行 = -1）
}
```

这是 screen row → buffer line 的正向映射。已有方法 `screenOffsetToBufferLine()` 利用它做点击坐标映射。我们只需**反向查找**。

## 修复方案

### 1. 新增 `BufferLineToScreenOffset()` 方法

在 `bufwindow_md.go` 中新增方法，遍历 `mdCache`，给定 buffer line 找到对应的 screen offset：

```go
// BufferLineToScreenOffset finds the screen row offset for a given buffer line.
// Only valid for MD files with rendering enabled, after Display() has populated mdCache.
func (w *BufWindow) BufferLineToScreenOffset(bufLine int) (int, bool) {
    if len(w.mdCache) == 0 {
        return 0, false
    }
    offset := 0
    for _, meta := range w.mdCache {
        for i, bl := range meta.RowBufLines {
            if bl == bufLine {
                return offset + i, true
            }
        }
        offset += len(meta.RowBufLines)
    }
    return 0, false // bufLine 不在可见区域
}
```

**注意**：一个 buffer line 可能因 softwrap 占多个 screen row。此方法返回**第一个**匹配的 offset（即该 buffer line 在屏幕上最顶部的位置），这对 NotePane 定位来说是正确的（我们不想盖住光标所在行的任何部分）。

### 2. 修改 `locToScreenRow()` 在 MD 文件时走 mdCache 路径

```go
func (n *NotePane) locToScreenRow(bw *display.BufWindow, view *display.View, loc buffer.Loc) int {
    if bw.Buf.IsMD && bw.IsMDRender() {  // 需要一个公开方法判断 MD 渲染是否开启
        if offset, ok := bw.BufferLineToScreenOffset(loc.Y); ok {
            return view.Y + offset
        }
        // 回退到原始逻辑（mdCache 未填充等边界情况）
    }
    sloc := bw.SLocFromLoc(loc)
    row := bw.Diff(view.StartLine, sloc)
    return row + view.Y
}
```

### 3. 可能需要公开 MD 渲染状态判断

检查 `BufWindow` 是否有公开方法判断 `mdConfig.MDRender`。如果没有，需要加一个：

```go
func (w *BufWindow) IsMDRender() bool {
    return w.Buf.IsMD && w.mdConfig.MDRender
}
```

## 改动清单

| 文件 | 改动 |
|------|------|
| `internal/display/bufwindow_md.go` | 新增 `BufferLineToScreenOffset()` 方法 |
| `internal/display/bufwindow.go` 或 `bufwindow_md.go` | 新增 `IsMDRender()` 方法（如不存在） |
| `internal/action/notepane.go` | `locToScreenRow()` 中 MD 文件走 mdCache 路径 |

## 注意事项

- `mdCache` 在每帧 `Display()` → `displayBufferMD()` 中重建。`open()` 在 Alt-i 按下时调用，此时 mdCache 是上一帧的数据，应该是最新的。
- `lowestCursorScreenRow()` 不需要改动，它调用 `locToScreenRow()`，后者会自动走 MD 路径。
- 装饰行的 `RowBufLines[i] = -1`，不会匹配任何 buffer line，所以不会误返回装饰行的 offset。
- 如果 buffer line 不在可见区域（用户滚动走了），`BufferLineToScreenOffset` 返回 false，回退到原始逻辑。
