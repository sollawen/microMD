# NotePane 滚动后位置修正

## 问题

MD 文件中打开 NotePane（Alt-i），光标下方空间不足需要滚动时，NotePane 出现的位置不对。

不滚动时位置正确。

## 原因

`open()` 中滚动后重新计算 `lowestRow` 的代码有 bug。

滚动前 `lowestRow = 25`（准确）。向上滚 3 行后，`Diff` 返回更小的值如 22，但 `if 22 > 25` 不成立，`lowestRow` 没被更新，仍然是 25。NotePane 出现在错误位置。

## 修复

滚动前 `lowestRow` 是准确的，向上滚 N 行后，光标在屏幕上的位置就是 `lowestRow - N`。不需要重新查 `viewportRowBufLine`（它过时了），也不需要用 `SLocFromLoc + Diff` 重算。

```go
if deficit > 0 {
    oldStartLine := view.StartLine
    view.StartLine = bw.Scroll(view.StartLine, deficit)
    lowestRow -= bw.Diff(oldStartLine, view.StartLine)
}
```

`Diff(oldStartLine, newStartLine)` 得到实际滚动量，正确处理 deficit 被 clamp 截断的情况。

## 改动文件

| 文件 | 改动 |
|------|------|
| `internal/action/notepane.go` | `open()` 中滚动后的重算逻辑替换为减法 |
