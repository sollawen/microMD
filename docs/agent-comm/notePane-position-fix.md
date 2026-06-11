# NotePane 位置计算修复

## 问题

当前 `open()` 用 `bufLoc.Y - startLine.Line + view.Y` 计算光标的屏幕行，存在两个问题：

1. **不考虑 softwrap**：一行 buffer 可能占多个屏幕 row，简单的 `Y - Line` 不准确
2. **不处理多光标和选择**：只取 active cursor 的位置，应该取最下方的那个

## 修改文件

`internal/action/notepane.go` — 仅修改 `open()` 方法，新增 3 个辅助方法

## 方案

### 1. 正确的 buffer Loc → 屏幕 row 转换

BufWindow 已有现成 API：

```go
sloc := bw.SLocFromLoc(loc)           // buffer Loc → SLoc（处理 softwrap）
row  := bw.Diff(view.StartLine, sloc)  // StartLine 到目标的 visual 行数差
screenRow := row + view.Y              // 加上窗口 Y 偏移 → 绝对屏幕 row
```

封装为 `locToScreenRow(bw, view, loc) int`

### 2. 多光标 + 选择：取最低屏幕 row

遍历 `Buf.GetCursors()`，对每个 cursor：

- 如果有 selection（`c.HasSelection()`）：取 `CurSelection[0]` 和 `CurSelection[1]` 中 Y 更大的那个（顺序不保证，需比较）
- 否则：用 `c.Loc`

计算每个 loc 的屏幕 row，取最大值。

封装为 `lowestCursorScreenRow(bw, view) int`

### 3. 空间不足时：向上滚动主编辑器

用户要求：NotePane 始终在光标下方。如果下方空间不够，**滚动主编辑器内容向上**，为 NotePane 腾出空间。

#### 可行性分析

主编辑器每帧调用 `BufWindow.Relocate()`，它会将 `StartLine` 调整到让光标在 `[scrollmargin, height-1-scrollmargin]` 范围内。

如果我们在 `open()` 中手动将 `StartLine` 向上滚动：
- 光标在屏幕上变高（row 数变小）
- 只要光标仍在 `[scrollmargin, height-1-scrollmargin]` 范围内，Relocate 不会干预
- NotePane 获得了下方的空间

#### 滚动量计算

```
deficit = desiredNotePaneBottom - screenHeight
         = (lowestRow + 1 + height + 1) - screenHeight
         = lowestRow + height + 2 - screenHeight

如果 deficit > 0：
  newStartLine = bw.Scroll(view.StartLine, -deficit)
  
  // 安全约束：确保光标不低于 scrollmargin
  minCursorRow = scrollmargin
  newCursorRow = lowestRow - deficit
  if newCursorRow < minCursorRow:
    // 再多滚会导致 Relocate 把 StartLine 拉回来
    deficit = lowestRow - minCursorRow
  
  view.StartLine = bw.Scroll(view.StartLine, -deficit)
  // 重新计算 lowestRow（变小了 deficit）
```

#### close() 时恢复

NotePane 关闭时调用 `pane.BWindow.Relocate()`，让主编辑器恢复正常滚动位置。

### 4. 修改后的 open() 伪代码

```
open():
    pane = MainTab().CurPane()
    view = pane.BWindow.GetView()
    bw = pane.BWindow.(*BufWindow)

    // 1. 找到最低的光标/选择端点的屏幕 row
    lowestRow = lowestCursorScreenRow(bw, view)

    // 2. NotePane 的目标位置
    notePaneTopBorder = lowestRow + 1
    notePaneBottomBorder = notePaneTopBorder + height + 1

    // 3. 如果空间不足，滚动主编辑器向上
    screenHeight = screen.Screen.Size()
    if notePaneBottomBorder >= screenHeight:
        deficit = notePaneBottomBorder - screenHeight + 1
        scrollmargin = int(pane.Buf.Settings["scrollmargin"].(float64))
        
        // 安全约束：不要让光标滚到 scrollmargin 以上
        maxDeficit = lowestRow - scrollmargin
        if deficit > maxDeficit:
            deficit = maxDeficit
        
        if deficit > 0:
            view.StartLine = bw.Scroll(view.StartLine, -deficit)
            // 重新计算位置
            lowestRow = lowestRow - deficit
            notePaneTopBorder = lowestRow + 1

    // 4. 设置 NotePane 位置
    x = view.X
    y = notePaneTopBorder
    width = view.Width

    // 5. 定位 BufWindow（边框内部）
    nbw.X = x + 1
    nbw.Y = y + 1
    BufPane.Resize(width-2, height)
```

### 5. close() 修改

```go
func (n *NotePane) close() {
    n.BufPane.Buf.Save()
    n.isOpen = false
    
    // 恢复主编辑器正常滚动
    if pane := MainTab().CurPane(); pane != nil {
        pane.BWindow.Relocate()
    }
}
```

### 6. 新增方法签名

```go
func (n *NotePane) locToScreenRow(bw *display.BufWindow, view *display.View, loc buffer.Loc) int
func (n *NotePane) lowestCursorScreenRow(bw *display.BufWindow, view *display.View) int
```

（去掉了 `highestCursorScreenRow`，因为不再需要把 NotePane 放到上方）

## 不涉及的文件

- `bufwindow.go` — 不改
- `micro.go` — 不改
- `bindings` — 不改

## 验证场景

- **普通光标**：NotePane 出现在光标下方 1 行
- **多光标**：NotePane 出现在最下方光标下面
- **有选择**：NotePane 出现在选择区域最下方下面
- **开启 softwrap**：位置仍然正确
- **下方空间不足**：主编辑器向上滚动，NotePane 仍出现在光标下方
- **极端情况（文件末尾）**：即使滚动也无法腾出足够空间时，光标被推到 scrollmargin 位置，NotePane 尽量靠下
