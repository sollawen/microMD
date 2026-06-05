# RenderedSegment 数据结构

> render 函数的输出产出物，定义在 `internal/md/md.go`

## 总览

```
RenderedSegment
  ├── BufStartLine, BufEndLine     // 片边界（buffer 行号）
  └── Rows []RenderedRow           // screen 行列表
        ├── BufLine                // 此 screen 行对应哪个 buffer 行
        └── Cells []Cell           // 字符列表
              ├── Rune             // 显示的字符
              ├── Combining        // 组合字符（通常 nil）
              ├── Style            // tcell 样式（颜色 + 粗斜体）
              ├── BufLine          // 对应 buffer 行号（装饰行为 -1）
              ├── BufX             // 对应 buffer 行内 rune 偏移（装饰行为 -1）
              └── IsDecorative     // true = 装饰字符，点击时忽略
```

## 逐层说明

### RenderedSegment

一个渲染片的完整渲染输出。

| 字段 | 类型 | 说明 |
|------|------|------|
| `Rows` | `[]RenderedRow` | 渲染后的 screen 行，数量可能多于 buffer 行（如表格有装饰行） |
| `BufStartLine` | `int` | 片起始 buffer 行号 |
| `BufEndLine` | `int` | 片结束 buffer 行号（含） |

### RenderedRow

渲染后的一行屏幕输出。

| 字段 | 类型 | 说明 |
|------|------|------|
| `Cells` | `[]Cell` | 这一行的所有字符，长度 = 屏幕宽度 |
| `BufLine` | `int` | 对应的 buffer 行号。首行有值，续行和装饰行为 `-1` |

BufLine 用于行号显示判断：有值则显示行号，`-1` 则行号位留空。复用 softwrap 的多行规则。

### Cell

渲染管线输出的最小单位，一个屏幕字符。

| 字段 | 类型 | 说明 |
|------|------|------|
| `Rune` | `rune` | 要显示的字符 |
| `Combining` | `[]rune` | 组合字符（如变音符号），通常为 nil |
| `Style` | `tcell.Style` | 颜色和字体样式。颜色来自 Micro 语法高亮，render 叠加粗体/斜体 |
| `BufLine` | `int` | 对应 buffer 行号。装饰字符为 `-1` |
| `BufX` | `int` | 对应 buffer 行内的 rune 偏移（与 `buffer.Loc.X` 一致）。装饰字符为 `-1` |
| `IsDecorative` | `bool` | `true` = 装饰字符（如表格边框），鼠标点击时忽略 |

Cell 级别的 `(BufLine, BufX)` 用于鼠标点击反向定位到 buffer 位置。

## Row.BufLine vs Cell.BufLine

两处都有 BufLine，用途不同：

- **Row.BufLine**：行号区显示判断——有值显示行号，`-1` 留空
- **Cell.BufLine**：点击定位——查表得到 `(bufLine, bufX)`，光标定位到 buffer 位置

## CJK 宽字符处理

宽字符（占 2 列）后自动补一个空占位 Cell，保持背景色连续和对齐正确：

```
真实 Cell:  Rune='你', BufX=3, IsDecorative=false
占位 Cell:  Rune=' ',  BufX=-1, IsDecorative=false（不算装饰，但无 buffer 映射）
```

占位 Cell 的 BufX 为 `-1`，点击时跳过。

## 生命周期

- **每帧即弃**：RenderedSegment 不持久化，每帧由 render 函数重新生成
- **纯数据**：render 函数不碰 tcell screen，只产出数据，由 `displayBufferMD()` 负责写入 screen
